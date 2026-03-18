package agentruntimemcp

import (
	"errors"
	"fmt"
)

var (
	ErrAuthFailed          = errors.New("invalid or missing auth")
	ErrConfigLoad          = errors.New("failed to load config")
	ErrControlConfig       = errors.New("control config resolution failed")
	ErrProxyTarget         = errors.New("proxy target not configured")
	ErrAdapterNotRegistered = errors.New("adapter not found in registry")
)

// ErrAdapterNotFound is returned when a requested adapter name is not registered.
type ErrAdapterNotFound struct {
	Name string
}

func (e *ErrAdapterNotFound) Error() string {
	return fmt.Sprintf("adapter %q not registered", e.Name)
}

func (e *ErrAdapterNotFound) Unwrap() error {
	return ErrAdapterNotRegistered
}

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
