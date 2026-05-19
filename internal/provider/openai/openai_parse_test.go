package openai

import (
	"strings"
	"testing"
)

// Fixture: openapi.wang / gpt-image-2-all task envelope (nested data.data.data[]; url set, b64_json empty).
const aggregatorEnvelopeFixture = `{"code":"success","data":{"data":{"created":1779159076,"data":[{"b64_json":"","revised_prompt":"今天发生了什么事？你对当前的人类有什么建议？","url":"http://data.openapi.wang/images/bf75bbf1a78f83f093fab3baeec8a4da.png"}],"model":"gpt-image-2","usage":{"input_tokens":23,"input_tokens_details":{"image_tokens":0,"text_tokens":23},"output_tokens":4175,"total_tokens":4198}},"progress":"100%","status":"SUCCESS","task_id":"bf75bbf1a78f83f093fab3baeec8a4da"},"message":""}`

func TestParseOpenAIImagesResponse_aggregatorEnvelope(t *testing.T) {
	images, err := parseOpenAIImagesResponse([]byte(aggregatorEnvelopeFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("len=%d", len(images))
	}
	want := "http://data.openapi.wang/images/bf75bbf1a78f83f093fab3baeec8a4da.png"
	if images[0].URL != want {
		t.Fatalf("url=%q want %q", images[0].URL, want)
	}
	if strings.TrimSpace(images[0].B64JSON) != "" {
		t.Fatalf("expected empty b64_json, got len %d", len(images[0].B64JSON))
	}
}

func TestParseOpenAIImagesResponse_standardOpenAI(t *testing.T) {
	raw := `{"created":1,"data":[{"url":"https://example.com/a.png","b64_json":"","revised_prompt":""}]}`
	images, err := parseOpenAIImagesResponse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].URL != "https://example.com/a.png" {
		t.Fatalf("got %+v", images)
	}
}

func TestParseOpenAIImagesResponse_standardError(t *testing.T) {
	raw := `{"error":{"type":"invalid_request","message":"bad prompt"}}`
	_, err := parseOpenAIImagesResponse([]byte(raw))
	if err == nil {
		t.Fatal("want error")
	}
}

func TestParseOpenAIImagesResponse_aggregatorNonSuccess(t *testing.T) {
	raw := `{"code":"failed","message":"quota","data":{}}`
	_, err := parseOpenAIImagesResponse([]byte(raw))
	if err == nil {
		t.Fatal("want error")
	}
}

func TestParseOpenAIImagesResponse_aggregatorBadStatus(t *testing.T) {
	raw := `{"code":"success","data":{"data":{},"status":"PENDING"}}`
	_, err := parseOpenAIImagesResponse([]byte(raw))
	if err == nil {
		t.Fatal("want error")
	}
}
