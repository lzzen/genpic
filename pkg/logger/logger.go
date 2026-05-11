package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// DevMode is true when GENPIC_DEV is set to 1/true/yes/on (case-insensitive).
// Used to print upstream request/response traces and routing diagnostics to stderr.
func DevMode() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GENPIC_DEV")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

type contextKey struct{}

// Init configures the default slog handler based on the LOG_FORMAT environment
// variable. Set LOG_FORMAT=json for production; default is text for local dev.
// Call once from main() before any other logging.
func Init() {
	format := strings.ToLower(os.Getenv("LOG_FORMAT"))
	level := slog.LevelInfo
	if strings.ToLower(os.Getenv("LOG_LEVEL")) == "debug" {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
	if DevMode() {
		slog.Info("GENPIC_DEV is enabled: verbose upstream traces and model-routing diagnostics are on")
	}
}

// WithTraceID returns a child context that carries the given trace ID. The
// logger retrieved via FromContext will include it in every log record.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	l := slog.Default().With("trace_id", traceID)
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext retrieves the context-bound logger, or falls back to the global
// default. Always returns a non-nil *slog.Logger.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(contextKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// Redact replaces known sensitive substrings in a log value so that keys and
// tokens are never written to the log stream in full.
// Use it for any field whose value might contain a credential.
func Redact(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	// Keep prefix and suffix, redact the middle
	return s[:4] + "****" + s[len(s)-4:]
}
