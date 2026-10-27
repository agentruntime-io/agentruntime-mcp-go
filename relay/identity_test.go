package relay_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentruntime-io/agentruntime-mcp-go/relay"
)

func TestVerifier_tokenKindNeedsItsBackend(t *testing.T) {
	platform := (&fakePlatform{}).server(t)
	defer platform.Close()

	onlyPAT := relay.NewVerifier(relay.VerifierConfig{WheelhouseURL: platform.URL, WheelhouseToken: testWheelhouseToken})
	if _, err := onlyPAT.Verify(context.Background(), testJWTAlice, "tenant-a"); !errors.Is(err, relay.ErrInvalidToken) {
		t.Fatalf("JWT with no BFF configured: err = %v, want ErrInvalidToken", err)
	}
	if id, err := onlyPAT.Verify(context.Background(), testPATAlice, ""); err != nil || id.TenantID != "tenant-a" || id.UserID != "user-alice" {
		t.Fatalf("PAT: id=%+v err=%v", id, err)
	}

	onlyBFF := relay.NewVerifier(relay.VerifierConfig{BFFURL: platform.URL})
	if _, err := onlyBFF.Verify(context.Background(), testPATAlice, ""); !errors.Is(err, relay.ErrInvalidToken) {
		t.Fatalf("PAT with no Wheelhouse configured: err = %v, want ErrInvalidToken", err)
	}
	if id, err := onlyBFF.Verify(context.Background(), testJWTAlice, "tenant-a"); err != nil || id.UserID != "user-alice" {
		t.Fatalf("JWT: id=%+v err=%v", id, err)
	}
	if _, err := onlyBFF.Verify(context.Background(), "opaque-token", ""); !errors.Is(err, relay.ErrInvalidToken) {
		t.Fatalf("unrecognised token: err = %v, want ErrInvalidToken", err)
	}
}

func TestVerifier_wrongWheelhouseTokenIsInvalid(t *testing.T) {
	platform := (&fakePlatform{}).server(t)
	defer platform.Close()
	v := relay.NewVerifier(relay.VerifierConfig{WheelhouseURL: platform.URL, WheelhouseToken: "not-the-secret"})
	if _, err := v.Verify(context.Background(), testPATAlice, ""); !errors.Is(err, relay.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestVerifier_unreachableBackendIsUnavailable(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	v := relay.NewVerifier(relay.VerifierConfig{BFFURL: dead.URL, WheelhouseURL: dead.URL, WheelhouseToken: "x"})
	for _, tok := range []string{testJWTAlice, testPATAlice} {
		if _, err := v.Verify(context.Background(), tok, "tenant-a"); !errors.Is(err, relay.ErrVerifierUnavailable) {
			t.Fatalf("%s: err = %v, want ErrVerifierUnavailable", tok, err)
		}
	}
}
