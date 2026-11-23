package agentruntimemcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// Trigger manifest (version 1): what a connector can start workflows with.
//
// A connector that implements TriggerAdapter declares its events, how the vendor reaches the
// platform, how the vendor webhook is registered and the vendor-side choices (scope). The
// router serves the manifest at GET /{adapter}/triggers; control-service validates it and
// stores it in the catalog. Shapes are fixed by the triggers engine contract (section 8.2).
const (
	// TriggerManifestVersion is the only manifest version the platform understands.
	TriggerManifestVersion = 1
	// TriggerTrustFirstParty marks a manifest written by the platform's own connector code.
	TriggerTrustFirstParty = "first_party"
	// MaxTriggerManifestBytes is the size cap control applies to a stored manifest.
	MaxTriggerManifestBytes = 64 * 1024

	// Delivery modes.
	TriggerDeliveryConnectorWebhook = "connector_webhook" // the vendor posts to the connector route
	TriggerDeliveryManualURL        = "manual_url"        // a person pastes the URL into the vendor dashboard
	TriggerDeliveryPlatformWebhook  = "platform_webhook"  // the platform's own edge receives the event

	// Registration modes.
	TriggerRegistrationAuto   = "auto"
	TriggerRegistrationManual = "manual"
	TriggerRegistrationNone   = "none"

	// Registration limits.
	TriggerLimitOnePerConnection  = "one_per_connection"
	TriggerLimitManyPerConnection = "many_per_connection"

	// RoutePlaceholder is the literal path segment standing for the ingress route token.
	RoutePlaceholder = "{route_token}"
)

// Filter operators the engine evaluates generically (contract section 6.2).
var triggerFilterOps = map[string]struct{}{"eq": {}, "in": {}, "prefix": {}, "contains": {}}

// TriggerAdapter may be implemented alongside Adapter by connectors that start workflows.
// RunWithRouter serves the manifest at GET /{adapter}/triggers (internal token required).
type TriggerAdapter interface {
	TriggerManifest() TriggerManifest
}

// TriggerManifest is the declaration served at GET /{adapter}/triggers.
type TriggerManifest struct {
	Version      int                    `json:"version"`
	Trust        string                 `json:"trust"`
	Label        string                 `json:"label"`
	Events       []TriggerManifestEvent `json:"events"`
	Delivery     TriggerDelivery        `json:"delivery"`
	Registration TriggerRegistration    `json:"registration"`
	ScopeSchema  map[string]any         `json:"scope_schema"`
	Connection   TriggerConnection      `json:"connection"`
	Channel      *TriggerChannel        `json:"channel"`
}

// TriggerManifestEvent is one event a person can pick.
type TriggerManifestEvent struct {
	ID           string               `json:"id"`
	Label        string               `json:"label"`
	Default      bool                 `json:"default"`
	Description  string               `json:"description"`
	Sample       map[string]any       `json:"sample"`
	Fields       []TriggerField       `json:"fields"`
	FilterFields []TriggerFilterField `json:"filter_fields"`
}

// TriggerField describes one payload field a workflow can read.
type TriggerField struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

// TriggerFilterField is a field the person may filter on. The engine evaluates it against
// the payload generically: the value at Path (dot path) is compared with Op.
type TriggerFilterField struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Path  string `json:"path"`
	Op    string `json:"op"`
}

// TriggerDelivery says how the vendor reaches the platform.
type TriggerDelivery struct {
	Mode         string        `json:"mode"`
	Path         string        `json:"path"`
	Verify       TriggerVerify `json:"verify"`
	Idempotency  string        `json:"idempotency"`
	MaxBodyBytes int64         `json:"max_body_bytes"`
}

// TriggerVerify describes (it does not implement) how the vendor call is authenticated.
// The code that verifies lives in the connector and is checked by connectortest.
type TriggerVerify struct {
	Kind   string `json:"kind"`
	Header string `json:"header"`
}

// TriggerRegistration says how the vendor webhook is created and removed.
type TriggerRegistration struct {
	Mode           string   `json:"mode"`
	Limit          string   `json:"limit"`
	Unit           string   `json:"unit,omitempty"`
	RegisterPath   string   `json:"register_path"`
	StatusPath     string   `json:"status_path"`
	UnregisterPath string   `json:"unregister_path"`
	ManualSteps    []string `json:"manual_steps"`
	DashboardURL   *string  `json:"dashboard_url"`
}

// TriggerConnection names the credential keys a connection needs to count as connected.
type TriggerConnection struct {
	RequiredCredentialKeys []string `json:"required_credential_keys"`
}

// TriggerChannel marks sources whose events carry a channel envelope.
type TriggerChannel struct {
	Channel string `json:"channel"`
	Contact bool   `json:"contact"`
}

// TriggerDeliveryPathTemplate is the one path a connector_webhook manifest may declare.
func TriggerDeliveryPathTemplate(adapter string) string {
	return "/" + normalizeAdapterSlug(adapter) + "/webhook/" + RoutePlaceholder
}

// TriggerRegistrationPathTemplate is the one path a registration route may declare.
// action is register-webhook, webhook-status or unregister-webhook.
func TriggerRegistrationPathTemplate(adapter, action string) string {
	return "/" + normalizeAdapterSlug(adapter) + "/inbound/" + action + "/" + RoutePlaceholder
}

// TriggerManifestPath returns GET /{adapter}/triggers.
func TriggerManifestPath(adapter string) string {
	return "/" + normalizeAdapterSlug(adapter) + "/triggers"
}

func normalizeAdapterSlug(adapter string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(adapter)), "/")
}

// Normalized returns a copy in which every list is an empty array rather than null, so the
// served JSON has the exact shape of the contract.
func (m TriggerManifest) Normalized() TriggerManifest {
	out := m
	out.Events = make([]TriggerManifestEvent, len(m.Events))
	for i, ev := range m.Events {
		if ev.Fields == nil {
			ev.Fields = []TriggerField{}
		}
		if ev.FilterFields == nil {
			ev.FilterFields = []TriggerFilterField{}
		}
		out.Events[i] = ev
	}
	if out.Registration.ManualSteps == nil {
		out.Registration.ManualSteps = []string{}
	}
	if out.Connection.RequiredCredentialKeys == nil {
		out.Connection.RequiredCredentialKeys = []string{}
	}
	return out
}

// Validate applies the rules control applies at registration (contract section 8.3) so a
// connector fails its own tests, and its startup, instead of failing discovery in production.
func (m TriggerManifest) Validate(adapter string) error {
	adapter = normalizeAdapterSlug(adapter)
	if adapter == "" {
		return fmt.Errorf("trigger manifest: adapter name required")
	}
	if m.Version != TriggerManifestVersion {
		return fmt.Errorf("trigger manifest: version %d is not supported (want %d)", m.Version, TriggerManifestVersion)
	}
	if m.Trust != TriggerTrustFirstParty {
		return fmt.Errorf("trigger manifest: trust must be %q, got %q", TriggerTrustFirstParty, m.Trust)
	}
	if strings.TrimSpace(m.Label) == "" {
		return fmt.Errorf("trigger manifest: label required")
	}
	if err := m.validateEvents(); err != nil {
		return err
	}
	if err := m.validateDelivery(adapter); err != nil {
		return err
	}
	if err := m.validateRegistration(adapter); err != nil {
		return err
	}
	if err := validateScopeSchema(m.ScopeSchema); err != nil {
		return err
	}
	raw, err := json.Marshal(m.Normalized())
	if err != nil {
		return fmt.Errorf("trigger manifest: %w", err)
	}
	if len(raw) > MaxTriggerManifestBytes {
		return fmt.Errorf("trigger manifest: %d bytes exceeds the 64 KB limit", len(raw))
	}
	return nil
}

func (m TriggerManifest) validateEvents() error {
	if len(m.Events) == 0 {
		return fmt.Errorf("trigger manifest: events required")
	}
	seen := map[string]struct{}{}
	for _, ev := range m.Events {
		id := strings.TrimSpace(ev.ID)
		if id == "" {
			return fmt.Errorf("trigger manifest: event id required")
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("trigger manifest: duplicate event id %q", id)
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(ev.Label) == "" {
			return fmt.Errorf("trigger manifest: event %q needs a label", id)
		}
		for _, f := range ev.Fields {
			if strings.TrimSpace(f.Path) == "" {
				return fmt.Errorf("trigger manifest: event %q has a field with an empty path", id)
			}
		}
		filterIDs := map[string]struct{}{}
		for _, f := range ev.FilterFields {
			fid := strings.TrimSpace(f.ID)
			if fid == "" {
				return fmt.Errorf("trigger manifest: event %q has a filter field id missing", id)
			}
			if _, dup := filterIDs[fid]; dup {
				return fmt.Errorf("trigger manifest: event %q has a duplicate filter field %q", id, fid)
			}
			filterIDs[fid] = struct{}{}
			if strings.TrimSpace(f.Path) == "" {
				return fmt.Errorf("trigger manifest: event %q filter field %q has an empty path", id, fid)
			}
			if _, ok := triggerFilterOps[f.Op]; !ok {
				return fmt.Errorf("trigger manifest: event %q filter field %q has op %q (allowed: eq, in, prefix, contains)", id, fid, f.Op)
			}
		}
	}
	return nil
}

func (m TriggerManifest) validateDelivery(adapter string) error {
	d := m.Delivery
	switch d.Mode {
	case TriggerDeliveryConnectorWebhook:
		if want := TriggerDeliveryPathTemplate(adapter); d.Path != want {
			return fmt.Errorf("trigger manifest: delivery.path must be exactly %q, got %q", want, d.Path)
		}
		if d.MaxBodyBytes <= 0 {
			return fmt.Errorf("trigger manifest: delivery.max_body_bytes must be positive")
		}
		if strings.TrimSpace(d.Verify.Kind) == "" {
			return fmt.Errorf("trigger manifest: delivery.verify.kind required")
		}
		if strings.TrimSpace(d.Idempotency) == "" {
			return fmt.Errorf("trigger manifest: delivery.idempotency required")
		}
	case TriggerDeliveryManualURL, TriggerDeliveryPlatformWebhook:
	default:
		return fmt.Errorf("trigger manifest: delivery.mode %q is not one of connector_webhook, manual_url, platform_webhook", d.Mode)
	}
	return nil
}

func (m TriggerManifest) validateRegistration(adapter string) error {
	r := m.Registration
	switch r.Mode {
	case TriggerRegistrationAuto, TriggerRegistrationManual, TriggerRegistrationNone:
	default:
		return fmt.Errorf("trigger manifest: registration.mode %q is not one of auto, manual, none", r.Mode)
	}
	switch r.Limit {
	case TriggerLimitOnePerConnection:
	case TriggerLimitManyPerConnection:
		if strings.TrimSpace(r.Unit) == "" {
			return fmt.Errorf("trigger manifest: registration.unit required when limit is many_per_connection")
		}
	default:
		return fmt.Errorf("trigger manifest: registration.limit %q is not one of one_per_connection, many_per_connection", r.Limit)
	}
	for _, p := range []struct{ name, got, action string }{
		{"register_path", r.RegisterPath, "register-webhook"},
		{"status_path", r.StatusPath, "webhook-status"},
		{"unregister_path", r.UnregisterPath, "unregister-webhook"},
	} {
		want := TriggerRegistrationPathTemplate(adapter, p.action)
		if p.got == "" {
			if r.Mode == TriggerRegistrationAuto {
				return fmt.Errorf("trigger manifest: registration.%s required for auto registration (%s)", p.name, want)
			}
			continue
		}
		if p.got != want {
			return fmt.Errorf("trigger manifest: registration.%s must be exactly %q, got %q", p.name, want, p.got)
		}
	}
	if r.Mode == TriggerRegistrationManual {
		hasDashboard := r.DashboardURL != nil && strings.TrimSpace(*r.DashboardURL) != ""
		if len(r.ManualSteps) == 0 && !hasDashboard {
			return fmt.Errorf("trigger manifest: manual registration needs manual_steps or a dashboard_url")
		}
	}
	return nil
}

func validateScopeSchema(schema map[string]any) error {
	if schema == nil {
		return nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("trigger manifest: scope_schema: %w", err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("trigger manifest: scope_schema is not a JSON Schema: %w", err)
	}
	if _, err := s.Resolve(nil); err != nil {
		return fmt.Errorf("trigger manifest: scope_schema is not a valid JSON Schema: %w", err)
	}
	if err := checkSchemaTypes(schema, "scope_schema"); err != nil {
		return err
	}
	return checkFromEvents(schema)
}

var jsonSchemaTypes = map[string]struct{}{
	"object": {}, "array": {}, "string": {}, "number": {}, "integer": {}, "boolean": {}, "null": {},
}

func checkSchemaTypes(node any, where string) error {
	switch v := node.(type) {
	case map[string]any:
		if t, ok := v["type"]; ok {
			names := []string{}
			switch tt := t.(type) {
			case string:
				names = append(names, tt)
			case []any:
				for _, n := range tt {
					if s, ok := n.(string); ok {
						names = append(names, s)
					}
				}
			}
			for _, n := range names {
				if _, ok := jsonSchemaTypes[n]; !ok {
					return fmt.Errorf("trigger manifest: %s has unknown type %q", where, n)
				}
			}
		}
		for k, child := range v {
			if k == "enum" || k == "const" || k == "default" || k == "examples" {
				continue
			}
			if err := checkSchemaTypes(child, where+"."+k); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range v {
			if err := checkSchemaTypes(child, fmt.Sprintf("%s[%d]", where, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkFromEvents(schema map[string]any) error {
	props, _ := schema["properties"].(map[string]any)
	for name, raw := range props {
		prop, _ := raw.(map[string]any)
		if prop == nil {
			continue
		}
		if flag, _ := prop["x-from-events"].(bool); flag && prop["type"] != "array" {
			return fmt.Errorf("trigger manifest: scope_schema property %q uses x-from-events, which needs type array", name)
		}
	}
	return nil
}
