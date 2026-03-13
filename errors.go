package agentruntimemcp

import (
	"errors"
	"fmt"
)

var (
	ErrAuthFailed       = errors.New("invalid or missing auth")
	ErrConfigLoad       = errors.New("failed to load config")
	ErrControlConfig    = errors.New("control config resolution failed")
	ErrProxyTarget      = errors.New("proxy target not configured")
	ErrInvalidConfig    = errors.New("invalid config")
)

// ControlError wraps control server HTTP errors with status code.
type ControlError struct {
	Status int
	Body   string
}

func (e *ControlError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("control server returned %d: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("control server returned %d", e.Status)
}

func (e *ControlError) Unwrap() error {
	return ErrControlConfig
}

// IsControlError returns true if err is or wraps a ControlError.
func IsControlError(err error) bool {
	var ce *ControlError
	return errors.As(err, &ce)
}

// AuthError wraps auth validation failures.
type AuthError struct {
	Reason string
}

func (e *AuthError) Error() string {
	if e.Reason != "" {
		return "auth failed: " + e.Reason
	}
	return "auth failed"
}

func (e *AuthError) Unwrap() error {
	return ErrAuthFailed
}
