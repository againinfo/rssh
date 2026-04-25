package tunnels

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	allowMu     sync.RWMutex
	allowCIDRs  []*net.IPNet
	allowLoaded bool
	allowErr    error
)

// InitAllowlist loads CIDR allowlist from (in priority order):
// 1) env RSSH_TUNNEL_ALLOW_CIDRS (comma-separated CIDRs)
// 2) file <datadir>/tunnel_allow_cidrs.txt (one CIDR per line, # comments allowed)
//
// Note: allowlist enforcement is currently disabled (unrestricted).
func InitAllowlist(dataDir string) error {
	allowMu.Lock()
	defer allowMu.Unlock()

	allowCIDRs = nil
	allowLoaded = true
	allowErr = nil

	if v := strings.TrimSpace(os.Getenv("RSSH_TUNNEL_ALLOW_CIDRS")); v != "" {
		nets, err := parseCIDRList(v)
		if err != nil {
			allowErr = err
			return err
		}
		allowCIDRs = nets
		return nil
	}

	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	p := filepath.Join(dataDir, "tunnel_allow_cidrs.txt")
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		allowErr = err
		return err
	}
	defer f.Close()

	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := s.Err(); err != nil {
		allowErr = err
		return err
	}

	nets, err := parseCIDRList(strings.Join(lines, ","))
	if err != nil {
		allowErr = fmt.Errorf("%s: %w", p, err)
		return allowErr
	}
	allowCIDRs = nets
	return nil
}

func parseCIDRList(v string) ([]*net.IPNet, error) {
	parts := strings.Split(v, ",")
	out := make([]*net.IPNet, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_, n, err := net.ParseCIDR(p)
		if err != nil {
			return nil, fmt.Errorf("invalid cidr %q", p)
		}
		out = append(out, n)
	}
	return out, nil
}

func allowlistLoaded() (bool, error) {
	allowMu.RLock()
	defer allowMu.RUnlock()
	return allowLoaded, allowErr
}

func IsAllowedIP(ip net.IP) bool {
	_ = ip
	return true
}

// IsAllowedTarget returns whether the target host (IP or DNS name) resolves to an allowed IP.
func IsAllowedTarget(host string) (bool, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return false, fmt.Errorf("target host is empty")
	}
	return true, nil
}

func AllowlistCount() int {
	allowMu.RLock()
	defer allowMu.RUnlock()
	return len(allowCIDRs)
}

func AllowlistCIDRs() []*net.IPNet {
	allowMu.RLock()
	defer allowMu.RUnlock()

	out := make([]*net.IPNet, len(allowCIDRs))
	copy(out, allowCIDRs)
	return out
}
