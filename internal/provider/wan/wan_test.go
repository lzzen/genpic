package wan

import (
	"testing"
)

func TestParseDashScopeResponse_metadataEnvelope(t *testing.T) {
	raw := []byte(`{"data":[{"url":"https://example.com/out.png","b64_json":"","revised_prompt":""}],"created":1778656461,"metadata":{"output":{"choices":[{"finish_reason":"stop","message":{"content":[{"image":"https://example.com/from-meta.png","type":"image"}],"role":"assistant"}}]},"usage":{"image_count":1},"request_id":"90f"}}`)

	images, err := parseDashScopeResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].URL != "https://example.com/from-meta.png" {
		t.Fatalf("got %+v want URL from metadata choices", images)
	}
}

func TestParseDashScopeResponse_topLevelFallback(t *testing.T) {
	raw := []byte(`{"code":"Success","output":{"choices":[{"message":{"content":[{"image":"https://example.com/direct.png"}]}}]}}`)
	images, err := parseDashScopeResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].URL != "https://example.com/direct.png" {
		t.Fatalf("got %+v", images)
	}
}

func TestParseDashScopeResponse_dataFallback(t *testing.T) {
	raw := []byte(`{"data":[{"url":"https://example.com/data-only.png","b64_json":""}],"created":1}`)
	images, err := parseDashScopeResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].URL != "https://example.com/data-only.png" {
		t.Fatalf("got %+v", images)
	}
}
