// Package main is the full Genpic platform binary (cmd/genpic).
//
// Architecture: the server holds upstream provider credentials for adapters;
// the embedded SPA supplies base_url + api_key per request on POST /api/generate.
//
// Route surface (matches openapi.yaml):
//   - GET  /models                  — list available models
//   - GET  /jobs/{job_id}           — poll job status and images
//   - GET  /jobs                    — list jobs (newest first, cursor pagination)
//   - GET  /api/artifacts/{job_id}/{name} — generated image file (PNG/JPEG/WebP/GIF)
//   - GET  /health                  — liveness check
//   - GET  /api/public-config       — non-secret defaults for the SPA
//   - GET  /api/ui/catalog          — vendor + model list for the embedded SPA (DB-backed later)
//   - GET  /integrate               — integration wizard (HTML; protect in production)
//   - GET  /admin                   — operator dashboard (HTML; protect in production)
//   - GET  /admin/jobs              — list all jobs (offset/limit; protect in production)
//   - GET  /admin/stats             — aggregate job stats (protect in production)
//   - POST /api/generate            — enqueue generation (202 + job); poll GET /jobs/{id}
//   - GET  /api/community/feed      — public jobs feed (optional auth for prompt/params rules)
//   - GET|POST /api/templates       — list presets (?model=) / save from succeeded job
//   - DELETE /api/templates/{id}    — delete own template (admins may delete public presets)
//   - POST /api/admin/users/reset-password — admin: set a user's password (invalidates their sessions)
//   - GET  /api/admin/templates     — admin: list all templates (pagination, optional visibility filter)
//   - PUT  /api/admin/templates/{id}/visibility — admin: set template public or private
//
// Rate limiting:
//
//	An optional global RPM cap (rate_limit.global_rpm) applies to POST /api/generate.
//
// Jobs: MySQL or in-memory store; successful b64 responses are written under
// server.artifacts_dir (default data/genpic-artifacts) and exposed as /api/artifacts/....
package main

import (
	"context"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	genpic "genpic"
	"genpic/internal/api"
	"genpic/internal/auth"
	"genpic/internal/dbmigrate"
	"genpic/internal/geminiconfig"
	"genpic/internal/jobstore"
	"genpic/internal/provider/gemini"
	"genpic/internal/provider/openai"
	"genpic/internal/provider/wan"
	"genpic/internal/storage"
	"genpic/internal/templatestore"
	"genpic/internal/userstorage"
	"genpic/pkg/logger"
	"genpic/pkg/mvpconfig"
	"genpic/pkg/objstore"
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
	geminiconfig.Install(cfg.GeminiImageSize4KModelMap)
	registerProviders(log, cfg)

	// ── Job store ────────────────────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var store jobstore.Store
	if cfg.Database.DSN != "" {
		ms, err := jobstore.NewMySQL(cfg.Database.DSN, cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns)
		if err != nil {
			slog.Error("job store: mysql init failed", "error", err)
			os.Exit(1)
		}
		store = ms
		log.Info("job store initialised", "type", "mysql")

		if err := dbmigrate.Up(ms.DB()); err != nil {
			slog.Error("database migrations failed", "error", err)
			os.Exit(1)
		}
		log.Info("database migrations applied")
		ts, err := templatestore.NewMySQL(ms.DB())
		if err != nil {
			slog.Error("template store: init failed", "error", err)
			os.Exit(1)
		}
		api.SetTemplateStore(ts)
	} else {
		store = jobstore.NewMemory(ctx, 2*time.Hour)
		log.Info("job store initialised", "type", "in-memory", "ttl", "2h",
			"note", "set database.dsn (or DB_DSN) to enable persistent MySQL storage")
	}
	api.SetJobStore(store)
	api.SetAdminEmails(cfg.Auth.AdminEmails)

	// ── Auth store (shares the MySQL DB, no-op when in-memory) ───────────────
	if ms, ok := store.(*jobstore.MySQL); ok {
		db := ms.DB()
		authStore, err := auth.NewStore(db, cfg.Auth.SessionTTL)
		if err != nil {
			slog.Error("auth store: init failed", "error", err)
			os.Exit(1)
		}
		api.SetAuthStore(authStore)
		log.Info("auth store initialised")

		// Periodically remove expired sessions.
		go func() {
			t := time.NewTicker(6 * time.Hour)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					authStore.DeleteExpiredSessions()
				}
			}
		}()
	} else {
		log.Info("auth store disabled", "reason", "no MySQL DSN; user accounts require a database")
	}

	// ── Object storage + per-user quota (requires MySQL) ─────────────────────
	if ms, ok := store.(*jobstore.MySQL); ok {
		api.SetQuotaDB(ms.DB())
		if err := userstorage.MigrateUserQuotaColumns(ms.DB()); err != nil {
			slog.Error("user storage quota columns migrate failed", "error", err)
			os.Exit(1)
		}
	}
	osCfg := cfg.ObjectStorage
	if osCfg.Enabled && osCfg.Bucket != "" && osCfg.AccessKey != "" && osCfg.SecretKey != "" {
		api.SetOSSMaterializeConfig(osCfg)
		s3s, err := storage.NewS3Compatible(ctx, storage.S3Config{
			Region:        osCfg.Region,
			Endpoint:      osCfg.Endpoint,
			Bucket:        osCfg.Bucket,
			AccessKey:     osCfg.AccessKey,
			SecretKey:     osCfg.SecretKey,
			UsePathStyle:  osCfg.UsePathStyle,
			PublicBaseURL: osCfg.PublicBaseURL,
			KeyPrefix:     osCfg.KeyPrefix,
		})
		if err != nil {
			slog.Error("object storage init failed", "error", err)
			os.Exit(1)
		}
		api.SetObjectStore(s3s)
		api.SetObjectURLResolver(func(cx context.Context, logicalKey string) (string, error) {
			if pub := strings.TrimSpace(osCfg.PublicBaseURL); pub != "" {
				return strings.TrimRight(pub, "/") + "/" + strings.TrimLeft(logicalKey, "/"), nil
			}
			return s3s.SignedURL(cx, objstore.SignedURLInput{Key: logicalKey, ExpiresIn: 7 * 24 * time.Hour})
		})
		slog.Info("object storage initialised", "bucket", osCfg.Bucket, "artifact_mode", osCfg.ArtifactMode)
	} else if osCfg.Enabled {
		slog.Warn("object_storage.enabled but bucket, access_key, or secret_key missing — OSS uploads disabled")
	}

	// ── Artifact files (b64 → disk, GET /api/artifacts/...) ─────────────────
	artifactsDir := resolveGenpicArtifactsDir(cfg)
	api.SetArtifactsRoot(artifactsDir)
	if artifactsDir != "" {
		if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
			slog.Error("artifacts: mkdir", "error", err, "dir", artifactsDir)
			os.Exit(1)
		}
		if abs, err := filepath.Abs(artifactsDir); err == nil {
			log.Info("artifacts enabled", "dir", abs)
		} else {
			log.Info("artifacts enabled", "dir", artifactsDir)
		}
	} else {
		log.Info("artifacts disabled", "reason", "server.artifacts_dir or GENPIC_ARTIFACTS_DIR set to \"-\"")
	}

	// ── Rate limiter ──────────────────────────────────────────────────────
	var globalLimiter ratelimit.Limiter = ratelimit.Unlimited{}
	if cfg.GlobalRPM > 0 {
		globalLimiter = ratelimit.NewInMemory(cfg.GlobalRPM, time.Minute)
		slog.Info("genpic: global rate limit", "rpm", cfg.GlobalRPM)
	}

	// ── Static frontend ───────────────────────────────────────────────────
	webRoot, err := fs.Sub(genpic.WebStatic, "web")
	if err != nil {
		slog.Error("failed to open embedded web root", "error", err)
		os.Exit(1)
	}

	defaultBaseURL := cfg.DefaultBaseURL

	// ── Auth middleware ───────────────────────────────────────────────────────
	// authStore may be nil (no DB); OptionalAuth handles nil gracefully.
	var authStoreForMW *auth.Store
	if as, ok := api.GetAuthStore(); ok {
		authStoreForMW = as
	}
	optAuth := auth.OptionalAuth(authStoreForMW)

	// ── Routes ───────────────────────────────────────────────────────────
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/public-config", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]string{"default_base_url": defaultBaseURL})
	})
	mux.HandleFunc("GET /api/ui/catalog", api.HandleUICatalog)
	mux.HandleFunc("GET /api/artifacts/{job_id}/{name}", api.HandleServeArtifact)
	mux.Handle("POST /api/generate", optAuth(rateMiddleware(globalLimiter, api.HandleCompatGenerate)))
	mux.Handle("POST /v1/chat/completions", optAuth(rateMiddleware(globalLimiter, api.HandleChatCompletions)))

	// Auth routes (no OptionalAuth needed — they manage the cookie themselves)
	mux.HandleFunc("POST /api/auth/register", api.HandleRegister)
	mux.HandleFunc("POST /api/auth/login", api.HandleLogin)
	mux.HandleFunc("POST /api/auth/logout", api.HandleLogout)
	mux.Handle("GET /api/auth/me", optAuth(http.HandlerFunc(api.HandleMe)))
	mux.Handle("GET /api/user/settings", optAuth(http.HandlerFunc(api.HandleGetSettings)))
	mux.Handle("PUT /api/user/settings", optAuth(http.HandlerFunc(api.HandleUpdateSettings)))

	// Job routes — wrap with OptionalAuth so session resolves user_id
	mux.Handle("GET /models", optAuth(http.HandlerFunc(api.HandleListModels)))
	mux.Handle("GET /jobs/{job_id}", optAuth(http.HandlerFunc(api.HandleGetJob)))
	mux.Handle("GET /jobs", optAuth(http.HandlerFunc(api.HandleListJobs)))
	mux.Handle("PUT /api/jobs/{job_id}/visibility", optAuth(http.HandlerFunc(api.HandleSetVisibility)))

	// Community feed (M5)
	mux.Handle("GET /api/community/feed", optAuth(http.HandlerFunc(api.HandleCommunityFeed)))

	mux.Handle("GET /api/templates", optAuth(http.HandlerFunc(api.HandleListTemplates)))
	mux.Handle("POST /api/templates", optAuth(http.HandlerFunc(api.HandleCreateTemplate)))
	mux.Handle("DELETE /api/templates/{id}", optAuth(http.HandlerFunc(api.HandleDeleteTemplate)))

	mux.Handle("POST /api/admin/users/reset-password", optAuth(http.HandlerFunc(api.HandleAdminResetPassword)))
	mux.Handle("GET /api/admin/templates", optAuth(http.HandlerFunc(api.HandleAdminListTemplates)))
	mux.Handle("PUT /api/admin/templates/{id}/visibility", optAuth(http.HandlerFunc(api.HandleAdminPutTemplateVisibility)))

	mux.HandleFunc("GET /admin/jobs", api.HandleAdminJobs)
	mux.HandleFunc("GET /admin/stats", api.HandleAdminStats)
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		serveEmbeddedHTML(w, webRoot, "admin.html")
	})
	mux.HandleFunc("GET /integrate", func(w http.ResponseWriter, r *http.Request) {
		serveEmbeddedHTML(w, webRoot, "integrate.html")
	})

	mux.Handle("/", optAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		http.FileServer(http.FS(webRoot)).ServeHTTP(w, r)
	})))

	port := strings.TrimSpace(cfg.ServerPort)
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
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

// registerProviders wires all provider adapters using server-side credentials
// from config.yaml (with env var fallback). The embedded SPA overrides these
// per request via compatctx (JSON base_url + api_key on POST /api/generate).
func registerProviders(log *slog.Logger, cfg mvpconfig.Config) {
	provider.Register(openai.New(openai.Config{
		BaseURL: cfg.OpenAI.BaseURL,
		APIKey:  cfg.OpenAI.APIKey,
	}))
	log.Info("registered provider", "name", "openai",
		"base_url_set", cfg.OpenAI.BaseURL != "",
		"api_key_set", cfg.OpenAI.APIKey != "")

	provider.Register(gemini.New(gemini.Config{
		BaseURL:              cfg.Gemini.BaseURL,
		APIKey:               cfg.Gemini.APIKey,
		ExtraImageWireModels: geminiconfig.ExtraUpstreamWireModels(),
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

// rateMiddleware applies the global limiter when configured. On reject, returns
// 429 with Retry-After: 60.
func rateMiddleware(global ratelimit.Limiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

// resolveGenpicArtifactsDir picks the on-disk directory for materialized images.
// GENPIC_ARTIFACTS_DIR overrides server.artifacts_dir from YAML. "-" disables writes.
// When unset, defaults to data/genpic-artifacts.
func serveEmbeddedHTML(w http.ResponseWriter, fsys fs.FS, name string) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func resolveGenpicArtifactsDir(cfg mvpconfig.Config) string {
	d := strings.TrimSpace(cfg.ArtifactsDir)
	if v := strings.TrimSpace(os.Getenv("GENPIC_ARTIFACTS_DIR")); v != "" {
		d = v
	}
	if d == "-" {
		return ""
	}
	if d == "" {
		return "data/genpic-artifacts"
	}
	return d
}
