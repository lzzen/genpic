// Package compatctx carries per-request upstream credentials for POST /api/generate
// (browser / SPA supplied base_url + api_key), overriding server environment for
// that single request.
package compatctx

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
)

type ctxKey struct{}

// Override is attached to the request context by POST /api/generate only.
type Override struct {
	BaseURL string
	APIKey  string
	// LogToStderr dumps the full upstream HTTP exchange to os.Stderr (the terminal
	// running genpic), including request JSON and raw response bytes.
	LogToStderr bool
}

// With returns a child context carrying upstream override (nil o is a no-op).
func With(ctx context.Context, o *Override) context.Context {
	if o == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, o)
}

// From returns the override attached with [With], or nil.
func From(ctx context.Context) *Override {
	v, _ := ctx.Value(ctxKey{}).(*Override)
	return v
}

// Resolve returns upstream base URL and API key: per-request override wins,
// otherwise cfgBase/cfgKey. traceStderr is true when full stderr tracing is enabled.
func Resolve(ctx context.Context, cfgBase, cfgKey string) (baseURL, apiKey string, traceStderr bool) {
	baseURL = strings.TrimSpace(cfgBase)
	apiKey = strings.TrimSpace(cfgKey)
	if o := From(ctx); o != nil {
		traceStderr = o.LogToStderr
		if s := strings.TrimSpace(o.BaseURL); s != "" {
			baseURL = s
		}
		if s := strings.TrimSpace(o.APIKey); s != "" {
			apiKey = s
		}
	}
	return baseURL, apiKey, traceStderr
}

// RedactAuthHeader shortens Bearer tokens for safe console display.
func RedactAuthHeader(v string) string {
	v = strings.TrimSpace(v)
	low := strings.ToLower(v)
	if strings.HasPrefix(low, "bearer ") {
		tok := strings.TrimSpace(v[7:])
		if len(tok) <= 10 {
			return "Bearer ****"
		}
		return "Bearer " + tok[:4] + "…" + tok[len(tok)-4:]
	}
	return "***"
}

// LogStderrRoundTrip writes method, URL, sorted headers (Authorization redacted),
// full request body, HTTP status, and full raw response body to os.Stderr.
func LogStderrRoundTrip(providerName, method, url string, headers map[string]string, reqBody []byte, status int, respBody []byte) {
	var b strings.Builder
	fmt.Fprintf(&b, "\n========== genpic POST /api/generate → %s upstream ==========\n", providerName)
	fmt.Fprintf(&b, "%s %s\n", method, url)
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := headers[k]
		if strings.EqualFold(k, "Authorization") {
			v = RedactAuthHeader(v)
		}
		fmt.Fprintf(&b, "%s: %s\n", k, v)
	}
	fmt.Fprintf(&b, "\n-- request body (raw JSON) --\n%s\n", string(reqBody))
	fmt.Fprintf(&b, "\n-- response HTTP status --\n%d\n", status)
	fmt.Fprintf(&b, "\n-- response body (raw) --\n%s\n", string(respBody))
	fmt.Fprintf(&b, "========== end upstream trace ==========\n\n")
	_, _ = os.Stderr.WriteString(b.String())
}
