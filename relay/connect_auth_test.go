package relay_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentruntime-io/agentruntime-mcp-go/relay"
	"github.com/gorilla/websocket"
)

// fakeUserVerifier accepts the tokens in ids and rejects everything else, as a real verifier rejects a bad
// signature, issuer, audience or expiry. calls counts how often it was asked.
type fakeUserVerifier struct {
	ids   map[string]relay.UserIdentity
	calls atomic.Int32
}

func (f *fakeUserVerifier) VerifyUserToken(_ context.Context, token string) (relay.UserIdentity, error) {
	f.calls.Add(1)
	id, ok := f.ids[token]
	if !ok {
		return relay.UserIdentity{}, errors.New("token rejected")
	}
	return id, nil
}

const (
	userTokenAlice = "jwt-alice"
	userTokenBob   = "jwt-bob"
	userTokenNoIDs = "jwt-no-identity"
)

func newFakeUserVerifier() *fakeUserVerifier {
	return &fakeUserVerifier{ids: map[string]relay.UserIdentity{
		userTokenAlice: {UserID: "u-alice", TenantIDs: []string{"t1", "t2"}},
		userTokenBob:   {UserID: "u-bob", TenantIDs: []string{"t9"}},
		// A token that verifies but names no user or tenant must not open anything.
		userTokenNoIDs: {},
	}}
}

func newConnectFixture(t *testing.T, cfg relay.ServerConfig) (*httptest.Server, *relay.Hub) {
	t.Helper()
	hub := relay.NewHub()
	srv := relay.NewServer(cfg)
	srv.Hub = hub
	m := http.NewServeMux()
	srv.RegisterRoutes(m)
	ts := httptest.NewServer(m)
	t.Cleanup(ts.Close)
	return ts, hub
}

func harnessConnectURL(ts *httptest.Server, query string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/harness/connect?" + query
}

func dialWithBearer(url, bearer string) (*websocket.Conn, *http.Response, error) {
	header := http.Header{}
	if bearer != "" {
		header.Set("Authorization", "Bearer "+bearer)
	}
	return websocket.DefaultDialer.Dial(url, header)
}

func waitHarnessOnline(t *testing.T, hub *relay.Hub, workerID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.HarnessOnline(workerID) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("worker %q never registered", workerID)
}

func TestHarnessConnect_userTokenBindsWorkerToUser(t *testing.T) {
	ts, hub := newConnectFixture(t, relay.ServerConfig{AuthToken: authTestToken, UserVerifier: newFakeUserVerifier()})

	cases := []struct {
		name   string
		bearer string
		query  string
		want   int // 0 = the upgrade succeeds
	}{
		{"own worker, user and tenant", userTokenAlice, "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=t1", 0},
		{"second tenant of the token", userTokenAlice, "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=t2", 0},
		{"no worker id: the relay assigns one", userTokenAlice, "user_id=u-alice&tenant_id=t1", 0},
		{"another user's id", userTokenAlice, "worker_id=workbench:u-alice&user_id=u-bob&tenant_id=t1", http.StatusForbidden},
		{"user_id missing", userTokenAlice, "worker_id=workbench:u-alice&tenant_id=t1", http.StatusForbidden},
		{"another user's worker id", userTokenAlice, "worker_id=workbench:u-bob&user_id=u-alice&tenant_id=t1", http.StatusForbidden},
		{"worker id outside the workbench namespace", userTokenAlice, "worker_id=shared:a&user_id=u-alice&tenant_id=t1", http.StatusForbidden},
		{"worker id with the prefix only", userTokenAlice, "worker_id=workbench:&user_id=u-alice&tenant_id=t1", http.StatusForbidden},
		{"tenant of someone else", userTokenAlice, "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=t9", http.StatusForbidden},
		{"tenant_id missing", userTokenAlice, "worker_id=workbench:u-alice&user_id=u-alice", http.StatusForbidden},
		{"tenant_id blank", userTokenAlice, "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=%20", http.StatusForbidden},
		{"another user's token cannot take alice's worker", userTokenBob, "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=t1", http.StatusForbidden},
		{"token without user or tenant", userTokenNoIDs, "user_id=&tenant_id=", http.StatusForbidden},
		{"token the verifier rejects", "jwt-forged", "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=t1", http.StatusUnauthorized},
		{"no credentials", "", "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=t1", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, resp, err := dialWithBearer(harnessConnectURL(ts, tc.query), tc.bearer)
			if tc.want == 0 {
				if err != nil {
					t.Fatalf("dial failed: %v", err)
				}
				ws.Close()
				return
			}
			if err == nil {
				ws.Close()
				t.Fatalf("dial must fail with %d", tc.want)
			}
			if resp == nil || resp.StatusCode != tc.want {
				t.Fatalf("expected %d, got %v", tc.want, resp)
			}
			body, _ := io.ReadAll(resp.Body)
			if want := map[int]string{http.StatusUnauthorized: "unauthorized\n", http.StatusForbidden: "forbidden\n"}[tc.want]; string(body) != want {
				t.Fatalf("body must be generic, got %q", body)
			}
		})
	}

	// None of the rejected dials may have left a worker behind.
	for _, id := range []string{"workbench:u-bob", "shared:a", "workbench:"} {
		if hub.HarnessOnline(id) {
			t.Fatalf("rejected connect registered worker %q", id)
		}
	}
}

func TestHarnessConnect_userTokenRegistersVerifiedIdentity(t *testing.T) {
	ts, hub := newConnectFixture(t, relay.ServerConfig{AuthToken: authTestToken, UserVerifier: newFakeUserVerifier()})

	ws, _, err := dialWithBearer(harnessConnectURL(ts, "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=t2&tier=heavy"), userTokenAlice)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	waitHarnessOnline(t, hub, "workbench:u-alice")

	conn, _ := hub.GetHarness("workbench:u-alice")
	if conn.UserID != "u-alice" || conn.TenantID != "t2" {
		t.Fatalf("worker registered as user=%q tenant=%q, want u-alice / t2", conn.UserID, conn.TenantID)
	}
	if got, ok := hub.HarnessForUser("t2", "u-alice"); !ok || got.WorkerID != "workbench:u-alice" {
		t.Fatalf("worker is not addressable as its user's heavy worker: %v %v", got, ok)
	}
}

func TestHarnessConnect_relayTokenStillTrustedForServiceWorkers(t *testing.T) {
	v := newFakeUserVerifier()
	ts, hub := newConnectFixture(t, relay.ServerConfig{AuthToken: authTestToken, UserVerifier: v})

	// A service worker names its own id and tenant: no user binding applies to it, and the verifier is not asked.
	ws, _, err := dialWithBearer(harnessConnectURL(ts, "worker_id=shared:svc&tenant_id=t-any&tier=lightweight"), authTestToken)
	if err != nil {
		t.Fatalf("relay token must still connect a service worker: %v", err)
	}
	defer ws.Close()
	waitHarnessOnline(t, hub, "shared:svc")
	if v.calls.Load() != 0 {
		t.Fatalf("verifier was called %d times for a relay-token connect", v.calls.Load())
	}

	header := http.Header{relay.RelayTokenHeader: {authTestToken}}
	ws2, _, err := websocket.DefaultDialer.Dial(harnessConnectURL(ts, "worker_id=shared:svc2&tenant_id=t-any"), header)
	if err != nil {
		t.Fatalf("X-Relay-Token must connect a service worker: %v", err)
	}
	ws2.Close()
}

func TestHarnessConnect_userTokenIsRejectedWithoutAVerifier(t *testing.T) {
	ts, _ := newConnectFixture(t, relay.ServerConfig{AuthToken: authTestToken})
	_, resp, err := dialWithBearer(harnessConnectURL(ts, "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=t1"), userTokenAlice)
	if err == nil {
		t.Fatal("a user token must not connect when no verifier is configured")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", resp)
	}
}

func TestHarnessConnect_failsClosedWithNeitherCredentialConfigured(t *testing.T) {
	ts, _ := newConnectFixture(t, relay.ServerConfig{})
	for _, bearer := range []string{"", "anything", authTestToken, userTokenAlice} {
		_, resp, err := dialWithBearer(harnessConnectURL(ts, "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=t1"), bearer)
		if err == nil {
			t.Fatalf("bearer %q connected with nothing configured", bearer)
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("bearer %q: expected 401, got %v", bearer, resp)
		}
	}
}

func TestHarnessConnect_userTokenWorksWithoutRelayToken(t *testing.T) {
	// No RELAY_AUTH_TOKEN: the relay-token path is off (a blank token must not match), the user-token path stays.
	ts, _ := newConnectFixture(t, relay.ServerConfig{UserVerifier: newFakeUserVerifier()})

	ws, _, err := dialWithBearer(harnessConnectURL(ts, "worker_id=workbench:u-alice&user_id=u-alice&tenant_id=t1"), userTokenAlice)
	if err != nil {
		t.Fatalf("user token must connect: %v", err)
	}
	ws.Close()

	for _, bearer := range []string{"", "anything"} {
		if _, resp, err := dialWithBearer(harnessConnectURL(ts, "worker_id=shared:svc&tenant_id=t1"), bearer); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("bearer %q must be rejected without a configured relay token: err=%v resp=%v", bearer, err, resp)
		}
	}
}

// A user token is an identity for the connect handshake only. Every other route stays relay-token only, so a
// user can neither pick a worker nor run on one, forward to a connector, or register an MCP instance.
func TestUserToken_doesNotOpenOtherRoutes(t *testing.T) {
	v := newFakeUserVerifier()
	f := newAuthFixture(t, authTestToken)
	srv := relay.NewServer(relay.ServerConfig{AuthToken: authTestToken, UserVerifier: v})
	srv.Hub = f.hub
	m := http.NewServeMux()
	srv.RegisterRoutes(m)
	ts := httptest.NewServer(m)
	t.Cleanup(ts.Close)

	for _, route := range relayRoutes {
		if route.name == "harness connect" {
			continue
		}
		t.Run(route.name, func(t *testing.T) {
			req, err := http.NewRequest(route.method, ts.URL+route.path, strings.NewReader(route.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+userTokenAlice)
			resp := doRequest(t, req)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", resp.StatusCode)
			}
		})
	}
	if v.calls.Load() != 0 {
		t.Fatalf("verifier was called %d times on routes that do not take user tokens", v.calls.Load())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.mcpSeen) != 0 || len(f.jobsSeen) != 0 {
		t.Fatalf("user token reached a connection: mcp=%d jobs=%d", len(f.mcpSeen), len(f.jobsSeen))
	}
}

func TestUserJWTFromEnv(t *testing.T) {
	const (
		issuer   = "RELAY_USER_JWT_ISSUER"
		audience = "RELAY_USER_JWT_AUDIENCE"
		jwks     = "RELAY_USER_JWT_JWKS_URL"
	)
	cases := []struct {
		name         string
		iss, aud, ks string
		wantOK       bool
	}{
		{"none set: off, quietly", "", "", "", false},
		{"all set", "https://auth.test/realms/r/", "https://mcp.test", "https://auth.test/realms/r/protocol/openid-connect/certs", true},
		{"issuer only", "https://auth.test/realms/r", "", "", false},
		{"no audience", "https://auth.test/realms/r", "", "https://auth.test/certs", false},
		{"no jwks url", "https://auth.test/realms/r", "https://mcp.test", "", false},
		{"blank values", "  ", "  ", "  ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(issuer, tc.iss)
			t.Setenv(audience, tc.aud)
			t.Setenv(jwks, tc.ks)
			gotIss, gotAud, gotKS, ok := relay.UserJWTFromEnv()
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				if gotIss != "" || gotAud != "" || gotKS != "" {
					t.Fatalf("partial config leaked values: %q %q %q", gotIss, gotAud, gotKS)
				}
				return
			}
			if gotIss != "https://auth.test/realms/r" || gotAud != tc.aud || gotKS != tc.ks {
				t.Fatalf("got %q %q %q", gotIss, gotAud, gotKS)
			}
		})
	}
}
