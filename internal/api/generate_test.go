package api

import "testing"

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
