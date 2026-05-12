package api

import "net/http"

// HandleUICatalog serves GET /api/ui/catalog — vendor + model list for the embedded SPA.
// Today this is static JSON; swap for a DB-backed catalog (e.g. generation_catalog_models)
// when an admin API exists, without changing the response shape.
func HandleUICatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	JSON(w, http.StatusOK, uiCatalogPayload)
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
				map[string]string{"id": "gemini/gemini-2.5-flash-image", "label": "Gemini 2.5 Flash"},
				map[string]string{"id": "gemini/gemini-3.1-flash-image-preview", "label": "Gemini 3.1"},
				map[string]string{"id": "gemini/gemini-3-pro-image-preview", "label": "Gemini 3 Pro"},
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
