// Package openai implements the provider.Provider interface for OpenAI-compatible
// image generation (gpt-image-2 and any other model routed through the OpenAI
// images/generations endpoint).
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"genpic/pkg/compatctx"
	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/httpclient"
	"genpic/pkg/logger"
	"genpic/pkg/openaiimg"
	"genpic/pkg/provider"
)

// Config holds default upstream connection details (optional when using POST /api/generate with JSON base_url + api_key).
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
					provider.CapImageToImage,
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

	baseURL, apiKey, trace := compatctx.Resolve(ctx, p.cfg.BaseURL, p.cfg.APIKey)
	if baseURL == "" || apiKey == "" {
		return nil, pkgerrors.BadRequest("upstream_credentials", "set base_url and api_key in the POST /api/generate JSON body.")
	}

	var (
		url     string
		headers map[string]string
		body    []byte
		err     error
	)

	if len(req.ReferenceImages) > 0 {
		url = strings.TrimRight(baseURL, "/") + "/v1/images/edits"
		parts, err := referencePartsOpenAI(req.ReferenceImages)
		if err != nil {
			return nil, pkgerrors.Wrap(http.StatusBadRequest, pkgerrors.TypeValidation, "reference_images", "invalid reference_images", err)
		}
		extra := map[string]string{}
		if req.N > 0 {
			extra["n"] = strconv.Itoa(req.N)
		}
		if req.Size != "" {
			extra["size"] = req.Size
		}
		if req.Quality != "" {
			extra["quality"] = req.Quality
		}
		format := req.ResponseFormat
		if format == "" {
			format = "url"
		}
		extra["response_format"] = format
		var ct string
		body, ct, err = openaiimg.BuildEditsMultipart(req.Model, req.Prompt, parts, extra)
		if err != nil {
			return nil, pkgerrors.Wrap(http.StatusBadRequest, pkgerrors.TypeValidation, "build_edits", "could not build multipart edits request", err)
		}
		headers = map[string]string{
			"Content-Type":  ct,
			"Authorization": "Bearer " + apiKey,
		}
	} else {
		url = strings.TrimRight(baseURL, "/") + "/v1/images/generations"
		body, err = buildRequest(req)
		if err != nil {
			return nil, pkgerrors.Wrap(http.StatusBadRequest, pkgerrors.TypeValidation, "build_request", "could not build upstream request", err)
		}
		headers = map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + apiKey,
		}
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
		reqLog := body
		if len(req.ReferenceImages) > 0 {
			reqLog = []byte(`"(multipart/form-data; image bytes omitted from log)"`)
		}
		compatctx.LogStderrRoundTrip("openai", http.MethodPost, url, headers, reqLog, status, raw)
	}
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		msg := extractError(raw, resp.StatusCode)
		return nil, pkgerrors.UpstreamErr("upstream_http_error", msg, nil)
	}

	images, err := parseOpenAIImagesResponse(raw)
	if err != nil {
		return nil, err
	}

	latency := time.Since(start)
	log.Info("openai generate ok", "model", req.Model, "n", len(images), "latency_ms", latency.Milliseconds())

	return &provider.GenerateResponse{
		Images:  images,
		Latency: latency,
	}, nil
}

func referencePartsOpenAI(refs []provider.ReferenceImage) ([]openaiimg.ImagePart, error) {
	out := make([]openaiimg.ImagePart, 0, len(refs))
	for i, ref := range refs {
		b64 := strings.TrimSpace(ref.B64)
		if b64 == "" {
			return nil, fmt.Errorf("reference %d: empty b64_json", i)
		}
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("reference %d: %w", i, err)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("reference %d: decoded empty", i)
		}
		if len(data) > 4<<20 {
			return nil, fmt.Errorf("reference %d: image exceeds 4 MiB", i)
		}
		mt := strings.TrimSpace(ref.MIMEType)
		if mt == "" {
			mt = "image/png"
		}
		ext := "png"
		switch strings.ToLower(mt) {
		case "image/jpeg":
			ext = "jpg"
		case "image/webp":
			ext = "webp"
		case "image/gif":
			ext = "gif"
		case "image/png":
		default:
			return nil, fmt.Errorf("reference %d: unsupported mime_type %q", i, mt)
		}
		out = append(out, openaiimg.ImagePart{
			Filename: fmt.Sprintf("ref%d.%s", i, ext),
			MIMEType: mt,
			Data:     data,
		})
	}
	return out, nil
}

// openAIImageSlot is one element of OpenAI images API `data[]` (or equivalent in aggregators).
type openAIImageSlot struct {
	URL           string `json:"url"`
	B64JSON       string `json:"b64_json"`
	RevisedPrompt string `json:"revised_prompt"`
}

func parseOpenAIImagesResponse(raw []byte) ([]provider.Image, error) {
	var head struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, pkgerrors.UpstreamErr("parse_error", "could not parse upstream response", err)
	}
	if strings.TrimSpace(head.Code) != "" {
		return parseOpenAIAggregatorEnvelope(head.Code, head.Message, head.Data)
	}
	return parseOpenAIDirectImagesResponse(raw)
}

// parseOpenAIDirectImagesResponse handles the official OpenAI images JSON: { "data": [...], "error": ... }.
func parseOpenAIDirectImagesResponse(raw []byte) ([]provider.Image, error) {
	var out struct {
		Data  []openAIImageSlot `json:"data"`
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
	if len(out.Data) == 0 {
		return nil, pkgerrors.UpstreamErr("empty_response", "upstream returned no images", nil)
	}
	return openAIImageSlotsToImages(out.Data), nil
}

// parseOpenAIAggregatorEnvelope handles task-style wrappers such as openapi.wang:
// { "code":"success", "data":{ "data":{ "data":[{ "url":"...", "b64_json":"" }] }, "status":"SUCCESS" } }.
func parseOpenAIAggregatorEnvelope(code, message string, outerData json.RawMessage) ([]provider.Image, error) {
	c := strings.TrimSpace(code)
	if !strings.EqualFold(c, "success") {
		msg := strings.TrimSpace(message)
		if msg == "" {
			msg = "aggregator returned code " + c
		}
		return nil, pkgerrors.UpstreamErr("aggregator_error", msg, nil)
	}
	var mid struct {
		Inner  json.RawMessage `json:"data"`
		Status string          `json:"status"`
	}
	if err := json.Unmarshal(outerData, &mid); err != nil {
		return nil, pkgerrors.UpstreamErr("parse_error", "could not parse aggregator task envelope", err)
	}
	if st := strings.TrimSpace(mid.Status); st != "" && !strings.EqualFold(st, "SUCCESS") {
		return nil, pkgerrors.UpstreamErr("aggregator_status", "task status: "+st, nil)
	}
	return parseOpenAIImagesInnerPayload(mid.Inner)
}

func parseOpenAIImagesInnerPayload(blob json.RawMessage) ([]provider.Image, error) {
	blob = bytes.TrimSpace(blob)
	if len(blob) == 0 {
		return nil, pkgerrors.UpstreamErr("empty_response", "upstream returned no images", nil)
	}
	if blob[0] == '[' {
		var slots []openAIImageSlot
		if err := json.Unmarshal(blob, &slots); err != nil {
			return nil, pkgerrors.UpstreamErr("parse_error", "could not parse image array payload", err)
		}
		if len(slots) == 0 {
			return nil, pkgerrors.UpstreamErr("empty_response", "upstream returned no images", nil)
		}
		return openAIImageSlotsToImages(slots), nil
	}
	var obj struct {
		Data []openAIImageSlot `json:"data"`
	}
	if err := json.Unmarshal(blob, &obj); err != nil {
		return nil, pkgerrors.UpstreamErr("parse_error", "could not parse nested images object", err)
	}
	if len(obj.Data) == 0 {
		return nil, pkgerrors.UpstreamErr("empty_response", "upstream returned no images", nil)
	}
	return openAIImageSlotsToImages(obj.Data), nil
}

func openAIImageSlotsToImages(slots []openAIImageSlot) []provider.Image {
	images := make([]provider.Image, 0, len(slots))
	for _, d := range slots {
		images = append(images, provider.Image{
			URL:           d.URL,
			B64JSON:       d.B64JSON,
			RevisedPrompt: d.RevisedPrompt,
		})
	}
	return images
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
	var buf bytes.Buffer
	_ = json.Compact(&buf, body)
	if buf.Len() > 200 {
		buf.Truncate(200)
	}
	return "upstream HTTP " + http.StatusText(statusCode) + ": " + buf.String()
}
