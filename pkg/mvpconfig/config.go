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

// rateLimitYAML configures the in-process rate limiter.
type rateLimitYAML struct {
	GlobalRPM int `yaml:"global_rpm"`
}

// rootYAML is the full config.yaml structure. Unknown keys are ignored.
type rootYAML struct {
	Server       serverYAML        `yaml:"server"`
	MvpLite      mvpLiteYAML       `yaml:"mvp_lite"`
	ModelIDMap   map[string]string `yaml:"model_id_map"`
	OpenAI       providerYAML      `yaml:"openai"`
	Gemini       providerYAML      `yaml:"gemini"`
	Wan       providerYAML  `yaml:"wan"`
	RateLimit rateLimitYAML `yaml:"rate_limit"`
}

// ProviderConfig holds resolved credentials for one upstream provider.
// BaseURL and APIKey are populated from config.yaml first, then env vars.
type ProviderConfig struct {
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	MaxRetries int
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
	Wan ProviderConfig

	// Rate-limiting: global RPM cap for /v1/images/generations (0 = unlimited).
	GlobalRPM int
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
		Gemini:         resolveProvider(root.Gemini, "GEMINI"),
		Wan:            resolveProvider(root.Wan, "WAN"),
		GlobalRPM: root.RateLimit.GlobalRPM,
	}

	return c, nil
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
