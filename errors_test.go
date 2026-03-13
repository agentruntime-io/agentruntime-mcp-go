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
