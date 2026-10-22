// Package rulebooks embeds the built-in declarative rulebooks. Each YAML
// file is one complete rule system; user-provided rulebooks under
// <data-root>/rulebooks/ use the exact same schema and are otherwise
// identical citizens — the engine has no system-specific code.
package rulebooks

import "embed"

// FS holds the built-in rulebooks compiled into the binary.
//
//go:embed *.yaml
var FS embed.FS
