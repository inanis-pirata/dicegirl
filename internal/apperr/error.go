package apperr

import (
	"errors"
)

// Error is the structured error type returned by all dicegirl services.
//
// Op names the operation or package location that failed (e.g.
// "storage.Update"), Code is the stable category, Message is a human-readable
// explanation, and Err wraps the underlying cause (if any).
type Error struct {
	Op      string
	Code    Code
	Message string
	Err     error
}

// New builds an *Error without a wrapped cause.
func New(op string, code Code, message string) *Error {
	return &Error{Op: op, Code: code, Message: message}
}

// Wrap builds an *Error wrapping cause.
func Wrap(op string, code Code, message string, cause error) *Error {
	return &Error{Op: op, Code: code, Message: message, Err: cause}
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	s := string(e.Code)
	if e.Op != "" {
		s += " [" + e.Op + "]"
	}
	if e.Message != "" {
		s += ": " + e.Message
	}
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

// Unwrap returns the wrapped cause.
func (e *Error) Unwrap() error { return e.Err }

// Is reports whether the target error, when it is an *Error, shares the same
// code. A nil-code target matches on type only.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	if t.Code == "" {
		return true
	}
	return e.Code == t.Code
}

// CodeOf returns the Code of err if it is (or wraps) an *Error, otherwise
// Internal for non-nil errors and the empty string for nil.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Internal
}

// IsCode reports whether err carries the given code.
func IsCode(err error, code Code) bool {
	return CodeOf(err) == code
}

// AsError extracts the *Error from an error chain, or nil.
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}
