package agentruntimemcp

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type jwtVerifier struct {
	issuer   string
	audience string
	jwksURL  string

	httpClient *http.Client
	mu         sync.Mutex
	keys       map[string]any
	cachedAt   time.Time
	ttl        time.Duration
}

type jwksPayload struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func newJWTVerifier(issuer, audience, jwksURL string) *jwtVerifier {
	issuer = strings.TrimSpace(issuer)
	audience = strings.TrimSpace(audience)
	jwksURL = strings.TrimSpace(jwksURL)
	if issuer == "" || audience == "" || jwksURL == "" {
		return nil
	}
	return &jwtVerifier{
		issuer:     issuer,
		audience:   audience,
		jwksURL:    jwksURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		keys:       map[string]any{},
		ttl:        5 * time.Minute,
	}
}

func (v *jwtVerifier) Verify(ctx context.Context, tokenStr string) (jwt.MapClaims, error) {
	if v == nil {
		return nil, fmt.Errorf("jwt verifier not configured")
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
	)
	claims := jwt.MapClaims{}
	token, err := parser.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		keys, err := v.getKeys(ctx)
		if err != nil {
			return nil, err
		}
		if key, ok := keys[kid]; ok {
			return key, nil
		}
		keys, err = v.refreshKeys(ctx)
		if err != nil {
			return nil, err
		}
		key, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("jwt key id not found")
		}
		return key, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid jwt token")
	}
	return claims, nil
}

func (v *jwtVerifier) getKeys(ctx context.Context) (map[string]any, error) {
	v.mu.Lock()
	if len(v.keys) > 0 && time.Since(v.cachedAt) < v.ttl {
		keys := v.keys
		v.mu.Unlock()
		return keys, nil
	}
	v.mu.Unlock()
	return v.refreshKeys(ctx)
}

func (v *jwtVerifier) refreshKeys(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jwks fetch failed: %s", strings.TrimSpace(string(body)))
	}

	var payload jwksPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	keys := map[string]any{}
	for _, key := range payload.Keys {
		if strings.ToUpper(key.Kty) != "RSA" {
			continue
		}
		pubKey, err := rsaPublicKeyFromJWKS(key.N, key.E)
		if err != nil {
			continue
		}
		keys[key.Kid] = pubKey
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("jwks contains no usable rsa keys")
	}

	v.mu.Lock()
	v.keys = keys
	v.cachedAt = time.Now()
	v.mu.Unlock()
	return keys, nil
}

func rsaPublicKeyFromJWKS(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}
	if n.Sign() <= 0 || e <= 0 {
		return nil, fmt.Errorf("invalid jwk rsa params")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}

func looksLikeJWT(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	return strings.Count(token, ".") == 2
}

func claimString(claims map[string]any, key string) string {
	v, ok := claims[key]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return strings.TrimSpace(t.String())
	case float64:
		return fmt.Sprintf("%v", t)
	default:
		return ""
	}
}

func claimScopes(claims map[string]any) []string {
	if s := claimString(claims, "scope"); s != "" {
		return splitScopeList(s)
	}
	if arr, ok := claims["scopes"].([]any); ok {
		out := make([]string, 0, len(arr))
		for _, v := range arr {
			if vv, ok := v.(string); ok && strings.TrimSpace(vv) != "" {
				out = append(out, strings.TrimSpace(vv))
			}
		}
		return out
	}
	return nil
}

func scopesSatisfied(have []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(have))
	for _, s := range have {
		set[s] = struct{}{}
	}
	for _, req := range required {
		if _, ok := set[req]; !ok {
			return false
		}
	}
	return true
}
