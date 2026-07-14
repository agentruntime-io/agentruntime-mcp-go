package connectortest

import "testing"

func TestValidateDescriptionsNoEllipsis(t *testing.T) {
	if err := ValidateDescriptionsNoEllipsis(`Description: "Get foo (GET /api/v2/foo)."`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDescriptionsNoEllipsis(`Description: "Get foo (GET /api/v3/.../foo)."`); err == nil {
		t.Fatal("expected error for ellipsis in path")
	}
}
