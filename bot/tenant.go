// Package bot holds platform-independent machinery for chat-platform
// adapters: one isolated application state (data root + App) per
// conversation scope, LRU eviction of idle scopes, and the shared
// chatcore command runner. Concrete platform drives (QQ, ...) live under
// cmd/ and only translate transport messages into scoped command lines.
//
// Dependenc方向与 CLI 相同：bot → chatcore → internal/app → internal/*；
// internal/ 不导入本包。
package bot

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"sync"

	"dicegirl/internal/app"
	"dicegirl/internal/config"
)

// Scope kinds.
const (
	ScopeGroup = "groups" // a group chat: state shared by everyone in it
	ScopeC2C   = "c2c"    // a private chat: state owned by one user
)

// safeID matches the short, filesystem-safe identifiers platform openids
// use in practice. Anything else (empty, unexpected characters, excessive
// length) is stored under its SHA-256 digest, so a hostile ID can never
// escape the base data root.
var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func dirName(kind, id string) string {
	if safeID.MatchString(id) {
		return filepath.Join(kind, id)
	}
	sum := sha256.Sum256([]byte(kind + ":" + id))
	return filepath.Join(kind, "sha256-"+hex.EncodeToString(sum[:16]))
}

// Tenant is one conversation scope: its own data root and App instance
// (hence its own SQLite database). The mutex serializes read-modify-write
// command flows within the scope (initiative lists, decks, choice options),
// mirroring the single-writer assumption the services make.
type Tenant struct {
	Kind string
	ID   string
	Root string
	App  *app.App

	mu     sync.Mutex
	active int // commands currently running; guarded by Manager.mu
}

// Lock runs fn while holding the scope's command lock. Every inbound
// message of a scope is handled through here, so two messages cannot
// interleave their read-modify-write sequences.
func (t *Tenant) Lock(fn func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fn()
}

type lruEntry struct {
	key    string
	tenant *Tenant
}

// Manager keeps one App per conversation scope under a common base data
// root, evicting least-recently-used scopes (closing their databases) when
// the open-scope limit is reached. Safe for concurrent use.
type Manager struct {
	baseRoot string
	maxOpen  int

	mu  sync.Mutex
	lru map[string]*list.Element
	ll  *list.List // front = most recently used
}

// DefaultMaxOpenScopes bounds simultaneously open per-scope databases.
const DefaultMaxOpenScopes = 64

// NewManager creates a Manager rooted at baseRoot (created lazily by the
// storage layer when a scope first writes).
func NewManager(baseRoot string) *Manager {
	return &Manager{
		baseRoot: baseRoot,
		maxOpen:  DefaultMaxOpenScopes,
		lru:      map[string]*list.Element{},
		ll:       list.New(),
	}
}

// scopeKey is the map key for a kind/id pair.
func scopeKey(kind, id string) string { return kind + "\x00" + id }

// Get returns the tenant for a scope, opening (and migrating, on first use)
// its data root when needed. Each scope gets an independent config resolved
// from the same environment defaults; DICEGIRL_DATA_ROOT does not leak in
// because the scope root is always overridden.
func (m *Manager) Get(ctx context.Context, kind, id string) (*Tenant, error) {
	key := scopeKey(kind, id)
	m.mu.Lock()
	if el, ok := m.lru[key]; ok {
		m.ll.MoveToFront(el)
		t := el.Value.(*lruEntry).tenant
		m.mu.Unlock()
		return t, nil
	}
	m.mu.Unlock()

	root := filepath.Join(m.baseRoot, dirName(kind, id))
	cfg, err := config.FromEnv()
	if err != nil {
		return nil, err
	}
	cfg.DataRoot = root
	if err := cfg.Resolve(""); err != nil {
		return nil, err
	}
	application, err := app.New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	t := &Tenant{Kind: kind, ID: id, Root: root, App: application}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Another goroutine may have opened the same scope while we were
	// building: keep the winner and close the duplicate handle.
	if el, ok := m.lru[key]; ok {
		m.ll.MoveToFront(el)
		winner := el.Value.(*lruEntry).tenant
		m.mu.Unlock()
		_ = application.Close()
		m.mu.Lock()
		return winner, nil
	}
	m.lru[key] = m.ll.PushFront(&lruEntry{key: key, tenant: t})
	m.evictLocked()
	return t, nil
}

// evictLocked closes least-recently-used *idle* scopes until at most maxOpen
// stay. Busy scopes (mid-command) are skipped: when every open scope is busy
// the set may transiently exceed maxOpen, which is bounded by in-flight
// concurrency and resolves on the next eviction pass. Callers hold m.mu.
func (m *Manager) evictLocked() {
	for m.ll.Len() > m.maxOpen {
		var victim *list.Element
		for el := m.ll.Back(); el != nil; el = el.Prev() {
			if el.Value.(*lruEntry).tenant.active == 0 {
				victim = el
				break
			}
		}
		if victim == nil {
			return
		}
		entry := victim.Value.(*lruEntry)
		m.ll.Remove(victim)
		delete(m.lru, entry.key)
		_ = entry.tenant.App.Close()
	}
}

// CloseAll closes every open scope database (used on graceful shutdown).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for el := m.ll.Front(); el != nil; el = el.Next() {
		_ = el.Value.(*lruEntry).tenant.App.Close()
	}
	m.ll.Init()
	m.lru = map[string]*list.Element{}
}
