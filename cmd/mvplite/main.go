// Package main is the MVP Lite entry point: a single Go binary that serves
// a native HTML/JS frontend and proxies image-generation requests to any
// OpenAI-compatible upstream (NewAPI aggregator or direct provider).
//
// Design constraints (see docs/genpic_生图应用设计.plan.md §2.2):
//   - One small YAML dependency (gopkg.in/yaml.v3) for config.yaml; otherwise stdlib.
//   - One binary: static assets embedded via the root genpic package.
//   - Upstream base_url and api_key are supplied per-request from the browser form;
//     default base URL may come from config.yaml (GET /api/public-config).
//   - The "/ fallback" routing pattern is used instead of "GET /" because
//     Go 1.22 ServeMux does not route GET / to a handler registered as "GET /"
//     when method-qualified patterns are mixed with catch-all patterns.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	genpic "genpic"
	"genpic/pkg/mvpconfig"
)

// GenerateRequest is the JSON body posted by the browser to /api/generate.
// It passes the upstream coordinates together with generation parameters so
// the MVP Lite server can forward a well-formed OpenAI-compatible request.
type GenerateRequest struct {
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"` // "url" or "b64_json"
	Style          string `json:"style,omitempty"`
}

// GenerateResponse is the JSON body returned to the browser.
type GenerateResponse struct {
	Images []ImageResult `json:"images"`
}

// ImageResult holds a single generated image, either as a URL or base64 data.
type ImageResult struct {
	URL     string `json:"url,omitempty"`
	B64JSON string `json:"b64_json,omitempty"`
	// RevisedPrompt echoes the upstream-revised prompt when present.
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// APIError is the error envelope returned to the browser on failure.
type APIError struct {
	Error APIErrorBody `json:"error"`
}

// APIErrorBody matches OpenAI's error body shape so clients expecting that
// format receive something familiar.
type APIErrorBody struct {
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// upstreamImageResponse is a partial decode of the OpenAI images/generations
// response, covering both url and b64_json response formats.
type upstreamImageResponse struct {
	Data []struct {
		URL           string `json:"url"`
		B64JSON       string `json:"b64_json"`
		RevisedPrompt string `json:"revised_prompt"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// mvpState holds values exposed to the static frontend (no secrets).
var mvpState struct {
	DefaultBaseURL string
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config.yaml (mvp_lite.default_base_url, optional mvp_lite.port for listen)")
	flag.Parse()

	cfg, err := mvpconfig.Read(*configPath)
	if err != nil {
		log.Fatalf("mvplite: config: %v", err)
	}
	if !cfg.Found {
		log.Printf("mvplite: config file %q not found; default Base URL empty until you add mvp_lite.default_base_url", *configPath)
	}
	mvpState.DefaultBaseURL = cfg.DefaultBaseURL

	port := strings.TrimSpace(cfg.MvpLitePort)
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		port = p
	}
	if port == "" {
		port = "8080"
	}

	webRoot, err := fs.Sub(genpic.WebStatic, "web")
	if err != nil {
		log.Fatalf("failed to open embedded web root: %v", err)
	}

	mux := http.NewServeMux()

	// Specific routes must be registered before the catch-all "/".
	mux.HandleFunc("GET /api/public-config", handlePublicConfig)
	mux.HandleFunc("POST /api/generate", handleGenerate)
	mux.HandleFunc("GET /health", handleHealth)

	// "/" is a catch-all; we enforce GET inside the handler.
	// Using "/" instead of "GET /" avoids a Go 1.22 ServeMux routing quirk
	// where "GET /" does not fire for requests to the root path when other
	// method-qualified patterns are also registered.
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		http.FileServer(http.FS(webRoot)).ServeHTTP(w, r)
	}))

	addr := ":" + port
	log.Printf("genpic mvp-lite listening on %s", addr)
	if err := http.ListenAndServe(addr, withLogging(mux)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"status":"ok"}`)
}

// handlePublicConfig serves non-secret defaults for the browser (default Base URL).
func handlePublicConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"default_base_url": mvpState.DefaultBaseURL,
	})
}

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB request cap

	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not parse request body: "+err.Error())
		return
	}
	if err := validateRequest(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	upstreamURL, err := buildGenerationsURL(req.BaseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_base_url", err.Error())
		return
	}

	upstreamBody, err := buildUpstreamBody(&req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "upstream_timeout", "upstream did not respond in time")
			return
		}
		writeError(w, http.StatusBadGateway, "upstream_error", "upstream request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20)) // 32 MiB cap
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_read_error", err.Error())
		return
	}

	if resp.StatusCode != http.StatusOK {
		msg := parseUpstreamError(raw, resp.StatusCode)
		writeError(w, resp.StatusCode, "upstream_error", msg)
		return
	}

	var upstreamResp upstreamImageResponse
	if err := json.Unmarshal(raw, &upstreamResp); err != nil {
		writeError(w, http.StatusBadGateway, "upstream_parse_error", "could not parse upstream response")
		return
	}

	if upstreamResp.Error != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", upstreamResp.Error.Message)
		return
	}

	if len(upstreamResp.Data) == 0 {
		writeError(w, http.StatusBadGateway, "upstream_empty", "upstream returned no images")
		return
	}

	results := make([]ImageResult, 0, len(upstreamResp.Data))
	for _, d := range upstreamResp.Data {
		results = append(results, ImageResult{
			URL:           d.URL,
			B64JSON:       d.B64JSON,
			RevisedPrompt: d.RevisedPrompt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(GenerateResponse{Images: results})
}

// buildGenerationsURL constructs the OpenAI-compatible generations endpoint
// from the caller-supplied base URL. It tolerates trailing slashes and an
// already-present /v1 suffix.
func buildGenerationsURL(baseURL string) (string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return "", fmt.Errorf("base_url is required")
	}
	u, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return "", fmt.Errorf("base_url is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("base_url must use http or https scheme")
	}
	if !strings.HasSuffix(u.Path, "/v1") {
		u.Path = strings.TrimRight(u.Path, "/") + "/v1"
	}
	u.Path += "/images/generations"
	return u.String(), nil
}

// upstreamModelForOpenAIImages strips the internal catalog prefix (openai/,
// gemini/, wan/) from model IDs sent by the web UI so the value matches what
// POST /v1/images/generations expects (e.g. gpt-image-2, not openai/gpt-image-2).
func upstreamModelForOpenAIImages(model string) string {
	model = strings.TrimSpace(model)
	for _, prefix := range []string{"openai/", "gemini/", "wan/"} {
		if strings.HasPrefix(model, prefix) {
			return strings.TrimPrefix(model, prefix)
		}
	}
	return model
}

// buildUpstreamBody serialises the generation parameters into the OpenAI
// images/generations request body, omitting zero-value optional fields.
func buildUpstreamBody(req *GenerateRequest) ([]byte, error) {
	body := map[string]any{
		"model":  upstreamModelForOpenAIImages(req.Model),
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

// validateRequest checks mandatory fields before touching the network.
func validateRequest(req *GenerateRequest) error {
	if strings.TrimSpace(req.BaseURL) == "" {
		return errors.New("base_url is required")
	}
	if strings.TrimSpace(req.APIKey) == "" {
		return errors.New("api_key is required")
	}
	if strings.TrimSpace(req.Model) == "" {
		return errors.New("model is required")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return errors.New("prompt is required")
	}
	if len(req.Prompt) > 32000 {
		return errors.New("prompt exceeds 32 000 character limit")
	}
	return nil
}

// parseUpstreamError extracts a human-readable message from a non-200 upstream
// response body. Falls back to a generic message when the body is unparseable.
func parseUpstreamError(body []byte, statusCode int) string {
	var env struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error != nil && env.Error.Message != "" {
		return env.Error.Message
	}
	return fmt.Sprintf("upstream returned HTTP %d", statusCode)
}

// writeError writes an OpenAI-shaped error JSON response.
func writeError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIError{
		Error: APIErrorBody{Type: errType, Message: message},
	})
}

// statusRecorder wraps http.ResponseWriter to capture the written status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// withLogging wraps a handler with structured access logging.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		srw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(srw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, srw.status, time.Since(start).Round(time.Millisecond))
	})
}
