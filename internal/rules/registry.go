package rules

import (
	"context"
	"sort"
	"sync"

	"dicegirl/internal/apperr"
)

// Registry holds rulebooks loaded at startup (built-in embedded books plus
// user files from <data-root>/rulebooks/). It is safe for concurrent use
// after registration; registration itself is also locked.
type Registry struct {
	mu      sync.RWMutex
	systems map[string]RuleSystem
	// defaultID is the rulebook that commands select when no system is
	// named and several books are loaded (last declared default wins).
	defaultID string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{systems: make(map[string]RuleSystem)}
}

// Register adds a system, rejecting empty IDs, invalid manifests and
// duplicate IDs, and running the system's own config validation.
func (r *Registry) Register(ctx context.Context, system RuleSystem) error {
	const op = "rules.Registry.Register"
	if system == nil {
		return apperr.New(op, apperr.InvalidArgument, "cannot register nil rule system")
	}
	m := system.Manifest()
	if err := m.Validate(); err != nil {
		return err
	}
	if err := system.ValidateConfig(ctx); err != nil {
		return apperr.Wrap(op, apperr.ConfigInvalid,
			"rule system "+m.ID+" has invalid configuration", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.systems[m.ID]; exists {
		return apperr.New(op, apperr.Conflict, "rule system already registered: "+m.ID)
	}
	// A command alias belongs to exactly one rulebook.
	for _, alias := range m.Commands {
		for _, other := range r.systems {
			for _, taken := range other.Manifest().Commands {
				if taken == alias {
					return apperr.New(op, apperr.Conflict,
						"command alias already registered: "+alias+" ("+m.ID+" and "+other.Manifest().ID+")")
				}
			}
		}
	}
	r.systems[m.ID] = system
	if m.Default {
		r.defaultID = m.ID
	}
	return nil
}

// Replace hot-swaps the system already registered under system.Manifest().ID.
// The id must be registered (otherwise a RULE_NOT_FOUND is returned; use
// Register for a new id). Manifest validation and the system's own config
// validation run exactly like Register; command aliases are checked against
// every OTHER book, so re-registering the same id with unchanged aliases is
// allowed. The default selection is reconciled when the replacement drops
// `default: true`.
func (r *Registry) Replace(ctx context.Context, system RuleSystem) error {
	const op = "rules.Registry.Replace"
	if system == nil {
		return apperr.New(op, apperr.InvalidArgument, "cannot replace with nil rule system")
	}
	m := system.Manifest()
	if err := m.Validate(); err != nil {
		return err
	}
	if err := system.ValidateConfig(ctx); err != nil {
		return apperr.Wrap(op, apperr.ConfigInvalid,
			"rule system "+m.ID+" has invalid configuration", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.systems[m.ID]; !exists {
		return apperr.New(op, apperr.RuleNotFound, "rule system not registered: "+m.ID)
	}
	for _, alias := range m.Commands {
		for otherID, other := range r.systems {
			if otherID == m.ID {
				continue // a replacement may keep its own aliases
			}
			for _, taken := range other.Manifest().Commands {
				if taken == alias {
					return apperr.New(op, apperr.Conflict,
						"command alias already registered: "+alias+" ("+m.ID+" and "+otherID+")")
				}
			}
		}
	}
	r.systems[m.ID] = system
	if m.Default {
		r.defaultID = m.ID
	} else if r.defaultID == m.ID {
		r.defaultID = ""
	}
	return nil
}

// Unregister removes a user-installed system (`rule del`). The id must be
// registered; if the removed book was the default, the default selection is
// cleared (the next `rule add`/`rule patch` that declares default re-fills
// it, and commands needing a book report the missing default until then).
func (r *Registry) Unregister(id string) error {
	const op = "rules.Registry.Unregister"
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.systems[id]; !exists {
		return apperr.New(op, apperr.RuleNotFound, "rule system not registered: "+id)
	}
	delete(r.systems, id)
	if r.defaultID == id {
		r.defaultID = ""
	}
	return nil
}

// Get returns the system with id, or false.
func (r *Registry) Get(id string) (RuleSystem, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.systems[id]
	return s, ok
}

// MustGet returns the system or a typed RULE_NOT_FOUND error.
func (r *Registry) MustGet(id string) (RuleSystem, error) {
	s, ok := r.Get(id)
	if !ok {
		return nil, apperr.New("rules.Registry.Get", apperr.RuleNotFound,
			"rule system not registered: "+id)
	}
	return s, nil
}

// List returns all manifests sorted by ID for stable output.
func (r *Registry) List() []Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Manifest, 0, len(r.systems))
	for _, s := range r.systems {
		out = append(out, s.Manifest())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// DefaultID returns the id of the rulebook declared as the default, or ""
// when none is declared. It is the last registered book with
// `default: true`, so a user book can override a built-in default.
func (r *Registry) DefaultID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultID
}

// AliasOwner returns the system id that owns the given command
// alias, or "" when no book claims it.
func (r *Registry) AliasOwner(alias string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.systems {
		for _, taken := range s.Manifest().Commands {
			if taken == alias {
				return s.Manifest().ID, true
			}
		}
	}
	return "", false
}
