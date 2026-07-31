package chat

import (
	"sync"
	"time"

	"stele/chat/provider"
)

type Session struct {
	Change       string             `json:"change"`
	Messages     []provider.Message `json:"messages"`
	ContextFiles []string           `json:"context_files"`
	Usage        UsageStats         `json:"usage"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

type UsageStats struct {
	TotalInput  int `json:"total_input"`
	TotalOutput int `json:"total_output"`
}

type SessionStore struct {
	mu    sync.RWMutex
	items map[string]*Session
}

var store = &SessionStore{items: make(map[string]*Session)}

func GetSession(change string) *Session {
	store.mu.RLock()
	s, ok := store.items[change]
	store.mu.RUnlock()
	if !ok {
		store.mu.Lock()
		s = &Session{
			Change:    change,
			Messages:  []provider.Message{},
			CreatedAt: time.Now(),
		}
		store.items[change] = s
		store.mu.Unlock()
	}
	return s
}

func (s *Session) AddMessage(msg provider.Message) {
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
}

func (s *Session) AddUsage(input, output int) {
	s.Usage.TotalInput += input
	s.Usage.TotalOutput += output
}

func DeleteSession(change string) {
	store.mu.Lock()
	delete(store.items, change)
	store.mu.Unlock()
}
