// Package xiangyun implements a meta-provider that tries real image adapters
// (Gemini, OpenAI, Wan) in a configured order until one succeeds on upstream
// failure (502-class APIErr).
package xiangyun

import (
	"context"
	"errors"
	"net/http"
	"strings"

	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/logger"
	"genpic/pkg/modelmap"
	"genpic/pkg/provider"
)

// Config controls backend order and default catalog model ids per backend.
type Config struct {
	TryOrder   []string
	Models     map[string]string // key: gemini|openai|wan → catalog id, e.g. gemini/gemini-3.1-flash-image-preview
	ModelIDMap map[string]string // same semantics as config.yaml model_id_map
}

// Provider is the 祥云 meta-adapter.
type Provider struct {
	cfg Config
}

// New returns a 祥云 provider. cfg.TryOrder must be non-empty (callers pass
// defaults from mvpconfig).
func New(cfg Config) *Provider {
	return &Provider{cfg: cfg}
}

func (p *Provider) Name() string { return "xiangyun" }

func (p *Provider) Models() []provider.ModelInfo {
	return []provider.ModelInfo{
		{
			ID:             "xiangyun/auto",
			DisplayName:    "祥云（自动聚合）",
			UpstreamModel:  "xiangyun-auto",
			TimeoutSeconds: 900,
			Capabilities: []provider.Capability{
				provider.CapTextToImage,
				provider.CapImageToImage,
				provider.CapResponseFormatURL,
				provider.CapResponseFormatB64,
			},
		},
	}
}

func defaultCatalogFor(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "openai":
		return "openai/gpt-image-2"
	case "gemini":
		return "gemini/gemini-3.1-flash-image-preview"
	case "wan":
		return "wan/wan2.7-image"
	default:
		return ""
	}
}

func (p *Provider) catalogModel(backend string) string {
	b := strings.ToLower(strings.TrimSpace(backend))
	if p.cfg.Models != nil {
		if id := strings.TrimSpace(p.cfg.Models[b]); id != "" {
			return id
		}
	}
	return defaultCatalogFor(b)
}

func isRetriableUpstreamFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	ae, ok := pkgerrors.As(err)
	if !ok || ae.Type != pkgerrors.TypeUpstream {
		return false
	}
	if ae.Code == "upstream_timeout" {
		return false
	}
	return true
}

func cloneGenerateRequest(base provider.GenerateRequest, upstreamWire string) provider.GenerateRequest {
	out := base
	out.Model = upstreamWire
	return out
}

// responseHasRenderableImages is true only when every slot has a non-empty URL or b64 payload.
// Some adapters may return len(Images)>0 with empty slots; we must not treat that as success
// or we would stop the fallback chain while still having nothing to show (and might look like
// "another model was still requested" downstream).
func responseHasRenderableImages(resp *provider.GenerateResponse) bool {
	if resp == nil || len(resp.Images) == 0 {
		return false
	}
	for _, im := range resp.Images {
		if strings.TrimSpace(im.URL) == "" && strings.TrimSpace(im.B64JSON) == "" {
			return false
		}
	}
	return true
}

// Generate tries each backend in TryOrder. Child adapters use the same
// credential resolution as direct calls: per-request base_url/api_key from the
// JSON body take precedence when present; otherwise config.yaml defaults apply.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResponse, error) {
	log := logger.FromContext(ctx)

	var lastErr error
	for _, backend := range p.cfg.TryOrder {
		b := strings.ToLower(strings.TrimSpace(backend))
		if b == "" || b == p.Name() {
			continue
		}
		catalogID := p.catalogModel(b)
		if catalogID == "" {
			continue
		}
		subProv, modelInfo, ok := provider.ProviderForModel(catalogID)
		if !ok || subProv == nil || subProv.Name() == p.Name() {
			lastErr = pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "xiangyun_backend", "backend "+b+": model "+catalogID+" is not registered")
			continue
		}
		normalised := strings.TrimSpace(catalogID)
		for _, prefix := range []string{"gemini/", "openai/", "wan/"} {
			if strings.HasPrefix(normalised, prefix) {
				normalised = strings.TrimPrefix(normalised, prefix)
				break
			}
		}
		upstreamWire := modelmap.Apply(p.cfg.ModelIDMap, []string{modelInfo.ID, normalised, modelInfo.UpstreamModel}, modelInfo.UpstreamModel)
		subReq := cloneGenerateRequest(req, upstreamWire)
		// OpenAI-compatible aggregators often return URLs even when the client asked for b64_json;
		// the SPA sends b64_json for 祥云 to suit Gemini — force URL for OpenAI only.
		if b == "openai" {
			subReq.ResponseFormat = "url"
		}

		resp, err := subProv.Generate(ctx, subReq)
		if err == nil && responseHasRenderableImages(resp) {
			out := *resp
			if out.EffectiveProvider == "" {
				out.EffectiveProvider = subProv.Name()
			}
			if out.EffectiveCatalogModelID == "" {
				out.EffectiveCatalogModelID = modelInfo.ID
			}
			return &out, nil
		}
		if err == nil {
			if resp == nil || len(resp.Images) == 0 {
				err = pkgerrors.UpstreamErr("empty_response", "upstream returned no images", nil)
			} else {
				err = pkgerrors.UpstreamErr("empty_image_slots", "upstream returned image slots without url or b64_json", nil)
			}
		}
		lastErr = err
		if !isRetriableUpstreamFailure(err) {
			return nil, err
		}
		if logger.DevMode() {
			log.Warn("xiangyun_try_upstream_failed", "backend", b, "catalog", catalogID, "err", err)
		} else {
			log.Info("xiangyun_try_upstream_failed", "backend", b, "catalog", catalogID)
		}
	}
	if lastErr == nil {
		return nil, pkgerrors.BadRequest("xiangyun_no_backend", "祥云未配置可用的后端顺序或模型")
	}
	return nil, lastErr
}

// DefaultTryOrder is used when config leaves try_order empty.
var DefaultTryOrder = []string{"gemini", "openai", "wan"}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)
