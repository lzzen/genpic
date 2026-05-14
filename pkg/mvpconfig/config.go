// Package mvpconfig reads shared settings from config.yaml for both
// cmd/mvplite and cmd/genpic. Unknown fields are silently ignored so that
// the full platform config.yaml is backward compatible with mvplite.
package mvpconfig

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// serverYAML is the server section (full platform listen port).
type serverYAML struct {
	Port         string `yaml:"port"`
	ReadTimeout  string `yaml:"read_timeout"`
	WriteTimeout string `yaml:"write_timeout"`
	IdleTimeout  string `yaml:"idle_timeout"`
	ArtifactsDir string `yaml:"artifacts_dir"`
}

// mvpLiteYAML is the mvp_lite section of config.yaml.
type mvpLiteYAML struct {
	Port           string `yaml:"port"`
	DefaultBaseURL string `yaml:"default_base_url"`
}

// providerYAML is the per-provider upstream credential block.
type providerYAML struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	Timeout    string `yaml:"timeout"`
	MaxRetries int    `yaml:"max_retries"`
}

// geminiYAML extends the Gemini provider block with SPA / routing hints.
type geminiYAML struct {
	BaseURL             string            `yaml:"base_url"`
	APIKey              string            `yaml:"api_key"`
	Timeout             string            `yaml:"timeout"`
	MaxRetries          int               `yaml:"max_retries"`
	ImageSize4KModelMap map[string]string `yaml:"image_size_4k_model_map"`
}

// rateLimitYAML configures the in-process rate limiter.
type rateLimitYAML struct {
	GlobalRPM int `yaml:"global_rpm"`
}

// databaseYAML is the database section of config.yaml.
type databaseYAML struct {
	DSN          string `yaml:"dsn"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

// authYAML holds optional auth/session tuning (cmd/genpic).
type authYAML struct {
	SessionTTL string `yaml:"session_ttl"`
}

// rootYAML is the full config.yaml structure. Unknown keys are ignored.
type rootYAML struct {
	Server     serverYAML        `yaml:"server"`
	MvpLite    mvpLiteYAML       `yaml:"mvp_lite"`
	ModelIDMap map[string]string `yaml:"model_id_map"`
	OpenAI     providerYAML      `yaml:"openai"`
	Gemini     geminiYAML        `yaml:"gemini"`
	Wan        providerYAML      `yaml:"wan"`
	RateLimit  rateLimitYAML     `yaml:"rate_limit"`
	Database   databaseYAML      `yaml:"database"`
	Auth       authYAML          `yaml:"auth"`
}

// ProviderConfig holds resolved credentials for one upstream provider.
// BaseURL and APIKey are populated from config.yaml first, then env vars.
type ProviderConfig struct {
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	MaxRetries int
}

// DatabaseConfig holds resolved database connection settings.
type DatabaseConfig struct {
	// DSN is the full MySQL data source name.
	// Must include parseTime=true, e.g.:
	//   user:pass@tcp(localhost:3306)/genpic?parseTime=true&charset=utf8mb4
	// Can also be supplied via DB_DSN env var.
	DSN          string
	MaxOpenConns int
	MaxIdleConns int
}

// Config holds all parsed settings from config.yaml.
// The struct is a superset of what each binary uses; unknown fields are ignored.
type Config struct {
	Found          bool
	MvpLitePort    string            // cmd/mvplite: mvp_lite.port
	ServerPort     string            // cmd/genpic: server.port
	DefaultBaseURL string            // mvp_lite.default_base_url (both)
	ModelIDMap     map[string]string // optional upstream model id remap

	// Full-platform (cmd/genpic) provider credentials:
	OpenAI ProviderConfig
	Gemini ProviderConfig
	Wan    ProviderConfig

	// Rate-limiting: global RPM cap for POST /api/generate (0 = unlimited).
	GlobalRPM int

	// Database connection settings (cmd/genpic only). DSN="" means in-memory fallback.
	Database DatabaseConfig

	// ArtifactsDir is the directory where generated images are written for GET /api/artifacts/...
	// Resolved in cmd/genpic with GENPIC_ARTIFACTS_DIR override; "-" means disabled.
	ArtifactsDir string

	// GeminiImageSize4KModelMap maps catalog model id -> alternate catalog model id when the SPA
	// selects Gemini image_size "4K" (see gemini.image_size_4k_model_map in config.yaml).
	GeminiImageSize4KModelMap map[string]string

	// Auth configures cookie-backed sessions (cmd/genpic; requires database).
	Auth AuthConfig
}

// AuthConfig holds session lifetime for auth.NewStore.
type AuthConfig struct {
	SessionTTL time.Duration
}

// Read loads config from a YAML file. A missing file is not an error (Found=false).
// Env var fallbacks for provider credentials are applied automatically.
func Read(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var root rootYAML
	if err := yaml.Unmarshal(data, &root); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}

	c := Config{
		Found:          true,
		MvpLitePort:    strings.TrimSpace(root.MvpLite.Port),
		ServerPort:     strings.TrimSpace(root.Server.Port),
		DefaultBaseURL: strings.TrimSpace(root.MvpLite.DefaultBaseURL),
		ModelIDMap:     stringMapOrNil(root.ModelIDMap),
		OpenAI:         resolveProvider(root.OpenAI, "OPENAI"),
		Gemini: resolveProvider(providerYAML{
			BaseURL:    root.Gemini.BaseURL,
			APIKey:     root.Gemini.APIKey,
			Timeout:    root.Gemini.Timeout,
			MaxRetries: root.Gemini.MaxRetries,
		}, "GEMINI"),
		GeminiImageSize4KModelMap: normalizeGeminiImageSize4KModelMap(root.Gemini.ImageSize4KModelMap),
		Wan:                       resolveProvider(root.Wan, "WAN"),
		GlobalRPM:                 root.RateLimit.GlobalRPM,
		Database:                  resolveDatabase(root.Database),
		ArtifactsDir:              strings.TrimSpace(root.Server.ArtifactsDir),
		Auth:                      resolveAuth(root.Auth),
	}

	return c, nil
}

// resolveDatabase merges config.yaml database settings with the DB_DSN env var fallback.
func resolveDatabase(y databaseYAML) DatabaseConfig {
	dsn := strings.TrimSpace(y.DSN)
	if v := strings.TrimSpace(os.Getenv("DB_DSN")); v != "" {
		dsn = v
	}
	maxOpen := y.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 10
	}
	maxIdle := y.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 5
	}
	return DatabaseConfig{DSN: dsn, MaxOpenConns: maxOpen, MaxIdleConns: maxIdle}
}

func resolveAuth(y authYAML) AuthConfig {
	def := 30 * 24 * time.Hour
	raw := strings.TrimSpace(y.SessionTTL)
	if raw == "" {
		return AuthConfig{SessionTTL: def}
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return AuthConfig{SessionTTL: def}
	}
	return AuthConfig{SessionTTL: d}
}

// resolveProvider fills base_url and api_key from env vars when not present
// in config. envPrefix is the uppercase provider name, e.g. "OPENAI".
func resolveProvider(y providerYAML, envPrefix string) ProviderConfig {
	baseURL := strings.TrimSpace(y.BaseURL)
	if v := strings.TrimSpace(os.Getenv(envPrefix + "_BASE_URL")); v != "" {
		baseURL = v
	}
	apiKey := strings.TrimSpace(y.APIKey)
	if v := strings.TrimSpace(os.Getenv(envPrefix + "_API_KEY")); v != "" {
		apiKey = v
	}

	timeout := 120 * time.Second
	if d, err := time.ParseDuration(strings.TrimSpace(y.Timeout)); err == nil && d > 0 {
		timeout = d
	}
	maxRetries := y.MaxRetries
	if maxRetries == 0 {
		maxRetries = 2
	}
	return ProviderConfig{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Timeout:    timeout,
		MaxRetries: maxRetries,
	}
}

func stringMapOrNil(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeGeminiCatalogModelID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "/") {
		return s
	}
	return "gemini/" + s
}

// normalizeGeminiImageSize4KModelMap normalizes YAML map keys/values to catalog-style ids (gemini/...).
func normalizeGeminiImageSize4KModelMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		nk := normalizeGeminiCatalogModelID(k)
		nv := normalizeGeminiCatalogModelID(v)
		if nk == "" || nv == "" {
			continue
		}
		out[nk] = nv
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
