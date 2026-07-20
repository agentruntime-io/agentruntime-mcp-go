// Package connectortest provides CI-friendly tests for MCP connector register.go catalogs.
package connectortest

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var registerNameRE = regexp.MustCompile(`Name:\s+"([^"]+)"`)

var registerHeldInStructRE = regexp.MustCompile(`(?s)\{[^{}]*Hold:\s*true[^{}]*Name:\s+"([^"]+)"|\{[^{}]*Name:\s+"([^"]+)"[^{}]*Hold:\s*true`)

var registerHeldWithOptionRE = regexp.MustCompile(`(?s)AddTool\([^)]*Name:\s*"([^"]+)"[^)]*WithHold\(\)`)

// ParseRegisterToolNames extracts unique tool wire names from register.go source.
func ParseRegisterToolNames(registerGoSource string) []string {
	matches := registerNameRE.FindAllStringSubmatch(registerGoSource, -1)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// WireNamePolicy configures wire-name validation for a connector vendor.
type WireNamePolicy struct {
	VendorPrefix        string   // e.g. "clickup"
	AllowedTopLevel     []string // e.g. clickup_search — {vendor}_{action} only
	AllowedUnprefixed   []string // legacy develop wires without vendor prefix (document in README)
	ForbiddenExact      []string // removed/deprecated tools that must not reappear
}

// ValidateWireNames checks register.go tool names against connector naming rules.
func ValidateWireNames(names []string, policy WireNamePolicy) error {
	prefix := strings.TrimSpace(policy.VendorPrefix)
	if prefix == "" {
		return fmt.Errorf("WireNamePolicy.VendorPrefix is required")
	}
	prefixWithUnderscore := prefix + "_"
	topLevel := map[string]struct{}{}
	for _, n := range policy.AllowedTopLevel {
		topLevel[strings.TrimSpace(n)] = struct{}{}
	}
	forbidden := map[string]struct{}{}
	for _, n := range policy.ForbiddenExact {
		forbidden[strings.TrimSpace(n)] = struct{}{}
	}
	unprefixed := map[string]struct{}{}
	for _, n := range policy.AllowedUnprefixed {
		unprefixed[strings.TrimSpace(n)] = struct{}{}
	}

	var violations []string
	for _, name := range names {
		if _, ok := forbidden[name]; ok {
			violations = append(violations, fmt.Sprintf("%s: forbidden (removed tool)", name))
			continue
		}
		if _, ok := unprefixed[name]; ok {
			continue
		}
		if !strings.HasPrefix(name, prefixWithUnderscore) {
			violations = append(violations, fmt.Sprintf("%s: must start with %q", name, prefixWithUnderscore))
			continue
		}
		body := strings.TrimPrefix(name, prefixWithUnderscore)
		parts := strings.Split(body, "_")
		if len(parts) < 1 || parts[0] == "" {
			violations = append(violations, fmt.Sprintf("%s: missing action segment", name))
			continue
		}
		if len(parts) == 1 {
			if _, ok := topLevel[name]; !ok {
				violations = append(violations, fmt.Sprintf("%s: top-level action not in AllowedTopLevel", name))
			}
			continue
		}
		if _, ok := topLevel[name]; ok {
			violations = append(violations, fmt.Sprintf("%s: listed as top-level but has resource segments", name))
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return fmt.Errorf("wire name policy:\n  - %s", strings.Join(violations, "\n  - "))
	}
	return nil
}

// ValidateToolCount fails when count is outside [min, max] (inclusive). Use min=max for exact.
func ValidateToolCount(names []string, min, max int) error {
	n := len(names)
	if n < min || n > max {
		return fmt.Errorf("tool count %d outside [%d, %d]", n, min, max)
	}
	return nil
}

// ParseHeldToolNames extracts wire names marked hold=true in register.go source.
// Matches Hold: true in a tool struct literal or agentmcp.WithHold() on the AddTool call.
func ParseHeldToolNames(registerGoSource string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, m := range registerHeldInStructRE.FindAllStringSubmatch(registerGoSource, -1) {
		name := strings.TrimSpace(firstNonEmpty(m[1], m[2]))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, m := range registerHeldWithOptionRE.FindAllStringSubmatch(registerGoSource, -1) {
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// PublishableNames returns registered names that are not held.
func PublishableNames(registered, held []string) []string {
	heldSet := map[string]struct{}{}
	for _, name := range held {
		heldSet[name] = struct{}{}
	}
	out := make([]string, 0, len(registered))
	for _, name := range registered {
		if _, ok := heldSet[name]; ok {
			continue
		}
		out = append(out, name)
	}
	return out
}

// ValidateCatalogExcludesHeld fails when any held wire appears in catalog tool names.
func ValidateCatalogExcludesHeld(held, catalogNames []string) error {
	catalog := map[string]struct{}{}
	for _, name := range catalogNames {
		catalog[name] = struct{}{}
	}
	var violations []string
	for _, name := range held {
		if _, ok := catalog[name]; ok {
			violations = append(violations, name)
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return fmt.Errorf("held tools must not appear in catalog: %s", strings.Join(violations, ", "))
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
