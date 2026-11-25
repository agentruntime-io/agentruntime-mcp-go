package agentruntimemcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func validTestManifest() TriggerManifest {
	return TriggerManifest{
		Version: TriggerManifestVersion,
		Trust:   TriggerTrustFirstParty,
		Label:   "Telegram",
		Events: []TriggerManifestEvent{{
			ID:          "message",
			Label:       "A message is sent to the bot",
			Default:     true,
			Description: "A person writes to the bot.",
			Sample:      map[string]any{"update_id": 1},
			Fields:      []TriggerField{{Path: "message.text", Label: "Message text", Type: "string"}},
			FilterFields: []TriggerFilterField{
				{ID: "chat_id", Label: "Only this chat", Type: "string", Path: "message.chat.id", Op: "eq"},
			},
		}},
		Delivery: TriggerDelivery{
			Mode:         TriggerDeliveryConnectorWebhook,
			Path:         "/telegram/webhook/{route_token}",
			Verify:       TriggerVerify{Kind: "shared_secret_header", Header: "X-Telegram-Bot-Api-Secret-Token"},
			Idempotency:  "update_id",
			MaxBodyBytes: 1 << 20,
		},
		Registration: TriggerRegistration{
			Mode:           TriggerRegistrationAuto,
			Limit:          TriggerLimitOnePerConnection,
			RegisterPath:   "/telegram/inbound/register-webhook/{route_token}",
			StatusPath:     "/telegram/inbound/webhook-status/{route_token}",
			UnregisterPath: "/telegram/inbound/unregister-webhook/{route_token}",
			ManualSteps:    []string{},
		},
		ScopeSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"allowed_updates": map[string]any{
					"type":          "array",
					"items":         map[string]any{"type": "string"},
					"x-from-events": true,
				},
			},
		},
		Connection: TriggerConnection{RequiredCredentialKeys: []string{"telegram_bot_token"}},
	}
}

func TestTriggerManifestValidate_acceptsValid(t *testing.T) {
	if err := validTestManifest().Validate("telegram"); err != nil {
		t.Fatalf("valid manifest refused: %v", err)
	}
}

func TestTriggerManifestValidate_refusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(m *TriggerManifest)
		want   string
	}{
		{"unknown version", func(m *TriggerManifest) { m.Version = 2 }, "version"},
		{"not first party", func(m *TriggerManifest) { m.Trust = "third_party" }, "trust"},
		{"no label", func(m *TriggerManifest) { m.Label = "" }, "label"},
		{"no events", func(m *TriggerManifest) { m.Events = nil }, "events"},
		{"duplicate event id", func(m *TriggerManifest) { m.Events = append(m.Events, m.Events[0]) }, "duplicate event id"},
		{"empty event id", func(m *TriggerManifest) { m.Events[0].ID = "" }, "event id"},
		{"empty filter path", func(m *TriggerManifest) { m.Events[0].FilterFields[0].Path = "" }, "path"},
		{"bad filter op", func(m *TriggerManifest) { m.Events[0].FilterFields[0].Op = "regex" }, "op"},
		{"empty filter id", func(m *TriggerManifest) { m.Events[0].FilterFields[0].ID = "" }, "filter field id"},
		{"duplicate filter id", func(m *TriggerManifest) {
			m.Events[0].FilterFields = append(m.Events[0].FilterFields, m.Events[0].FilterFields[0])
		}, "duplicate filter field"},
		{"wrong delivery path", func(m *TriggerManifest) { m.Delivery.Path = "/other/webhook/{route_token}" }, "delivery.path"},
		{"unknown delivery mode", func(m *TriggerManifest) { m.Delivery.Mode = "carrier_pigeon" }, "delivery.mode"},
		{"no body cap", func(m *TriggerManifest) { m.Delivery.MaxBodyBytes = 0 }, "max_body_bytes"},
		{"no verify kind", func(m *TriggerManifest) { m.Delivery.Verify.Kind = "" }, "verify"},
		{"wrong register path", func(m *TriggerManifest) { m.Registration.RegisterPath = "/telegram/inbound/register/{route_token}" }, "register_path"},
		{"wrong status path", func(m *TriggerManifest) { m.Registration.StatusPath = "/github/inbound/webhook-status/{route_token}" }, "status_path"},
		{"wrong unregister path", func(m *TriggerManifest) { m.Registration.UnregisterPath = "/telegram/inbound/unregister-webhook/x" }, "unregister_path"},
		{"auto without register path", func(m *TriggerManifest) { m.Registration.RegisterPath = "" }, "register_path"},
		{"unknown registration mode", func(m *TriggerManifest) { m.Registration.Mode = "sometimes" }, "registration.mode"},
		{"unknown limit", func(m *TriggerManifest) { m.Registration.Limit = "lots" }, "registration.limit"},
		{"many without unit", func(m *TriggerManifest) { m.Registration.Limit = TriggerLimitManyPerConnection }, "unit"},
		{"invalid scope schema", func(m *TriggerManifest) { m.ScopeSchema = map[string]any{"type": "nonsense"} }, "scope_schema"},
		{"x-from-events on a non-array", func(m *TriggerManifest) {
			m.ScopeSchema = map[string]any{"type": "object", "properties": map[string]any{
				"a": map[string]any{"type": "string", "x-from-events": true}}}
		}, "x-from-events"},
		{"too big", func(m *TriggerManifest) { m.Events[0].Description = strings.Repeat("x", MaxTriggerManifestBytes) }, "64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validTestManifest()
			tc.mutate(&m)
			err := m.Validate("telegram")
			if err == nil {
				t.Fatalf("want refusal mentioning %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}

func TestTriggerManifestValidate_manualNeedsInstructions(t *testing.T) {
	m := validTestManifest()
	m.Delivery.Mode = TriggerDeliveryConnectorWebhook
	m.Registration = TriggerRegistration{Mode: TriggerRegistrationManual, Limit: TriggerLimitOnePerConnection}
	if err := m.Validate("telegram"); err == nil || !strings.Contains(err.Error(), "manual") {
		t.Fatalf("manual registration without steps or dashboard must be refused, got %v", err)
	}
	dash := "https://example.test/dashboard"
	m.Registration.DashboardURL = &dash
	if err := m.Validate("telegram"); err != nil {
		t.Fatalf("manual registration with a dashboard url should pass: %v", err)
	}
}

func TestTriggerManifestJSONShape(t *testing.T) {
	raw, err := json.Marshal(validTestManifest().Normalized())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "trust", "label", "events", "delivery", "registration", "scope_schema", "connection", "channel"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("manifest JSON missing top-level key %q: %s", key, raw)
		}
	}
	if m["channel"] != nil {
		t.Fatalf("channel must be null for a connector without channel events, got %v", m["channel"])
	}
	reg := m["registration"].(map[string]any)
	if steps, ok := reg["manual_steps"].([]any); !ok || len(steps) != 0 {
		t.Fatalf("manual_steps must be an empty array, got %v", reg["manual_steps"])
	}
	if v, present := reg["dashboard_url"]; !present || v != nil {
		t.Fatalf("dashboard_url must be present and null, got %v (present=%v)", v, present)
	}
	ev := m["events"].([]any)[0].(map[string]any)
	for _, key := range []string{"id", "label", "default", "description", "sample", "fields", "filter_fields"} {
		if _, ok := ev[key]; !ok {
			t.Fatalf("event JSON missing key %q: %v", key, ev)
		}
	}
	del := m["delivery"].(map[string]any)
	if del["max_body_bytes"].(float64) != 1<<20 {
		t.Fatalf("max_body_bytes = %v", del["max_body_bytes"])
	}
	if _, ok := del["verify"].(map[string]any)["header"]; !ok {
		t.Fatalf("verify.header missing: %v", del)
	}
}

func TestTriggerManifestNormalized_emptyListsNotNull(t *testing.T) {
	m := TriggerManifest{Events: []TriggerManifestEvent{{ID: "x"}}}
	raw, _ := json.Marshal(m.Normalized())
	for _, bad := range []string{`"fields":null`, `"filter_fields":null`, `"manual_steps":null`, `"required_credential_keys":null`} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("normalized manifest still has %s: %s", bad, raw)
		}
	}
}

func TestTriggerManifestRegional_optionalAndOmittedWhenFalse(t *testing.T) {
	raw, _ := json.Marshal(validTestManifest().Normalized())
	if strings.Contains(string(raw), `"regional"`) {
		t.Fatalf("regional must be absent unless true: %s", raw)
	}
	m := validTestManifest()
	m.Regional = true
	if err := m.Validate("telegram"); err != nil {
		t.Fatalf("a regional manifest is valid: %v", err)
	}
	raw, _ = json.Marshal(m.Normalized())
	if !strings.Contains(string(raw), `"regional":true`) {
		t.Fatalf("regional:true missing: %s", raw)
	}
}

func TestTriggerManifestValidate_manyPerConnectionNeedsAFilterNamedAfterTheUnit(t *testing.T) {
	m := validTestManifest()
	m.Registration.Limit = TriggerLimitManyPerConnection
	m.Registration.Unit = "repo"
	err := m.Validate("telegram")
	if err == nil || !strings.Contains(err.Error(), `filter field "repo"`) {
		t.Fatalf("an event with no filter field named after the registration unit cannot be narrowed to a trigger's own scope, got %v", err)
	}
	m.Events[0].FilterFields = append(m.Events[0].FilterFields,
		TriggerFilterField{ID: "repo", Label: "Only this repository", Type: "string", Path: "repository.full_name", Op: "eq"})
	if err := m.Validate("telegram"); err != nil {
		t.Fatalf("a filter field named after the unit satisfies the rule: %v", err)
	}
	m.Events[0].FilterFields[len(m.Events[0].FilterFields)-1].Op = "contains"
	if err := m.Validate("telegram"); err == nil || !strings.Contains(err.Error(), "eq") {
		t.Fatalf("the unit filter must compare with eq (the engine matches the trigger's scope exactly), got %v", err)
	}
}

// control's scope_schema validator accepts a small subset: type is ONE of object, array, string, integer,
// number, boolean; properties and items are schema objects; required is a list of names; enum a list;
// additionalProperties a boolean. A manifest outside it is stored by nobody, so no trigger of that source
// could ever be created: the SDK refuses it first.
func TestTriggerManifestValidate_scopeSchemaStaysInsideControlsSubset(t *testing.T) {
	cases := []struct {
		name   string
		schema map[string]any
		want   string
	}{
		{"type list", map[string]any{"type": "object", "properties": map[string]any{
			"repo": map[string]any{"type": []any{"string", "array"}}}}, "type"},
		{"type null", map[string]any{"type": "null"}, "type"},
		{"type nonsense", map[string]any{"type": "text"}, "type"},
		{"items list", map[string]any{"type": "array", "items": []any{map[string]any{"type": "string"}}}, "items"},
		{"property not an object", map[string]any{"type": "object", "properties": map[string]any{"a": "string"}}, "properties"},
		{"required not a list", map[string]any{"type": "object", "required": "repo"}, "required"},
		{"required with a non-name", map[string]any{"type": "object", "required": []any{1}}, "required"},
		{"enum not a list", map[string]any{"type": "string", "enum": "a"}, "enum"},
		{"additionalProperties not a boolean", map[string]any{"type": "object", "additionalProperties": map[string]any{}}, "additionalProperties"},
		{"pattern not a string", map[string]any{"type": "string", "pattern": 7}, "pattern"},
		{"pattern that does not compile", map[string]any{"type": "string", "pattern": "([a-z"}, "pattern"},
		{"pattern with a construct RE2 lacks", map[string]any{"type": "string", "pattern": "(?=x)y"}, "pattern"},
		{"pattern over 512 characters", map[string]any{"type": "string", "pattern": strings.Repeat("a", 513)}, "pattern"},
		{"nested pattern that does not compile", map[string]any{"type": "object", "properties": map[string]any{
			"repo": map[string]any{"type": "string", "pattern": "["}}}, "pattern"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validTestManifest()
			m.ScopeSchema = tc.schema
			err := m.Validate("telegram")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a refusal naming %q, got %v", tc.want, err)
			}
		})
	}
	ok := map[string]any{"type": "object", "required": []any{"repo"}, "additionalProperties": false, "properties": map[string]any{
		"repo":   map[string]any{"type": "string", "title": "Repository", "minLength": 3, "pattern": "^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$"},
		"events": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []any{"push"}}, "x-from-events": true},
		"limit":  map[string]any{"type": "integer", "minimum": 1, "default": 5},
	}}
	m := validTestManifest()
	m.ScopeSchema = ok
	if err := m.Validate("telegram"); err != nil {
		t.Fatalf("a schema inside control's subset (annotations pass through): %v", err)
	}
}
