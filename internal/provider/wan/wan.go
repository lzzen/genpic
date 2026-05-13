// Package wan implements provider.Provider for Aliyun Tongyi Wanxiang 2.7
// image generation models (wan2.7-image and wan2.7-image-pro).
//
// Requests use the multimodal body shape (input.messages + parameters); the
// upstream HTTP path is POST {base}/v1/images/generations. The synchronous
// response often wraps the native DashScope object under "metadata"; parsing
// prefers that field and falls back to top-level output or OpenAI-like "data".
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

	"genpic/pkg/compatctx"
	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/httpclient"
	"genpic/pkg/logger"
	"genpic/pkg/provider"
)

// Config holds default DashScope connection details (optional when using POST /api/generate with JSON base_url + api_key).
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
					provider.CapImageToImage,
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

// Generate calls POST {base}/v1/images/generations with a DashScope-shaped body
// and parses images from metadata.output (or legacy / data fallbacks).
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResponse, error) {
	log := logger.FromContext(ctx)
	start := time.Now()

	baseURL, apiKey, trace := compatctx.Resolve(ctx, p.cfg.BaseURL, p.cfg.APIKey)
	if baseURL == "" || apiKey == "" {
		return nil, pkgerrors.BadRequest("upstream_credentials", "set base_url and api_key in the POST /api/generate JSON body.")
	}

	body, err := buildDashScopeRequest(req)
	if err != nil {
		return nil, pkgerrors.Wrap(http.StatusBadRequest, pkgerrors.TypeValidation, "build_request", "could not build DashScope request", err)
	}
	url := strings.TrimRight(baseURL, "/") + "/v1/images/generations"
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + apiKey,
		// DashScope requires this header for async polling; for synchronous mode
		// we use X-DashScope-Async: disable (default).
		"X-DashScope-Async": "disable",
	}

	resp, raw, err := p.client.Do(ctx, http.MethodPost, url, headers, body)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	if raw == nil {
		raw = []byte{}
	}
	if trace {
		compatctx.LogStderrRoundTrip("wan", http.MethodPost, url, headers, body, status, raw)
	}
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
	content := make([]map[string]any, 0, 1+len(req.ReferenceImages))
	for _, ref := range req.ReferenceImages {
		b64 := strings.TrimSpace(ref.B64)
		if b64 == "" {
			return nil, fmt.Errorf("reference image: empty b64_json")
		}
		mt := strings.TrimSpace(ref.MIMEType)
		if mt == "" {
			mt = "image/png"
		}
		content = append(content, map[string]any{
			"image": "data:" + mt + ";base64," + b64,
		})
	}
	content = append(content, map[string]any{"text": req.Prompt})

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

// parseDashScopeResponse extracts images from the upstream JSON.
//
// Wan on /v1/images/generations typically returns an OpenAI-like envelope with
// the native DashScope payload under "metadata"; we read that object only.
// If "metadata" is absent, we treat the whole body as the legacy top-level shape.
func parseDashScopeResponse(raw []byte) ([]provider.Image, error) {
	var root struct {
		Metadata json.RawMessage `json:"metadata"`
		Data     []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, pkgerrors.UpstreamErr("parse_error", "could not parse Wan response", err)
	}

	inner := raw
	if len(root.Metadata) > 0 && string(root.Metadata) != "null" {
		inner = root.Metadata
	}

	images, err := imagesFromDashScopeShape(inner)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 && len(root.Data) > 0 {
		for _, d := range root.Data {
			if d.URL != "" {
				images = append(images, provider.Image{URL: d.URL})
			} else if d.B64JSON != "" {
				images = append(images, provider.Image{B64JSON: d.B64JSON})
			}
		}
	}
	return images, nil
}

// imagesFromDashScopeShape parses the native DashScope sync object:
// output.choices[].message.content[].image plus optional code/message.
func imagesFromDashScopeShape(raw []byte) ([]provider.Image, error) {
	var resp struct {
		Output struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, pkgerrors.UpstreamErr("parse_error", "could not parse Wan metadata", err)
	}
	if resp.Code != "" && resp.Code != "Success" {
		return nil, pkgerrors.UpstreamErr(resp.Code, resp.Message, nil)
	}

	var images []provider.Image
	for _, choice := range resp.Output.Choices {
		for _, part := range choice.Message.Content {
			if part.Image == "" {
				continue
			}
			if strings.HasPrefix(part.Image, "data:") {
				if idx := strings.Index(part.Image, ","); idx >= 0 {
					images = append(images, provider.Image{B64JSON: part.Image[idx+1:]})
				}
			} else {
				images = append(images, provider.Image{URL: part.Image})
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
