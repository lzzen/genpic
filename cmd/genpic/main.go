// Package main is the full Genpic platform binary.
//
// It registers all three provider adapters (OpenAI, Gemini, Wan), wires up
// the v1 route surface defined in openapi.yaml, and serves the multi-model
// frontend from the embedded web/ directory.
//
// Provider credentials for POST /v1/images/generations may be loaded from
// environment variables. POST /api/generate uses base_url + api_key from the
// JSON body per request (printed to stderr for debugging).
//
// Optional -config (default config.yaml): mvp_lite.default_base_url is exposed
// as GET /api/public-config for the embedded web UI; mvp_lite.port is used
// unless overridden by PORT (same as mvplite).
//
// Milestone coverage:
//   - M0: /v1/models, /v1/images/generations (sync), /v1/jobs (stub),
//     /health, static frontend, structured logging.
//   - M1+: async job queue, /v1/jobs full implementation (see internal/api/jobs.go TODOs).
package main

import (
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	genpic "genpic"
	"genpic/internal/api"
	"genpic/internal/provider/gemini"
	"genpic/internal/provider/openai"
	"genpic/internal/provider/wan"
	"genpic/pkg/logger"
	"genpic/pkg/mvpconfig"
	"genpic/pkg/provider"
)

func main() {
	logger.Init()
	log := slog.Default()

	configPath := flag.String("config", "config.yaml", "path to config.yaml (mvp_lite.default_base_url, optional mvp_lite.port)")
	flag.Parse()

	filePort, defaultBaseURL, cfgFound, err := mvpconfig.Read(*configPath)
	if err != nil {
		slog.Error("genpic: config", "error", err)
		os.Exit(1)
	}
	if !cfgFound {
		slog.Info("genpic: config file not found; default Base URL empty until you add mvp_lite.default_base_url", "path", *configPath)
	}

	registerProviders(log)

	webRoot, err := fs.Sub(genpic.WebStatic, "web")
	if err != nil {
		slog.Error("failed to open embedded web root", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()

	// v1 API surface (matches openapi.yaml)
	mux.HandleFunc("GET /v1/models", api.HandleListModels)
	mux.HandleFunc("POST /v1/images/generations", api.HandleImageGeneration)
	// Same generation pipeline as /v1/images/generations; body may include
	// base_url/api_key for SPA compatibility (ignored here; keys are env-only).
	mux.HandleFunc("POST /api/generate", api.HandleCompatGenerate)
	mux.HandleFunc("GET /api/public-config", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]string{"default_base_url": defaultBaseURL})
	})
	mux.HandleFunc("GET /v1/jobs/{job_id}", api.HandleGetJob)
	mux.HandleFunc("GET /v1/jobs", api.HandleListJobs)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Static frontend — catch-all, registered after specific routes.
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		http.FileServer(http.FS(webRoot)).ServeHTTP(w, r)
	}))

	port := strings.TrimSpace(filePort)
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		port = p
	}
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	slog.Info("genpic platform starting", "addr", addr)

	srv := &http.Server{
		Addr:         addr,
		Handler:      withLogging(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 300 * time.Second, // long timeout for slow image generation
		IdleTimeout:  120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

// registerProviders wires all provider adapters. Environment variables supply
// defaults for POST /v1/images/generations; POST /api/generate uses JSON
// base_url + api_key per request instead (env may be empty).
func registerProviders(log *slog.Logger) {
	provider.Register(openai.New(openai.Config{
		BaseURL: strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")),
		APIKey:  strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
	}))
	log.Info("registered provider", "name", "openai", "base_url_set", os.Getenv("OPENAI_BASE_URL") != "")

	provider.Register(gemini.New(gemini.Config{
		BaseURL: strings.TrimSpace(os.Getenv("GEMINI_BASE_URL")),
		APIKey:  strings.TrimSpace(os.Getenv("GEMINI_API_KEY")),
	}))
	log.Info("registered provider", "name", "gemini", "base_url_set", os.Getenv("GEMINI_BASE_URL") != "")

	provider.Register(wan.New(wan.Config{
		BaseURL: strings.TrimSpace(os.Getenv("WAN_BASE_URL")),
		APIKey:  strings.TrimSpace(os.Getenv("WAN_API_KEY")),
	}))
	log.Info("registered provider", "name", "wan", "base_url_set", os.Getenv("WAN_BASE_URL") != "")
}

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
		slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", srw.status, "latency_ms", time.Since(start).Milliseconds())
	})
}

// Ensure logger package is used (imported for side effects of Init).
var _ = logger.FromContext
