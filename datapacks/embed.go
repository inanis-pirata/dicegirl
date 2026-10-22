// Package datapacks embeds the built-in non-rule data packages. Data packs
// carry lookup content that belongs to no game (random name lists, town
// tables, ...); game rules stay exclusively in the rulebooks. User-provided
// packs under <data-root>/datapacks/ use the exact same schema.
package datapacks

import "embed"

// FS holds the built-in data packs compiled into the binary.
//
//go:embed *.yaml
var FS embed.FS
