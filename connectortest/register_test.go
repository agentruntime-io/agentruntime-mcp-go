package connectortest

import "testing"

func TestParseRegisterToolNames_uniqueSorted(t *testing.T) {
	src := `
		Name: "clickup_get_task",
		Name: "clickup_get_task",
		Name: "clickup_search",
	`
	got := ParseRegisterToolNames(src)
	want := []string{"clickup_get_task", "clickup_search"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestValidateWireNames_clickupPolicy(t *testing.T) {
	policy := WireNamePolicy{
		VendorPrefix:    "clickup",
		AllowedTopLevel: []string{"clickup_search"},
		ForbiddenExact:  []string{"clickup_get_workspace_hierarchy"},
	}
	names := []string{"clickup_get_task", "clickup_search"}
	if err := ValidateWireNames(names, policy); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWireNames([]string{"clickup_get_workspace_hierarchy"}, policy); err == nil {
		t.Fatal("expected forbidden tool error")
	}
	if err := ValidateWireNames([]string{"clickup_foo"}, policy); err == nil {
		t.Fatal("expected top-level allowlist error")
	}
}
