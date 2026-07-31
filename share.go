package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	mu      sync.RWMutex
	tokens  map[string]*ShareEntry
	baseURL string
}

// NewShareManager creates a new ShareManager.
func NewShareManager(baseURL string) *ShareManager {
	if baseURL == "" {
		baseURL = "http://localhost:8989"
	}
	m := &ShareManager{
		tokens:  make(map[string]*ShareEntry),
		baseURL: baseURL,
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

// CreateShare generates a new share token and persists it.
func (m *ShareManager) CreateShare(path, workspace string, ttl time.Duration) (token, url string, err error) {
	token, err = generateToken()
	if err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	m.mu.Lock()
	m.tokens[token] = &ShareEntry{Path: path, Workspace: workspace, CreatedAt: time.Now()}
	if ttl > 0 {
		m.tokens[token].ExpiresAt = time.Now().Add(ttl)
	}
	m.mu.Unlock()
	m.save()
	url = fmt.Sprintf("%s/share/%s", m.baseURL, token)
	return token, url, nil
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

// ListShares returns all active share entries.
func (m *ShareManager) ListShares() []ShareInfo {
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
			URL:       fmt.Sprintf("%s/share/%s", m.baseURL, token),
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

// detectLANIP returns an IPv4 address reachable from the LAN.
func detectLANIP() string {
	if _, err := os.Stat("/mnt/c/Windows/System32/ipconfig.exe"); err == nil {
		out, cmdErr := exec.Command("/mnt/c/Windows/System32/ipconfig.exe").Output()
		if cmdErr == nil {
			var firstIP, fallbackIP string
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if strings.Contains(line, "IPv4") {
					parts := strings.Split(line, ":")
					if len(parts) >= 2 {
						ip := strings.TrimSpace(parts[len(parts)-1])
						if ip == "" || strings.HasPrefix(ip, "127.") {
							continue
						}
						if firstIP == "" {
							firstIP = ip
						}
						if strings.HasPrefix(ip, "10.") {
							return ip
						}
						fallbackIP = ip
					}
				}
			}
			if fallbackIP != "" {
				return fallbackIP
			}
			if firstIP != "" {
				return firstIP
			}
		}
	}
	out, err := exec.Command("ip", "-4", "-br", "addr", "show").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if strings.HasPrefix(fields[1], "lo") {
			continue
		}
		ip := strings.SplitN(fields[2], "/", 2)[0]
		if ip != "" && !strings.HasPrefix(ip, "127.") {
			return ip
		}
	}
	return ""
}
