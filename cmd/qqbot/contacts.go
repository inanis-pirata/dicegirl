package main

// Contact mapping for hidden rolls. Group events carry member_openid while
// the C2C send API needs user_openid; the two namespaces only correlate via
// the stable union_openid present on both event kinds. We learn a mapping
// when a person DMs the bot and persist it as one small JSON file in the
// bot data root.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type contact struct {
	UserOpenid string    `json:"user_openid"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type contactStore struct {
	path string
	mu   sync.Mutex
	m    map[string]contact
}

func loadContactStore(dataRoot string) (*contactStore, error) {
	s := &contactStore{
		path: filepath.Join(dataRoot, "contacts.json"),
		m:    map[string]contact{},
	}
	raw, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s.m); err != nil {
			return nil, err
		}
	case os.IsNotExist(err):
		// First contact creates the file.
	default:
		return nil, err
	}
	return s, nil
}

// remember records a union_openid → user_openid mapping learned from a C2C
// event. Writes are atomic (temp file + rename) and throttled by the
// UpdatedAt check: repeat messages from known users cost nothing.
func (s *contactStore) remember(unionOpenid, userOpenid string) error {
	if unionOpenid == "" || userOpenid == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur := s.m[unionOpenid]; cur.UserOpenid == userOpenid {
		return nil
	}
	s.m[unionOpenid] = contact{UserOpenid: userOpenid, UpdatedAt: time.Now()}
	return s.persistLocked()
}

// userOpenid returns the learned C2C openid for a union_openid, if any.
func (s *contactStore) userOpenid(unionOpenid string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[unionOpenid]
	return c.UserOpenid, ok
}

func (s *contactStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.m, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
