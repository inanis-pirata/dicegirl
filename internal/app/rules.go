package app

import (
	"context"
	"sort"
	"strings"

	"dicegirl/internal/apperr"
	"dicegirl/internal/engine"
	"dicegirl/internal/rules"
)

// The quick-reference dictionary (the Dice! `.rules`): every rulebook may
// declare entries, and the search spans the loaded rulebooks. The preferred
// rulebook (set with `rules set <系统>`) ranks first.

// RuleMatch is one matched quick-reference entry.
type RuleMatch struct {
	SystemID   string           `json:"system_id"`
	SystemName string           `json:"system_name"`
	Entry      engine.RuleEntry `json:"entry"`
	Score      int              `json:"score"`
}

// RulesResult is the outcome of a quick-reference search.
type RulesResult struct {
	Kind      string      `json:"kind"`
	Query     string      `json:"query"`
	System    string      `json:"system,omitempty"`
	Preferred string      `json:"preferred,omitempty"`
	Matches   []RuleMatch `json:"matches"`
}

// SearchRules searches the quick-reference entries of the loaded rulebooks.
// The query may name a system ("COC:大失败"); the preferred rulebook ranks
// first.
func (a *App) SearchRules(ctx context.Context, query string) (*RulesResult, error) {
	const op = "app.SearchRules"
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, apperr.New(op, apperr.InvalidArgument, "search query must not be empty")
	}
	preferred, err := a.state.RulesSystem(ctx)
	if err != nil {
		return nil, err
	}
	systemFilter := ""
	if i := strings.IndexAny(query, ":："); i > 0 {
		systemFilter = strings.TrimSpace(query[:i])
		query = strings.TrimSpace(query[i+1:])
		if query == "" {
			return nil, apperr.New(op, apperr.InvalidArgument, "search query must not be empty")
		}
	}

	res := &RulesResult{Kind: "rules", Query: query, Preferred: preferred}
	for _, m := range a.registry.List() {
		book, ok := a.bookOf(m.ID)
		if !ok {
			continue
		}
		if systemFilter != "" && !matchesSystem(m, systemFilter) {
			continue
		}
		for _, hit := range book.SearchRules(query) {
			score := hit.Score
			if preferred != "" && m.ID == preferred {
				score += 1000 // the preferred rulebook wins
			}
			res.Matches = append(res.Matches, RuleMatch{
				SystemID:   m.ID,
				SystemName: m.Name,
				Entry:      hit.Entry,
				Score:      score,
			})
		}
	}
	sort.SliceStable(res.Matches, func(i, j int) bool {
		if res.Matches[i].Score != res.Matches[j].Score {
			return res.Matches[i].Score > res.Matches[j].Score
		}
		if res.Matches[i].SystemID != res.Matches[j].SystemID {
			return res.Matches[i].SystemID < res.Matches[j].SystemID
		}
		return res.Matches[i].Entry.ID < res.Matches[j].Entry.ID
	})
	if systemFilter != "" && len(res.Matches) > 0 {
		res.System = res.Matches[0].SystemID
	}
	return res, nil
}

// RuleEntries lists every quick-reference entry of the loaded rulebooks.
func (a *App) RuleEntries(ctx context.Context) ([]RuleMatch, error) {
	out := []RuleMatch{}
	for _, m := range a.registry.List() {
		book, ok := a.bookOf(m.ID)
		if !ok {
			continue
		}
		for _, entry := range book.Rules() {
			out = append(out, RuleMatch{
				SystemID:   m.ID,
				SystemName: m.Name,
				Entry:      entry,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SystemID != out[j].SystemID {
			return out[i].SystemID < out[j].SystemID
		}
		return out[i].Entry.ID < out[j].Entry.ID
	})
	return out, nil
}

// SetRulesSystem sets (or clears) the rulebook preferred by the search.
func (a *App) SetRulesSystem(ctx context.Context, systemID string) error {
	const op = "app.SetRulesSystem"
	systemID = strings.TrimSpace(systemID)
	if systemID == "" {
		return a.state.SetRulesSystem(ctx, "")
	}
	sys, ok := a.registry.Get(systemID)
	if !ok {
		return apperr.New(op, apperr.RuleNotFound, "rule system not registered: "+systemID)
	}
	return a.state.SetRulesSystem(ctx, sys.Manifest().ID)
}

// RulesSystem returns the preferred rulebook of the search.
func (a *App) RulesSystem(ctx context.Context) (string, error) {
	return a.state.RulesSystem(ctx)
}

// matchesSystem reports whether a system matches the given filter (by id or
// by name, case-insensitively).
func matchesSystem(m rules.Manifest, filter string) bool {
	f := strings.ToLower(strings.TrimSpace(filter))
	return strings.ToLower(m.ID) == f || strings.ToLower(m.Name) == f ||
		strings.Contains(strings.ToLower(m.Name), f)
}

// bookOf returns the compiled rulebook of a system id.
func (a *App) bookOf(systemID string) (*engine.Book, bool) {
	sys, ok := a.registry.Get(systemID)
	if !ok {
		return nil, false
	}
	book, ok := sys.(*engine.Book)
	return book, ok
}

// hasSubstringKeyword reports whether one keyword contains q.
func hasSubstringKeyword(keywords []string, q string) bool {
	for _, k := range keywords {
		if strings.Contains(strings.ToLower(k), q) {
			return true
		}
	}
	return false
}
