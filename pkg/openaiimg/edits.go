// Package openaiimg builds OpenAI Image API request payloads (JSON generations,
// multipart edits with reference images).
package openaiimg

import (
	"bytes"
	"fmt"
	"mime/multipart"
)

// ImagePart is one reference image for POST /v1/images/edits.
type ImagePart struct {
	Filename string
	MIMEType string
	Data     []byte
}

// BuildEditsMultipart builds a multipart/form-data body for OpenAI-compatible
// POST /v1/images/edits (reference images + prompt). Extra string fields are
// written when non-empty (e.g. size, quality, response_format, n).
func BuildEditsMultipart(model, prompt string, images []ImagePart, extra map[string]string) (body []byte, contentType string, err error) {
	if len(images) == 0 {
		return nil, "", fmt.Errorf("openaiimg: at least one image required for edits")
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("model", model); err != nil {
		return nil, "", err
	}
	if err := mw.WriteField("prompt", prompt); err != nil {
		return nil, "", err
	}
	for i, img := range images {
		fn := img.Filename
		if fn == "" {
			fn = fmt.Sprintf("ref%d.png", i)
		}
		part, err := mw.CreateFormFile("image", fn)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(img.Data); err != nil {
			return nil, "", err
		}
	}
	for k, v := range extra {
		if v == "" {
			continue
		}
		if err := mw.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}
