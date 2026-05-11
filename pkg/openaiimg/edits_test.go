package openaiimg

import (
	"bytes"
	"testing"
)

func TestBuildEditsMultipart(t *testing.T) {
	body, ct, err := BuildEditsMultipart("gpt-image-2", "hello", []ImagePart{
		{Filename: "a.png", MIMEType: "image/png", Data: []byte{1, 2, 3}},
	}, map[string]string{"n": "1", "response_format": "b64_json"})
	if err != nil {
		t.Fatal(err)
	}
	if ct == "" {
		t.Fatal("empty content type")
	}
	if !bytes.Contains(body, []byte("gpt-image-2")) || !bytes.Contains(body, []byte("hello")) {
		t.Fatalf("body missing expected strings, len=%d", len(body))
	}
}

func TestBuildEditsMultipartRequiresImage(t *testing.T) {
	_, _, err := BuildEditsMultipart("m", "p", nil, nil)
	if err == nil {
		t.Fatal("want error")
	}
}
