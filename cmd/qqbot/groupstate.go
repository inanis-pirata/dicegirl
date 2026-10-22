package main

// Per-group adapter state: the member→private-openid links learned through
// binding codes, the member→card bindings (`.pc tag`), the hidden-roll
// observer list (`.ob`) and the current session log (`.log`). It lives next
// to the group's own data root, so it is isolated per group like the games.
//
// All access goes through groupStore, which serializes read-modify-write and
// persists atomically.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type memberLink struct {
	UserOpenid string    `json:"user_openid"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type cardBinding struct {
	CardID    string    `json:"card_id"`
	CardName  string    `json:"card_name"`
	UpdatedAt time.Time `json:"updated_at"`
}

type observerEntry struct {
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updated_at"`
}

type logState struct {
	Active    bool      `json:"active"`
	Title     string    `json:"title"`
	File      string    `json:"file"` // relative to the group root
	StartedAt time.Time `json:"started_at"`
	Entries   int       `json:"entries"`
}

type groupState struct {
	Links     map[string]memberLink    `json:"links"`
	Bindings  map[string]cardBinding   `json:"bindings"`
	Observers map[string]observerEntry `json:"observers"`
	ObEnabled bool                     `json:"ob_enabled"`
	Log       logState                 `json:"log"`
}

func (s *groupState) normalize() {
	if s.Links == nil {
		s.Links = map[string]memberLink{}
	}
	if s.Bindings == nil {
		s.Bindings = map[string]cardBinding{}
	}
	if s.Observers == nil {
		s.Observers = map[string]observerEntry{}
	}
}

type groupStore struct {
	path string
	mu   sync.Mutex
	st   groupState
}

// loadGroupStore loads (or initializes) the adapter state of one group. The
// file is optional; a missing file is not an error.
func loadGroupStore(root string) (*groupStore, error) {
	s := &groupStore{path: filepath.Join(root, "adapter.json")}
	raw, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s.st); err != nil {
			return nil, err
		}
	case os.IsNotExist(err):
	default:
		return nil, err
	}
	s.st.normalize()
	return s, nil
}

// update mutates the state under the lock and persists it. The callback must
// not retain pointers into the state.
func (s *groupStore) update(fn func(*groupState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.st); err != nil {
		return err
	}
	return s.persistLocked()
}

// read runs fn under the lock without persisting.
func (s *groupStore) read(fn func(*groupState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.st)
}

func (s *groupStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// link returns the private openid for a group member, if bound.
func (s *groupStore) link(memberOpenid string) (string, bool) {
	var out string
	var ok bool
	s.read(func(st *groupState) {
		l, found := st.Links[memberOpenid]
		if found && l.UserOpenid != "" {
			out, ok = l.UserOpenid, true
		}
	})
	return out, ok
}

// binding returns the card bound to a group member, if any.
func (s *groupStore) binding(memberOpenid string) (cardBinding, bool) {
	var out cardBinding
	var ok bool
	s.read(func(st *groupState) {
		out, ok = st.Bindings[memberOpenid]
	})
	return out, ok
}

// observerUserOpenids returns the private openids of every enabled observer
// that has completed the private-chat binding, excluding skip.
func (s *groupStore) observerUserOpenids(skip string) []string {
	var out []string
	s.read(func(st *groupState) {
		if !st.ObEnabled {
			return
		}
		for member, ob := range st.Observers {
			if !ob.Enabled || member == skip {
				continue
			}
			if l, ok := st.Links[member]; ok && l.UserOpenid != "" {
				out = append(out, l.UserOpenid)
			}
		}
	})
	return out
}

// groupStores caches one groupStore per group root.
type groupStores struct {
	mu sync.Mutex
	m  map[string]*groupStore
}

func newGroupStores() *groupStores {
	return &groupStores{m: map[string]*groupStore{}}
}

func (g *groupStores) get(root string) (*groupStore, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if s, ok := g.m[root]; ok {
		return s, nil
	}
	s, err := loadGroupStore(root)
	if err != nil {
		return nil, err
	}
	g.m[root] = s
	return s, nil
}
