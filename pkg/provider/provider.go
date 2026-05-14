package provider

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ─── Core types ────────────────────────────────────────────────────────────

// GenerateRequest is the normalised, provider-agnostic request handed to every
// Provider.Generate call. The adapter is responsible for translating it into
// the upstream-specific wire format.
type GenerateRequest struct {
	// Model is the upstream model identifier (from contracts/providers.yaml).
	Model string
	// Prompt is the text description of the desired image.
	Prompt string
	// N is the number of images to generate (1–4).
	N int
	// Size is an opaque size string; interpretation is provider-specific.
	// GPT Image: "1024x1024"; Wan: "1024*1024"; Gemini native: use ImageSize.
	Size string
	// ImageSize is Gemini imageConfig.imageSize (3.1: 512|1K|2K|4K; 3 Pro: 1K|2K|4K).
	ImageSize string
	// Quality is an optional quality hint: auto | low | medium | high.
	Quality string
	// ResponseFormat controls whether the provider should return URLs or b64_json.
	// Not all providers support both; the adapter normalises the response regardless.
	ResponseFormat string // "url" | "b64_json"
	// Style is GPT Image specific (vivid | natural).
	Style string
	// AspectRatio is Gemini specific (e.g. "16:9").
	AspectRatio string
	// ThinkingBudget is the max thinking-token budget for Gemini thinking models.
	ThinkingBudget int
	// ThinkingMode enables slow-think on Wan Pro.
	ThinkingMode bool
	// WanEditType specifies the Wan editing mode for image-editing tasks.
	// Valid values: "" (auto/text_to_image), "image_edit", "inpaint".
	// Ignored by non-Wan providers.
	WanEditType string
	// WanBboxList is the list of bounding boxes for region-specific Wan editing.
	// Each entry is [x1,y1,x2,y2] in pixel coordinates.
	// Ignored by non-Wan providers and when WanEditType does not require bboxes.
	WanBboxList []WanBbox
	// RawParams carries any provider-specific parameters not captured above.
	// The adapter reads what it understands and ignores the rest.
	RawParams map[string]any
	// ReferenceImages optional inputs for image-to-image / style reference flows.
	ReferenceImages []ReferenceImage
}

// WanBbox is a bounding box for region-specific Wan image editing.
// Coordinates are pixel-space [x1, y1, x2, y2] relative to the source image.
type WanBbox struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

// ReferenceImage is one client-supplied reference image (raw base64, no data: prefix).
type ReferenceImage struct {
	MIMEType string
	B64      string
}

// Image is a single generated image in the normalised response.
type Image struct {
	// URL is a direct or pre-signed HTTPS URL. Empty when only B64JSON is available.
	URL string
	// B64JSON is a base64-encoded image (PNG/JPEG). Empty when URL is provided.
	B64JSON string
	// RevisedPrompt is the prompt as revised by the upstream model, when available.
	RevisedPrompt string
	// MIMEType is the detected MIME type, e.g. "image/png".
	MIMEType string
	// Width and Height are populated when the upstream provides dimensions.
	Width  int
	Height int
}

// GenerateResponse is the normalised provider response.
type GenerateResponse struct {
	// Images contains the generated images in the order returned by the upstream.
	Images []Image
	// UpstreamRequestID is the provider's own request/generation ID for tracing.
	UpstreamRequestID string
	// TokensUsed is the total token count when the upstream reports it (Gemini).
	TokensUsed int
	// Latency is the wall-clock time from request dispatch to response receipt.
	Latency time.Duration
}

// Capability is a feature flag that a provider model may or may not support.
type Capability string

const (
	CapTextToImage       Capability = "text_to_image"
	CapImageToImage      Capability = "image_to_image"
	CapResponseFormatURL Capability = "response_format_url"
	CapResponseFormatB64 Capability = "response_format_b64_json"
	CapThinking          Capability = "thinking"
	CapSynthIDWatermark  Capability = "synthid_watermark"
	CapWatermark         Capability = "watermark"
)

// ModelInfo describes a single model offered by a provider.
type ModelInfo struct {
	// ID is the stable internal identifier, e.g. "openai/gpt-image-2".
	ID string
	// DisplayName is the human-readable label for the UI.
	DisplayName string
	// UpstreamModel is the exact model string sent to the upstream.
	UpstreamModel string
	// Capabilities lists features available for this model.
	Capabilities []Capability
	// TimeoutSeconds is the per-request hard timeout.
	TimeoutSeconds int
}

// ─── Interface ─────────────────────────────────────────────────────────────

// Provider is the interface every upstream adapter must implement.
// Implementations live in internal/provider/<name>/ and are registered via
// Register. Unit tests use Fake (see fake.go in each adapter package).
type Provider interface {
	// Name returns the stable provider key: "openai", "gemini", or "wan".
	Name() string
	// Models returns the list of models this provider can serve. The list may
	// vary at runtime based on configuration (e.g. which models are enabled).
	Models() []ModelInfo
	// Generate dispatches the request to the upstream and returns the
	// normalised response. The context carries the deadline; implementations
	// must honour ctx.Done() and wrap context.DeadlineExceeded appropriately.
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
}

// ─── Registry ──────────────────────────────────────────────────────────────

var (
	mu       sync.RWMutex
	registry = map[string]Provider{}
)

// Register adds a provider to the global registry. Typically called from
// each provider package's init() or from main() setup. Panics on duplicate
// names to catch misconfiguration early.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	name := p.Name()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("provider %q already registered", name))
	}
	registry[name] = p
}

// Unregister removes a provider from the global registry by name.
// It is a no-op when no provider is registered under name.
// Primarily used in tests to clean up after registering a Fake.
func Unregister(name string) {
	mu.Lock()
	defer mu.Unlock()
	delete(registry, name)
}

// Get returns the provider registered under name, or (nil, false) if absent.
func Get(name string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[name]
	return p, ok
}

// All returns every registered provider in non-deterministic order.
func All() []Provider {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	return out
}

// AllModels returns a flat list of ModelInfo across all registered providers,
// deduplicated by model ID.
func AllModels() []ModelInfo {
	mu.RLock()
	defer mu.RUnlock()
	seen := map[string]struct{}{}
	var out []ModelInfo
	for _, p := range registry {
		for _, m := range p.Models() {
			if _, dup := seen[m.ID]; dup {
				continue
			}
			seen[m.ID] = struct{}{}
			out = append(out, m)
		}
	}
	return out
}

// ProviderForModel returns the registered Provider that owns the given model ID,
// matching either ModelInfo.ID (catalog id, e.g. openai/gpt-image-2) or
// ModelInfo.UpstreamModel (wire id, e.g. gpt-image-2) so OpenAI-compatible
// clients can send the upstream name directly.
func ProviderForModel(modelID string) (Provider, ModelInfo, bool) {
	mu.RLock()
	defer mu.RUnlock()
	for _, p := range registry {
		for _, m := range p.Models() {
			if m.ID == modelID || m.UpstreamModel == modelID {
				return p, m, true
			}
		}
	}
	return nil, ModelInfo{}, false
}

// DebugRegisteredModelLines returns one human-readable line per registered model
// (for GENPIC_DEV diagnostics when model resolution fails).
func DebugRegisteredModelLines() []string {
	mu.RLock()
	defer mu.RUnlock()
	var lines []string
	for provName, p := range registry {
		for _, m := range p.Models() {
			lines = append(lines, fmt.Sprintf("provider=%s catalog_id=%s upstream=%s", provName, m.ID, m.UpstreamModel))
		}
	}
	sort.Strings(lines)
	return lines
}
