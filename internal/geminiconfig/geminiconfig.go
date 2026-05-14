// Package geminiconfig holds runtime configuration for Gemini image_size → 4K upstream
// model routing (config.yaml gemini.image_size_4k_model_map). One place for install,
// request rewrite, extra provider registrations, and SPA catalog merge.
package geminiconfig

import (
	"encoding/json"
	"strings"
	"sync"
)

var (
	mu sync.RWMutex
	m  map[string]string // normalized catalog keys → catalog values
)

// Install replaces the in-memory map (nil or empty clears). Safe for concurrent reads after install.
func Install(mm map[string]string) {
	mu.Lock()
	defer mu.Unlock()
	if len(mm) == 0 {
		m = nil
		return
	}
	c := make(map[string]string, len(mm))
	for k, v := range mm {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		c[k] = v
	}
	if len(c) == 0 {
		m = nil
		return
	}
	m = c
}

// ResolveMappedCatalogID returns the configured catalog model id (e.g. gemini/banana-2-4K)
// when image_size is exactly "4K" and modelNormalized matches a map key.
// modelNormalized is the wire id after stripping openai/gemini/wan catalog prefix.
func ResolveMappedCatalogID(modelNormalized, imageSize string) string {
	if strings.TrimSpace(imageSize) != "4K" {
		return ""
	}
	w := strings.TrimSpace(modelNormalized)
	if w == "" {
		return ""
	}
	mu.RLock()
	cur := m
	mu.RUnlock()
	if len(cur) == 0 {
		return ""
	}
	for _, k := range []string{"gemini/" + w, w} {
		if v, ok := cur[k]; ok {
			v = strings.TrimSpace(v)
			if v != "" {
				return v
			}
		}
	}
	return ""
}

// ExtraUpstreamWireModels returns unique wire model names from map values (for Gemini provider registry).
func ExtraUpstreamWireModels() []string {
	mu.RLock()
	cur := m
	mu.RUnlock()
	if len(cur) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, v := range cur {
		w := strings.TrimSpace(v)
		if w == "" {
			continue
		}
		w = strings.TrimPrefix(w, "gemini/")
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	return out
}

// MergeUICatalogPayload returns a deep copy of base with gemini_image_size_4k_model_map
// attached for debugging / optional clients. Route targets are not added to vendor model lists
// (they are server-side only; see ResolveMappedCatalogID in generate path).
func MergeUICatalogPayload(base map[string]any) map[string]any {
	mu.RLock()
	g4 := m
	mu.RUnlock()

	raw, err := json.Marshal(base)
	if err != nil {
		return base
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return base
	}
	if len(g4) == 0 {
		return out
	}
	out["gemini_image_size_4k_model_map"] = g4
	return out
}
