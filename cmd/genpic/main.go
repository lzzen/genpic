// Package main is the full Genpic platform binary (cmd/genpic).
//
// Architecture (§2.1 Mode A): the server holds all upstream provider credentials;
// callers authenticate with a platform-issued API key (Bearer token). Provider
// credentials are never returned to callers.
//
// Route surface (matches openapi.yaml):
//   - GET  /v1/models               — list available models
//   - POST /v1/images/generations   — enqueue generation job (202 Accepted + job)
//   - GET  /v1/jobs/{job_id}        — poll job status and images
//   - GET  /v1/jobs                 — list jobs (newest first, cursor pagination)
//   - GET  /health                  — liveness check
//   - GET  /api/public-config       — non-secret defaults for the SPA
//   - POST /api/generate            — SPA compat: sync generation with per-request credentials
//
// Auth:
//   When platform_keys are configured in config.yaml, all /v1/* routes require
//   a matching Bearer token. When the list is empty (dev/single-user mode) auth
//   is skipped with a startup warning. /api/* and /health are always unprotected.
//
// Rate limiting:
//   An in-process sliding-window limiter is applied after auth on /v1/images/generations.
//   Global and per-key RPM caps are read from config.yaml rate_limit section.
//
// Jobs:
//   POST /v1/images/generations creates an in-memory job (M1); the generation
//   runs in a goroutine. For production replace the Memory store with a DB-backed
//   implementation satisfying the jobstore.Store interface.
package main

import (
	"context"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	genpic "genpic"
	"genpic/internal/api"
	"genpic/internal/jobstore"
	"genpic/internal/provider/gemini"
	"genpic/internal/provider/openai"
	"genpic/internal/provider/wan"
	"genpic/pkg/auth"
	"genpic/pkg/logger"
	"genpic/pkg/mvpconfig"
	"genpic/pkg/provider"
	"genpic/pkg/ratelimit"
)

func main() {
	logger.Init()
	log := slog.Default()

	configPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := mvpconfig.Read(*configPath)
	if err != nil {
		slog.Error("genpic: config", "error", err)
		os.Exit(1)
	}
	if !cfg.Found {
		slog.Info("genpic: config file not found; using defaults and env vars", "path", *configPath)
	}

	api.SetModelIDMap(cfg.ModelIDMap)
	registerProviders(log, cfg)

	// ── Job store (M1 in-memory) ──────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := jobstore.NewMemory(ctx, 2*time.Hour)
	api.SetJobStore(store)
	log.Info("job store initialised", "type", "in-memory", "ttl", "2h")

	// ── Auth validator ────────────────────────────────────────────────────
	configKeys := make([]auth.ConfigKey, 0, len(cfg.PlatformKeys))
	for _, k := range cfg.PlatformKeys {
		configKeys = append(configKeys, auth.ConfigKey{
			Name:     k.Name,
			RawKey:   k.Key,
			Scopes:   k.Scopes,
			RPMLimit: k.RPMLimit,
		})
	}
	validator := auth.NewConfigValidator(configKeys)
	if validator.Empty() {
		slog.Warn("genpic: no platform_keys configured — /v1/* routes are OPEN (dev mode); add platform_keys to config.yaml for production")
	} else {
		slog.Info("genpic: auth enabled", "key_count", len(configKeys))
	}

	// ── Rate limiter ──────────────────────────────────────────────────────
	var globalLimiter ratelimit.Limiter = ratelimit.Unlimited{}
	if cfg.GlobalRPM > 0 {
		globalLimiter = ratelimit.NewInMemory(cfg.GlobalRPM, time.Minute)
		slog.Info("genpic: global rate limit", "rpm", cfg.GlobalRPM)
	}

	defaultKeyRPM := cfg.DefaultKeyRPM
	if defaultKeyRPM <= 0 {
		defaultKeyRPM = 0 // 0 = unlimited per-key (use only global)
	}

	// ── Static frontend ───────────────────────────────────────────────────
	webRoot, err := fs.Sub(genpic.WebStatic, "web")
	if err != nil {
		slog.Error("failed to open embedded web root", "error", err)
		os.Exit(1)
	}

	defaultBaseURL := cfg.DefaultBaseURL

	// ── Routes ───────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Unprotected routes (health, SPA compat, public config)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/public-config", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]string{"default_base_url": defaultBaseURL})
	})
	mux.HandleFunc("POST /api/generate", api.HandleCompatGenerate)

	// /v1/* routes — wrapped with optional auth + rate limiting.
	v1Mux := http.NewServeMux()
	v1Mux.HandleFunc("GET /v1/models", api.HandleListModels)
	v1Mux.HandleFunc("POST /v1/images/generations", rateMiddleware(globalLimiter, defaultKeyRPM, api.HandleImageGeneration))
	v1Mux.HandleFunc("GET /v1/jobs/{job_id}", api.HandleGetJob)
	v1Mux.HandleFunc("GET /v1/jobs", api.HandleListJobs)

	var v1Handler http.Handler = v1Mux
	if !validator.Empty() {
		v1Handler = auth.Middleware(validator, v1Mux)
	}
	mux.Handle("/v1/", v1Handler)

	// Static frontend — catch-all after specific routes.
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		http.FileServer(http.FS(webRoot)).ServeHTTP(w, r)
	}))

	// ── Listen ────────────────────────────────────────────────────────────
	port := strings.TrimSpace(cfg.ServerPort)
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		port = p
	}
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	slog.Info("genpic platform starting", "addr", addr, "auth", !validator.Empty())

	srv := &http.Server{
		Addr:         addr,
		Handler:      withLogging(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

// registerProviders wires all provider adapters using server-side credentials
// from config.yaml (with env var fallback). The MVP SPA (/api/generate) may
// override these per-request via compatctx; /v1/images/generations uses only
// the server-side credentials.
func registerProviders(log *slog.Logger, cfg mvpconfig.Config) {
	provider.Register(openai.New(openai.Config{
		BaseURL: cfg.OpenAI.BaseURL,
		APIKey:  cfg.OpenAI.APIKey,
	}))
	log.Info("registered provider", "name", "openai",
		"base_url_set", cfg.OpenAI.BaseURL != "",
		"api_key_set", cfg.OpenAI.APIKey != "")

	provider.Register(gemini.New(gemini.Config{
		BaseURL: cfg.Gemini.BaseURL,
		APIKey:  cfg.Gemini.APIKey,
	}))
	log.Info("registered provider", "name", "gemini",
		"base_url_set", cfg.Gemini.BaseURL != "",
		"api_key_set", cfg.Gemini.APIKey != "")

	provider.Register(wan.New(wan.Config{
		BaseURL: cfg.Wan.BaseURL,
		APIKey:  cfg.Wan.APIKey,
	}))
	log.Info("registered provider", "name", "wan",
		"base_url_set", cfg.Wan.BaseURL != "",
		"api_key_set", cfg.Wan.APIKey != "")
}

// rateMiddleware applies the global limiter and an optional per-key limiter
// (keyed on Identity.KeyID) to a handler. If either limiter rejects the
// request, a 429 is returned with a Retry-After: 60 header.
func rateMiddleware(global ratelimit.Limiter, defaultKeyRPM int, next http.HandlerFunc) http.HandlerFunc {
	// Per-key limiters are created lazily in a sync map.
	var keyLimiters sync.Map

	return func(w http.ResponseWriter, r *http.Request) {
		// Global limit.
		if !global.Allow("global") {
			w.Header().Set("Retry-After", "60")
			api.JSON(w, http.StatusTooManyRequests, map[string]any{
				"error": map[string]string{
					"type":    "rate_limit_error",
					"message": "global rate limit exceeded; retry after 60 seconds",
				},
			})
			return
		}

		// Per-key limit (skip if unlimited).
		if id, ok := auth.IdentityFromContext(r.Context()); ok && defaultKeyRPM > 0 {
			rpm := id.RPMLimit
			if rpm <= 0 {
				rpm = defaultKeyRPM
			}
			raw, _ := keyLimiters.LoadOrStore(id.KeyID, ratelimit.NewInMemory(rpm, time.Minute))
			limiter := raw.(*ratelimit.InMemory)
			if !limiter.Allow(id.KeyID) {
				w.Header().Set("Retry-After", "60")
				api.JSON(w, http.StatusTooManyRequests, map[string]any{
					"error": map[string]string{
						"type":    "rate_limit_error",
						"message": "per-key rate limit exceeded; retry after 60 seconds",
					},
				})
				return
			}
		}
		next(w, r)
	}
}

// ── Logging ───────────────────────────────────────────────────────────────────

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		srw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(srw, r)
		slog.Info("http", "method", r.Method, "path", r.URL.Path,
			"status", srw.status, "latency_ms", time.Since(start).Milliseconds())
	})
}

var _ = logger.FromContext // ensure logger package side-effects are applied
