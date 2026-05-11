// Package gemini implements provider.Provider for Google Gemini image models
// ("Banana" / gemini-*-image), using the native Gemini REST shape:
// POST {base}/v1beta/models/{model}:generateContent
//
// Request/response follow Google's generateContent wire format (contents.parts,
// generationConfig.responseModalities IMAGE, imageConfig, inlineData in candidates).
// See model-fingers/gemini-image.md for the finger document used by this adapter.
//
// SynthID watermarking is always applied by Google and cannot be disabled.
package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/httpclient"
	"genpic/pkg/logger"
	"genpic/pkg/provider"
)

// Config holds connection details for GEMINI_BASE_URL (scheme + host only, no path suffix).
type Config struct {
	BaseURL string
	APIKey  string
}

// Provider implements provider.Provider for Gemini image models.
type Provider struct {
	cfg    Config
	client *httpclient.Client
	models []provider.ModelInfo
}

// New creates a Gemini provider. All three image tier models are registered.
func New(cfg Config) *Provider {
	caps := func(extra ...provider.Capability) []provider.Capability {
		base := []provider.Capability{
			provider.CapTextToImage,
			provider.CapResponseFormatB64,
			provider.CapSynthIDWatermark,
		}
		return append(base, extra...)
	}
	return &Provider{
		cfg:    cfg,
		client: httpclient.New(httpclient.WithMaxRetries(1)),
		models: []provider.ModelInfo{
			{
				ID:             "gemini/gemini-2.5-flash-image",
				DisplayName:    "Gemini 2.5 Flash Image",
				UpstreamModel:  "gemini-2.5-flash-image",
				TimeoutSeconds: 90,
				Capabilities:   caps(),
			},
			{
				ID:             "gemini/gemini-3.1-flash-image-preview",
				DisplayName:    "Gemini 3.1 Flash Image (Preview)",
				UpstreamModel:  "gemini-3.1-flash-image-preview",
				TimeoutSeconds: 120,
				Capabilities:   caps(provider.CapThinking),
			},
			{
				ID:             "gemini/gemini-3-pro-image-preview",
				DisplayName:    "Gemini 3 Pro Image (Preview)",
				UpstreamModel:  "gemini-3-pro-image-preview",
				TimeoutSeconds: 180,
				Capabilities:   caps(provider.CapThinking),
			},
		},
	}
}

func (p *Provider) Name() string                 { return "gemini" }
func (p *Provider) Models() []provider.ModelInfo { return p.models }

func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResponse, error) {
	log := logger.FromContext(ctx)
	start := time.Now()

	n := req.N
	if n == 0 {
		n = 1
	}
	if n < 1 || n > 4 {
		return nil, pkgerrors.Wrap(http.StatusBadRequest, pkgerrors.TypeValidation, "invalid_n", "n must be between 1 and 4", nil)
	}

	var images []provider.Image
	var totalTokens int
	var reqID string

	round := 0
	for range n {
		round++
		body, err := buildGenerateContentBody(req)
		if err != nil {
			return nil, pkgerrors.Wrap(http.StatusBadRequest, pkgerrors.TypeValidation, "build_request", "could not build Gemini generateContent request", err)
		}

		url := generateContentURL(p.cfg.BaseURL, req.Model)
		headers := map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + p.cfg.APIKey,
		}

		if logger.DevMode() {
			log.Info("gemini_generateContent_request",
				"round", round,
				"of", n,
				"method", http.MethodPost,
				"url", url,
				"gemini_base_url", strings.TrimRight(strings.TrimSpace(p.cfg.BaseURL), "/"),
				"api_key", logger.Redact(p.cfg.APIKey),
				"body_bytes", len(body),
				"body_json", clipJSON(string(body), 900),
			)
		}

		resp, raw, err := p.client.Do(ctx, http.MethodPost, url, headers, body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			if logger.DevMode() {
				log.Warn("gemini_generateContent_non_ok",
					"round", round,
					"url", url,
					"status", resp.StatusCode,
					"response_bytes", len(raw),
					"response_body", clipJSON(redactLargeInlineDataJSON(string(raw)), 1400),
				)
			}
			return nil, pkgerrors.UpstreamErr("upstream_http_error", extractGeminiAPIError(raw, resp.StatusCode), nil)
		}

		if logger.DevMode() {
			log.Info("gemini_generateContent_http_ok",
				"round", round,
				"url", url,
				"status", resp.StatusCode,
				"response_bytes", len(raw),
				"response_json", clipJSON(redactLargeInlineDataJSON(string(raw)), 2000),
			)
		}

		batch, tokens, rid, err := parseGenerateContentResponse(raw)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			return nil, pkgerrors.UpstreamErr("empty_response", "Gemini returned no images in generateContent response", nil)
		}
		images = append(images, batch...)
		totalTokens += tokens
		if rid != "" {
			reqID = rid
		}
		if logger.DevMode() {
			log.Info("gemini_generateContent_parsed",
				"round", round,
				"inline_images", len(batch),
				"usage_total_tokens", tokens,
				"response_id", rid,
			)
		}
	}

	latency := time.Since(start)
	log.Info("gemini generate ok", "model", req.Model, "n", len(images), "tokens", totalTokens, "latency_ms", latency.Milliseconds())

	return &provider.GenerateResponse{
		Images:            images,
		TokensUsed:        totalTokens,
		Latency:           latency,
		UpstreamRequestID: reqID,
	}, nil
}

func generateContentURL(baseURL, model string) string {
	b := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return fmt.Sprintf("%s/v1beta/models/%s:generateContent", b, model)
}

func buildGenerateContentBody(req provider.GenerateRequest) ([]byte, error) {
	aspect := strings.TrimSpace(req.AspectRatio)
	if aspect == "" {
		aspect = "1:1"
	}
	imgSize := strings.TrimSpace(req.ImageSize)
	if imgSize == "" {
		imgSize = "512"
	}

	imageConfig := map[string]any{
		"aspectRatio": aspect,
		"imageSize":   imgSize,
	}

	genCfg := map[string]any{
		"responseModalities": []string{"IMAGE"},
		"imageConfig":        imageConfig,
	}
	if req.ThinkingBudget > 0 {
		genCfg["thinkingConfig"] = map[string]any{
			"thinkingBudget": req.ThinkingBudget,
		}
	}

	body := map[string]any{
		"contents": []any{
			map[string]any{
				"parts": []any{
					map[string]any{"text": req.Prompt},
				},
			},
		},
		"generationConfig": genCfg,
	}
	return json.Marshal(body)
}

func parseGenerateContentResponse(raw []byte) (images []provider.Image, totalTokens int, responseID string, err error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						MIMEType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			TotalTokenCount int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
		ResponseID string `json:"responseId"`
		Error      *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, "", pkgerrors.UpstreamErr("parse_error", "could not parse Gemini generateContent response", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return nil, 0, "", pkgerrors.UpstreamErr("upstream_error", resp.Error.Message, nil)
	}

	for _, cand := range resp.Candidates {
		for _, part := range cand.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				mime := part.InlineData.MIMEType
				if mime == "" {
					mime = "image/png"
				}
				images = append(images, provider.Image{
					B64JSON:  part.InlineData.Data,
					MIMEType: mime,
				})
			}
		}
	}

	return images, resp.UsageMetadata.TotalTokenCount, resp.ResponseID, nil
}

func extractGeminiAPIError(body []byte, statusCode int) string {
	var env struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Error != nil && env.Error.Message != "" {
		return env.Error.Message
	}
	return "upstream HTTP " + http.StatusText(statusCode)
}

func clipJSON(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// Redacts very long JSON string values for "data" (typical base64 image payloads) so dev logs stay readable.
var longInlineDataField = regexp.MustCompile(`"data"\s*:\s*"[^"]{200,}"`)

func redactLargeInlineDataJSON(s string) string {
	return longInlineDataField.ReplaceAllString(s, `"data":"[redacted large payload]"`)
}
