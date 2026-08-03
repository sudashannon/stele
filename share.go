package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"stele/internal/appdir"
)

// ShareEntry represents a single shareable document link.
type ShareEntry struct {
	Path      string    `json:"path"`
	Workspace string    `json:"workspace"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ShareInfo is a lightweight summary returned by ListShares.
type ShareInfo struct {
	Token     string    `json:"token"`
	Path      string    `json:"path"`
	Workspace string    `json:"workspace"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	URL       string    `json:"url"`
}

// ShareManager creates and validates time-limited share tokens for documents.
type ShareManager struct {
	mu     sync.RWMutex
	tokens map[string]*ShareEntry

	// override holds --share-url. When set it wins over everything else: it is
	// the only way to publish links for a host the panel cannot observe from
	// the inside (a reverse proxy, a tunnel, a public DNS name).
	override string

	// A detected LAN address is consulted only for loopback callers. Detection
	// shells out (ipconfig.exe under WSL), so it is cached behind lanTTL.
	lanMu     sync.Mutex
	lanIP     string
	lanProbed time.Time
	detect    func() string
}

// lanTTL bounds how long a detected LAN address is reused: short enough that a
// DHCP lease change is picked up within one refresh, long enough that a burst of
// share requests does not spawn a process each.
const lanTTL = 30 * time.Second

// NewShareManager creates a new ShareManager. shareURL is the optional
// --share-url override; when empty, every link origin is derived per request.
func NewShareManager(shareURL string) *ShareManager {
	m := &ShareManager{
		tokens:   make(map[string]*ShareEntry),
		override: strings.TrimRight(strings.TrimSpace(shareURL), "/"),
		detect:   detectLANIP,
	}
	m.load()
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			m.sweep()
		}
	}()
	return m
}

// shareCachePath returns <data dir>/share-tokens.json.
func shareCachePath() string {
	return appdir.Path("share-tokens.json")
}

// load restores persisted tokens on startup.
func (m *ShareManager) load() {
	data, err := os.ReadFile(shareCachePath())
	if err != nil {
		return
	}
	var saved map[string]*ShareEntry
	if json.Unmarshal(data, &saved) != nil {
		return
	}
	m.mu.Lock()
	for k, v := range saved {
		m.tokens[k] = v
	}
	m.mu.Unlock()
}

// save persists the token map to disk.
func (m *ShareManager) save() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	os.MkdirAll(filepath.Dir(shareCachePath()), 0755)
	data, _ := json.Marshal(m.tokens)
	os.WriteFile(shareCachePath(), data, 0644)
}

// CreateShare generates a new share token and persists it. A token carries no
// origin of its own - ShareURL composes the public link per request.
func (m *ShareManager) CreateShare(path, workspace string, ttl time.Duration) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	entry := &ShareEntry{Path: path, Workspace: workspace, CreatedAt: time.Now()}
	if ttl > 0 {
		entry.ExpiresAt = entry.CreatedAt.Add(ttl)
	}
	m.mu.Lock()
	m.tokens[token] = entry
	m.mu.Unlock()
	m.save()
	return token, nil
}

// ShareURL returns the link for a token as this caller should see it.
func (m *ShareManager) ShareURL(r *http.Request, token string) string {
	return m.BaseURL(r) + "/share/" + token
}

// BaseURL derives the origin that links handed to this caller must carry.
//
// The caller's own Host is authoritative: whatever host:port the browser used
// to reach the panel is reachable from that browser by construction, and it
// follows the machine around as its address changes. Snapshotting a detected
// LAN IP once at startup - what this replaced - silently rots every later link
// the moment DHCP moves the adapter.
//
// Loopback is the one host that cannot be handed to somebody else, so those
// callers get a freshly detected LAN address instead, keeping the port they
// dialed: that is the port a portproxy forwards.
func (m *ShareManager) BaseURL(r *http.Request) string {
	if m.override != "" {
		return m.override
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	hostname, port, err := net.SplitHostPort(host)
	if err != nil {
		hostname, port = host, ""
	}
	if isLoopbackHost(hostname) {
		if lan := m.lanAddr(); lan != "" {
			host = lan
			if port != "" {
				host = net.JoinHostPort(lan, port)
			}
		}
	}
	return scheme + "://" + host
}

// isLoopbackHost reports whether a link built on this host could only ever
// resolve back to the caller's own machine.
func isLoopbackHost(hostname string) bool {
	if hostname == "" || strings.EqualFold(hostname, "localhost") {
		return true
	}
	if ip := net.ParseIP(strings.Trim(hostname, "[]")); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}

// lanAddr returns the cached LAN address, refreshing it once per lanTTL.
func (m *ShareManager) lanAddr() string {
	m.lanMu.Lock()
	defer m.lanMu.Unlock()
	if !m.lanProbed.IsZero() && time.Since(m.lanProbed) < lanTTL {
		return m.lanIP
	}
	m.lanIP, m.lanProbed = m.detect(), time.Now()
	return m.lanIP
}

// ValidateShare looks up a token and returns its ShareEntry.
func (m *ShareManager) ValidateShare(token string) (*ShareEntry, error) {
	m.mu.RLock()
	entry, ok := m.tokens[token]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("token not found")
	}
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		m.mu.Lock()
		delete(m.tokens, token)
		m.mu.Unlock()
		return nil, fmt.Errorf("token expired")
	}
	return entry, nil
}

// RevokeShare removes a token and persists the change.
func (m *ShareManager) RevokeShare(token string) error {
	m.mu.Lock()
	if _, ok := m.tokens[token]; !ok {
		m.mu.Unlock()
		return fmt.Errorf("token not found")
	}
	delete(m.tokens, token)
	m.mu.Unlock()

	// save acquires a read lock to snapshot the map. Calling it while holding
	// the write lock deadlocks sync.RWMutex (and previously hung every revoke).
	m.save()
	return nil
}

// ListShares returns all active share entries, with link origins as seen by
// this caller.
func (m *ShareManager) ListShares(r *http.Request) []ShareInfo {
	base := m.BaseURL(r)
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ShareInfo, 0, len(m.tokens))
	for token, entry := range m.tokens {
		result = append(result, ShareInfo{
			Token:     token,
			Path:      entry.Path,
			Workspace: entry.Workspace,
			ExpiresAt: entry.ExpiresAt,
			CreatedAt: entry.CreatedAt,
			URL:       base + "/share/" + token,
		})
	}
	return result
}

// sweep removes expired tokens and persists the change.
func (m *ShareManager) sweep() {
	m.mu.Lock()
	now := time.Now()
	changed := false
	for token, entry := range m.tokens {
		if !entry.ExpiresAt.IsZero() && now.After(entry.ExpiresAt) {
			delete(m.tokens, token)
			changed = true
		}
	}
	m.mu.Unlock()
	if changed {
		m.save()
	}
}

// generateToken produces a cryptographically random 32-byte hex string.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// windowsIPConfig is the WSL view of the Windows network tool. The panel's own
// interfaces are NAT-internal under WSL, so Windows' adapters are the only ones
// another machine can reach.
const windowsIPConfig = "/mnt/c/Windows/System32/ipconfig.exe"

// ipv4Pattern matches an address literal anywhere in a line, which keeps the
// parser independent of localized field labels, of the GBK bytes a Chinese
// Windows emits, and of decorations such as "(Preferred)".
var ipv4Pattern = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)

// virtualAdapterMarkers identify adapters whose addresses are useless to a
// colleague: NAT bridges to this VM, and tunnels only its own client can enter.
// Adapter names stay ASCII on a localized Windows ("以太网适配器 vEthernet
// (WSL (Hyper-V firewall))"), so matching the header is safe where matching a
// field label would not be.
var virtualAdapterMarkers = []string{
	"vethernet", "wsl", "hyper-v", "virtualbox", "vmware", "docker", "loopback",
	"isatap", "teredo", "bluetooth", "wintun", "tap-windows", "openvpn",
	"wireguard", "tailscale", "zerotier",
}

// detectLANIP returns an address other machines can reach, or "" when nothing
// qualifies - in which case the caller keeps the host it already had.
func detectLANIP() string {
	if out, err := exec.Command(windowsIPConfig).Output(); err == nil {
		if ip := pickWindowsLANIP(string(out)); ip != "" {
			return ip
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	var public string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip.IsPrivate() {
			return ip.String()
		}
		if public == "" {
			public = ip.String()
		}
	}
	return public
}

// pickWindowsLANIP selects a reachable address from ipconfig output. A private
// address wins because a LAN link is the point; a routable one is accepted when
// that is all there is.
func pickWindowsLANIP(out string) string {
	skipAdapter := false
	var public string
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Adapter headers are the only lines starting at column zero and
		// ending in a colon; every field under them is indented.
		if !strings.HasPrefix(raw, " ") && !strings.HasPrefix(raw, "\t") && strings.HasSuffix(line, ":") {
			skipAdapter = false
			lower := strings.ToLower(line)
			for _, marker := range virtualAdapterMarkers {
				if strings.Contains(lower, marker) {
					skipAdapter = true
					break
				}
			}
			continue
		}
		if skipAdapter || !strings.Contains(line, "IPv4") {
			continue
		}
		ip := net.ParseIP(ipv4Pattern.FindString(line))
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			continue
		}
		if ip.IsPrivate() {
			return ip.String()
		}
		if public == "" {
			public = ip.String()
		}
	}
	return public
}
