package api

import "testing"

func TestResolveGeminiImageSize4KMappedCatalogID(t *testing.T) {
	SetGeminiImageSize4KModelMap(nil)
	if got := ResolveGeminiImageSize4KMappedCatalogID("gemini-3.1-flash-image-preview", "4K"); got != "" {
		t.Fatalf("no map: got %q", got)
	}
	SetGeminiImageSize4KModelMap(map[string]string{
		"gemini/gemini-3.1-flash-image-preview": "gemini/banana-2-4K",
	})
	defer SetGeminiImageSize4KModelMap(nil)
	if got := ResolveGeminiImageSize4KMappedCatalogID("gemini-3.1-flash-image-preview", "4K"); got != "gemini/banana-2-4K" {
		t.Fatalf("4K rewrite: got %q", got)
	}
	if got := ResolveGeminiImageSize4KMappedCatalogID("gemini-3.1-flash-image-preview", "1K"); got != "" {
		t.Fatalf("non-4K: want empty, got %q", got)
	}
}

func TestNormalizeModelID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"gemini/gemini-3.1-flash-image-preview", "gemini-3.1-flash-image-preview"},
		{"gemini-3.1-flash-image-preview", "gemini-3.1-flash-image-preview"},
		{"openai/gpt-image-2", "gpt-image-2"},
		{"wan/wan2.7-image", "wan2.7-image"},
		{"  gemini/gemini-2.5-flash-image  ", "gemini-2.5-flash-image"},
	}
	for _, tt := range tests {
		if got := normalizeModelID(tt.in); got != tt.want {
			t.Errorf("normalizeModelID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
