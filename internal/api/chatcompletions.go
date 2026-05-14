// Package api — HandleChatCompletions serves POST /v1/chat/completions.
//
// This endpoint lets OpenAI-compatible clients (Cherry Studio, AI as Workspace,
// Lobe Chat, raw OpenAI SDK, …) generate images through a standard Chat API call.
//
// # Routing
//
// When `model` identifies an image-generation model (openai/*, gemini/*, wan/*),
// the handler builds a [GenerateRequest] from the last user message and calls
// [executeImageGeneration] — the same code path as POST /api/generate — so all
// three provider adapters work transparently.
//
// # Authentication / upstream credentials
//
//   - api_key  — taken from the `Authorization: Bearer <token>` header.
//     The token is forwarded as the upstream api_key via [compatctx.Override].
//   - base_url — resolution order:
//     1. `X-Base-Url` request header (explicit override).
//     2. Server-configured credentials in config.yaml (Mode A: server-side key).
//     If neither resolves at runtime the provider returns 400 "upstream_credentials".
//
// # Request
//
// Standard OpenAI chat completions body. Notable extensions:
//   - `aspect_ratio`    — Gemini imageConfig.aspectRatio (e.g. "16:9").
//   - `image_size`      — Gemini imageConfig.imageSize   (e.g. "1K", "2K").
//   - `thinking_budget` — Gemini thinkingConfig.thinkingBudget.
//   - `size`            — OpenAI / Wan size string.
//   - `quality`         — OpenAI quality hint.
//
// Reference images in the last user message are extracted from parts with
// `type:"image_url"` whose `url` is a data URI (`data:<mime>;base64,<b64>`).
//
// # Response
//
// Synchronous OpenAI chat completions shape. `choices[0].message.content` is a
// multimodal array: one `image_url` part per generated image containing a
// `data:` URI. A leading text part "Here is your generated image:" is prepended
// so plain-text renderers show something useful.
//
// Stream mode (`"stream":true`) is not supported; the handler returns 400.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"genpic/pkg/compatctx"
	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/refimages"
)

// ── Request types ─────────────────────────────────────────────────────────────

// chatCompletionsRequest is the JSON body for POST /v1/chat/completions.
type chatCompletionsRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	N        int           `json:"n,omitempty"`
	Stream   bool          `json:"stream,omitempty"`

	// Genpic image-generation extensions (ignored by standard chat clients).
	AspectRatio    string `json:"aspect_ratio,omitempty"`
	ImageSize      string `json:"image_size,omitempty"`
	ThinkingBudget int    `json:"thinking_budget,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
}

// chatMessage is one message in the chat completions request.
// Content may be a bare string or a JSON array of content parts.
type chatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// chatContentPart is a single item in the multimodal content array.
type chatContentPart struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *chatImageURLRef `json:"image_url,omitempty"`
}

type chatImageURLRef struct {
	URL string `json:"url"`
}

// ── Response types ────────────────────────────────────────────────────────────

// chatCompletionsResponse is the synchronous OpenAI chat completions response.
type chatCompletionsResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []chatChoice       `json:"choices"`
	Usage   chatUsage          `json:"usage"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatOutMsg  `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type chatOutMsg struct {
	Role    string            `json:"role"`
	Content []chatContentPart `json:"content"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ── Handler ───────────────────────────────────────────────────────────────────

// HandleChatCompletions serves POST /v1/chat/completions for OpenAI-compatible
// clients. It translates the chat request into an image generation call and
// returns the results synchronously in the standard chat completions shape.
func HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxGenerateBodyBytes)

	var req chatCompletionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, pkgerrors.BadRequest("parse_error", "could not parse request body: "+err.Error()))
		return
	}

	if req.Stream {
		Error(w, pkgerrors.BadRequest("stream_not_supported",
			"stream:true is not supported for image generation; set stream:false or omit the field"))
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		Error(w, pkgerrors.BadRequest("missing_field", "model is required"))
		return
	}
	if len(req.Messages) == 0 {
		Error(w, pkgerrors.BadRequest("missing_field", "messages must not be empty"))
		return
	}

	// Build upstream credentials from request context.
	apiKey := extractBearerToken(r.Header.Get("Authorization"))
	baseURL := strings.TrimSpace(r.Header.Get("X-Base-Url"))

	// Attach per-request credential override when at least one value is present.
	// Provider adapters fall back to server-side config when the override is empty.
	if apiKey != "" || baseURL != "" {
		ov := &compatctx.Override{
			BaseURL:     baseURL,
			APIKey:      apiKey,
			LogToStderr: true,
		}
		r = r.WithContext(compatctx.With(r.Context(), ov))
	}

	// Extract prompt and reference images from the last user message.
	prompt, refs, err := extractChatPrompt(req.Messages)
	if err != nil {
		Error(w, pkgerrors.BadRequest("invalid_messages", err.Error()))
		return
	}
	if strings.TrimSpace(prompt) == "" {
		Error(w, pkgerrors.BadRequest("missing_field", "no user message text found in messages"))
		return
	}

	n := req.N
	if n == 0 {
		n = 1
	}

	genReq := GenerateRequest{
		Model:          req.Model,
		Prompt:         prompt,
		N:              n,
		Size:           req.Size,
		Quality:        req.Quality,
		AspectRatio:    req.AspectRatio,
		ImageSize:      req.ImageSize,
		ThinkingBudget: req.ThinkingBudget,
		ReferenceImages: refs,
	}

	out, err := executeImageGeneration(r.Context(), genReq)
	if err != nil {
		Error(w, err)
		return
	}

	resp := buildChatCompletionsResponse(req.Model, out)
	JSON(w, http.StatusOK, resp)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// extractBearerToken returns the token part of "Bearer <token>", or "".
func extractBearerToken(authHeader string) string {
	v := strings.TrimSpace(authHeader)
	lower := strings.ToLower(v)
	if strings.HasPrefix(lower, "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	return ""
}

// extractChatPrompt finds the last user message and extracts the text prompt
// and any reference images from its content. Content can be:
//   - a bare JSON string: the entire string is the prompt.
//   - a JSON array of parts: text parts are joined; image_url data URIs become refs.
func extractChatPrompt(messages []chatMessage) (prompt string, refs []refimages.Input, err error) {
	// Find the last user message.
	var lastUser *chatMessage
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			lastUser = &messages[i]
			break
		}
	}
	if lastUser == nil {
		// Fall back to the last message of any role.
		lastUser = &messages[len(messages)-1]
	}

	raw := lastUser.Content
	if len(raw) == 0 {
		return "", nil, nil
	}

	// Try bare string first.
	if raw[0] == '"' {
		var s string
		if e := json.Unmarshal(raw, &s); e == nil {
			return s, nil, nil
		}
	}

	// Try array of parts.
	if raw[0] == '[' {
		var parts []chatContentPart
		if e := json.Unmarshal(raw, &parts); e != nil {
			return "", nil, fmt.Errorf("could not parse content parts: %w", e)
		}
		var textParts []string
		for _, p := range parts {
			switch p.Type {
			case "text":
				if strings.TrimSpace(p.Text) != "" {
					textParts = append(textParts, p.Text)
				}
			case "image_url":
				if p.ImageURL == nil || p.ImageURL.URL == "" {
					continue
				}
				ri, parseErr := dataURIToRefImage(p.ImageURL.URL)
				if parseErr != nil {
					// Non-data URIs (remote URLs) are not usable as reference images; skip.
					continue
				}
				refs = append(refs, ri)
			}
		}
		return strings.Join(textParts, "\n"), refs, nil
	}

	// Fallback: treat raw JSON value as a string representation.
	return string(raw), nil, nil
}

// dataURIToRefImage converts a data URI ("data:<mime>;base64,<b64>") into a
// refimages.Input suitable for provider adapters. Returns an error for any other
// URI scheme (remote URLs, etc.).
func dataURIToRefImage(uri string) (refimages.Input, error) {
	if !strings.HasPrefix(uri, "data:") {
		return refimages.Input{}, fmt.Errorf("not a data URI")
	}
	// data:<mediatype>[;base64],<data>
	rest := uri[5:] // strip "data:"
	commaIdx := strings.IndexByte(rest, ',')
	if commaIdx < 0 {
		return refimages.Input{}, fmt.Errorf("malformed data URI: missing comma")
	}
	meta := rest[:commaIdx]
	b64Data := rest[commaIdx+1:]

	mime := ""
	parts := strings.Split(meta, ";")
	if len(parts) > 0 {
		mime = strings.TrimSpace(parts[0])
	}
	if mime == "" {
		mime = "image/png"
	}

	return refimages.Input{
		MIMEType: mime,
		B64JSON:  b64Data,
	}, nil
}

// buildChatCompletionsResponse wraps the image generation result into the
// OpenAI chat completions wire shape expected by standard API clients.
func buildChatCompletionsResponse(model string, out map[string]any) chatCompletionsResponse {
	var contentParts []chatContentPart

	// Introductory text part so plain-text renderers show something meaningful.
	contentParts = append(contentParts, chatContentPart{
		Type: "text",
		Text: "Here is your generated image:",
	})

	if data, ok := out["data"].([]imageData); ok {
		for _, img := range data {
			var dataURL string
			switch {
			case img.B64JSON != "":
				mime := img.MimeType
				if mime == "" {
					mime = "image/png"
				}
				dataURL = "data:" + mime + ";base64," + img.B64JSON
			case img.URL != "":
				dataURL = img.URL
			}
			if dataURL == "" {
				continue
			}
			contentParts = append(contentParts, chatContentPart{
				Type:     "image_url",
				ImageURL: &chatImageURLRef{URL: dataURL},
			})
		}
	}

	return chatCompletionsResponse{
		ID:      fmt.Sprintf("chatcmpl-genpic-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []chatChoice{
			{
				Index: 0,
				Message: chatOutMsg{
					Role:    "assistant",
					Content: contentParts,
				},
				FinishReason: "stop",
			},
		},
		Usage: chatUsage{},
	}
}
