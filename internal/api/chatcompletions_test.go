package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"genpic/pkg/provider"
)

func TestExtractBearerToken(t *testing.T) {
	tests := []struct{ header, want string }{
		{"Bearer sk-abc123", "sk-abc123"},
		{"bearer SK-ABC", "SK-ABC"},
		{"Bearer   trimmed  ", "trimmed"},
		{"", ""},
		{"Basic dXNlcjpwYXNz", ""},
		{"sk-nobearer", ""},
	}
	for _, tt := range tests {
		if got := extractBearerToken(tt.header); got != tt.want {
			t.Errorf("extractBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestExtractChatPrompt_String(t *testing.T) {
	msgs := []chatMessage{
		{Role: "user", Content: json.RawMessage(`"generate a cat"`)},
	}
	prompt, refs, err := extractChatPrompt(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "generate a cat" {
		t.Errorf("want %q, got %q", "generate a cat", prompt)
	}
	if len(refs) != 0 {
		t.Errorf("expected no refs, got %d", len(refs))
	}
}

func TestExtractChatPrompt_Array(t *testing.T) {
	content := json.RawMessage(`[{"type":"text","text":"a sunny field"},{"type":"text","text":"with flowers"}]`)
	msgs := []chatMessage{
		{Role: "user", Content: content},
	}
	prompt, _, err := extractChatPrompt(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "a sunny field") || !strings.Contains(prompt, "with flowers") {
		t.Errorf("unexpected prompt: %q", prompt)
	}
}

func TestExtractChatPrompt_ImageURLDataURI(t *testing.T) {
	// A tiny 1x1 transparent PNG as base64 (well under size limit).
	tinyB64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	dataURI := "data:image/png;base64," + tinyB64
	content, _ := json.Marshal([]chatContentPart{
		{Type: "text", Text: "edit this image"},
		{Type: "image_url", ImageURL: &chatImageURLRef{URL: dataURI}},
	})
	msgs := []chatMessage{{Role: "user", Content: content}}

	prompt, refs, err := extractChatPrompt(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "edit this image" {
		t.Errorf("want %q, got %q", "edit this image", prompt)
	}
	if len(refs) != 1 {
		t.Errorf("expected 1 ref image, got %d", len(refs))
	}
	if refs[0].MIMEType != "image/png" {
		t.Errorf("unexpected mime: %q", refs[0].MIMEType)
	}
}

func TestHandleChatCompletions_MissingModel(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandleChatCompletions(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleChatCompletions_StreamNotSupported(t *testing.T) {
	body := `{"model":"gemini/gemini-2.5-flash-image","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HandleChatCompletions(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleChatCompletions_OK(t *testing.T) {
	// Use a unique provider name to avoid conflicts with other tests in the package.
	const fakeProviderName = "chattest-openai"
	fake := &provider.Fake{
		ProviderName: fakeProviderName,
		ModelList: []provider.ModelInfo{
			{ID: fakeProviderName + "/gpt-image-2", UpstreamModel: "gpt-image-2", TimeoutSeconds: 30},
		},
		Response: &provider.GenerateResponse{
			Images: []provider.Image{{B64JSON: "dGVzdA==", MIMEType: "image/png"}},
		},
	}
	provider.Register(fake)
	t.Cleanup(func() { provider.Unregister(fakeProviderName) })

	body := `{"model":"chattest-openai/gpt-image-2","messages":[{"role":"user","content":"a red apple"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Provide upstream credentials via headers so compatctx resolves them.
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("X-Base-Url", "https://fake.upstream.test")
	w := httptest.NewRecorder()
	HandleChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp chatCompletionsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("unexpected object: %q", resp.Object)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	content := resp.Choices[0].Message.Content
	if len(content) < 2 {
		t.Errorf("expected text + image_url parts, got %d parts", len(content))
	}
}
