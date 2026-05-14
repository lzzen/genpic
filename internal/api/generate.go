package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"genpic/internal/jobstore"
	"genpic/pkg/compatctx"
	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/logger"
	"genpic/pkg/modelmap"
	"genpic/pkg/provider"
	"genpic/pkg/refimages"
)

// imageData is the per-image element in the generation response JSON and the
// async job result. Promoted to package level so runJob can populate jobstore.Image.
type imageData struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	MimeType      string `json:"mime_type,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

const maxGenerateBodyBytes = 32 << 20 // base64 reference images in JSON

// GenerateRequest is the JSON body for image generation (POST /api/generate
// and internal callers). Provider-specific extensions are optional top-level
// fields with clear documentation.
type GenerateRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	Style          string `json:"style,omitempty"`

	// Gemini-specific (native generateContent / imageConfig)
	AspectRatio    string `json:"aspect_ratio,omitempty"`
	ImageSize      string `json:"image_size,omitempty"`
	ThinkingBudget int    `json:"thinking_budget,omitempty"`

	// Wan-specific
	Watermark    *bool              `json:"watermark,omitempty"`
	ThinkingMode bool               `json:"thinking_mode,omitempty"`
	WanEditType  string             `json:"wan_edit_type,omitempty"`
	WanBboxList  []provider.WanBbox `json:"wan_bbox_list,omitempty"`

	// Reference images (图生图 / 参考); max 6; each ≤ 4 MiB decoded.
	ReferenceImages []refimages.Input `json:"reference_images,omitempty"`

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

func providerRefs(in []refimages.Input) ([]provider.ReferenceImage, error) {
	items, err := refimages.Parse(in)
	if err != nil {
		return nil, pkgerrors.BadRequest("invalid_reference", err.Error())
	}
	out := make([]provider.ReferenceImage, 0, len(items))
	for _, it := range items {
		out = append(out, provider.ReferenceImage{MIMEType: it.MIMEType, B64: it.B64})
	}
	return out, nil
}

// normalizeModelID removes a leading catalog prefix (gemini/, openai/, wan/) so
// POST /api/generate accepts either full ids
// (e.g. gemini/gemini-3.1-flash-image-preview) or upstream wire ids
// (e.g. gemini-3.1-flash-image-preview). Gemini generateContent URLs must not
// include the "gemini/" provider segment in the model path segment.
func normalizeModelID(model string) string {
	s := strings.TrimSpace(model)
	for _, p := range []string{"gemini/", "openai/", "wan/"} {
		if strings.HasPrefix(s, p) {
			return strings.TrimPrefix(s, p)
		}
	}
	return s
}

func looksLikeGeminiImageModel(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "gemini") && strings.Contains(m, "image")
}

// compatGenerateBody is the SPA body for POST /api/generate: same fields as
// GenerateRequest plus base_url and api_key (required) used for the upstream
// HTTP call; each request carries its own credentials (no server env required).
type compatGenerateBody struct {
	GenerateRequest
	BaseURL string `json:"base_url,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}

func executeImageGeneration(ctx context.Context, req GenerateRequest) (map[string]any, error) {
	log := logger.FromContext(ctx)
	req.Model = normalizeModelID(req.Model)
	if err := req.validate(); err != nil {
		return nil, err
	}
	refs, err := providerRefs(req.ReferenceImages)
	if err != nil {
		return nil, err
	}

	prov, modelInfo, ok := provider.ProviderForModel(req.Model)
	if !ok {
		msg := "model " + req.Model + " is not available"
		if looksLikeGeminiImageModel(req.Model) {
			msg += " — include base_url and api_key in the POST /api/generate JSON body (same as the web form)"
		}
		if logger.DevMode() {
			log.Warn("model_not_found",
				"requested_model", req.Model,
				"registered_models", provider.DebugRegisteredModelLines())
		}
		return nil, pkgerrors.New(http.StatusNotFound, pkgerrors.TypeNotFound, "model_not_found", msg)
	}

	upstreamWire := modelmap.Apply(getModelIDMap(), []string{modelInfo.ID, req.Model, modelInfo.UpstreamModel}, modelInfo.UpstreamModel)

	if logger.DevMode() {
		log.Info("generate_dispatch",
			"request_model", req.Model,
			"provider", prov.Name(),
			"upstream_model", modelInfo.UpstreamModel,
			"upstream_wire", upstreamWire,
			"n", req.N,
			"aspect_ratio", req.AspectRatio,
			"image_size", req.ImageSize,
			"prompt_bytes", len(req.Prompt),
		)
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
		Model:           upstreamWire,
		Prompt:          req.Prompt,
		N:               n,
		Size:            req.Size,
		Quality:         req.Quality,
		ResponseFormat:  format,
		Style:           req.Style,
		AspectRatio:     req.AspectRatio,
		ImageSize:       req.ImageSize,
		ThinkingBudget:  req.ThinkingBudget,
		ThinkingMode:    req.ThinkingMode,
		WanEditType:     req.WanEditType,
		WanBboxList:     req.WanBboxList,
		ReferenceImages: refs,
	}

	timeout := time.Duration(modelInfo.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	if n > 1 {
		timeout *= time.Duration(n)
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

	data := make([]imageData, 0, len(resp.Images))
	for _, img := range resp.Images {
		data = append(data, imageData{
			URL:           img.URL,
			B64JSON:       img.B64JSON,
			MimeType:      img.MIMEType,
			RevisedPrompt: img.RevisedPrompt,
		})
	}

	return map[string]any{
		"created":    time.Now().Unix(),
		"data":       data,
		"x_provider": prov.Name(),
	}, nil
}

// jobFromGenerateRequest builds a queued job record (provider name resolved when known).
func jobFromGenerateRequest(req GenerateRequest, owner jobstore.OwnerScope) *jobstore.Job {
	normalised := normalizeModelID(req.Model)
	providerName := ""
	if prov, _, ok := provider.ProviderForModel(normalised); ok {
		providerName = prov.Name()
	}
	uid := owner.UserID
	sid := owner.SessionID
	if uid != "" {
		sid = ""
	}
	n := req.N
	if n == 0 {
		n = 1
	}
	return &jobstore.Job{
		Model:     req.Model,
		Provider:  providerName,
		Prompt:    req.Prompt,
		Status:    jobstore.StatusQueued,
		UserID:    uid,
		SessionID: sid,
		Params: &jobstore.JobParams{
			Model:       req.Model,
			AspectRatio: req.AspectRatio,
			ImageSize:   req.ImageSize,
			Size:        req.Size,
			N:           n,
			Quality:     req.Quality,
			Style:       req.Style,
		},
	}
}

// finalizeJobResult updates the job to succeeded or failed after generation.
func finalizeJobResult(jobID string, out map[string]any, genErr error) {
	finished := time.Now()
	if genErr != nil {
		code := "generation_error"
		msg := genErr.Error()
		jobStoreInstance.Update(jobID, func(j *jobstore.Job) {
			j.Status = jobstore.StatusFailed
			j.ErrorCode = code
			j.ErrorMsg = msg
			j.FinishedAt = finished
		})
		return
	}
	var images []jobstore.Image
	if data, ok := out["data"].([]imageData); ok {
		for _, d := range data {
			images = append(images, jobstore.SanitizeImageForStorage(jobstore.Image{
				URL:           d.URL,
				B64JSON:       d.B64JSON,
				MIMEType:      d.MimeType,
				RevisedPrompt: d.RevisedPrompt,
			}))
		}
	}
	jobStoreInstance.Update(jobID, func(j *jobstore.Job) {
		j.Status = jobstore.StatusSucceeded
		j.Prompt = jobstore.StripThoughtSignatureFromJSON(j.Prompt)
		j.Images = images
		j.FinishedAt = finished
	})
}

// runJob executes image generation for the given job in the background.
func runJob(ctx context.Context, jobID string, req GenerateRequest) {
	now := time.Now()
	jobStoreInstance.Update(jobID, func(j *jobstore.Job) {
		j.Status = jobstore.StatusRunning
		j.StartedAt = now
	})

	out, err := executeImageGeneration(ctx, req)
	if err == nil {
		if e := materializeJobImages(jobID, out); e != nil {
			err = pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "artifact_write", e.Error())
		}
	}
	finalizeJobResult(jobID, out, err)
}

// HandleCompatGenerate serves POST /api/generate for the embedded SPA.
// base_url and api_key in the JSON body are required and are sent to the third-party
// upstream as-is; the terminal running genpic prints the full upstream request
// and response JSON to stderr for each call (large base64 and thoughtSignature
// strings are replaced with placeholders).
//
// When a job store is configured (cmd/genpic), the handler returns 202 Accepted
// with a job record immediately and runs generation in the background; clients
// poll GET /jobs/{id}. Without a job store (e.g. tests), it waits and returns 200.
func HandleCompatGenerate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxGenerateBodyBytes)

	var body compatGenerateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body: "+err.Error()))
		return
	}

	base := strings.TrimSpace(body.BaseURL)
	key := strings.TrimSpace(body.APIKey)
	if base == "" || key == "" {
		Error(w, pkgerrors.BadRequest("missing_field", "base_url and api_key are required in POST /api/generate"))
		return
	}

	ov := &compatctx.Override{
		BaseURL:     base,
		APIKey:      key,
		LogToStderr: true,
	}
	ctx := compatctx.With(r.Context(), ov)

	// Full platform (cmd/genpic): always wire a job store — enqueue async and
	// return 202; clients poll GET /jobs/{id}. compatctx must be attached to
	// a detached context so closing the HTTP connection does not cancel upstream.
	if jobStoreInstance != nil {
		owner := callerScopeFromRequest(r)
		job := jobFromGenerateRequest(body.GenerateRequest, owner)
		id, err := jobStoreInstance.Create(job)
		if err != nil {
			Error(w, pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "job_create", err.Error()))
			return
		}
		bgCtx := compatctx.With(context.Background(), ov)
		go runJob(bgCtx, id, body.GenerateRequest)
		j, _ := jobStoreInstance.Get(id)
		JSON(w, http.StatusAccepted, toJobResponseOwner(j))
		return
	}

	out, err := executeImageGeneration(ctx, body.GenerateRequest)
	if err != nil {
		Error(w, err)
		return
	}
	JSON(w, http.StatusOK, out)
}
