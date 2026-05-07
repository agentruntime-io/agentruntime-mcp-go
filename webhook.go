package agentruntimemcp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SignModeB returns the X-Agentruntime-Signature header value for a Mode B delivery.
//
// The signature is HMAC-SHA256 over strconv.FormatInt(unix, 10) + "." + body,
// matching go-wheelhouse/pkg/workflowwebhookdelivery SignBody and BFF
// VerifyAgentruntimeSignature. Pass time.Now().Unix() for unix in production.
// Pass a fixed value only in tests.
func SignModeB(signingSecret string, body []byte, unix int64) string {
	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, _ = mac.Write([]byte(strconv.FormatInt(unix, 10) + "."))
	_, _ = mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", unix, hex.EncodeToString(mac.Sum(nil)))
}

// ModeBRequest holds parameters for one Mode B delivery to the BFF.
type ModeBRequest struct {
	// BFFBaseURL is the platform BFF base URL (e.g. "https://api.example.com").
	BFFBaseURL string

	// SubscriptionID is the inbound webhook subscription UUID (URL path component).
	// Obtain it from POST /v1/inbound-webhooks at subscription create time.
	SubscriptionID string

	// SigningSecret is the per-subscription signing_secret returned once at create.
	// Keep this private — never log or expose it.
	SigningSecret string

	// IdempotencyKey deduplicates deliveries for safe retries.
	// Use the vendor-supplied delivery ID when available (e.g. X-GitHub-Delivery).
	IdempotencyKey string

	// Body is the raw bytes to forward. The HMAC is computed over exactly these bytes,
	// so sign and POST the same slice without re-serializing.
	Body []byte

	// ContentType is forwarded as Content-Type. Defaults to "application/json".
	ContentType string
}

// DeliverModeB forwards body to POST /v1/inbound-webhooks/{SubscriptionID} on the
// BFF with a valid X-Agentruntime-Signature and Idempotency-Key header.
//
// Returns the raw *http.Response (caller must close Body) or an error if the HTTP
// request could not be sent. Non-2xx BFF responses are not treated as errors —
// inspect resp.StatusCode to handle 400/409/429 etc.
func DeliverModeB(ctx context.Context, req ModeBRequest) (*http.Response, error) {
	if req.BFFBaseURL == "" || req.SubscriptionID == "" || req.SigningSecret == "" || req.IdempotencyKey == "" {
		return nil, fmt.Errorf("agentruntime-mcp-go DeliverModeB: BFFBaseURL, SubscriptionID, SigningSecret, and IdempotencyKey are required")
	}

	ct := req.ContentType
	if ct == "" {
		ct = "application/json"
	}

	unix := time.Now().Unix()
	sig := SignModeB(req.SigningSecret, req.Body, unix)
	url := strings.TrimRight(req.BFFBaseURL, "/") + "/v1/inbound-webhooks/" + req.SubscriptionID

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.Body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", ct)
	httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	httpReq.Header.Set("X-Agentruntime-Signature", sig)

	return http.DefaultClient.Do(httpReq)
}
