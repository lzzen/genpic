package api

import (
	"encoding/json"
	"net/http"

	"genpic/internal/geminiconfig"
)

// HandleUICatalog serves GET /api/ui/catalog — vendor + model list for the embedded SPA.
// Optional gemini_image_size_4k_model_map is merged in by [geminiconfig.MergeUICatalogPayload]
// (map only; 4K route targets are never added to the vendor model list).
func HandleUICatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, err := json.Marshal(uiCatalogPayload)
	if err != nil {
		JSON(w, http.StatusOK, geminiconfig.MergeUICatalogPayload(uiCatalogPayload))
		return
	}
	var merged map[string]any
	if err := json.Unmarshal(raw, &merged); err != nil {
		JSON(w, http.StatusOK, geminiconfig.MergeUICatalogPayload(uiCatalogPayload))
		return
	}
	if xiangyunUICatalogOn() {
		vendors, _ := merged["vendors"].([]any)
		vendors = append(vendors, map[string]any{
			"id":   "xiangyun",
			"name": "祥云",
			"models": []any{
				map[string]string{"id": "xiangyun/auto", "label": "祥云（自动聚合）"},
			},
		})
		merged["vendors"] = vendors
	}
	JSON(w, http.StatusOK, geminiconfig.MergeUICatalogPayload(merged))
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
