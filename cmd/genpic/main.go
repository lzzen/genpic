// Package main is the full Genpic platform binary.
//
// It registers all three provider adapters (OpenAI, Gemini, Wan), wires up
// the v1 route surface defined in openapi.yaml, and serves the multi-model
// frontend from the embedded web/ directory.
//
// Provider credentials are loaded from environment variables only — never
// from the caller's request body. See config section below for variable names.
//
// Milestone coverage:
//   - M0: /v1/models, /v1/images/generations (sync), /v1/jobs (stub),
//     /health, static frontend, structured logging.
//   - M1+: async job queue, /v1/jobs full implementation (see internal/api/jobs.go TODOs).
package main

import (
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	genpic "genpic"
	"genpic/internal/api"
	"genpic/internal/provider/gemini"
	"genpic/internal/provider/openai"
	"genpic/internal/provider/wan"
	"genpic/pkg/logger"
	"genpic/pkg/provider"
)

func main() {
	logger.Init()
	log := slog.Default()

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

	port := os.Getenv("PORT")
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

// registerProviders reads upstream credentials from environment variables and
// registers each provider into the global registry. If a required variable is
// missing the provider is silently skipped — this allows partial deployments
// where only some providers are configured.
func registerProviders(log *slog.Logger) {
	if baseURL := os.Getenv("OPENAI_BASE_URL"); baseURL != "" {
		provider.Register(openai.New(openai.Config{
			BaseURL: baseURL,
			APIKey:  os.Getenv("OPENAI_API_KEY"),
		}))
		log.Info("registered provider", "name", "openai", "base_url", baseURL)
	} else {
		log.Warn("OPENAI_BASE_URL not set; OpenAI provider disabled")
	}

	if baseURL := os.Getenv("GEMINI_BASE_URL"); baseURL != "" {
		provider.Register(gemini.New(gemini.Config{
			BaseURL: baseURL,
			APIKey:  os.Getenv("GEMINI_API_KEY"),
		}))
		log.Info("registered provider", "name", "gemini", "base_url", baseURL)
	} else {
		log.Warn("GEMINI_BASE_URL not set; Gemini provider disabled")
	}

	if baseURL := os.Getenv("WAN_BASE_URL"); baseURL != "" {
		provider.Register(wan.New(wan.Config{
			BaseURL: baseURL,
			APIKey:  os.Getenv("WAN_API_KEY"),
		}))
		log.Info("registered provider", "name", "wan", "base_url", baseURL)
	} else {
		log.Warn("WAN_BASE_URL not set; Wan provider disabled")
	}
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
