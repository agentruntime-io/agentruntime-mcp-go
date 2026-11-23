package agentruntimemcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewTriggerEvent_objectBodyBecomesPayload(t *testing.T) {
	ev := NewTriggerEvent("tok", "push", "github-1", "application/json", []byte(`{"ref":"refs/heads/main","big":9007199254740993}`))
	if ev.PayloadRaw != "" {
		t.Fatalf("object body must not use payload_raw: %q", ev.PayloadRaw)
	}
	if string(ev.Payload) != `{"ref":"refs/heads/main","big":9007199254740993}` {
		t.Fatalf("payload must keep the vendor bytes (large integers included), got %s", ev.Payload)
	}
}

func TestNewTriggerEvent_nonObjectBodyBecomesPayloadRaw(t *testing.T) {
	for _, body := range []string{`[1,2]`, `"text"`, `plain=form&data=1`, ``, `null`} {
		ev := NewTriggerEvent("tok", "x", "k", "text/plain", []byte(body))
		if len(ev.Payload) != 0 {
			t.Fatalf("body %q: payload must be empty, got %s", body, ev.Payload)
		}
		if ev.PayloadRaw != body {
			t.Fatalf("body %q: payload_raw = %q", body, ev.PayloadRaw)
		}
	}
}

func TestTriggerEvent_wireShape(t *testing.T) {
	ev := NewTriggerEvent("tok", "push", "github-1", "application/json", []byte(`{"a":1}`))
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for _, k := range []string{"route_token", "event_type", "event_key", "content_type", "payload", "channel"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("event JSON missing %q: %s", k, raw)
		}
	}
	if m["channel"] != nil {
		t.Fatalf("channel must be null: %v", m["channel"])
	}
	if _, ok := m["payload_raw"]; ok {
		t.Fatalf("payload_raw must be absent for an object body: %s", raw)
	}
}

func TestPostTriggerEvent_sendsContractRequest(t *testing.T) {
	var gotPath, gotToken, gotService, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.Method + " " + r.URL.Path
		gotToken = r.Header.Get("X-Internal-Token")
		gotService = r.Header.Get("X-Service-Name")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"event_id":"e1","duplicate":false,"matched":2,"fires":[{"trigger_id":"t1","fire_id":"f1"},{"trigger_id":"t2","fire_id":"f2"}]}`))
	}))
	defer srv.Close()

	ack, err := PostTriggerEvent(context.Background(), TriggerEnv{ControlURL: srv.URL + "/", InternalToken: "internal"},
		NewTriggerEvent("route-1", "push", "github-9", "application/json", []byte(`{"ref":"main"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "POST /internal/trigger-events/connector" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotToken != "internal" || gotService != ServiceConnectorRouter || gotCT != "application/json" {
		t.Fatalf("headers: token=%q service=%q ct=%q", gotToken, gotService, gotCT)
	}
	if gotBody["route_token"] != "route-1" || gotBody["event_type"] != "push" || gotBody["event_key"] != "github-9" {
		t.Fatalf("body = %v", gotBody)
	}
	if ack.EventID != "e1" || ack.Duplicate || ack.Matched != 2 || len(ack.Fires) != 2 || ack.Fires[1].FireID != "f2" {
		t.Fatalf("ack = %+v", ack)
	}
}

func TestPostTriggerEvent_errors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"unknown route", 404, `{"error":"not_found"}`},
		{"disabled", 403, `{"error":"forbidden"}`},
		{"engine down", 503, `upstream`},
		{"bad request", 400, `{"error":"validation_error"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := PostTriggerEvent(context.Background(), TriggerEnv{ControlURL: srv.URL, InternalToken: "t"},
				NewTriggerEvent("r", "e", "k", "", []byte(`{}`)))
			pe, ok := err.(*TriggerPostError)
			if !ok || pe.Status != tc.status {
				t.Fatalf("want *TriggerPostError status %d, got %#v", tc.status, err)
			}
		})
	}
}

func TestPostTriggerEvent_requiresConfigAndKey(t *testing.T) {
	ev := NewTriggerEvent("r", "e", "k", "", []byte(`{}`))
	if _, err := PostTriggerEvent(context.Background(), TriggerEnv{}, ev); err == nil {
		t.Fatal("missing control url and token must be an error")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) }))
	defer srv.Close()
	env := TriggerEnv{ControlURL: srv.URL, InternalToken: "t"}
	for _, bad := range []TriggerEvent{
		NewTriggerEvent("", "e", "k", "", []byte(`{}`)),
		NewTriggerEvent("r", "", "k", "", []byte(`{}`)),
		NewTriggerEvent("r", "e", "", "", []byte(`{}`)),
	} {
		if _, err := PostTriggerEvent(context.Background(), env, bad); err == nil {
			t.Fatalf("event %+v must be refused before any call", bad)
		}
	}
}

func TestResolveTriggerIngress(t *testing.T) {
	type call struct{ token, service, route string }
	var got call
	respond := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			var req struct {
				RouteToken string `json:"route_token"`
			}
			_ = json.Unmarshal(b, &req)
			got = call{r.Header.Get("X-Internal-Token"), r.Header.Get("X-Service-Name"), req.RouteToken}
			if r.Method != http.MethodPost || r.URL.Path != "/internal/trigger-ingress/resolve" {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}

	t.Run("found", func(t *testing.T) {
		srv := respond(200, `{"vendor_secret":"s3","connection_id":"c1","enabled":true,"tenant_id":"t","project_id":"p","server_id":"srv"}`)
		defer srv.Close()
		ing, err := ResolveTriggerIngress(context.Background(), TriggerEnv{ControlURL: srv.URL, InternalToken: "internal"}, " tok ")
		if err != nil || ing == nil {
			t.Fatalf("ing=%v err=%v", ing, err)
		}
		if ing.VendorSecret != "s3" || ing.ConnectionID != "c1" || ing.TenantID != "t" || ing.ProjectID != "p" || ing.ServerID != "srv" {
			t.Fatalf("ingress = %+v", ing)
		}
		if got.token != "internal" || got.service != ServiceConnectorRouter || got.route != "tok" {
			t.Fatalf("call = %+v", got)
		}
	})
	for name, resp := range map[string]struct {
		status int
		body   string
	}{
		"unknown":  {404, `{}`},
		"disabled": {403, `{}`},
		"flagged":  {200, `{"vendor_secret":"s","enabled":false}`},
	} {
		t.Run(name, func(t *testing.T) {
			srv := respond(resp.status, resp.body)
			defer srv.Close()
			ing, err := ResolveTriggerIngress(context.Background(), TriggerEnv{ControlURL: srv.URL, InternalToken: "i"}, "tok")
			if err != nil || ing != nil {
				t.Fatalf("a route that does not exist or is off is (nil, nil), got %v, %v", ing, err)
			}
		})
	}
	t.Run("control error", func(t *testing.T) {
		srv := respond(500, `boom`)
		defer srv.Close()
		if _, err := ResolveTriggerIngress(context.Background(), TriggerEnv{ControlURL: srv.URL, InternalToken: "i"}, "tok"); err == nil {
			t.Fatal("5xx from control must be an error so the vendor retries")
		}
	})
	t.Run("not configured", func(t *testing.T) {
		if _, err := ResolveTriggerIngress(context.Background(), TriggerEnv{}, "tok"); err == nil {
			t.Fatal("missing control config must be an error")
		}
	})
}

func TestLoadTriggerEnv(t *testing.T) {
	t.Setenv("MCP_CONTROL_SERVER_URL", " https://control.example/ ")
	t.Setenv("MCP_CONTROL_INTERNAL_TOKEN", " tok ")
	env := LoadTriggerEnv()
	if env.ControlURL != "https://control.example" || env.InternalToken != "tok" {
		t.Fatalf("env = %+v", env)
	}
}
