// Package httpclient provides a thin wrapper around [net/http.Client] with:
//   - Per-request timeouts via context.
//   - Structured request/response logging (with credential redaction).
//   - Automatic retry on idempotent upstream errors (429 with Retry-After,
//     502/503/504) with jittered exponential back-off.
//   - Normalised error wrapping using [genpic/pkg/errors].
//
// All provider adapters MUST use this package instead of constructing
// http.Client instances directly, so that retry, timeout, and log behaviour
// is consistent across providers.
package httpclient
