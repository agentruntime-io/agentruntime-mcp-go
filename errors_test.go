package agentruntimemcp

import (
	"errors"
	"testing"
)

func TestControlError(t *testing.T) {
	err := &ControlError{Status: 401, Body: "unauthorized"}
	if err.Error() != "control server returned 401: unauthorized" {
		t.Errorf("unexpected error string: %s", err.Error())
	}
	if !errors.Is(err, ErrControlConfig) {
		t.Error("ControlError should wrap ErrControlConfig")
	}
}

func TestHumanMessageFromControlAPIBody(t *testing.T) {
	const raw = `{"details":{},"error":"validation_error","message":"config_schema must not be empty"}`
	if got := HumanMessageFromControlAPIBody(raw); got != "config_schema must not be empty" {
		t.Fatalf("got %q", got)
	}
	if HumanMessageFromControlAPIBody("") != "" {
		t.Fatal("empty in")
	}
}
