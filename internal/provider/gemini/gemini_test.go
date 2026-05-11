package gemini

import (
	"strings"
	"testing"

	"genpic/pkg/provider"
)

func TestParseGenerateContentResponse(t *testing.T) {
	const sample = `{
  "candidates": [
    {
      "content": {
        "parts": [
          {
            "inlineData": {
              "mimeType": "image/jpeg",
              "data": "QUJD"
            }
          }
        ],
        "role": "model"
      },
      "finishReason": "STOP"
    }
  ],
  "usageMetadata": {
    "totalTokenCount": 1146
  },
  "responseId": "fzgBaoLTBK2fz7IP1JbRoAQ"
}`

	images, tok, rid, err := parseGenerateContentResponse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("images: got %d want 1", len(images))
	}
	if images[0].B64JSON != "QUJD" {
		t.Errorf("b64: %q", images[0].B64JSON)
	}
	if images[0].MIMEType != "image/jpeg" {
		t.Errorf("mime: %q", images[0].MIMEType)
	}
	if tok != 1146 {
		t.Errorf("tokens: %d", tok)
	}
	if rid != "fzgBaoLTBK2fz7IP1JbRoAQ" {
		t.Errorf("responseId: %q", rid)
	}
}

func TestGenerateContentURL(t *testing.T) {
	u := generateContentURL("https://api.example.com/", "gemini-3.1-flash-image-preview")
	want := "https://api.example.com/v1beta/models/gemini-3.1-flash-image-preview:generateContent"
	if u != want {
		t.Errorf("got %q want %q", u, want)
	}
}

func TestBuildGenerateContentBody_imageSizeByModel(t *testing.T) {
	reqBase := provider.GenerateRequest{Prompt: "hi", AspectRatio: "1:1"}

	b25, err := buildGenerateContentBody(provider.GenerateRequest{
		Model: "gemini-2.5-flash-image", Prompt: reqBase.Prompt, AspectRatio: reqBase.AspectRatio,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b25), `"imageSize"`) {
		t.Errorf("gemini-2.5 must not include imageSize: %s", b25)
	}
	if strings.Contains(string(b25), `"imageConfig"`) {
		t.Errorf("gemini-2.5 must not include imageConfig: %s", b25)
	}

	b31, err := buildGenerateContentBody(provider.GenerateRequest{
		Model: "gemini-3.1-flash-image-preview", Prompt: reqBase.Prompt, AspectRatio: reqBase.AspectRatio, ImageSize: "512",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b31), `"imageSize":"512"`) {
		t.Errorf("3.1 expected imageSize 512: %s", b31)
	}

	b3p, err := buildGenerateContentBody(provider.GenerateRequest{
		Model: "gemini-3-pro-image-preview", Prompt: reqBase.Prompt, AspectRatio: reqBase.AspectRatio, ImageSize: "2K",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b3p), `"imageSize":"2K"`) {
		t.Errorf("3 pro expected imageSize 2K: %s", b3p)
	}
}
