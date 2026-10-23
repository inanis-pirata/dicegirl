package main

// Per-group adapter state: the member→private-openid links learned through
// binding codes, the per-table member→card bindings (`.pc tag`) and the
// hidden-result observer list (`.ob`). It lives next to the group's own data
// root, so it is isolated per group like the games. Play logs are stored in
// that root's database, not here.
//
// Bindings are scoped by game table: one member may be bound to a different
// character card on each table the group runs. Files written before tables
// existed keep a flat member→card shape and are migrated on load to the
// implicit default table.
//
// All access goes through groupStore, which serializes read-modify-write and
// persists atomically.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"dicegirl/internal/storage"
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

// legacyLogState is the pre-database `.log start/stop` state. It is kept
// only so the QQ adapter can import the referenced Markdown file into the
// shared log store on first use. New sessions never write this field.
type legacyLogState struct {
	Active    bool      `json:"active"`
	Title     string    `json:"title"`
	File      string    `json:"file"`
	StartedAt time.Time `json:"started_at"`
	Entries   int       `json:"entries"`
}

type groupState struct {
	Links       map[string]memberLink             `json:"links"`
	Bindings    map[string]map[string]cardBinding `json:"bindings"`
	Observers   map[string]observerEntry          `json:"observers"`
	ObEnabled   bool                              `json:"ob_enabled"`
	LegacyLog   legacyLogState                    `json:"log"`
	LogMigrated bool                              `json:"log_migrated,omitempty"`
}

// UnmarshalJSON accepts both the current nested bindings shape
// (member → table → card) and the legacy flat one (member → card), migrating
// flat entries to the implicit default table.
func (s *groupState) UnmarshalJSON(data []byte) error {
	type alias struct {
		Links       map[string]memberLink    `json:"links"`
		Bindings    json.RawMessage          `json:"bindings"`
		Observers   map[string]observerEntry `json:"observers"`
		ObEnabled   bool                     `json:"ob_enabled"`
		LegacyLog   legacyLogState           `json:"log"`
		LogMigrated bool                     `json:"log_migrated"`
	}
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Links = raw.Links
	s.Observers = raw.Observers
	s.ObEnabled = raw.ObEnabled
	s.LegacyLog = raw.LegacyLog
	s.LogMigrated = raw.LogMigrated

	nested := map[string]map[string]cardBinding{}
	if len(raw.Bindings) > 0 {
		if err := json.Unmarshal(raw.Bindings, &nested); err == nil {
			s.Bindings = nested
		} else {
			flat := map[string]cardBinding{}
			if err := json.Unmarshal(raw.Bindings, &flat); err != nil {
				return err
			}
			s.Bindings = make(map[string]map[string]cardBinding, len(flat))
			for member, b := range flat {
				s.Bindings[member] = map[string]cardBinding{storage.DefaultTableID: b}
			}
		}
	}
	return nil
}

func (s *groupState) normalize() {
	if s.Links == nil {
		s.Links = map[string]memberLink{}
	}
	if s.Bindings == nil {
		s.Bindings = map[string]map[string]cardBinding{}
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

// withLegacyLogMigration runs fn once, under the state lock, and persists the
// migration marker only after fn succeeds. The callback receives a copy of
// the old adapter log state; it must not call back into the store (the lock
// is held) and should only touch the database.
func (s *groupStore) withLegacyLogMigration(fn func(legacyLogState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.LogMigrated {
		return nil
	}
	if err := fn(s.st.LegacyLog); err != nil {
		return err
	}
	s.st.LogMigrated = true
	return s.persistLocked()
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

// binding returns the card a member has bound on the given table.
func (s *groupStore) binding(memberOpenid, tableID string) (cardBinding, bool) {
	var out cardBinding
	var ok bool
	s.read(func(st *groupState) {
		out, ok = bindingOn(st, memberOpenid, tableID)
	})
	return out, ok
}

// bindingOn is binding for callers that already hold the state lock.
func bindingOn(gs *groupState, memberOpenid, tableID string) (cardBinding, bool) {
	if perTable, ok := gs.Bindings[memberOpenid]; ok {
		if b, ok := perTable[tableID]; ok {
			return b, true
		}
	}
	return cardBinding{}, false
}

// anyBinding returns one of the member's bindings (the current table
// preferred, then the lexicographically first table), for labels where the
// table is unknown.
func anyBinding(gs *groupState, memberOpenid, tableID string) (cardBinding, bool) {
	perTable, ok := gs.Bindings[memberOpenid]
	if !ok || len(perTable) == 0 {
		return cardBinding{}, false
	}
	if b, ok := perTable[tableID]; ok {
		return b, true
	}
	ids := make([]string, 0, len(perTable))
	for id := range perTable {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	b := perTable[ids[0]]
	return b, true
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
