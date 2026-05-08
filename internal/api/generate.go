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

// HandleImageGeneration serves POST /v1/images/generations.
// In the current synchronous (MVP) mode it calls the upstream directly and
// returns a 200 with the images. In Full Platform async mode it would enqueue
// a job and return 202 (not yet wired; the async job queue is planned for M1).
func HandleImageGeneration(w http.ResponseWriter, r *http.Request) {
	log := logger.FromContext(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body: "+err.Error()))
		return
	}
	if err := req.validate(); err != nil {
		Error(w, err)
		return
	}

	// Resolve the provider for the requested model.
	prov, modelInfo, ok := provider.ProviderForModel(req.Model)
	if !ok {
		Error(w, pkgerrors.New(http.StatusNotFound, pkgerrors.TypeNotFound, "model_not_found", "model "+req.Model+" is not available"))
		return
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
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	resp, err := prov.Generate(ctx, provReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			Error(w, pkgerrors.UpstreamTimeout())
			return
		}
		log.Error("generation failed", "model", req.Model, "error", err)
		Error(w, err)
		return
	}

	if len(resp.Images) == 0 {
		Error(w, pkgerrors.UpstreamErr("empty_response", "upstream returned no images", nil))
		return
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

	JSON(w, http.StatusOK, map[string]any{
		"created":    time.Now().Unix(),
		"data":       data,
		"x_provider": prov.Name(),
	})
}
