package agentruntimemcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultInboundWebhookMaxBody = 1 << 20 // 1 MiB

// ConstantTimeEqual compares two secrets without leaking where they differ.
// Two empty strings are equal: callers must refuse an empty secret before comparing.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ConnectorWebhookPath returns the public path of a connector webhook route:
// /{adapter}/webhook/{routeToken}.
func ConnectorWebhookPath(adapter, routeToken string) string {
	adapter = normalizeAdapterSlug(adapter)
	token := strings.Trim(strings.TrimSpace(routeToken), "/")
	if adapter == "" || token == "" {
		return ""
	}
	return fmt.Sprintf("/%s/webhook/%s", adapter, token)
}

// InboundWebhookSpec describes one connector's vendor webhook. ServeInboundWebhook turns it
// into the HTTP handler, so every connector gets the same route-token lookup, body cap,
// constant-time secret handling, idempotency key, hand-over to control and reply. Only what
// is vendor specific is written per connector: Verify, EventType and EventKey.
type InboundWebhookSpec struct {
	// Adapter is the registry name (telegram, github, ...). The route is /{Adapter}/webhook/{route_token}.
	Adapter string
	// MaxBodyBytes caps the body; 0 means 1 MiB. It must match delivery.max_body_bytes in the manifest.
	MaxBodyBytes int64
	// Verify authenticates the vendor call against the secret and returns nil only when it is
	// genuine. It is never called with an empty secret.
	Verify func(r *http.Request, body []byte, vendorSecret string) error
	// EventType returns the manifest event id of this delivery, or "" when the body is not a
	// recognised event (the delivery is then refused with 400 and nothing is posted).
	EventType func(r *http.Request, body []byte) string
	// EventKey returns the vendor's own delivery id, prefixed with the adapter name, so a
	// vendor retry maps to the same event and the platform drops the duplicate. When it
	// returns "" the key falls back to a digest of the body.
	EventKey func(r *http.Request, body []byte) string
	// Secret overrides which secret Verify receives. The default is the ingress vendor_secret.
	// payments-lk uses it to read the merchant key from the connection: its calls are signed
	// with that key, not with a secret the platform chose.
	Secret func(ctx context.Context, ingress TriggerIngress) (string, error)
	// Env defaults to LoadTriggerEnv (read per request, so tests can point it at a fake).
	Env func() TriggerEnv
}

// MountInboundWebhook registers the handler for /{adapter}/webhook and /{adapter}/webhook/.
func MountInboundWebhook(mux *http.ServeMux, spec InboundWebhookSpec) {
	h := ServeInboundWebhook(spec)
	prefix := "/" + normalizeAdapterSlug(spec.Adapter) + "/webhook"
	mux.HandleFunc(prefix, h)
	mux.HandleFunc(prefix+"/", h)
}

// ServeInboundWebhook returns the handler for a connector's vendor webhook route.
//
// Order of work, and what the vendor sees:
//  1. only POST (405); the route token is the one path segment after /{adapter}/webhook/ (404)
//  2. body read under the cap (413)
//  3. the route token is resolved by control (404 unknown or off, 503 control unavailable)
//  4. an empty secret is refused (401), then the connector's Verify runs (401)
//  5. the event is posted to control, which answers after committing it (502 when control
//     fails, so the vendor retries; 404/400 from control are passed on)
//  6. 200 {"ok":true}, also for a duplicate and for an event no trigger wants
func ServeInboundWebhook(spec InboundWebhookSpec) http.HandlerFunc {
	adapter := normalizeAdapterSlug(spec.Adapter)
	prefix := "/" + adapter + "/webhook/"
	maxBody := spec.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultInboundWebhookMaxBody
	}
	loadEnv := spec.Env
	if loadEnv == nil {
		loadEnv = LoadTriggerEnv
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		token := ""
		if strings.HasPrefix(r.URL.Path, prefix) {
			token = strings.TrimSpace(strings.TrimPrefix(r.URL.Path, prefix))
		}
		if token == "" || strings.Contains(token, "/") {
			http.Error(w, "unknown webhook route", http.StatusNotFound)
			return
		}

		if r.ContentLength > maxBody {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		if int64(len(body)) > maxBody {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		env := loadEnv()

		ingress, err := ResolveTriggerIngress(ctx, env, token)
		if err != nil {
			logError("%s webhook: ingress lookup failed: %v", adapter, err)
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		if ingress == nil {
			http.Error(w, "unknown webhook route", http.StatusNotFound)
			return
		}

		secret := strings.TrimSpace(ingress.VendorSecret)
		if spec.Secret != nil {
			s, err := spec.Secret(ctx, *ingress)
			if err != nil {
				logError("%s webhook: credential lookup failed (connection %s): %v", adapter, ingress.ConnectionID, err)
				http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			secret = strings.TrimSpace(s)
		}
		if secret == "" {
			// Fail closed: an HMAC with an empty key is a valid HMAC, so a route without a
			// secret would accept anyone who guesses the URL.
			logWarn("%s webhook refused: no secret to verify against (connection %s)", adapter, ingress.ConnectionID)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if spec.Verify == nil {
			logError("%s webhook: no Verify configured", adapter)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := spec.Verify(r, body, secret); err != nil {
			logWarn("%s webhook refused: %v (connection %s)", adapter, err, ingress.ConnectionID)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		eventType := ""
		if spec.EventType != nil {
			eventType = strings.TrimSpace(spec.EventType(r, body))
		}
		if eventType == "" {
			http.Error(w, "unrecognised event", http.StatusBadRequest)
			return
		}
		eventKey := ""
		if spec.EventKey != nil {
			eventKey = strings.TrimSpace(spec.EventKey(r, body))
		}
		if eventKey == "" {
			eventKey = adapter + "-sha256-" + BodyDigest(body)
		}

		ev := NewTriggerEvent(token, eventType, eventKey, r.Header.Get("Content-Type"), body)
		if _, err := PostTriggerEvent(ctx, env, ev); err != nil {
			var pe *TriggerPostError
			if errors.As(err, &pe) {
				switch {
				case pe.Status == http.StatusNotFound || pe.Status == http.StatusForbidden:
					http.Error(w, "unknown webhook route", http.StatusNotFound)
					return
				case pe.Status >= 400 && pe.Status < 500:
					logWarn("%s webhook: control refused the event: %v", adapter, err)
					http.Error(w, "event refused", http.StatusBadRequest)
					return
				}
			}
			logError("%s webhook: handing the event to control failed: %v", adapter, err)
			http.Error(w, "forward failed", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

// BodyDigest is the first 32 hex characters of sha256(body): stable for a re-send of the same
// body, distinct for different bodies. Connectors use it for the event key when the vendor
// sends no delivery id.
func BodyDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:32]
}
