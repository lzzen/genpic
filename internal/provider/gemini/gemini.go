// Package gemini implements provider.Provider for Google Gemini "Banana" series
// image-generation models, routed through the aggregator's OpenAI-compatible
// chat/completions endpoint (single-turn user message with image instruction).
//
// The aggregator translates the chat completion into a Gemini generateContent
// call; this adapter only needs to construct the correct chat shape.
//
// SynthID watermarking is always applied by Google and cannot be disabled.
// This must be disclosed to end users in the UI (see design §7.2).
package gemini

import (
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

// Config holds the aggregator connection details for Gemini routing.
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

// New creates a Gemini provider. All three "Banana" tier models are registered.
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

	body, err := buildChatRequest(req)
	if err != nil {
		return nil, pkgerrors.Wrap(http.StatusBadRequest, pkgerrors.TypeValidation, "build_request", "could not build Gemini chat request", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/v1/chat/completions"
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + p.cfg.APIKey,
	}

	resp, raw, err := p.client.Do(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, pkgerrors.UpstreamErr("upstream_http_error", extractChatError(raw, resp.StatusCode), nil)
	}

	images, tokensUsed, err := parseChatResponse(raw)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, pkgerrors.UpstreamErr("empty_response", "Gemini returned no images in chat response", nil)
	}

	latency := time.Since(start)
	log.Info("gemini generate ok", "model", req.Model, "n", len(images), "tokens", tokensUsed, "latency_ms", latency.Milliseconds())

	return &provider.GenerateResponse{
		Images:     images,
		TokensUsed: tokensUsed,
		Latency:    latency,
	}, nil
}

// buildChatRequest constructs an OpenAI chat/completions body that instructs
// the model to generate an image. The aggregator translates this to Gemini's
// generateContent wire format.
func buildChatRequest(req provider.GenerateRequest) ([]byte, error) {
	userContent := req.Prompt
	if req.AspectRatio != "" {
		// Hint the aggregator / model about the desired aspect ratio.
		// The exact instruction format may vary by aggregator; this is a
		// best-effort hint and must be validated against the live aggregator.
		userContent += " --aspect-ratio " + req.AspectRatio
	}

	msg := map[string]any{
		"role":    "user",
		"content": userContent,
	}

	body := map[string]any{
		"model":    req.Model,
		"messages": []any{msg},
		// n is passed as a top-level field; aggregators that support it will
		// generate multiple images in one call.
		"n": req.N,
	}

	if req.ThinkingBudget > 0 {
		// Thinking budget is passed as a model-specific extension. The exact
		// field name must be confirmed against the aggregator's documentation.
		// TODO(@pyq, #15): Validate thinking_budget field name against live aggregator.
		body["thinking_budget"] = req.ThinkingBudget
	}

	return json.Marshal(body)
}

// parseChatResponse extracts images from the chat/completions response.
// Gemini returns images as base64-encoded content within the assistant message.
func parseChatResponse(raw []byte) ([]provider.Image, int, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"` // may be string or array of parts
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, pkgerrors.UpstreamErr("parse_error", "could not parse Gemini chat response", err)
	}
	if resp.Error != nil {
		return nil, 0, pkgerrors.UpstreamErr("upstream_error", resp.Error.Message, nil)
	}

	var images []provider.Image
	for _, choice := range resp.Choices {
		// Content can be a string (text) or an array of parts (multimodal).
		// When it is an array, look for parts with type "image_url" or "image".
		switch v := choice.Message.Content.(type) {
		case string:
			// Text-only response — no image
		case []any:
			for _, part := range v {
				m, ok := part.(map[string]any)
				if !ok {
					continue
				}
				switch m["type"] {
				case "image_url":
					if iu, ok := m["image_url"].(map[string]any); ok {
						if url, ok := iu["url"].(string); ok {
							if strings.HasPrefix(url, "data:") {
								// data URI — extract base64 payload
								if idx := strings.Index(url, ","); idx >= 0 {
									images = append(images, provider.Image{B64JSON: url[idx+1:]})
								}
							} else {
								images = append(images, provider.Image{URL: url})
							}
						}
					}
				case "image":
					// Some aggregators use a direct "image" type with base64.
					if b64, ok := m["data"].(string); ok {
						images = append(images, provider.Image{B64JSON: b64})
					}
				}
			}
		}
	}

	return images, resp.Usage.TotalTokens, nil
}

func extractChatError(body []byte, statusCode int) string {
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
