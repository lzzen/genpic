package jobstore

import (
	"regexp"
	"strings"
)

// Limits for persisted image metadata (DB / long-lived job records).
const (
	maxPersistedB64Len       = 512
	maxPersistedRevisedRunes = 4096
)

var reLongThoughtSignature = regexp.MustCompile(`"thoughtSignature"\s*:\s*"[^"]{64,}"`)

// SanitizeImageForStorage returns a copy safe to persist: drops oversized base64,
// strips long thoughtSignature fragments from revised_prompt, and truncates prompt text.
func SanitizeImageForStorage(img Image) Image {
	out := img
	if len(out.B64JSON) > maxPersistedB64Len {
		out.B64JSON = ""
	}
	out.RevisedPrompt = stripThoughtSignatureFromText(out.RevisedPrompt)
	if r := []rune(out.RevisedPrompt); len(r) > maxPersistedRevisedRunes {
		out.RevisedPrompt = string(r[:maxPersistedRevisedRunes]) + "…"
	}
	return out
}

// SanitizeImagesForStorage applies [SanitizeImageForStorage] to each element.
func SanitizeImagesForStorage(images []Image) []Image {
	if len(images) == 0 {
		return nil
	}
	out := make([]Image, len(images))
	for i := range images {
		out[i] = SanitizeImageForStorage(images[i])
	}
	return out
}

func stripThoughtSignatureFromText(s string) string {
	if s == "" {
		return ""
	}
	return reLongThoughtSignature.ReplaceAllString(s, `"thoughtSignature":"[omitted]"`)
}

// StripThoughtSignatureFromJSON removes long thoughtSignature string values from raw JSON
// (e.g. embedded provider blobs) before persisting auxiliary text fields.
func StripThoughtSignatureFromJSON(s string) string {
	return strings.TrimSpace(stripThoughtSignatureFromText(s))
}
