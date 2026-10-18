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

func TestValidateWireNames_allowedUnprefixed(t *testing.T) {
	policy := WireNamePolicy{
		VendorPrefix:      "browserless",
		AllowedUnprefixed: []string{"capture_screenshot"},
	}
	if err := ValidateWireNames([]string{"capture_screenshot", "browserless_scrape_content"}, policy); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWireNames([]string{"scrape_url"}, policy); err == nil {
		t.Fatal("expected prefix error for unprefixed legacy not in allowlist")
	}
}

func TestParseHeldToolNames_structAndOption(t *testing.T) {
	src := `
	agentmcp.AddTool(server, &agentmcp.ToolDef{
		Name: "shopify_get_shop",
		Description: "ok",
	}, tools.ShopifyGetShop)
	agentmcp.AddTool(server, &agentmcp.ToolDef{
		Name:        "shopify_create_theme",
		Description: "held in struct",
		Hold:        true,
	}, tools.ShopifyCreateTheme)
	agentmcp.AddTool(server, &mcp.Tool{
		Name: "shopify_list_products",
		Description: "held via option",
	}, tools.ShopifyListProducts, agentmcp.WithHold())
	`
	got := ParseHeldToolNames(src)
	want := []string{"shopify_create_theme", "shopify_list_products"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestPublishableNames_andValidateCatalogExcludesHeld(t *testing.T) {
	registered := []string{"a", "b", "c"}
	held := []string{"b"}
	pub := PublishableNames(registered, held)
	if len(pub) != 2 || pub[0] != "a" || pub[1] != "c" {
		t.Fatalf("publishable: got %v", pub)
	}
	if err := ValidateCatalogExcludesHeld(held, []string{"a", "c"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCatalogExcludesHeld(held, []string{"a", "b"}); err == nil {
		t.Fatal("expected catalog violation")
	}
}
