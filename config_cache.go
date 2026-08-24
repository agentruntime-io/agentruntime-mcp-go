package agentruntimemcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	defaultConfigCacheTTLSec  = 60
	defaultConfigRetryBudgetS = 30
	minConfigCacheTTLSec      = 30
	maxConfigCacheTTLSec      = 120
	defaultRateLimitWaitSec   = 5
	rateLimitWaitLogThreshold = 2 * time.Second
)

type configCacheEntry struct {
	config    ConfigView
	expiresAt time.Time
}

var (
	configCacheMu sync.RWMutex
	configCache   = map[string]configCacheEntry{}
	configFlight  singleflight.Group
)

func fetchControlConfigCached(token string, configSchema map[string]any, runtimeContext map[string]any) (ConfigView, error) {
	if token == "" {
		return fetchControlConfigWithRetry(token, configSchema, runtimeContext)
	}

	key := configCacheKey(token, configSchema, runtimeContext)
	now := time.Now()

	configCacheMu.RLock()
	if ent, ok := configCache[key]; ok && now.Before(ent.expiresAt) && ent.config != nil {
		cfg := cloneConfigView(ent.config)
		configCacheMu.RUnlock()
		return cfg, nil
	}
	configCacheMu.RUnlock()

	v, err, _ := configFlight.Do(key, func() (any, error) {
		now := time.Now()
		configCacheMu.RLock()
		if ent, ok := configCache[key]; ok && now.Before(ent.expiresAt) && ent.config != nil {
			cfg := cloneConfigView(ent.config)
			configCacheMu.RUnlock()
			return cfg, nil
		}
		configCacheMu.RUnlock()

		cfg, err := fetchControlConfigWithRetry(token, configSchema, runtimeContext)
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			putConfigCacheEntry(key, cfg)
		}
		return cloneConfigView(cfg), nil
	})
	if err != nil {
		return nil, err
	}
	if v == nil {
		return ConfigView{}, nil
	}
	out, _ := v.(ConfigView)
	return out, nil
}

func putConfigCacheEntry(key string, cfg ConfigView) {
	ttl := configCacheTTL()
	configCacheMu.Lock()
	defer configCacheMu.Unlock()
	configCache[key] = configCacheEntry{
		config:    cloneConfigView(cfg),
		expiresAt: time.Now().Add(ttl),
	}
}

func configCacheKey(token string, configSchema map[string]any, runtimeContext map[string]any) string {
	parts := []string{
		tokenFingerprint(token),
		runtimeContextInstanceKey(runtimeContext),
		configSchemaHash(configSchema),
	}
	return strings.Join(parts, "|")
}

func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:8])
}

func runtimeContextInstanceKey(runtimeContext map[string]any) string {
	if runtimeContext == nil {
		return ""
	}
	if v, ok := runtimeContext["instance_id"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := runtimeContext["server_id"].(string); ok && strings.TrimSpace(v) != "" {
		return "server:" + strings.TrimSpace(v)
	}
	return ""
}

func configSchemaHash(configSchema map[string]any) string {
	if len(configSchema) == 0 {
		return "empty"
	}
	b, err := json.Marshal(configSchema)
	if err != nil {
		return "marshal-error"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func configCacheTTL() time.Duration {
	sec := defaultConfigCacheTTLSec
	if s := strings.TrimSpace(os.Getenv("MCP_CONFIG_CACHE_TTL_SEC")); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			sec = n
		}
	}
	if sec < minConfigCacheTTLSec {
		sec = minConfigCacheTTLSec
	}
	if sec > maxConfigCacheTTLSec {
		sec = maxConfigCacheTTLSec
	}
	return time.Duration(sec) * time.Second
}

func configRetryBudget() time.Duration {
	sec := defaultConfigRetryBudgetS
	if s := strings.TrimSpace(os.Getenv("MCP_CONFIG_RETRY_BUDGET_SEC")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			sec = n
		}
	}
	return time.Duration(sec) * time.Second
}

func fetchControlConfigWithRetry(token string, configSchema map[string]any, runtimeContext map[string]any) (ConfigView, error) {
	deadline := time.Now().Add(configRetryBudget())

	for attempt := 0; ; attempt++ {
		p, err := fetchControlPayload(token, configSchema, runtimeContext)
		if err == nil {
			if p == nil || p.Config == nil {
				return ConfigView{}, nil
			}
			return p.Config, nil
		}

		var ce *ControlError
		if !errors.As(err, &ce) || ce.Status != 429 {
			return nil, err
		}

		wait := rateLimitWaitDuration(ce, attempt)
		if wait <= 0 {
			return nil, err
		}
		if time.Now().Add(wait).After(deadline) {
			logWarn("mcp control config: rate limit retry budget exhausted after %d attempt(s)", attempt+1)
			return nil, err
		}
		if wait >= rateLimitWaitLogThreshold {
			logWarn("mcp control config: rate limited (capacity), waiting %s before retry (attempt %d)", wait, attempt+1)
		}
		time.Sleep(wait)
	}
}

func rateLimitWaitDuration(ce *ControlError, attempt int) time.Duration {
	if ce == nil {
		return 0
	}
	sec := ce.RetryAfterSec
	if sec <= 0 {
		sec = retryAfterFromControlBody(ce.Body)
	}
	if sec <= 0 {
		sec = defaultRateLimitWaitSec
	}
	if sec < 1 {
		sec = 1
	}
	if sec > 60 {
		sec = 60
	}
	wait := time.Duration(sec) * time.Second
	// Gentle backoff when server omits retry_after (should be rare once Control sends it).
	if attempt > 0 && ce.RetryAfterSec <= 0 && retryAfterFromControlBody(ce.Body) <= 0 {
		wait = time.Duration(min(sec*(1<<attempt), 60)) * time.Second
	}
	return wait
}

func retryAfterFromControlBody(body string) int {
	body = strings.TrimSpace(body)
	if body == "" {
		return 0
	}
	var top struct {
		RetryAfter int `json:"retry_after"`
		Details    struct {
			RetryAfter int `json:"retry_after"`
		} `json:"details"`
	}
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		return 0
	}
	if top.RetryAfter > 0 {
		return top.RetryAfter
	}
	return top.Details.RetryAfter
}

func cloneConfigView(in ConfigView) ConfigView {
	if in == nil {
		return nil
	}
	out := make(ConfigView, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func resetConfigCacheForTest() {
	configCacheMu.Lock()
	configCache = map[string]configCacheEntry{}
	configCacheMu.Unlock()
}
