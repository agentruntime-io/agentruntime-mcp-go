package toolorg

import "testing"

func TestMergeEffective_explicitOnly(t *testing.T) {
	overlayGroup := "docs"
	overlayRank := "n"

	eff := MergeEffective("clickup_get_doc", Metadata{}, &overlayGroup, &overlayRank)
	if eff.GroupID != "docs" {
		t.Fatalf("overlay group = %q", eff.GroupID)
	}
	if eff.RankKey != "n" {
		t.Fatalf("overlay rank = %q", eff.RankKey)
	}

	eff = MergeEffective("clickup_get_doc", Metadata{SuggestedGroup: "tasks"}, nil, nil)
	if eff.GroupID != "tasks" {
		t.Fatalf("published group = %q", eff.GroupID)
	}

	eff = MergeEffective("clickup_get_doc", Metadata{}, nil, nil)
	if eff.GroupID != "" {
		t.Fatalf("empty publish metadata should not infer group, got %q", eff.GroupID)
	}
	if len(eff.Tags) != 0 {
		t.Fatalf("empty publish metadata should not infer tags, got %v", eff.Tags)
	}
}
