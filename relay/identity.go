package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Identity is the tenant and user a bearer token was issued to.
type Identity struct {
	TenantID string
	UserID   string
}

// TokenVerifier turns a bearer token presented on WSS connect into a verified Identity.
// tenantHint is the tenant the client says it is connecting for; some token kinds
// (a user's Keycloak session) need it to pick the workspace, and the verifier
// checks the user belongs to it.
type TokenVerifier interface {
	Verify(ctx context.Context, bearer, tenantHint string) (Identity, error)
}

var (
	// ErrInvalidToken means the platform rejected the token (unknown, expired, revoked,
	// wrong tenant). The connect is answered with 401.
	ErrInvalidToken = errors.New("invalid token")
	// ErrVerifierUnavailable means the token could not be checked (the platform was
	// unreachable or answered 5xx). The connect is answered with 503 and the client retries.
	ErrVerifierUnavailable = errors.New("token verification unavailable")
)

// VerifierConfig points the relay at the services that validate tokens.
type VerifierConfig struct {
	// BFFURL validates a user's Keycloak access token (Workbench) with GET /v1/me,
	// the same check the BFF applies to every console request.
	BFFURL string
	// WheelhouseURL and WheelhouseToken validate pat_ tokens with
	// POST /internal/pats/introspect, as the BFF and control-service do.
	WheelhouseURL   string
	WheelhouseToken string
}

// NewVerifier returns a verifier for the configured backends, or nil when none is set.
func NewVerifier(cfg VerifierConfig) TokenVerifier {
	bff := trimURL(cfg.BFFURL)
	wh := trimURL(cfg.WheelhouseURL)
	whToken := strings.TrimSpace(cfg.WheelhouseToken)
	if wh != "" && whToken == "" {
		wh = "" // introspection needs the internal token; without it the backend is off
	}
	if bff == "" && wh == "" {
		return nil
	}
	client := &http.Client{Timeout: 5 * time.Second}
	return &routingVerifier{
		bff:        bff,
		wheelhouse: wh,
		whToken:    whToken,
		client:     client,
	}
}

type routingVerifier struct {
	bff        string
	wheelhouse string
	whToken    string
	client     *http.Client
}

func (v *routingVerifier) Verify(ctx context.Context, bearer, tenantHint string) (Identity, error) {
	bearer = strings.TrimSpace(bearer)
	switch {
	case strings.HasPrefix(bearer, "pat_"):
		if v.wheelhouse == "" {
			return Identity{}, fmt.Errorf("%w: PAT validation is not configured", ErrInvalidToken)
		}
		return v.verifyPAT(ctx, bearer)
	case strings.Count(bearer, ".") == 2:
		if v.bff == "" {
			return Identity{}, fmt.Errorf("%w: JWT validation is not configured", ErrInvalidToken)
		}
		return v.verifyJWT(ctx, bearer, tenantHint)
	default:
		return Identity{}, fmt.Errorf("%w: unrecognised token", ErrInvalidToken)
	}
}

func (v *routingVerifier) verifyJWT(ctx context.Context, bearer, tenantHint string) (Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.bff+"/v1/me", nil)
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")
	if tenantHint != "" {
		req.Header.Set("X-Tenant-Id", tenantHint)
	}
	body, status, err := v.do(req)
	if err != nil {
		return Identity{}, err
	}
	if status != http.StatusOK {
		return Identity{}, statusError("bff", status)
	}
	var me struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		return Identity{}, fmt.Errorf("%w: bff response: %v", ErrVerifierUnavailable, err)
	}
	return identityOrInvalid(me.TenantID, me.UserID, true)
}

func (v *routingVerifier) verifyPAT(ctx context.Context, bearer string) (Identity, error) {
	payload, _ := json.Marshal(map[string]string{"token": bearer})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.wheelhouse+"/internal/pats/introspect", bytes.NewReader(payload))
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Internal-Token", v.whToken)
	body, status, err := v.do(req)
	if err != nil {
		return Identity{}, err
	}
	if status != http.StatusOK {
		return Identity{}, statusError("wheelhouse", status)
	}
	var pat struct {
		TenantID  string    `json:"tenant_id"`
		UserID    string    `json:"user_id"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &pat); err != nil {
		return Identity{}, fmt.Errorf("%w: wheelhouse response: %v", ErrVerifierUnavailable, err)
	}
	if !pat.ExpiresAt.IsZero() && time.Now().After(pat.ExpiresAt) {
		return Identity{}, fmt.Errorf("%w: expired", ErrInvalidToken)
	}
	return identityOrInvalid(pat.TenantID, pat.UserID, false)
}

func (v *routingVerifier) do(req *http.Request) ([]byte, int, error) {
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrVerifierUnavailable, err)
	}
	return body, resp.StatusCode, nil
}

func statusError(backend string, status int) error {
	if status >= 500 || status == http.StatusTooManyRequests {
		return fmt.Errorf("%w: %s answered %d", ErrVerifierUnavailable, backend, status)
	}
	return fmt.Errorf("%w: %s answered %d", ErrInvalidToken, backend, status)
}

// identityOrInvalid refuses a token with no tenant, or no user where one is
// required: a connection without them could not be bound to anyone. A PAT may be
// issued to a tenant without a user (a service token); a user session never is.
func identityOrInvalid(tenantID, userID string, requireUser bool) (Identity, error) {
	id := Identity{TenantID: strings.TrimSpace(tenantID), UserID: strings.TrimSpace(userID)}
	if id.TenantID == "" || (requireUser && id.UserID == "") {
		return Identity{}, fmt.Errorf("%w: token has no tenant or user", ErrInvalidToken)
	}
	return id, nil
}
