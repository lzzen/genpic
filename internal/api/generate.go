package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/logger"
	"genpic/pkg/provider"
)

// GenerateRequest is the JSON body for POST /v1/images/generations.
// It is kept OpenAI-compatible while carrying provider-specific extensions
// as top-level optional fields with clear documentation.
type GenerateRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	Style          string `json:"style,omitempty"`

	// Gemini-specific
	AspectRatio    string `json:"aspect_ratio,omitempty"`
	ThinkingBudget int    `json:"thinking_budget,omitempty"`

	// Wan-specific
	Watermark    *bool `json:"watermark,omitempty"`
	ThinkingMode bool  `json:"thinking_mode,omitempty"`

	// Idempotency
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

func (r *GenerateRequest) validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return pkgerrors.BadRequest("missing_field", "model is required")
	}
	if strings.TrimSpace(r.Prompt) == "" {
		return pkgerrors.BadRequest("missing_field", "prompt is required")
	}
	if len(r.Prompt) > 32000 {
		return pkgerrors.BadRequest("prompt_too_long", "prompt exceeds 32 000 characters")
	}
	if r.N < 0 || r.N > 4 {
		return pkgerrors.BadRequest("invalid_n", "n must be between 1 and 4")
	}
	return nil
}

// compatGenerateBody is the SPA body for POST /api/generate: same fields as
// GenerateRequest plus optional base_url and api_key (used by MVP Lite proxy;
// full platform ignores them and uses server-side env credentials).
type compatGenerateBody struct {
	GenerateRequest
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}

func executeImageGeneration(ctx context.Context, req GenerateRequest) (map[string]any, error) {
	log := logger.FromContext(ctx)
	if err := req.validate(); err != nil {
		return nil, err
	}

	prov, modelInfo, ok := provider.ProviderForModel(req.Model)
	if !ok {
		return nil, pkgerrors.New(http.StatusNotFound, pkgerrors.TypeNotFound, "model_not_found", "model "+req.Model+" is not available")
	}

	log.Info("generating image", "model", req.Model, "provider", prov.Name(), "n", req.N)

	n := req.N
	if n == 0 {
		n = 1
	}
	format := req.ResponseFormat
	if format == "" {
		format = "url"
	}

	provReq := provider.GenerateRequest{
		Model:          modelInfo.UpstreamModel,
		Prompt:         req.Prompt,
		N:              n,
		Size:           req.Size,
		Quality:        req.Quality,
		ResponseFormat: format,
		Style:          req.Style,
		AspectRatio:    req.AspectRatio,
		ThinkingBudget: req.ThinkingBudget,
		ThinkingMode:   req.ThinkingMode,
	}

	timeout := time.Duration(modelInfo.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := prov.Generate(ctx, provReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, pkgerrors.UpstreamTimeout()
		}
		log.Error("generation failed", "model", req.Model, "error", err)
		return nil, err
	}

	if len(resp.Images) == 0 {
		return nil, pkgerrors.UpstreamErr("empty_response", "upstream returned no images", nil)
	}

	type imageData struct {
		URL           string `json:"url,omitempty"`
		B64JSON       string `json:"b64_json,omitempty"`
		RevisedPrompt string `json:"revised_prompt,omitempty"`
	}
	data := make([]imageData, 0, len(resp.Images))
	for _, img := range resp.Images {
		data = append(data, imageData{
			URL:           img.URL,
			B64JSON:       img.B64JSON,
			RevisedPrompt: img.RevisedPrompt,
		})
	}

	return map[string]any{
		"created":    time.Now().Unix(),
		"data":       data,
		"x_provider": prov.Name(),
	}, nil
}

// HandleImageGeneration serves POST /v1/images/generations.
// In the current synchronous (MVP) mode it calls the upstream directly and
// returns a 200 with the images. In Full Platform async mode it would enqueue
// a job and return 202 (not yet wired; the async job queue is planned for M1).
func HandleImageGeneration(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body: "+err.Error()))
		return
	}

	out, err := executeImageGeneration(r.Context(), req)
	if err != nil {
		Error(w, err)
		return
	}
	JSON(w, http.StatusOK, out)
}

// HandleCompatGenerate serves POST /api/generate for the embedded SPA (same
// JSON shape as MVP Lite). Upstream credentials come from environment
// variables on the full platform; base_url and api_key in the body are ignored.
func HandleCompatGenerate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var body compatGenerateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body: "+err.Error()))
		return
	}

	out, err := executeImageGeneration(r.Context(), body.GenerateRequest)
	if err != nil {
		Error(w, err)
		return
	}
	JSON(w, http.StatusOK, out)
}
