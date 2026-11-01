package relay

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
)

// RelayTokenHeader carries the relay token on requests whose Authorization header belongs to someone else, such
// as MCP forward, where Authorization is the downstream connector's credential. Authorization: Bearer <token>
// is accepted everywhere too.
const RelayTokenHeader = "X-Relay-Token"

// WorkbenchWorkerPrefix starts the worker id of an IDE (workbench) worker: "workbench:<user_id>". A worker that
// connects with a user token may use no other id.
const WorkbenchWorkerPrefix = "workbench:"

// Reasons authorize rejects a request. They go to the log only; the HTTP body is always the same generic text.
var (
	errAuthNotConfigured = errors.New("relay auth token not configured")
	errAuthMissing       = errors.New("no relay token presented")
	errAuthInvalid       = errors.New("relay token mismatch")
)

// UserIdentity is what a UserTokenVerifier reads from a verified platform access token: the platform user and
// every tenant the token is valid for.
type UserIdentity struct {
	UserID    string
	TenantIDs []string
}

// UserTokenVerifier checks a user's platform access token (signature, issuer, audience, expiry) and returns the
// identity it carries. The relay has no JWT code of its own; the router injects one through
// ServerConfig.UserVerifier.
type UserTokenVerifier interface {
	VerifyUserToken(ctx context.Context, token string) (UserIdentity, error)
}

// errBindingMismatch marks a valid user token used for a connect it does not cover (another user's id, another
// tenant, a worker id that is not the user's own).
var errBindingMismatch = errors.New("connect request does not match the token's user or tenants")

// authorize is the single authentication check for every relay route. It fails closed: with no AuthToken
// configured nothing is authorized, whatever the caller presents.
func (s *Server) authorize(r *http.Request) error {
	expected := strings.TrimSpace(s.Config.AuthToken)
	if expected == "" {
		return errAuthNotConfigured
	}
	presented := presentedRelayToken(r)
	if presented == "" {
		return errAuthMissing
	}
	if !tokensEqual(presented, expected) {
		return errAuthInvalid
	}
	return nil
}

// requireAuth runs h only for callers that present the relay token. Anyone else gets a bare 401: no reason in
// the body and no hint whether the route, worker or instance exists.
func (s *Server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.authorize(r); err != nil {
			log.Printf("relay: rejected %s %s from %s (%v)", r.Method, r.URL.Path, r.RemoteAddr, err)
			w.Header().Set("WWW-Authenticate", `Bearer realm="relay"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

// requireHarnessConnectAuth guards the harness worker connect route. A caller with the relay token (a service
// worker) is trusted as before. Otherwise the Authorization bearer must be a user's platform access token that
// the configured UserVerifier accepts, and the worker id, user id and tenant in the query must be that user's
// own, so an IDE connects with the token it already holds and never needs the relay token. With no
// UserVerifier only the relay token works.
func (s *Server) requireHarnessConnectAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenErr := s.authorize(r)
		if tokenErr == nil {
			h(w, r)
			return
		}
		err := s.authorizeUserConnect(r)
		if err == nil {
			h(w, r)
			return
		}
		log.Printf("relay: rejected %s %s from %s (relay token: %v; user token: %v)", r.Method, r.URL.Path, r.RemoteAddr, tokenErr, err)
		if errors.Is(err, errBindingMismatch) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="relay"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

// authorizeUserConnect accepts a harness connect that carries a verified user token and asks for no more than
// that token covers.
func (s *Server) authorizeUserConnect(r *http.Request) error {
	if s.Config.UserVerifier == nil {
		return errors.New("user token verification not configured")
	}
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		return errAuthMissing
	}
	id, err := s.Config.UserVerifier.VerifyUserToken(r.Context(), token)
	if err != nil {
		return fmt.Errorf("user token rejected: %w", err)
	}
	return bindHarnessConnect(id, r.URL.Query())
}

// bindHarnessConnect ties a harness connect to the identity in the user's token. user_id and tenant_id are
// required, because a worker without a tenant would match every tenant's pick; the worker id must be the user's
// own ("workbench:<user_id>"), or empty for the relay to assign one.
func bindHarnessConnect(id UserIdentity, q url.Values) error {
	userID := strings.TrimSpace(id.UserID)
	if userID == "" || len(id.TenantIDs) == 0 {
		return fmt.Errorf("%w: token has no user or tenant", errBindingMismatch)
	}
	if got := strings.TrimSpace(q.Get("user_id")); got != userID {
		return fmt.Errorf("%w: user_id %q is not the token's user", errBindingMismatch, got)
	}
	tenantID := strings.TrimSpace(q.Get("tenant_id"))
	if tenantID == "" || !containsString(id.TenantIDs, tenantID) {
		return fmt.Errorf("%w: tenant_id %q is not one of the token's tenants", errBindingMismatch, tenantID)
	}
	if workerID := strings.TrimSpace(q.Get("worker_id")); workerID != "" && workerID != WorkbenchWorkerPrefix+userID {
		return fmt.Errorf("%w: worker_id %q is not the user's own", errBindingMismatch, workerID)
	}
	return nil
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if strings.TrimSpace(s) == want {
			return true
		}
	}
	return false
}

// downstreamAuthorization is the Authorization header to hand to a connector on MCP forward. A caller that used
// the header to present the relay token has it withheld: that secret must not reach a connector's host.
func (s *Server) downstreamAuthorization(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if token, ok := bearerToken(auth); ok {
		if expected := strings.TrimSpace(s.Config.AuthToken); expected != "" && tokensEqual(token, expected) {
			return ""
		}
	}
	return auth
}

// presentedRelayToken is the token the caller sent: X-Relay-Token if set, else the Authorization bearer.
func presentedRelayToken(r *http.Request) string {
	if t := strings.TrimSpace(r.Header.Get(RelayTokenHeader)); t != "" {
		return t
	}
	token, _ := bearerToken(r.Header.Get("Authorization"))
	return token
}

func bearerToken(authorization string) (string, bool) {
	auth := strings.TrimSpace(authorization)
	if len(auth) < len("bearer ") || !strings.EqualFold(auth[:len("bearer ")], "bearer ") {
		return "", false
	}
	token := strings.TrimSpace(auth[len("bearer "):])
	return token, token != ""
}

// tokensEqual compares in constant time. Hashing first gives ConstantTimeCompare equal-length inputs, so the
// length of the configured token does not leak either.
func tokensEqual(presented, expected string) bool {
	p := sha256.Sum256([]byte(presented))
	e := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(p[:], e[:]) == 1
}
