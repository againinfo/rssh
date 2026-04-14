package tunnels

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"rssh/internal"
	"rssh/internal/server/users"
	"golang.org/x/crypto/ssh"
)

type Tunnel struct {
	ID          string    `json:"id"`
	Fingerprint string    `json:"fingerprint"`
	ListenAddr  string    `json:"listen_addr"`
	Target      string    `json:"target"`
	CreatedAt   time.Time `json:"created_at"`
}

type tunnelRuntime struct {
	Tunnel
	ln   net.Listener
	stop chan struct{}
	once sync.Once
	cl   *ssh.Client
}

type Manager struct {
	mu      sync.RWMutex
	tunnels map[string]*tunnelRuntime
}

func NewManager() *Manager {
	return &Manager{tunnels: map[string]*tunnelRuntime{}}
}

func (m *Manager) List() []Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		out = append(out, t.Tunnel)
	}
	return out
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	t, ok := m.tunnels[id]
	if ok {
		delete(m.tunnels, id)
	}
	m.mu.Unlock()
	if !ok {
		return errors.New("not found")
	}
	t.once.Do(func() { close(t.stop) })
	_ = t.ln.Close()
	if t.cl != nil {
		_ = t.cl.Close()
	}
	return nil
}

func (m *Manager) Create(fingerprint, listenPortStr, target string) (Tunnel, error) {
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return Tunnel{}, errors.New("fingerprint is empty")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return Tunnel{}, errors.New("target is empty")
	}

	lp, err := strconv.Atoi(strings.TrimSpace(listenPortStr))
	if err != nil || lp <= 0 || lp > 65535 {
		return Tunnel{}, errors.New("invalid listen port")
	}

	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return Tunnel{}, errors.New("target must be host:port")
	}
	if host == "" {
		return Tunnel{}, errors.New("target host is empty")
	}
	pn, err := strconv.Atoi(port)
	if err != nil || pn <= 0 || pn > 65535 {
		return Tunnel{}, errors.New("target port invalid")
	}

	conn, ok := users.GetClientConnByFingerprint(fingerprint)
	if !ok {
		return Tunnel{}, errors.New("client not connected")
	}

	jumpCh, jumpReqs, err := conn.OpenChannel("jump", nil)
	if err != nil {
		return Tunnel{}, fmt.Errorf("failed to open jump channel: %w", err)
	}
	go ssh.DiscardRequests(jumpReqs)

	// Create an SSH client over the jump channel (client runs an embedded SSH server there).
	ephemeralSigner, err := internal.GeneratePrivateKey()
	if err != nil {
		_ = jumpCh.Close()
		return Tunnel{}, err
	}
	sshPriv, err := ssh.ParsePrivateKey(ephemeralSigner)
	if err != nil {
		_ = jumpCh.Close()
		return Tunnel{}, err
	}

	ccfg := &ssh.ClientConfig{
		User:            "tunnel",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(sshPriv)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         0,
		ClientVersion:   "SSH-" + internal.Version + "-tunnel",
	}

	cconn, chans, reqs, err := ssh.NewClientConn(&ChannelConn{Channel: jumpCh}, "jump", ccfg)
	if err != nil {
		_ = jumpCh.Close()
		return Tunnel{}, fmt.Errorf("failed to start jump ssh client: %w", err)
	}
	client := ssh.NewClient(cconn, chans, reqs)

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(lp)))
	if err != nil {
		_ = client.Close()
		return Tunnel{}, err
	}

	id, _ := internal.RandomString(12)
	rt := &tunnelRuntime{
		Tunnel: Tunnel{
			ID:          id,
			Fingerprint: fingerprint,
			ListenAddr:  ln.Addr().String(),
			Target:      net.JoinHostPort(host, strconv.Itoa(pn)),
			CreatedAt:   time.Now(),
		},
		ln:   ln,
		stop: make(chan struct{}),
		cl:   client,
	}

	m.mu.Lock()
	m.tunnels[id] = rt
	m.mu.Unlock()

	go rt.acceptLoop()

	return rt.Tunnel, nil
}

func (t *tunnelRuntime) acceptLoop() {
	defer t.ln.Close()
	for {
		c, err := t.ln.Accept()
		if err != nil {
			select {
			case <-t.stop:
				return
			default:
			}
			return
		}
		go t.handleConn(c)
	}
}

func (t *tunnelRuntime) handleConn(local net.Conn) {
	defer local.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type dialRes struct {
		c   net.Conn
		err error
	}
	ch := make(chan dialRes, 1)
	go func() {
		rc, err := t.cl.Dial("tcp", t.Target)
		ch <- dialRes{c: rc, err: err}
	}()

	var remote net.Conn
	select {
	case <-ctx.Done():
		return
	case r := <-ch:
		if r.err != nil {
			return
		}
		remote = r.c
	}
	defer remote.Close()

	go func() {
		_, _ = io.Copy(remote, local)
		_ = remote.Close()
	}()
	_, _ = io.Copy(local, remote)
}

// allowlist helpers live in allowlist.go
