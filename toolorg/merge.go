package toolorg

import (
	"encoding/json"
	"strings"
)

// EffectiveOrganization is merged presentation for one tool.
type EffectiveOrganization struct {
	GroupID    string
	GroupLabel string
	RankKey    string
	Tags       []string
}

// ToolGroup is a palette group definition.
type ToolGroup struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	RankKey string `json:"rankKey"`
	Source  string `json:"source,omitempty"`
}

func normalizeGroupID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.ReplaceAll(id, " ", "_")
	return id
}

// ParseMetadata unmarshals mcp_tools.metadata JSONB.
func ParseMetadata(raw []byte) Metadata {
	if len(raw) == 0 || !json.Valid(raw) {
		return Metadata{}
	}
	var m Metadata
	_ = json.Unmarshal(raw, &m)
	m.SuggestedGroup = normalizeGroupID(m.SuggestedGroup)
	m.SuggestedTags = normalizeTags(m.SuggestedTags)
	return m
}

// MetadataIsEmpty reports whether publish metadata has no organization fields set.
func MetadataIsEmpty(m map[string]any) bool {
	if len(m) == 0 {
		return true
	}
	for k, v := range m {
		if v == nil {
			continue
		}
		switch k {
		case "suggested_tags":
			switch tags := v.(type) {
			case []any:
				if len(tags) > 0 {
					return false
				}
			case []string:
				if len(tags) > 0 {
					return false
				}
			}
		default:
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return false
			}
		}
	}
	return true
}

// DefaultPublisherMetadata returns full publish metadata for a wire name.
func DefaultPublisherMetadata(toolName string) map[string]any {
	return PublisherMetadata(toolName, Metadata{})
}

// MergeEffective resolves group, rank, and tags from published metadata + tenant overlay only.
// Wire names are not inferred at read time; connectors declare groups at publish (see PublisherMetadata).
func MergeEffective(
	_ string,
	published Metadata,
	overlayGroupID *string,
	overlayRankKey *string,
) EffectiveOrganization {
	groupID := strings.TrimSpace(published.SuggestedGroup)
	tags := published.SuggestedTags
	if overlayGroupID != nil {
		if g := normalizeGroupID(*overlayGroupID); g != "" {
			groupID = g
		}
	}
	rankKey := strings.TrimSpace(published.SuggestedRankKey)
	if overlayRankKey != nil {
		if rk := strings.TrimSpace(*overlayRankKey); rk != "" {
			rankKey = rk
		}
	}
	return EffectiveOrganization{
		GroupID:    groupID,
		GroupLabel: GroupLabel(groupID),
		RankKey:    rankKey,
		Tags:       tags,
	}
}
