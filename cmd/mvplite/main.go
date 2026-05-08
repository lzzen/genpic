// mvplite: minimal image generation demo — single binary, OpenAI-compatible proxy.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"genpic"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// #region agent log
const agentDebugLogPath = "/home/pozenqi/workspace/genpic/.cursor/debug-8b59ed.log"

func agentDebugLog(runID, hypothesisID, location, message string, data map[string]any) {
	f, err := os.OpenFile(agentDebugLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	payload := map[string]any{
		"sessionId":    "8b59ed",
		"runId":        runID,
		"hypothesisId": hypothesisID,
		"location":     location,
		"message":      message,
		"data":         data,
		"timestamp":    time.Now().UnixMilli(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
}

// #endregion

func main() {
	// #region agent log
	agentDebugLog("run1", "H1", "cmd/mvplite/main.go:main", "startup", map[string]any{
		"goVersion": runtime.Version(),
		"note":      "method patterns require go1.22+",
	})
	// #endregion

	webRoot, err := fs.Sub(genpic.WebStatic, "web")
	if err != nil {
		// #region agent log
		agentDebugLog("run1", "H2", "cmd/mvplite/main.go:main", "fs.Sub failed", map[string]any{"error": err.Error()})
		// #endregion
		log.Fatal(err)
	}
	// #region agent log
	agentDebugLog("run1", "H2", "cmd/mvplite/main.go:main", "fs.Sub ok", map[string]any{"subdir": "web"})
	// #endregion

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/generate", handleGenerate)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	// Root static: pattern "GET /" did not match GET / in practice (404, handler never ran — see debug log H5 vs missing H3).
	// Use "/" fallback (registered after specific routes); only GET is allowed here, FileServer serves web/.
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// #region agent log
		agentDebugLog("post-fix", "H3", "cmd/mvplite/main.go:root-handler", "slash handler entered", map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
		})
		// #endregion
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		http.FileServer(http.FS(webRoot)).ServeHTTP(w, r)
	}))
	// #region agent log
	agentDebugLog("run1", "H4", "cmd/mvplite/main.go:main", "routes-registered", map[string]any{
		"rootPattern":   "/ (GET only inside handler)",
		"healthPattern": "GET /health",
		"genPattern":    "POST /api/generate",
	})
	// #endregion

	addr := ":8080"
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		addr = ":" + strings.TrimPrefix(p, ":")
	}
	log.Printf("mvplite listening on http://127.0.0.1%s", addr)
	if err := http.ListenAndServe(addr, withLogging(mux)); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		srw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(srw, r)
		dur := time.Since(start).Round(time.Millisecond)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, srw.status, dur)
		// #region agent log
		agentDebugLog("run1", "H5", "cmd/mvplite/main.go:withLogging", "request-finished", map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
			"status": srw.status,
			"durMs":  dur.Milliseconds(),
		})
		// #endregion
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

type generateReq struct {
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"api_key"`
	ModelID         string `json:"model_id"`
	Prompt          string `json:"prompt"`
	N               int    `json:"n"`
	Size            string `json:"size"`
	ResponseFormat  string `json:"response_format"` // url | b64_json
	Quality         string `json:"quality"`
}

type generateResp struct {
	Success bool            `json:"success"`
	Images  []imageOut      `json:"images,omitempty"`
	Error   *structuredErr  `json:"error,omitempty"`
}

type imageOut struct {
	URL     string `json:"url,omitempty"`
	B64JSON string `json:"b64_json,omitempty"`
}

type structuredErr struct {
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost {
		http.Error(w, `{"success":false,"error":{"message":"method not allowed"}}`, http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req generateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, generateResp{Success: false, Error: &structuredErr{Message: "invalid JSON body", Detail: err.Error()}})
		return
	}

	endpoint, err := buildGenerationsURL(req.BaseURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, generateResp{Success: false, Error: &structuredErr{Message: "invalid base_url", Detail: err.Error()}})
		return
	}
	if err := assertPublicHTTPURL(endpoint); err != nil {
		writeJSON(w, http.StatusBadRequest, generateResp{Success: false, Error: &structuredErr{Message: "base_url not allowed", Detail: err.Error()}})
		return
	}
	if strings.TrimSpace(req.APIKey) == "" {
		writeJSON(w, http.StatusBadRequest, generateResp{Success: false, Error: &structuredErr{Message: "api_key is required"}})
		return
	}
	if strings.TrimSpace(req.ModelID) == "" {
		writeJSON(w, http.StatusBadRequest, generateResp{Success: false, Error: &structuredErr{Message: "model_id is required"}})
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeJSON(w, http.StatusBadRequest, generateResp{Success: false, Error: &structuredErr{Message: "prompt is required"}})
		return
	}

	n := req.N
	if n <= 0 {
		n = 1
	}
	if n > 10 {
		n = 10
	}
	size := strings.TrimSpace(req.Size)
	if size == "" {
		size = "1024x1024"
	}
	respFmt := strings.TrimSpace(req.ResponseFormat)
	if respFmt == "" {
		respFmt = "url"
	}
	if respFmt != "url" && respFmt != "b64_json" {
		writeJSON(w, http.StatusBadRequest, generateResp{Success: false, Error: &structuredErr{Message: "response_format must be url or b64_json"}})
		return
	}

	upBody := map[string]any{
		"model":           req.ModelID,
		"prompt":          req.Prompt,
		"n":               n,
		"size":            size,
		"response_format": respFmt,
	}
	if q := strings.TrimSpace(req.Quality); q != "" {
		upBody["quality"] = q
	}
	payload, err := json.Marshal(upBody)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, generateResp{Success: false, Error: &structuredErr{Message: "marshal upstream body", Detail: err.Error()}})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, generateResp{Success: false, Error: &structuredErr{Message: "build upstream request", Detail: err.Error()}})
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(req.APIKey))

	client := &http.Client{Timeout: 125 * time.Second}
	upRes, err := client.Do(upReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, generateResp{Success: false, Error: &structuredErr{Message: "upstream request failed", Detail: err.Error()}})
		return
	}
	defer upRes.Body.Close()
	upBytes, err := io.ReadAll(io.LimitReader(upRes.Body, 32<<20))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, generateResp{Success: false, Error: &structuredErr{Message: "read upstream body", Detail: err.Error()}})
		return
	}

	if upRes.StatusCode < 200 || upRes.StatusCode >= 300 {
		msg := parseUpstreamError(upBytes, upRes.StatusCode)
		writeJSON(w, http.StatusOK, generateResp{Success: false, Error: &structuredErr{Message: msg, Detail: truncate(string(upBytes), 2000)}})
		return
	}

	var parsed struct {
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(upBytes, &parsed); err != nil {
		writeJSON(w, http.StatusBadGateway, generateResp{Success: false, Error: &structuredErr{Message: "invalid upstream JSON", Detail: err.Error()}})
		return
	}
	out := make([]imageOut, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		out = append(out, imageOut{URL: d.URL, B64JSON: d.B64JSON})
	}
	if len(out) == 0 {
		writeJSON(w, http.StatusOK, generateResp{Success: false, Error: &structuredErr{Message: "upstream returned no images", Detail: truncate(string(upBytes), 500)}})
		return
	}
	writeJSON(w, http.StatusOK, generateResp{Success: true, Images: out})
}

func buildGenerationsURL(baseURL string) (string, error) {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return "", errors.New("empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("missing host")
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path == "" || path == "/" {
		u.Path = "/v1/images/generations"
	} else if strings.HasSuffix(path, "/v1") {
		u.Path = path + "/images/generations"
	} else if strings.HasSuffix(path, "/v1/images/generations") {
		u.Path = path
	} else {
		// e.g. https://host/prefix → assume OpenAI root at /v1
		u.Path = strings.TrimSuffix(path, "/") + "/v1/images/generations"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func assertPublicHTTPURL(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("missing host")
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return errors.New("localhost not allowed")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// strict: block if we cannot resolve (prevents odd bypasses); MVP can relax by removing this
		return fmt.Errorf("dns lookup: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("resolved IP %s is not public", ip)
		}
	}
	return nil
}

func parseUpstreamError(body []byte, statusCode int) string {
	var e struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != nil && strings.TrimSpace(e.Error.Message) != "" {
		return e.Error.Message
	}
	return fmt.Sprintf("upstream HTTP %d", statusCode)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
