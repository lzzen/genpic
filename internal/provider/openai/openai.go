// Package openai implements the provider.Provider interface for OpenAI-compatible
// image generation (gpt-image-2 and any other model routed through the OpenAI
// images/generations endpoint).
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/httpclient"
	"genpic/pkg/logger"
	"genpic/pkg/provider"
)

// Config holds the connection details for the OpenAI-compatible upstream.
// In production these come from environment variables or a secrets manager,
// never from the API caller's request body.
type Config struct {
	// BaseURL is the upstream origin, e.g. "https://api.openai.com".
	// For NewAPI aggregator: your aggregator's base URL.
	BaseURL string
	// APIKey is the server-side upstream key (never the platform user's key).
	APIKey string
}

// Provider implements provider.Provider for OpenAI images/generations.
type Provider struct {
	cfg    Config
	client *httpclient.Client
	models []provider.ModelInfo
}

// New creates a configured OpenAI provider. It reads model definitions from
// the compiled-in contract table rather than hard-coding them here.
func New(cfg Config) *Provider {
	return &Provider{
		cfg:    cfg,
		client: httpclient.New(httpclient.WithMaxRetries(2)),
		models: []provider.ModelInfo{
			{
				ID:             "openai/gpt-image-2",
				DisplayName:    "GPT Image 2",
				UpstreamModel:  "gpt-image-2",
				TimeoutSeconds: 120,
				Capabilities: []provider.Capability{
					provider.CapTextToImage,
					provider.CapResponseFormatURL,
					provider.CapResponseFormatB64,
				},
			},
		},
	}
}

func (p *Provider) Name() string                 { return "openai" }
func (p *Provider) Models() []provider.ModelInfo { return p.models }

func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResponse, error) {
	log := logger.FromContext(ctx)
	start := time.Now()

	body, err := buildRequest(req)
	if err != nil {
		return nil, pkgerrors.Wrap(http.StatusBadRequest, pkgerrors.TypeValidation, "build_request", "could not build upstream request", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/v1/images/generations"
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + p.cfg.APIKey,
	}

	resp, raw, err := p.client.Do(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		msg := extractError(raw, resp.StatusCode)
		return nil, pkgerrors.UpstreamErr("upstream_http_error", msg, nil)
	}

	var out struct {
		Data []struct {
			URL           string `json:"url"`
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, pkgerrors.UpstreamErr("parse_error", "could not parse upstream response", err)
	}
	if out.Error != nil {
		return nil, pkgerrors.UpstreamErr(out.Error.Type, out.Error.Message, nil)
	}

	images := make([]provider.Image, 0, len(out.Data))
	for _, d := range out.Data {
		images = append(images, provider.Image{
			URL:           d.URL,
			B64JSON:       d.B64JSON,
			RevisedPrompt: d.RevisedPrompt,
		})
	}
	if len(images) == 0 {
		return nil, pkgerrors.UpstreamErr("empty_response", "upstream returned no images", nil)
	}

	latency := time.Since(start)
	log.Info("openai generate ok", "model", req.Model, "n", len(images), "latency_ms", latency.Milliseconds())

	return &provider.GenerateResponse{
		Images:  images,
		Latency: latency,
	}, nil
}

func buildRequest(req provider.GenerateRequest) ([]byte, error) {
	body := map[string]any{
		"model":  req.Model,
		"prompt": req.Prompt,
	}
	if req.N > 0 {
		body["n"] = req.N
	}
	if req.Size != "" {
		body["size"] = req.Size
	}
	if req.Quality != "" {
		body["quality"] = req.Quality
	}
	if req.Style != "" {
		body["style"] = req.Style
	}
	format := req.ResponseFormat
	if format == "" {
		format = "url"
	}
	body["response_format"] = format
	return json.Marshal(body)
}

func extractError(body []byte, statusCode int) string {
	var env struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Error != nil && env.Error.Message != "" {
		return env.Error.Message
	}
	// Avoid returning raw upstream bodies that might contain sensitive details.
	var buf bytes.Buffer
	_ = json.Compact(&buf, body)
	if buf.Len() > 200 {
		buf.Truncate(200)
	}
	return "upstream HTTP " + http.StatusText(statusCode) + ": " + buf.String()
}
