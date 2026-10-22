package rules

import "context"

// RuleSystem is implemented by each concrete rule system.
//
// Implementations register themselves at compile time in the app wiring
// (there is no dynamic plugin loading); they load their rule data from
// go:embed files and expose it through system-specific typed methods for the
// application layer. ValidateConfig verifies that the loaded data passes all
// semantic checks; it exists on the interface so generic tooling
// (`dicegirl config validate`) can validate any system without type
// assertions.
type RuleSystem interface {
	// Manifest returns the system manifest.
	Manifest() Manifest
	// ValidateConfig validates the system's loaded rule configuration.
	ValidateConfig(ctx context.Context) error
}
