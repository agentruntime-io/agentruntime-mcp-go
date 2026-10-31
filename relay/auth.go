package relay

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strings"
)

// RelayTokenHeader carries the relay token on requests whose Authorization header belongs to someone else, such
// as MCP forward, where Authorization is the downstream connector's credential. Authorization: Bearer <token>
// is accepted everywhere too.
const RelayTokenHeader = "X-Relay-Token"

// Reasons authorize rejects a request. They go to the log only; the HTTP body is always the same generic text.
var (
	errAuthNotConfigured = errors.New("relay auth token not configured")
	errAuthMissing       = errors.New("no relay token presented")
	errAuthInvalid       = errors.New("relay token mismatch")
)

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
