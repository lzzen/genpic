package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// HandleUICatalog serves GET /api/ui/catalog — vendor + model list for the embedded SPA.
// When gemini_image_size_4k_model_map is configured (see config.yaml), the JSON also
// includes that map and appends any missing target model ids to the Gemini vendor list.
func HandleUICatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	uiCatalogMu.RLock()
	g4 := uiCatalogGeminiImageSize4KModelMap
	uiCatalogMu.RUnlock()
	JSON(w, http.StatusOK, buildUICatalogResponse(g4))
}

var (
	uiCatalogMu                        sync.RWMutex
	uiCatalogGeminiImageSize4KModelMap map[string]string
)

// SetGeminiImageSize4KModelMap installs gemini.image_size_4k_model_map from config (nil clears).
func SetGeminiImageSize4KModelMap(m map[string]string) {
	uiCatalogMu.Lock()
	defer uiCatalogMu.Unlock()
	if len(m) == 0 {
		uiCatalogGeminiImageSize4KModelMap = nil
		return
	}
	c := make(map[string]string, len(m))
	for k, v := range m {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		c[k] = v
	}
	if len(c) == 0 {
		uiCatalogGeminiImageSize4KModelMap = nil
		return
	}
	uiCatalogGeminiImageSize4KModelMap = c
}

func buildUICatalogResponse(g4 map[string]string) map[string]any {
	raw, err := json.Marshal(uiCatalogPayload)
	if err != nil {
		return uiCatalogPayload
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return uiCatalogPayload
	}
	if len(g4) == 0 {
		return out
	}
	out["gemini_image_size_4k_model_map"] = g4
	vendorsAny, ok := out["vendors"].([]any)
	if !ok {
		return out
	}
	for i, v := range vendorsAny {
		vm, ok := v.(map[string]any)
		if !ok || vm["id"] != "gemini" {
			continue
		}
		modelsAny, ok := vm["models"].([]any)
		if !ok {
			continue
		}
		seen := map[string]struct{}{}
		for _, m := range modelsAny {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			id, _ := mm["id"].(string)
			id = strings.TrimSpace(id)
			if id != "" {
				seen[id] = struct{}{}
			}
		}
		for _, target := range g4 {
			t := strings.TrimSpace(target)
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			modelsAny = append(modelsAny, map[string]any{
				"id":    t,
				"label": geminiAutoModelLabel(t),
			})
		}
		vm["models"] = modelsAny
		vendorsAny[i] = vm
		break
	}
	out["vendors"] = vendorsAny
	return out
}

func geminiAutoModelLabel(catalogID string) string {
	s := strings.TrimSpace(catalogID)
	s = strings.TrimPrefix(s, "gemini/")
	if s == "" {
		return catalogID
	}
	return s + " (4K)"
}

// uiCatalogPayload mirrors the SPA's vendor rail + model dropdown.
var uiCatalogPayload = map[string]any{
	"vendors": []any{
		map[string]any{
			"id":   "openai",
			"name": "GPT image",
			"models": []any{
				map[string]string{"id": "openai/gpt-image-2", "label": "GPT Image 2"},
			},
		},
		map[string]any{
			"id":   "gemini",
			"name": "Banana",
			"models": []any{
				map[string]string{"id": "gemini/gemini-3.1-flash-image-preview", "label": "Gemini 3.1"},
				map[string]string{"id": "gemini/gemini-3-pro-image-preview", "label": "Gemini 3 Pro"},
				map[string]string{"id": "gemini/gemini-2.5-flash-image", "label": "Gemini 2.5 Flash"},
			},
		},
		map[string]any{
			"id":   "wan",
			"name": "万相生图",
			"models": []any{
				map[string]string{"id": "wan/wan2.7-image", "label": "万相 2.7"},
				map[string]string{"id": "wan/wan2.7-image-pro", "label": "万相 2.7 Pro"},
			},
		},
	},
}
