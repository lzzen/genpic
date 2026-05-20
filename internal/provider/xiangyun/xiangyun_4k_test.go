package xiangyun

import (
	"context"
	"testing"

	"genpic/internal/geminiconfig"
	"genpic/pkg/provider"
)

func TestProvider_Generate_gemini4KRemap(t *testing.T) {
	geminiconfig.Install(map[string]string{
		"gemini/gemini-3.1-flash-image-preview": "gemini/banana-2-4K",
	})
	t.Cleanup(func() { geminiconfig.Install(nil) })

	for _, n := range []string{"openai", "gemini", "wan", "xiangyun"} {
		provider.Unregister(n)
	}
	t.Cleanup(func() {
		for _, n := range []string{"openai", "gemini", "wan", "xiangyun"} {
			provider.Unregister(n)
		}
	})

	gemFake := &provider.Fake{
		ProviderName: "gemini",
		ModelList: []provider.ModelInfo{
			{ID: "gemini/banana-2-4K", UpstreamModel: "banana-2-4K", TimeoutSeconds: 30},
		},
		Response: &provider.GenerateResponse{
			Images: []provider.Image{{B64JSON: "dGVzdA=="}},
		},
	}
	provider.Register(gemFake)

	p := New(Config{Models: []string{"gemini/gemini-3.1-flash-image-preview"}})
	provider.Register(p)

	_, err := p.Generate(context.Background(), provider.GenerateRequest{
		Model:     "xiangyun-auto",
		Prompt:    "cat",
		N:         1,
		ImageSize: "4K",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(gemFake.Calls) != 1 {
		t.Fatalf("calls: %d", len(gemFake.Calls))
	}
	if gemFake.Calls[0].Model != "banana-2-4K" {
		t.Fatalf("upstream wire: got %q want banana-2-4K", gemFake.Calls[0].Model)
	}
}
