package provider

import (
	"sync"
	"testing"
)

// TestProviderForModel_acceptsUpstreamWireName ensures callers may pass either
// the catalog id (openai/gpt-image-2) or the upstream model string (gpt-image-2).
func TestProviderForModel_acceptsUpstreamWireName(t *testing.T) {
	registerTestProviderOnce(t)

	_, byCatalog, ok := ProviderForModel("openai/gpt-image-2")
	if !ok {
		t.Fatal("ProviderForModel(openai/gpt-image-2): want match")
	}
	if byCatalog.UpstreamModel != "gpt-image-2" {
		t.Fatalf("catalog match: UpstreamModel = %q", byCatalog.UpstreamModel)
	}

	_, byWire, ok := ProviderForModel("gpt-image-2")
	if !ok {
		t.Fatal("ProviderForModel(gpt-image-2): want match")
	}
	if byWire.ID != byCatalog.ID {
		t.Fatalf("wire vs catalog: ID mismatch %q vs %q", byWire.ID, byCatalog.ID)
	}
}

var testProviderOnce sync.Once

func registerTestProviderOnce(t *testing.T) {
	t.Helper()
	testProviderOnce.Do(func() {
		Register(&Fake{
			ProviderName: "testopenai",
			ModelList: []ModelInfo{
				{
					ID:             "openai/gpt-image-2",
					UpstreamModel:  "gpt-image-2",
					TimeoutSeconds: 120,
				},
			},
		})
	})
}
