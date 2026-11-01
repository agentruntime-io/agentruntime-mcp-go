package agentruntimemcp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer   = "https://auth.example.test/realms/test"
	testAudience = "https://mcp.example.test"
	testKid      = "key-1"
)

// jwksFixture serves a JWKS with one RSA key and counts how often it is fetched.
type jwksFixture struct {
	key  *rsa.PrivateKey
	url  string
	hits atomic.Int32
}

func newJWKSFixture(t *testing.T) *jwksFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &jwksFixture{key: key}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": testKid,
			"kty": "RSA",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

func (f *jwksFixture) verifier() relayUserVerifier {
	return relayUserVerifier{jwt: newJWTVerifier(testIssuer, testAudience, f.url)}
}

func platformClaims(mutate func(jwt.MapClaims)) jwt.MapClaims {
	c := jwt.MapClaims{
		"iss":        testIssuer,
		"aud":        testAudience,
		"sub":        "keycloak-sub",
		"exp":        time.Now().Add(5 * time.Minute).Unix(),
		"user_id":    "u-alice",
		"tenant_ids": []any{"t1", "t2"},
	}
	if mutate != nil {
		mutate(c)
	}
	return c
}

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRelayUserVerifier_readsUserAndTenantsFromVerifiedToken(t *testing.T) {
	f := newJWKSFixture(t)
	v := f.verifier()

	id, err := v.VerifyUserToken(context.Background(), signRS256(t, f.key, testKid, platformClaims(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if id.UserID != "u-alice" || len(id.TenantIDs) != 2 || id.TenantIDs[0] != "t1" || id.TenantIDs[1] != "t2" {
		t.Fatalf("identity = %+v", id)
	}

	// A single tenant_id claim counts too, next to tenant_ids.
	id, err = v.VerifyUserToken(context.Background(), signRS256(t, f.key, testKid, platformClaims(func(c jwt.MapClaims) {
		c["tenant_id"] = "t-default"
	})))
	if err != nil {
		t.Fatal(err)
	}
	if got := id.TenantIDs; len(got) != 3 || got[2] != "t-default" {
		t.Fatalf("tenants = %v", got)
	}
}

func TestRelayUserVerifier_rejectsTokensThatAreNotPlatformUserTokens(t *testing.T) {
	f := newJWKSFixture(t)
	v := f.verifier()
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	hs256 := func() string {
		// Algorithm confusion: an HMAC token keyed with the public modulus must not be accepted.
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, platformClaims(nil))
		tok.Header["kid"] = testKid
		s, err := tok.SignedString(f.key.N.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}()
	none := func() string {
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, platformClaims(nil))
		tok.Header["kid"] = testKid
		s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}()

	cases := []struct {
		name  string
		token string
	}{
		{"signed by another key", signRS256(t, other, testKid, platformClaims(nil))},
		{"wrong audience", signRS256(t, f.key, testKid, platformClaims(func(c jwt.MapClaims) { c["aud"] = "https://elsewhere.test" }))},
		{"wrong issuer", signRS256(t, f.key, testKid, platformClaims(func(c jwt.MapClaims) { c["iss"] = "https://evil.test/realms/test" }))},
		{"expired", signRS256(t, f.key, testKid, platformClaims(func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }))},
		{"no user_id", signRS256(t, f.key, testKid, platformClaims(func(c jwt.MapClaims) { delete(c, "user_id") }))},
		{"blank user_id", signRS256(t, f.key, testKid, platformClaims(func(c jwt.MapClaims) { c["user_id"] = "  " }))},
		{"no tenants", signRS256(t, f.key, testKid, platformClaims(func(c jwt.MapClaims) { delete(c, "tenant_ids") }))},
		{"tenant_ids not a list", signRS256(t, f.key, testKid, platformClaims(func(c jwt.MapClaims) { c["tenant_ids"] = "t1" }))},
		{"empty tenant_ids", signRS256(t, f.key, testKid, platformClaims(func(c jwt.MapClaims) { c["tenant_ids"] = []any{"", " "} }))},
		{"hmac keyed with the public key", hs256},
		{"alg none", none},
		{"not a jwt", "not-a-jwt"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if id, err := v.VerifyUserToken(context.Background(), tc.token); err == nil {
				t.Fatalf("accepted: %+v", id)
			}
		})
	}
}

// Forged key ids must not turn every unauthenticated connect attempt into a fetch of the issuer's JWKS.
func TestJWTVerifier_unknownKeyIDDoesNotRefetchJWKSPerRequest(t *testing.T) {
	f := newJWKSFixture(t)
	v := f.verifier()

	if _, err := v.VerifyUserToken(context.Background(), signRS256(t, f.key, testKid, platformClaims(nil))); err != nil {
		t.Fatal(err)
	}
	if got := f.hits.Load(); got != 1 {
		t.Fatalf("warm-up fetched JWKS %d times, want 1", got)
	}

	for i := 0; i < 25; i++ {
		if _, err := v.VerifyUserToken(context.Background(), signRS256(t, f.key, "forged-kid", platformClaims(nil))); err == nil {
			t.Fatal("token with an unknown kid was accepted")
		}
	}
	if got := f.hits.Load(); got != 1 {
		t.Fatalf("25 forged key ids caused %d JWKS fetches, want none beyond the first", got)
	}

	// A key rotated in after the throttle window is still picked up.
	v.jwt.mu.Lock()
	v.jwt.cachedAt = time.Now().Add(-2 * jwksMinRefresh)
	v.jwt.mu.Unlock()
	if _, err := v.VerifyUserToken(context.Background(), signRS256(t, f.key, "forged-kid", platformClaims(nil))); err == nil {
		t.Fatal("token with an unknown kid was accepted")
	}
	if got := f.hits.Load(); got != 2 {
		t.Fatalf("expected one refetch after the window, got %d fetches", got)
	}
}

func TestNewRelayUserVerifier_nilWithoutAVerifier(t *testing.T) {
	if v := newRelayUserVerifier(nil); v != nil {
		t.Fatalf("expected a nil interface, got %#v", v)
	}
	if v := newRelayUserVerifier(newJWTVerifier("", "", "")); v != nil {
		t.Fatalf("an unconfigured verifier must give a nil interface, got %#v", v)
	}
}
