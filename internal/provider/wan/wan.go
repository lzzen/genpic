// Package wan implements provider.Provider for Aliyun Tongyi Wanxiang 2.7
// image generation models (wan2.7-image and wan2.7-image-pro).
//
// Wan uses the DashScope multimodal-generation API, which differs significantly
// from the OpenAI shape. It is NOT aliased through images/generations on the
// external surface to avoid semantic mismatch (see design §5.2 and ADR-001).
//
// Watermarking is always applied per Aliyun TOS and cannot be disabled by callers.
package wan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/httpclient"
	"genpic/pkg/logger"
	"genpic/pkg/provider"
)

// Config holds the DashScope connection details.
type Config struct {
	// BaseURL is the DashScope endpoint (regional).
	// CN: https://dashscope.aliyuncs.com
	// AP: https://dashscope-intl.aliyuncs.com
	BaseURL string
	// APIKey is the DashScope API key stored server-side.
	APIKey string
}

// Provider implements provider.Provider for Wan2.7 models.
type Provider struct {
	cfg    Config
	client *httpclient.Client
	models []provider.ModelInfo
}

// New creates a Wan provider.
func New(cfg Config) *Provider {
	return &Provider{
		cfg:    cfg,
		client: httpclient.New(httpclient.WithMaxRetries(1)),
		models: []provider.ModelInfo{
			{
				ID:             "wan/wan2.7-image",
				DisplayName:    "通义万相 2.7",
				UpstreamModel:  "wan2.7-image",
				TimeoutSeconds: 120,
				Capabilities: []provider.Capability{
					provider.CapTextToImage,
					provider.CapWatermark,
				},
			},
			{
				ID:             "wan/wan2.7-image-pro",
				DisplayName:    "通义万相 2.7 Pro",
				UpstreamModel:  "wan2.7-image-pro",
				TimeoutSeconds: 180,
				Capabilities: []provider.Capability{
					provider.CapTextToImage,
					provider.CapImageToImage,
					provider.CapWatermark,
				},
			},
		},
	}
}

func (p *Provider) Name() string                 { return "wan" }
func (p *Provider) Models() []provider.ModelInfo { return p.models }

// Generate calls the DashScope multimodal-generation endpoint.
// The DashScope request body is substantially different from OpenAI; this
// adapter translates the normalised GenerateRequest into the required shape.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResponse, error) {
	log := logger.FromContext(ctx)
	start := time.Now()

	body, err := buildDashScopeRequest(req)
	if err != nil {
		return nil, pkgerrors.Wrap(http.StatusBadRequest, pkgerrors.TypeValidation, "build_request", "could not build DashScope request", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/api/v1/services/aigc/multimodal-generation/generation"
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + p.cfg.APIKey,
		// DashScope requires this header for async polling; for synchronous mode
		// we use X-DashScope-Async: disable (default).
		"X-DashScope-Async": "disable",
	}

	resp, raw, err := p.client.Do(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, pkgerrors.UpstreamErr("upstream_http_error", extractDashScopeError(raw, resp.StatusCode), nil)
	}

	images, err := parseDashScopeResponse(raw)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, pkgerrors.UpstreamErr("empty_response", "Wan returned no images", nil)
	}

	latency := time.Since(start)
	log.Info("wan generate ok", "model", req.Model, "n", len(images), "latency_ms", latency.Milliseconds())

	return &provider.GenerateResponse{
		Images:  images,
		Latency: latency,
	}, nil
}

// buildDashScopeRequest constructs the DashScope multimodal-generation body.
// Reference: https://help.aliyun.com/zh/model-studio/wan-image-generation-and-editing-api-reference
func buildDashScopeRequest(req provider.GenerateRequest) ([]byte, error) {
	content := []map[string]any{
		{"text": req.Prompt},
	}

	params := map[string]any{
		// Watermark is always true per Aliyun TOS; we set it explicitly
		// to be transparent about platform behaviour.
		"watermark": true,
	}
	if req.N > 0 {
		params["n"] = req.N
	}
	if req.Size != "" {
		params["size"] = req.Size
	}
	if req.ThinkingMode {
		params["thinking_mode"] = true
	}

	body := map[string]any{
		"model": req.Model,
		"input": map[string]any{
			"messages": []map[string]any{
				{"role": "user", "content": content},
			},
		},
		"parameters": params,
	}

	return json.Marshal(body)
}

// parseDashScopeResponse extracts images from the DashScope synchronous response.
// DashScope wraps images under output.choices[].message.content[].image.
func parseDashScopeResponse(raw []byte) ([]provider.Image, error) {
	var resp struct {
		Output struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
						Text  string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, pkgerrors.UpstreamErr("parse_error", "could not parse DashScope response", err)
	}
	// DashScope returns error as a code/message pair at the top level
	// (not nested under an "error" key like OpenAI).
	if resp.Code != "" && resp.Code != "Success" {
		return nil, pkgerrors.UpstreamErr(resp.Code, resp.Message, nil)
	}

	var images []provider.Image
	for _, choice := range resp.Output.Choices {
		for _, part := range choice.Message.Content {
			if part.Image != "" {
				// DashScope returns image as a URL or a data URI depending on the model.
				if strings.HasPrefix(part.Image, "data:") {
					if idx := strings.Index(part.Image, ","); idx >= 0 {
						images = append(images, provider.Image{B64JSON: part.Image[idx+1:]})
					}
				} else {
					images = append(images, provider.Image{URL: part.Image})
				}
			}
		}
	}
	return images, nil
}

func extractDashScopeError(body []byte, statusCode int) string {
	var env struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Message != "" {
		return fmt.Sprintf("%s: %s", env.Code, env.Message)
	}
	return "upstream HTTP " + http.StatusText(statusCode)
}
