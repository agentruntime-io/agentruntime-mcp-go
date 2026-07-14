// Package toolorg helps MCP connectors and Control derive tool organization metadata
// (groups, tags, display names) from wire names. See docs/mcp/MCP_TOOL_ORGANIZATION.md.
package toolorg

import (
	"sort"
	"strings"
)

// Metadata is stored in mcp_tools.metadata when the server version is published.
type Metadata struct {
	DisplayName      string   `json:"display_name,omitempty"`
	SuggestedGroup   string   `json:"suggested_group,omitempty"`
	SuggestedTags    []string `json:"suggested_tags,omitempty"`
	SuggestedRankKey string   `json:"suggested_rank_key,omitempty"`
}

var defaultGroupLabels = map[string]string{
	"hierarchy":      "Hierarchy",
	"tasks":          "Tasks",
	"search_members": "Search & members",
	"comments":       "Comments",
	"time":           "Time tracking",
	"docs":           "Docs",
	"chat":           "Chat",
	"uncategorized":  "Other",
}

// SuggestFromWireName infers group and tags from a wire name (vendor prefix required).
func SuggestFromWireName(toolName string) (groupID string, tags []string) {
	name := strings.ToLower(strings.TrimSpace(toolName))
	parts := strings.Split(name, "_")
	if len(parts) < 2 {
		return "uncategorized", []string{"mutating"}
	}
	body := strings.Join(parts[1:], "_")
	groupID = inferGroupID(body)
	tags = inferTags(body, groupID)
	return groupID, tags
}

func inferGroupID(body string) string {
	switch {
	case strings.Contains(body, "chat"):
		return "chat"
	case strings.Contains(body, "doc"):
		return "docs"
	case strings.Contains(body, "comment"):
		return "comments"
	case strings.Contains(body, "time"):
		return "time"
	case body == "search" || strings.Contains(body, "member") || strings.Contains(body, "assignee") || strings.Contains(body, "filtered_team"):
		return "search_members"
	case strings.Contains(body, "workspace") || strings.Contains(body, "space") ||
		strings.Contains(body, "folder") || strings.Contains(body, "hierarchy") ||
		body == "get_list" || strings.HasPrefix(body, "get_lists") || strings.HasPrefix(body, "create_list") ||
		strings.HasPrefix(body, "update_list") || strings.HasPrefix(body, "create_folder") || strings.HasPrefix(body, "update_folder"):
		return "hierarchy"
	default:
		return "tasks"
	}
}

func inferTags(body, groupID string) []string {
	tags := []string{groupID}
	if isReadOnlyVerb(body) {
		tags = append(tags, "read_only")
	} else {
		tags = append(tags, "mutating")
	}
	if strings.Contains(body, "chat") || strings.Contains(body, "doc") {
		tags = append(tags, "v3")
	} else {
		tags = append(tags, "v2")
	}
	return normalizeTags(tags)
}

func isReadOnlyVerb(body string) bool {
	for _, prefix := range []string{"get_", "list_", "find_", "resolve_", "search"} {
		if strings.HasPrefix(body, prefix) || body == strings.TrimSuffix(prefix, "_") {
			return true
		}
	}
	return false
}

func normalizeTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// FormatDisplayName turns clickup_get_task into "Get task".
func FormatDisplayName(toolName string) string {
	parts := strings.Split(strings.TrimSpace(toolName), "_")
	if len(parts) <= 1 {
		return toolName
	}
	body := strings.Join(parts[1:], " ")
	if body == "" {
		return toolName
	}
	return strings.ToUpper(body[:1]) + body[1:]
}

// PublisherMetadata builds publish metadata for registration/publish. When overrides are empty,
// SuggestFromWireName fills suggested_group and tags — authoring convenience only.
func PublisherMetadata(toolName string, overrides Metadata) map[string]any {
	out := map[string]any{}
	display := strings.TrimSpace(overrides.DisplayName)
	if display == "" {
		display = FormatDisplayName(toolName)
	}
	if display != "" {
		out["display_name"] = display
	}
	group := strings.ToLower(strings.TrimSpace(overrides.SuggestedGroup))
	if group == "" {
		group, _ = SuggestFromWireName(toolName)
	}
	if group != "" {
		out["suggested_group"] = group
	}
	tags := overrides.SuggestedTags
	if len(tags) == 0 {
		_, tags = SuggestFromWireName(toolName)
	}
	if len(tags) > 0 {
		out["suggested_tags"] = tags
	}
	if rk := strings.TrimSpace(overrides.SuggestedRankKey); rk != "" {
		out["suggested_rank_key"] = rk
	}
	return out
}

// GroupLabel returns a human label for a group slug.
func GroupLabel(groupID string) string {
	groupID = strings.ToLower(strings.TrimSpace(groupID))
	if label, ok := defaultGroupLabels[groupID]; ok {
		return label
	}
	parts := strings.Split(groupID, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
