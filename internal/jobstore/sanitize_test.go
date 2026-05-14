package jobstore_test

import (
	"strings"
	"testing"

	"genpic/internal/jobstore"
)

func TestSanitizeImageForStorage_stripsLargeB64AndThoughtSignature(t *testing.T) {
	img := jobstore.Image{
		B64JSON:       strings.Repeat("A", 900),
		RevisedPrompt: `prefix "thoughtSignature":"` + strings.Repeat("Z", 80) + `" suffix`,
		MIMEType:      "image/png",
		URL:           "https://example.com/x.png",
		ThumbURL:      "https://example.com/x_thumb.jpg",
	}
	out := jobstore.SanitizeImageForStorage(img)
	if out.B64JSON != "" {
		t.Fatalf("expected empty b64, got len %d", len(out.B64JSON))
	}
	if !strings.Contains(out.RevisedPrompt, "[omitted]") {
		t.Fatalf("revised_prompt: %q", out.RevisedPrompt)
	}
	if out.URL != img.URL || out.MIMEType != img.MIMEType || out.ThumbURL != img.ThumbURL {
		t.Fatalf("unexpected field change")
	}
}

func TestSanitizeImageForStorage_keepsShortB64(t *testing.T) {
	img := jobstore.Image{B64JSON: "YQ=="}
	out := jobstore.SanitizeImageForStorage(img)
	if out.B64JSON != "YQ==" {
		t.Fatalf("got %q", out.B64JSON)
	}
}

func TestStripThoughtSignatureFromJSON_promptField(t *testing.T) {
	in := `hello "thoughtSignature":"` + strings.Repeat("x", 70) + `" world`
	out := jobstore.StripThoughtSignatureFromJSON(in)
	if strings.Contains(out, strings.Repeat("x", 70)) {
		t.Fatal("expected redaction")
	}
}
