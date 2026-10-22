// Package apperr defines the stable, machine-readable error codes and the
// typed Error used across all dicegirl layers.
//
// Error codes are part of the public CLI/service contract: tests assert on
// codes rather than message text, and future chat-platform adapters can map
// codes to localized messages. Codes are defined centrally here so packages
// never invent conflicting numbers or strings.
package apperr

// Code is a stable, machine-readable error category.
type Code string

const (
	// InvalidArgument means the caller supplied a syntactically valid but
	// semantically unacceptable value (bad ID, out-of-range target, etc.).
	InvalidArgument Code = "INVALID_ARGUMENT"

	// ParseError means a user expression or text could not be parsed.
	ParseError Code = "PARSE_ERROR"

	// RollError means a roll was parsed but could not be executed
	// (overflow, impossible roll, etc.).
	RollError Code = "ROLL_ERROR"

	// DivideByZero means an expression attempted integer division by zero.
	DivideByZero Code = "DIVIDE_BY_ZERO"

	// LimitExceeded means a configured safety limit was hit (dice count,
	// die sides, expression nesting, etc.).
	LimitExceeded Code = "LIMIT_EXCEEDED"

	// RuleNotFound means a rule system ID is not registered.
	RuleNotFound Code = "RULE_NOT_FOUND"

	// InvalidData means a persisted file could not be parsed as YAML.
	InvalidData Code = "INVALID_DATA"

	// ValidationError means loaded data (character, config, manifest)
	// failed semantic validation.
	ValidationError Code = "VALIDATION_ERROR"

	// NotFound means a requested entity (character, file) does not exist.
	NotFound Code = "NOT_FOUND"

	// Conflict means an entity already exists (duplicate ID, etc.).
	Conflict Code = "CONFLICT"

	// ConfigInvalid means a configuration file is malformed or incomplete.
	ConfigInvalid Code = "CONFIG_INVALID"

	// UnsupportedSchemaVersion means persisted data declares a schema the
	// running binary cannot read or migrate.
	UnsupportedSchemaVersion Code = "UNSUPPORTED_SCHEMA_VERSION"

	// StorageReadFailed wraps a filesystem read failure (permissions, I/O).
	StorageReadFailed Code = "STORAGE_READ_FAILED"

	// StorageWriteFailed wraps a filesystem write failure (permissions,
	// atomic rename, I/O).
	StorageWriteFailed Code = "STORAGE_WRITE_FAILED"

	// Internal is a fallback for unexpected programmer/I/O errors.
	Internal Code = "INTERNAL"

	// --- chat-platform transport codes (used by adapters under cmd/) ---

	// Unauthorized means platform credentials were rejected or expired.
	Unauthorized Code = "UNAUTHORIZED"
	// UpstreamFailed wraps an unexpected platform API failure.
	UpstreamFailed Code = "UPSTREAM_FAILED"
	// RateLimited means the platform rejected the call for quota/frequency.
	RateLimited Code = "RATE_LIMITED"
	// PassiveWindowExpired means the inbound-message reply window closed.
	PassiveWindowExpired Code = "PASSIVE_WINDOW_EXPIRED"
	// MessageTooLong means the platform rejected the content length.
	MessageTooLong Code = "MESSAGE_TOO_LONG"
	// NoFriendRelation means a C2C send lacks the required friend relation.
	NoFriendRelation Code = "NO_FRIEND_RELATION"
	// UserDisabledMessages means the user turned off bot messages.
	UserDisabledMessages Code = "USER_DISABLED_MESSAGES"
	// BotMuted means the bot is muted in the group.
	BotMuted Code = "BOT_MUTED"
)
