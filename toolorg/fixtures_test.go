package toolorg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type suggestCase struct {
	ToolName string   `json:"tool_name"`
	Group    string   `json:"group"`
	Tags     []string `json:"tags"`
}

func loadSuggestCases(t *testing.T) []suggestCase {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "fixtures", "suggest_cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []suggestCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestSuggestFromWireName_Fixtures(t *testing.T) {
	for _, tc := range loadSuggestCases(t) {
		group, tags := SuggestFromWireName(tc.ToolName)
		if group != tc.Group {
			t.Fatalf("%s: group got %q want %q", tc.ToolName, group, tc.Group)
		}
		if len(tags) != len(tc.Tags) {
			t.Fatalf("%s: tags got %v want %v", tc.ToolName, tags, tc.Tags)
		}
		for i, want := range tc.Tags {
			if tags[i] != want {
				t.Fatalf("%s: tag[%d] got %q want %q (full %v)", tc.ToolName, i, tags[i], want, tags)
			}
		}
	}
}

func TestDefaultPublisherMetadata_Fixtures(t *testing.T) {
	for _, tc := range loadSuggestCases(t) {
		meta := DefaultPublisherMetadata(tc.ToolName)
		if meta["suggested_group"] != tc.Group {
			t.Fatalf("%s: suggested_group = %v", tc.ToolName, meta["suggested_group"])
		}
	}
}
