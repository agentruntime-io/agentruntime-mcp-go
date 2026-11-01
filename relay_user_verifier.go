package agentruntimemcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentruntime-io/agentruntime-mcp-go/relay"
)

// relayUserVerifier lets an IDE connect its harness worker to the relay with the platform access token it already
// holds, so the relay's shared secret never has to ship in a client. It checks the token with the same JWKS
// verifier as MCP OAuth (signature, issuer, audience, expiry) and reads the user and tenants from the
// mcp-platform-claims claims: user_id, and tenant_id / tenant_ids.
type relayUserVerifier struct {
	jwt *jwtVerifier
}

// newRelayUserVerifier returns nil, not a typed-nil interface, when there is no verifier to wrap.
func newRelayUserVerifier(v *jwtVerifier) relay.UserTokenVerifier {
	if v == nil {
		return nil
	}
	return relayUserVerifier{jwt: v}
}

func (a relayUserVerifier) VerifyUserToken(ctx context.Context, token string) (relay.UserIdentity, error) {
	claims, err := a.jwt.Verify(ctx, token)
	if err != nil {
		return relay.UserIdentity{}, err
	}
	userID := firstClaimString(claims, "user_id", "userId")
	if userID == "" {
		return relay.UserIdentity{}, fmt.Errorf("token has no user_id claim")
	}
	tenants := claimStringList(claims, "tenant_ids")
	if one := firstClaimString(claims, "tenant_id", "tenantId", "tid"); one != "" {
		tenants = append(tenants, one)
	}
	if len(tenants) == 0 {
		return relay.UserIdentity{}, fmt.Errorf("token has no tenant claim")
	}
	return relay.UserIdentity{UserID: userID, TenantIDs: tenants}, nil
}

func firstClaimString(claims map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := claimString(claims, key); s != "" {
			return s
		}
	}
	return ""
}

// claimStringList reads a claim that is a JSON array of strings, as Keycloak's multivalued mapper writes
// tenant_ids. Anything else yields nothing.
func claimStringList(claims map[string]any, key string) []string {
	arr, ok := claims[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
