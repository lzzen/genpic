package xiangyun

import (
	"context"
	"testing"

	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/provider"
)

func TestIsRetriableUpstreamFailure(t *testing.T) {
	if isRetriableUpstreamFailure(nil) {
		t.Fatal("nil")
	}
	if !isRetriableUpstreamFailure(pkgerrors.UpstreamErr("x", "msg", nil)) {
		t.Fatal("want upstream 502 retriable")
	}
	if isRetriableUpstreamFailure(pkgerrors.UpstreamTimeout()) {
		t.Fatal("timeout not retriable")
	}
	if isRetriableUpstreamFailure(pkgerrors.BadRequest("x", "msg")) {
		t.Fatal("400 not retriable")
	}
	if isRetriableUpstreamFailure(pkgerrors.RateLimit("slow")) {
		t.Fatal("429 not retriable")
	}
}

func TestProvider_Generate_tryOrder(t *testing.T) {
	for _, n := range []string{"openai", "gemini", "wan", "xiangyun"} {
		provider.Unregister(n)
	}
	t.Cleanup(func() {
		for _, n := range []string{"openai", "gemini", "wan", "xiangyun"} {
			provider.Unregister(n)
		}
	})

	provider.Register(&provider.Fake{
		ProviderName: "openai",
		ModelList: []provider.ModelInfo{
			{ID: "openai/gpt-image-2", UpstreamModel: "gpt-image-2", TimeoutSeconds: 30},
		},
		Err: pkgerrors.UpstreamErr("fail", "openai up", nil),
	})
	provider.Register(&provider.Fake{
		ProviderName: "gemini",
		ModelList: []provider.ModelInfo{
			{ID: "gemini/gemini-3.1-flash-image-preview", UpstreamModel: "gemini-3.1-flash-image-preview", TimeoutSeconds: 30},
		},
		Err: pkgerrors.UpstreamErr("fail", "gemini up", nil),
	})
	wantURL := "https://wan.ok/example.png"
	provider.Register(&provider.Fake{
		ProviderName: "wan",
		ModelList: []provider.ModelInfo{
			{ID: "wan/wan2.7-image", UpstreamModel: "wan2.7-image", TimeoutSeconds: 30},
		},
		Response: &provider.GenerateResponse{
			Images: []provider.Image{{URL: wantURL}},
		},
	})

	p := New(Config{
		TryOrder:   []string{"openai", "gemini", "wan"},
		ModelIDMap: nil,
	})
	provider.Register(p)

	resp, err := p.Generate(context.Background(), provider.GenerateRequest{
		Model:          "xiangyun-auto",
		Prompt:         "cat",
		N:              1,
		ResponseFormat: "url",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Images) != 1 || resp.Images[0].URL != wantURL {
		t.Fatalf("resp: %+v", resp)
	}
	if resp.EffectiveProvider != "wan" {
		t.Fatalf("effective provider: %q", resp.EffectiveProvider)
	}
	if resp.EffectiveCatalogModelID != "wan/wan2.7-image" {
		t.Fatalf("effective model: %q", resp.EffectiveCatalogModelID)
	}
}

func TestProvider_Generate_emptyImageSlotContinuesFallback(t *testing.T) {
	for _, n := range []string{"openai", "gemini", "wan", "xiangyun"} {
		provider.Unregister(n)
	}
	t.Cleanup(func() {
		for _, n := range []string{"openai", "gemini", "wan", "xiangyun"} {
			provider.Unregister(n)
		}
	})

	provider.Register(&provider.Fake{
		ProviderName: "openai",
		ModelList: []provider.ModelInfo{
			{ID: "openai/gpt-image-2", UpstreamModel: "gpt-image-2", TimeoutSeconds: 30},
		},
		Response: &provider.GenerateResponse{
			Images: []provider.Image{{URL: "", B64JSON: ""}},
		},
	})
	wantURL := "https://gem.ok/x.png"
	provider.Register(&provider.Fake{
		ProviderName: "gemini",
		ModelList: []provider.ModelInfo{
			{ID: "gemini/gemini-3.1-flash-image-preview", UpstreamModel: "gemini-3.1-flash-image-preview", TimeoutSeconds: 30},
		},
		Response: &provider.GenerateResponse{
			Images: []provider.Image{{URL: wantURL}},
		},
	})
	provider.Register(&provider.Fake{
		ProviderName: "wan",
		ModelList: []provider.ModelInfo{
			{ID: "wan/wan2.7-image", UpstreamModel: "wan2.7-image", TimeoutSeconds: 30},
		},
		Response: &provider.GenerateResponse{
			Images: []provider.Image{{URL: "https://wan.should-not-run/y.png"}},
		},
	})

	p := New(Config{TryOrder: []string{"openai", "gemini", "wan"}})
	provider.Register(p)

	resp, err := p.Generate(context.Background(), provider.GenerateRequest{
		Model:  "x",
		Prompt: "cat",
		N:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Images) != 1 || resp.Images[0].URL != wantURL {
		t.Fatalf("want gemini result, got %+v", resp)
	}
	if resp.EffectiveProvider != "gemini" {
		t.Fatalf("effective provider: %q", resp.EffectiveProvider)
	}
}

func TestProvider_Generate_nonUpstreamShortCircuit(t *testing.T) {
	for _, n := range []string{"openai", "gemini", "wan", "xiangyun"} {
		provider.Unregister(n)
	}
	t.Cleanup(func() {
		for _, n := range []string{"openai", "gemini", "wan", "xiangyun"} {
			provider.Unregister(n)
		}
	})

	provider.Register(&provider.Fake{
		ProviderName: "openai",
		ModelList: []provider.ModelInfo{
			{ID: "openai/gpt-image-2", UpstreamModel: "gpt-image-2", TimeoutSeconds: 30},
		},
		Err: pkgerrors.BadRequest("bad", "client error"),
	})
	provider.Register(&provider.Fake{
		ProviderName: "gemini",
		ModelList: []provider.ModelInfo{
			{ID: "gemini/gemini-3.1-flash-image-preview", UpstreamModel: "gemini-3.1-flash-image-preview", TimeoutSeconds: 30},
		},
		Response: &provider.GenerateResponse{
			Images: []provider.Image{{URL: "https://should-not-run"}},
		},
	})

	p := New(Config{TryOrder: []string{"openai", "gemini"}})
	provider.Register(p)

	_, err := p.Generate(context.Background(), provider.GenerateRequest{
		Model:  "x",
		Prompt: "cat",
		N:      1,
	})
	if err == nil {
		t.Fatal("want error")
	}
	ae, ok := pkgerrors.As(err)
	if !ok || ae.Type != pkgerrors.TypeInvalidRequest {
		t.Fatalf("want invalid_request, got %v", err)
	}
}
