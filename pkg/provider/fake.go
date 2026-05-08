package provider

import (
	"context"
	"time"
)

// Fake is a test-double Provider that returns configurable canned responses.
// Use it in unit tests to avoid real HTTP calls:
//
//	p := &provider.Fake{
//	    ProviderName: "openai",
//	    ModelList: []provider.ModelInfo{{ID: "openai/gpt-image-2", ...}},
//	    Response: &provider.GenerateResponse{Images: []provider.Image{{URL: "https://example.com/img.png"}}},
//	}
//	provider.Register(p)
//
// Fake is exported from pkg/provider so all test packages can reference it
// without an import cycle, without a separate fake sub-package.
type Fake struct {
	ProviderName string
	ModelList    []ModelInfo
	// Response is returned verbatim by Generate when Err is nil.
	Response *GenerateResponse
	// Err, when non-nil, is returned instead of Response.
	Err error
	// Calls records every GenerateRequest received, for assertion in tests.
	Calls []GenerateRequest
}

func (f *Fake) Name() string { return f.ProviderName }

func (f *Fake) Models() []ModelInfo { return f.ModelList }

func (f *Fake) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	f.Calls = append(f.Calls, req)
	if f.Err != nil {
		return nil, f.Err
	}
	if f.Response != nil {
		return f.Response, nil
	}
	// Default stub response so tests that don't care about content still pass.
	return &GenerateResponse{
		Images: []Image{
			{URL: "https://fake.provider.test/image-stub.png"},
		},
		Latency: 5 * time.Millisecond,
	}, nil
}
