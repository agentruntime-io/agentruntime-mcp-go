package agentruntimemcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// InboundVendorRequest is what control sends to the register, status and unregister routes
// (contract section 8.5). control passes the full public webhook URL and the ingress vendor
// secret, so the connector registers exactly that and never builds a URL of its own.
type InboundVendorRequest struct {
	// RouteToken is the token in the admin path; WebhookURL ends with /{adapter}/webhook/{RouteToken}.
	RouteToken   string
	ConnectionID string
	ServerID     string
	// Scope is the vendor-side choice (update types, repository, ...), validated by the connector.
	Scope map[string]any
	// WebhookURL is the vendor-facing URL to register.
	WebhookURL string
	// VendorSecret is the ingress secret to register at the vendor. Empty on unregister and status.
	VendorSecret string
	// VendorIDs is what the last register returned for this registration (a hook or webhook id).
	// Optional: it lets unregister and re-register find what to remove when the vendor has no list call.
	VendorIDs map[string]any
	// Tenant comes from the X-Tenant-Id / X-Project-Id headers; it is needed to read the connection.
	Tenant InboundVendorContext
}

// InboundRegisterResult is the answer to a register call.
type InboundRegisterResult struct {
	WebhookURL      string
	RegisteredScope map[string]any
	VendorIDs       map[string]any
	// VendorSecret is set only when the vendor generated the signing secret itself (Fathom):
	// control then stores it as the ingress vendor secret so verification can succeed.
	VendorSecret string
}

// InboundStatusResult is the answer to a status call. control reads Registered and
// WebhookURL and marks the ingress stale when Registered is false or WebhookURL is not the
// URL it registered. A vendor with no list call (Fathom, payments-lk) leaves both unset:
// absent means "unknown", never "stale".
type InboundStatusResult struct {
	// Registered is nil when the connector cannot tell.
	Registered *bool
	// WebhookURL is the URL the vendor reports for this route, when known.
	WebhookURL      string
	RegisteredScope map[string]any
	VendorIDs       map[string]any
	Note            string
}

// InboundUnregisterResult is the answer to an unregister call.
type InboundUnregisterResult struct {
	// Removed is false when there was nothing to remove (already gone); that is still success.
	Removed bool
}

// InboundVendorError carries an HTTP status for a failed vendor call. Use InboundBadRequest for
// a problem in the request itself (permanent); any other error becomes a retryable 502.
type InboundVendorError struct {
	Status  int
	Message string
}

func (e *InboundVendorError) Error() string { return e.Message }

// InboundBadRequest reports a permanent problem with the request, for example a scope the
// vendor cannot honour.
func InboundBadRequest(msg string) error {
	return &InboundVendorError{Status: http.StatusBadRequest, Message: msg}
}

// InboundVendorSpec is a connector's side of vendor webhook registration: only the vendor
// calls. MountInboundVendorRoutes owns authentication, parsing, validation and the responses.
type InboundVendorSpec struct {
	Adapter    string
	Register   func(ctx context.Context, env TriggerEnv, req InboundVendorRequest) (InboundRegisterResult, error)
	Status     func(ctx context.Context, env TriggerEnv, req InboundVendorRequest) (InboundStatusResult, error)
	Unregister func(ctx context.Context, env TriggerEnv, req InboundVendorRequest) (InboundUnregisterResult, error)
	// Env defaults to LoadTriggerEnv (read per request).
	Env func() TriggerEnv
}

// MountInboundVendorRoutes registers register-webhook, webhook-status and unregister-webhook
// under /{adapter}/inbound/. Every route requires the router's internal token.
func MountInboundVendorRoutes(mux *http.ServeMux, spec InboundVendorSpec) {
	adapter := normalizeAdapterSlug(spec.Adapter)
	prefix := InboundVendorMountPrefix(adapter)
	mux.HandleFunc(prefix+"register-webhook/", inboundVendorHandler(spec, "register-webhook", http.MethodPost))
	mux.HandleFunc(prefix+"webhook-status/", inboundVendorHandler(spec, "webhook-status", http.MethodGet))
	mux.HandleFunc(prefix+"unregister-webhook/", inboundVendorHandler(spec, "unregister-webhook", http.MethodPost))
}

type inboundVendorBody struct {
	TenantID     string         `json:"tenant_id"`
	ProjectID    string         `json:"project_id"`
	ConnectionID string         `json:"connection_id"`
	ServerID     string         `json:"server_id"`
	Scope        map[string]any `json:"scope"`
	WebhookURL   string         `json:"webhook_url"`
	VendorSecret string         `json:"vendor_secret"`
	VendorIDs    map[string]any `json:"vendor_ids"`
}

func inboundVendorHandler(spec InboundVendorSpec, action, method string) http.HandlerFunc {
	adapter := normalizeAdapterSlug(spec.Adapter)
	mount := InboundVendorMountPrefix(adapter)
	loadEnv := spec.Env
	if loadEnv == nil {
		loadEnv = LoadTriggerEnv
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		env := loadEnv()
		if err := ValidateInboundInternalToken(r, env); err != nil {
			WriteInboundVendorError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		token := InboundRouteToken(r.URL.Path, mount, action)
		if token == "" || strings.Contains(token, "/") {
			WriteInboundVendorError(w, http.StatusBadRequest, "route_token required")
			return
		}

		var in inboundVendorBody
		var err error
		if method == http.MethodPost {
			in, err = decodeInboundVendorBody(r)
		} else {
			in, err = decodeInboundVendorQuery(r)
		}
		if err != nil {
			WriteInboundVendorError(w, http.StatusBadRequest, err.Error())
			return
		}
		req := InboundVendorRequest{
			RouteToken:   token,
			ConnectionID: strings.TrimSpace(in.ConnectionID),
			ServerID:     strings.TrimSpace(in.ServerID),
			Scope:        in.Scope,
			WebhookURL:   strings.TrimSpace(in.WebhookURL),
			VendorSecret: strings.TrimSpace(in.VendorSecret),
			VendorIDs:    in.VendorIDs,
			Tenant:       InboundVendorContextFromRequest(r),
		}
		if req.Scope == nil {
			req.Scope = map[string]any{}
		}
		if req.ConnectionID == "" || req.ServerID == "" {
			WriteInboundVendorError(w, http.StatusBadRequest, "connection_id and server_id required")
			return
		}
		if req.Tenant.TenantID == "" {
			req.Tenant.TenantID = strings.TrimSpace(in.TenantID)
			if req.Tenant.ProjectID == "" {
				req.Tenant.ProjectID = strings.TrimSpace(in.ProjectID)
			}
		}
		if req.Tenant.TenantID == "" {
			WriteInboundVendorError(w, http.StatusBadRequest, "X-Tenant-Id header required")
			return
		}
		// Register needs the URL to register; status and unregister may omit it, but a URL
		// that is given must still be this route's own.
		if req.WebhookURL != "" || action == "register-webhook" {
			if err := checkInboundWebhookURL(adapter, token, req.WebhookURL); err != nil {
				WriteInboundVendorError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if action == "register-webhook" && req.VendorSecret == "" {
			WriteInboundVendorError(w, http.StatusBadRequest, "vendor_secret required")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		switch action {
		case "register-webhook":
			if spec.Register == nil {
				WriteInboundVendorError(w, http.StatusNotImplemented, "register not supported")
				return
			}
			res, err := spec.Register(ctx, env, req)
			if err != nil {
				writeInboundVendorFailure(w, err, req.VendorSecret)
				return
			}
			out := map[string]any{
				"ok":               true,
				"webhook_url":      firstNonEmpty(res.WebhookURL, req.WebhookURL),
				"registered_scope": nonNilMap(res.RegisteredScope),
				"vendor_ids":       nonNilMap(res.VendorIDs),
			}
			if s := strings.TrimSpace(res.VendorSecret); s != "" {
				out["vendor_secret"] = s
			}
			WriteInboundVendorJSON(w, http.StatusOK, out)
		case "webhook-status":
			if spec.Status == nil {
				WriteInboundVendorError(w, http.StatusNotImplemented, "status not supported")
				return
			}
			res, err := spec.Status(ctx, env, req)
			if err != nil {
				writeInboundVendorFailure(w, err, req.VendorSecret)
				return
			}
			out := map[string]any{
				"ok":               true,
				"registered_scope": nonNilMap(res.RegisteredScope),
				"vendor_ids":       nonNilMap(res.VendorIDs),
			}
			if res.Registered != nil {
				out["registered"] = *res.Registered
			}
			if res.WebhookURL != "" {
				out["webhook_url"] = res.WebhookURL
			}
			if res.Note != "" {
				out["note"] = res.Note
			}
			WriteInboundVendorJSON(w, http.StatusOK, out)
		case "unregister-webhook":
			if spec.Unregister == nil {
				WriteInboundVendorError(w, http.StatusNotImplemented, "unregister not supported")
				return
			}
			res, err := spec.Unregister(ctx, env, req)
			if err != nil {
				writeInboundVendorFailure(w, err, req.VendorSecret)
				return
			}
			WriteInboundVendorJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": res.Removed})
		}
	}
}

func decodeInboundVendorBody(r *http.Request) (inboundVendorBody, error) {
	var in inboundVendorBody
	if r.Body == nil {
		return in, errors.New("JSON body required")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return in, errors.New("failed to read body")
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, errors.New("invalid JSON body")
	}
	return in, nil
}

func decodeInboundVendorQuery(r *http.Request) (inboundVendorBody, error) {
	q := r.URL.Query()
	in := inboundVendorBody{
		TenantID:     q.Get("tenant_id"),
		ProjectID:    q.Get("project_id"),
		ConnectionID: q.Get("connection_id"),
		ServerID:     q.Get("server_id"),
		WebhookURL:   q.Get("webhook_url"),
	}
	if raw := strings.TrimSpace(q.Get("scope")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &in.Scope); err != nil {
			return in, errors.New("scope must be a JSON object")
		}
	}
	if raw := strings.TrimSpace(q.Get("vendor_ids")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &in.VendorIDs); err != nil {
			return in, errors.New("vendor_ids must be a JSON object")
		}
	}
	return in, nil
}

// checkInboundWebhookURL makes sure control asks for the connector's own route for this token:
// an http(s) URL without credentials, query or fragment whose path ends with
// /{adapter}/webhook/{token}. A connector never registers a URL pointing anywhere else.
func checkInboundWebhookURL(adapter, token, raw string) error {
	if raw == "" {
		return errors.New("webhook_url required")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New("webhook_url must be an http(s) URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("webhook_url must not carry credentials, a query or a fragment")
	}
	want := ConnectorWebhookPath(adapter, token)
	if !strings.HasSuffix(u.Path, want) {
		return errors.New("webhook_url must end with " + want)
	}
	return nil
}

// writeInboundVendorFailure answers a failed vendor call. The text goes back to control, which
// may store and show it, so the vendor secret control sent is removed from it first.
func writeInboundVendorFailure(w http.ResponseWriter, err error, secrets ...string) {
	redact := func(msg string) string {
		for _, s := range secrets {
			if len(s) >= 8 {
				msg = strings.ReplaceAll(msg, s, "<redacted>")
			}
		}
		return msg
	}
	var ve *InboundVendorError
	if errors.As(err, &ve) {
		WriteInboundVendorError(w, ve.Status, redact(ve.Message))
		return
	}
	WriteInboundVendorError(w, http.StatusBadGateway, redact(err.Error()))
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// BoolPtr returns a pointer to b, for InboundStatusResult.Registered.
func BoolPtr(b bool) *bool { return &b }
