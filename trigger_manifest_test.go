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
