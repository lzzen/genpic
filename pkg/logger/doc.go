// Package logger provides a structured logging wrapper around [log/slog].
//
// All log entries include a trace_id when one is set on the context, and
// automatically redact known sensitive fields (api_key, authorization).
// Use [FromContext] in handlers to get the per-request logger carrying the
// request's trace_id.
package logger
