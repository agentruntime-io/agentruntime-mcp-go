package relay

import (
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strings"
)

// WorkbenchWorkerPrefix is the reserved worker id namespace of a user's Workbench:
// workbench:{user_id}. Only that user's own session may register such an id.
const WorkbenchWorkerPrefix = "workbench:"

// connectIdentity is who a WSS connection belongs to after authentication.
type connectIdentity struct {
	TenantID string
	UserID   string
	// Verified is true when the platform validated the token; false for the static
	// dev bearer and insecure mode, where tenant and user are the client's claim.
	Verified bool
}

func bearerToken(r *http.Request) (string, error) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return "", errors.New("missing authorization")
	}
	if len(auth) < 7 || !strings.EqualFold(auth[:7], "bearer ") {
		return "", errors.New("authorization must be bearer token")
	}
	token := strings.TrimSpace(auth[7:])
	if token == "" {
		return "", errors.New("empty bearer token")
	}
	return token, nil
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// authenticateConnect validates a WSS connect and returns the identity the
// connection is bound to, or the HTTP status and reason to refuse it with.
//
// Order: the static dev bearer (RELAY_AUTH_TOKEN), then the platform verifier
// (PAT or Keycloak JWT). With neither configured the relay refuses everything
// unless RELAY_ALLOW_INSECURE is set. For a verified token the tenant and user
// come from the token; tenant_id / user_id query params, if sent, must agree.
func (s *Server) authenticateConnect(r *http.Request) (connectIdentity, int, error) {
	token, err := bearerToken(r)
	if err != nil {
		return connectIdentity{}, http.StatusUnauthorized, err
	}
	q := r.URL.Query()
	claimTenant := strings.TrimSpace(q.Get("tenant_id"))
	claimUser := strings.TrimSpace(q.Get("user_id"))

	if s.Config.AuthToken != "" && secureEqual(token, s.Config.AuthToken) {
		return connectIdentity{TenantID: claimTenant, UserID: claimUser}, 0, nil
	}
	if s.Config.Verifier != nil {
		id, err := s.Config.Verifier.Verify(r.Context(), token, claimTenant)
		if err != nil {
			if errors.Is(err, ErrVerifierUnavailable) {
				log.Printf("relay: connect refused, cannot verify token: %v", err)
				return connectIdentity{}, http.StatusServiceUnavailable, errors.New("token verification unavailable")
			}
			return connectIdentity{}, http.StatusUnauthorized, errors.New("invalid bearer token")
		}
		if claimTenant != "" && claimTenant != id.TenantID {
			return connectIdentity{}, http.StatusForbidden, errors.New("tenant_id does not match the token")
		}
		if claimUser != "" && !strings.EqualFold(claimUser, id.UserID) {
			return connectIdentity{}, http.StatusForbidden, errors.New("user_id does not match the token")
		}
		return connectIdentity{TenantID: id.TenantID, UserID: id.UserID, Verified: true}, 0, nil
	}
	if s.Config.AuthToken != "" {
		return connectIdentity{}, http.StatusUnauthorized, errors.New("invalid bearer token")
	}
	if s.Config.AllowInsecure {
		return connectIdentity{TenantID: claimTenant, UserID: claimUser}, 0, nil
	}
	return connectIdentity{}, http.StatusServiceUnavailable, errors.New("relay authentication is not configured")
}

// checkWorkerID enforces who may register a harness worker id. The workbench:
// namespace belongs to the token's own user; other ids are tenant-scoped inside
// the hub, so they cannot collide with or reach another tenant's workers.
func checkWorkerID(workerID string, id connectIdentity) error {
	if !id.Verified || workerID == "" {
		return nil
	}
	if len(workerID) >= len(WorkbenchWorkerPrefix) && strings.EqualFold(workerID[:len(WorkbenchWorkerPrefix)], WorkbenchWorkerPrefix) {
		owner := workerID[len(WorkbenchWorkerPrefix):]
		if id.UserID == "" || !strings.EqualFold(owner, id.UserID) {
			return errors.New("worker_id does not belong to the token's user")
		}
	}
	return nil
}

// authorizeInternal checks the X-Internal-Token AgentRuntime sends on dispatch
// and worker-pick requests. It returns a non-zero HTTP status to refuse with.
//
// RELAY_ALLOW_LEGACY_DISPATCH (rollout window) lets a request without a token
// through, with a warning; a request that carries a wrong token is always refused.
// RELAY_ALLOW_INSECURE (local dev) lets everything through.
func (s *Server) authorizeInternal(r *http.Request) (int, string) {
	want := s.Config.InternalToken
	got := strings.TrimSpace(r.Header.Get("X-Internal-Token"))
	if s.Config.AllowInsecure {
		return 0, ""
	}
	if got != "" && want != "" {
		if !secureEqual(got, want) {
			return http.StatusUnauthorized, "invalid internal token"
		}
		return 0, ""
	}
	if got == "" && s.Config.AllowLegacyDispatch {
		log.Printf("relay: accepting %s %s without X-Internal-Token (RELAY_ALLOW_LEGACY_DISPATCH)", r.Method, r.URL.Path)
		return 0, ""
	}
	if want == "" {
		return http.StatusServiceUnavailable, "relay internal token is not configured"
	}
	return http.StatusUnauthorized, "X-Internal-Token required"
}

// logAuthMode states at startup how the relay authenticates, and warns about
// settings that leave it open or that will make its callbacks fail.
func (s *Server) logAuthMode() {
	c := s.Config
	switch {
	case c.Verifier != nil && c.AuthToken != "":
		log.Printf("relay: connect auth = PAT/JWT verifier + static RELAY_AUTH_TOKEN")
	case c.Verifier != nil:
		log.Printf("relay: connect auth = PAT/JWT verifier")
	case c.AuthToken != "":
		log.Printf("relay: connect auth = static RELAY_AUTH_TOKEN only (tenant and user are client-claimed); set RELAY_AUTH_BFF_URL / RELAY_AUTH_WHEELHOUSE_URL to verify user tokens")
	case c.AllowInsecure:
		log.Printf("relay: WARNING connect auth DISABLED (RELAY_ALLOW_INSECURE)")
	default:
		log.Printf("relay: WARNING no connect auth configured; every WSS connect will be refused")
	}
	if c.AllowInsecure {
		log.Printf("relay: WARNING dispatch/pick auth DISABLED (RELAY_ALLOW_INSECURE)")
	} else if c.InternalToken == "" {
		if c.AllowLegacyDispatch {
			log.Printf("relay: WARNING MCP_CONTROL_INTERNAL_TOKEN unset; dispatch/pick are unauthenticated (RELAY_ALLOW_LEGACY_DISPATCH)")
		} else {
			log.Printf("relay: WARNING MCP_CONTROL_INTERNAL_TOKEN unset; dispatch/pick will be refused")
		}
	} else if c.AllowLegacyDispatch {
		log.Printf("relay: RELAY_ALLOW_LEGACY_DISPATCH on: dispatch/pick without X-Internal-Token are accepted until it is removed")
	}
	if c.OrchestratorCallback != nil && c.OrchestratorCallback.Token == "" {
		log.Printf("relay: WARNING MCP_CONTROL_INTERNAL_TOKEN unset; run-complete/worker-online callbacks carry no X-Internal-Token")
	}
}
