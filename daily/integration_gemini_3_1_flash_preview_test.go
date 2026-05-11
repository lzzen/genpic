//go:build integration

package daily_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	modelFullID   = "gemini/gemini-3.1-flash-image-preview"
	modelUpstream = "gemini-3.1-flash-image-preview"
	smokePrompt   = "Daily smoke: one blue circle on white background, flat minimal vector icon."
)

func joinV1ImagesGenerations(baseURL string) (string, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return "", fmt.Errorf("empty base URL")
	}
	u, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme")
	}
	if !strings.HasSuffix(u.Path, "/v1") {
		u.Path = strings.TrimRight(u.Path, "/") + "/v1"
	}
	u.Path += "/images/generations"
	return u.String(), nil
}

// TestGemini31FlashImagePreview calls either POST /api/generate on GENPIC_DAILY_MVPLITE_URL
// (e.g. genpic or MVP Lite) or the aggregator POST /v1/images/generations.
func TestGemini31FlashImagePreview(t *testing.T) {
	upstreamBase := strings.TrimSpace(os.Getenv("GENPIC_DAILY_UPSTREAM_BASE_URL"))
	key := strings.TrimSpace(os.Getenv("GENPIC_DAILY_UPSTREAM_API_KEY"))
	if upstreamBase == "" || key == "" {
		t.Skip("set GENPIC_DAILY_UPSTREAM_BASE_URL and GENPIC_DAILY_UPSTREAM_API_KEY")
	}

	mv := strings.TrimRight(strings.TrimSpace(os.Getenv("GENPIC_DAILY_MVPLITE_URL")), "/")

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	var (
		reqURL  string
		payload []byte
		err     error
	)
	if mv != "" {
		reqURL = mv + "/api/generate"
		payload, err = json.Marshal(map[string]any{
			"base_url":        upstreamBase,
			"api_key":         key,
			"model":           modelFullID,
			"prompt":          smokePrompt,
			"n":               1,
			"aspect_ratio":    "1:1",
			"image_size":      "512",
			"response_format": "b64_json",
		})
		if err != nil {
			t.Fatal(err)
		}
	} else {
		reqURL, err = joinV1ImagesGenerations(upstreamBase)
		if err != nil {
			t.Fatal(err)
		}
		payload, err = json.Marshal(map[string]any{
			"model":           modelUpstream,
			"prompt":          smokePrompt,
			"n":               1,
			"aspect_ratio":    "1:1",
			"image_size":      "512",
			"response_format": "b64_json",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if mv == "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP %d body=%s", resp.StatusCode, truncate(string(raw), 2000))
	}

	if mv != "" {
		var env struct {
			Images []struct {
				B64JSON string `json:"b64_json"`
				URL     string `json:"url"`
			} `json:"images"`
			Data []struct {
				B64JSON string `json:"b64_json"`
				URL     string `json:"url"`
			} `json:"data"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		if env.Error != nil && env.Error.Message != "" {
			t.Fatal("compat error: ", env.Error.Message)
		}
		slots := env.Images
		if len(slots) == 0 {
			slots = env.Data
		}
		if len(slots) == 0 {
			t.Fatal("no images in /api/generate response (expected images[] or data[])")
		}
		img := slots[0]
		if img.B64JSON == "" && img.URL == "" {
			t.Fatal("first image has neither b64_json nor url")
		}
		return
	}

	var openai struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &openai); err != nil {
		t.Fatal(err)
	}
	if openai.Error != nil && openai.Error.Message != "" {
		t.Fatal("upstream error: ", openai.Error.Message)
	}
	if len(openai.Data) == 0 {
		t.Fatal("no data[] from upstream")
	}
	d := openai.Data[0]
	if d.B64JSON == "" && d.URL == "" {
		t.Fatal("first datum has neither b64_json nor url")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
