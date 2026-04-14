package socksproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"rssh/internal"
	"rssh/internal/server/tunnels"
	"rssh/internal/server/users"
	socks5 "github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
	"golang.org/x/crypto/ssh"
)

// Proxy is a SOCKS5 listener that dials out through a selected client.
// It is restricted to CONNECT only.
type Proxy struct {
	ID          string    `json:"id"`
	Fingerprint string    `json:"fingerprint"`
	ListenAddr  string    `json:"listen_addr"`
	CreatedAt   time.Time `json:"created_at"`
}

type runtime struct {
	Proxy
	ln   net.Listener
	srv  *socks5.Server
	stop chan struct{}
	once sync.Once
	cl   *ssh.Client
}

type Manager struct {
	mu   sync.RWMutex
	prox map[string]*runtime
}

func NewManager() *Manager {
	return &Manager{prox: map[string]*runtime{}}
}

func (m *Manager) List() []Proxy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Proxy, 0, len(m.prox))
	for _, p := range m.prox {
		out = append(out, p.Proxy)
	}
	return out
}

func (m *Manager) Close(id string) error {
	m.mu.Lock()
	p, ok := m.prox[id]
	if ok {
		delete(m.prox, id)
	}
	m.mu.Unlock()
	if !ok {
		return errors.New("not found")
	}
	p.once.Do(func() { close(p.stop) })
	_ = p.ln.Close()
	if p.cl != nil {
		_ = p.cl.Close()
	}
	return nil
}

func (m *Manager) Create(fingerprint, bindAddr, listenPortStr string) (Proxy, error) {
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return Proxy{}, errors.New("fingerprint is empty")
	}

	bindAddr = strings.TrimSpace(bindAddr)
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}
	// Accept "[::1]" as well as "::1".
	bindAddr = strings.TrimPrefix(bindAddr, "[")
	bindAddr = strings.TrimSuffix(bindAddr, "]")
	if _, _, err := net.SplitHostPort(bindAddr); err == nil {
		return Proxy{}, errors.New("bind address must not include a port")
	}

	lp, err := strconv.Atoi(strings.TrimSpace(listenPortStr))
	if err != nil || lp <= 0 || lp > 65535 {
		return Proxy{}, errors.New("invalid listen port")
	}

	conn, ok := users.GetClientConnByFingerprint(fingerprint)
	if !ok {
		return Proxy{}, errors.New("client not connected")
	}

	jumpCh, jumpReqs, err := conn.OpenChannel("jump", nil)
	if err != nil {
		return Proxy{}, fmt.Errorf("failed to open jump channel: %w", err)
	}
	go ssh.DiscardRequests(jumpReqs)

	ephemeralSigner, err := internal.GeneratePrivateKey()
	if err != nil {
		_ = jumpCh.Close()
		return Proxy{}, err
	}
	sshPriv, err := ssh.ParsePrivateKey(ephemeralSigner)
	if err != nil {
		_ = jumpCh.Close()
		return Proxy{}, err
	}

	ccfg := &ssh.ClientConfig{
		User:            "socks",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(sshPriv)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		ClientVersion:   "SSH-" + internal.Version + "-socks",
	}

	cconn, chans, reqs, err := ssh.NewClientConn(&tunnels.ChannelConn{Channel: jumpCh}, "jump", ccfg)
	if err != nil {
		_ = jumpCh.Close()
		return Proxy{}, fmt.Errorf("failed to start jump ssh client: %w", err)
	}
	client := ssh.NewClient(cconn, chans, reqs)

	ln, err := net.Listen("tcp", net.JoinHostPort(bindAddr, strconv.Itoa(lp)))
	if err != nil {
		_ = client.Close()
		return Proxy{}, err
	}

	id, _ := internal.RandomString(12)
	rt := &runtime{
		Proxy: Proxy{
			ID:          id,
			Fingerprint: fingerprint,
			ListenAddr:  ln.Addr().String(),
			CreatedAt:   time.Now(),
		},
		ln:   ln,
		stop: make(chan struct{}),
		cl:   client,
	}

	rt.srv = socks5.NewServer(
		socks5.WithRule(&connectOnlyRule{}),
		socks5.WithDialAndRequest(func(ctx context.Context, network, addr string, req *socks5.Request) (net.Conn, error) {
			return rt.cl.Dial(network, addr)
		}),
	)

	m.mu.Lock()
	m.prox[id] = rt
	m.mu.Unlock()

	go func() {
		_ = rt.srv.Serve(rt.ln)
		_ = m.Close(id)
	}()

	return rt.Proxy, nil
}

type connectOnlyRule struct{}

func (r *connectOnlyRule) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	if req == nil {
		return ctx, false
	}
	if req.Command != statute.CommandConnect {
		return ctx, false
	}
	return ctx, true
}
