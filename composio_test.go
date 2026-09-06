package agentruntimemcp

import "testing"

func TestComposioPublishedTools(t *testing.T) {
	meta := map[string]any{
		"toolkit": "knack",
		"published_tools": []any{
			map[string]any{
				"name":        "knack_list_records",
				"description": "List records",
				"input_schema": map[string]any{
					"type": "object",
				},
			},
		},
	}
	tools := composioPublishedTools(meta)
	if len(tools) != 1 {
		t.Fatalf("len=%d", len(tools))
	}
	if tools[0]["name"] != "knack_list_records" {
		t.Fatalf("name=%v", tools[0]["name"])
	}
	if _, ok := tools[0]["inputSchema"]; !ok {
		t.Fatal("expected inputSchema")
	}
}

func TestComposioSlugForTool(t *testing.T) {
	meta := map[string]any{
		"published_tools": []any{
			map[string]any{
				"name":          "knack_list_records",
				"composio_slug": "KNACK_LIST_RECORDS",
			},
		},
	}
	slug, err := composioSlugForTool(meta, "knack_list_records")
	if err != nil || slug != "KNACK_LIST_RECORDS" {
		t.Fatalf("slug=%q err=%v", slug, err)
	}
}

func TestComposioSlugForTool_defaultUppercase(t *testing.T) {
	slug, err := composioSlugForTool(map[string]any{}, "knack_list_records")
	if err != nil || slug != "KNACK_LIST_RECORDS" {
		t.Fatalf("slug=%q err=%v", slug, err)
	}
}

func TestComposioAllowlisted(t *testing.T) {
	meta := map[string]any{
		"tool_allowlist": []any{"KNACK_LIST_RECORDS"},
	}
	if !composioAllowlisted(meta, "knack_list_records") {
		t.Fatal("expected allowlisted")
	}
	if composioAllowlisted(meta, "KNACK_DELETE") {
		t.Fatal("expected not allowlisted")
	}
}

func TestComposioMCPResult_error(t *testing.T) {
	res := composioMCPResult(map[string]any{
		"successful": false,
		"error":      "boom",
	})
	if res["isError"] != true {
		t.Fatalf("res=%v", res)
	}
}
