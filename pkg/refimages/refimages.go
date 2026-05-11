// Package refimages validates and normalizes client-supplied reference images
// (JSON: mime_type + b64_json or data: URLs) for genpic and mvplite.
package refimages

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Input is one JSON reference image from the client.
type Input struct {
	MIMEType string `json:"mime_type,omitempty"`
	B64JSON  string `json:"b64_json"`
}

// Item is validated raw base64 (no data: prefix) and optional mime type.
type Item struct {
	MIMEType string
	B64      string
}

const maxRefs = 6
const maxDecodedBytes = 4 << 20

// Parse validates at most maxRefs images, each ≤ maxDecodedBytes after base64 decode.
func Parse(in []Input) ([]Item, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxRefs {
		return nil, fmt.Errorf("at most %d reference images", maxRefs)
	}
	out := make([]Item, 0, len(in))
	for i, row := range in {
		b64, mt := stripDataURL(row.B64JSON, row.MIMEType)
		if b64 == "" {
			return nil, fmt.Errorf("reference_images[%d]: empty b64_json", i)
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("reference_images[%d]: invalid base64: %w", i, err)
		}
		if len(raw) > maxDecodedBytes {
			return nil, fmt.Errorf("reference_images[%d]: exceeds %d bytes decoded", i, maxDecodedBytes)
		}
		mt = strings.TrimSpace(mt)
		if mt != "" {
			switch strings.ToLower(mt) {
			case "image/png", "image/jpeg", "image/webp", "image/gif":
			default:
				return nil, fmt.Errorf("reference_images[%d]: unsupported mime_type %q", i, mt)
			}
		}
		out = append(out, Item{MIMEType: mt, B64: b64})
	}
	return out, nil
}

func stripDataURL(b64in, mimeIn string) (b64 string, mime string) {
	s := strings.TrimSpace(b64in)
	mime = strings.TrimSpace(mimeIn)
	if !strings.HasPrefix(s, "data:") {
		return s, mime
	}
	comma := strings.Index(s, ",")
	if comma < 0 {
		return "", mime
	}
	header := strings.TrimSpace(s[5:comma])
	payload := strings.TrimSpace(s[comma+1:])
	header = strings.TrimSuffix(header, ";base64")
	if header != "" && mime == "" {
		mime = header
	}
	return payload, mime
}
