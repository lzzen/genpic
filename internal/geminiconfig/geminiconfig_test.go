package geminiconfig

import "testing"

func TestResolveMappedCatalogID(t *testing.T) {
	Install(nil)
	if got := ResolveMappedCatalogID("gemini-3.1-flash-image-preview", "4K"); got != "" {
		t.Fatalf("no map: got %q", got)
	}
	Install(map[string]string{
		"gemini/gemini-3.1-flash-image-preview": "gemini/banana-2-4K",
	})
	defer Install(nil)
	if got := ResolveMappedCatalogID("gemini-3.1-flash-image-preview", "4K"); got != "gemini/banana-2-4K" {
		t.Fatalf("4K rewrite: got %q", got)
	}
	if got := ResolveMappedCatalogID("gemini-3.1-flash-image-preview", "1K"); got != "" {
		t.Fatalf("non-4K: want empty, got %q", got)
	}
}

func TestExtraUpstreamWireModels(t *testing.T) {
	Install(map[string]string{
		"gemini/gemini-3.1-flash-image-preview": "gemini/banana-2-4K",
		"gemini/gemini-3-pro-image-preview":     "banana-pro-4K",
	})
	defer Install(nil)
	got := ExtraUpstreamWireModels()
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
}

func TestMergeUICatalogPayload_doesNotAppendRouteModels(t *testing.T) {
	Install(map[string]string{
		"gemini/gemini-3.1-flash-image-preview": "gemini/banana-2-4K",
	})
	defer Install(nil)
	base := map[string]any{
		"vendors": []any{
			map[string]any{
				"id": "gemini",
				"models": []any{
					map[string]string{"id": "gemini/gemini-3.1-flash-image-preview", "label": "Gemini 3.1"},
				},
			},
		},
	}
	out := MergeUICatalogPayload(base)
	if out["gemini_image_size_4k_model_map"] == nil {
		t.Fatal("expected gemini_image_size_4k_model_map in payload")
	}
	vendors := out["vendors"].([]any)
	vm := vendors[0].(map[string]any)
	models := vm["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("vendor list must not include route-only models, got len=%d", len(models))
	}
}
