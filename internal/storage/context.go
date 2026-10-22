package storage

import "context"

// Game tables ("桌"/"团") scope the shared state of one play group: a single
// group data root can hold several parallel games, each with its own active
// character pointer, initiative encounter and card decks. The table id rides
// on the request context, the same way chat adapters pass the sender's bound
// card: no use-case signature enumerates it.

// tableKey is the context key carrying the current table id.
type tableKey struct{}

// DefaultTableID is the implicit table every fresh scope starts with; the
// one-time schema migration attaches all pre-table data to it.
const DefaultTableID = "default"

// WithTable returns a context scoped to one game table.
func WithTable(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, tableKey{}, id)
}

// TableFromContext returns the table id attached to ctx, or "" when the
// context is not table-scoped.
func TableFromContext(ctx context.Context) string {
	id, _ := ctx.Value(tableKey{}).(string)
	return id
}
