package gemini

import "testing"

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
