// Package xiangyun implements a meta-provider that tries catalog image models
// in a configured order until one succeeds on upstream failure (502-class APIErr).
package xiangyun

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"genpic/internal/geminiconfig"
	pkgerrors "genpic/pkg/errors"
	"genpic/pkg/logger"
	"genpic/pkg/modelmap"
	"genpic/pkg/provider"
)

// Config controls fallback order as an ordered list of catalog model ids.
type Config struct {
	Models     []string // e.g. gemini/gemini-3.1-flash-image-preview, openai/gpt-image-2
	ModelIDMap map[string]string // same semantics as config.yaml model_id_map
}

// Provider is the 祥云 meta-adapter.
type Provider struct {
	cfg Config
}

// New returns a 祥云 provider. cfg.Models must be non-empty (callers pass
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

// Generate tries each catalog model in Models order. Child adapters use the same
// credential resolution as direct calls: per-request base_url/api_key from the
// JSON body take precedence when present; otherwise config.yaml defaults apply.
func (p *Provider) Generate(ctx context.Context, req provider.GenerateRequest) (*provider.GenerateResponse, error) {
	log := logger.FromContext(ctx)

	var lastErr error
	for _, catalogID := range p.cfg.Models {
		catalogID = strings.TrimSpace(catalogID)
		if catalogID == "" || strings.HasPrefix(strings.ToLower(catalogID), "xiangyun/") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(catalogID), "gemini/") {
			norm := strings.TrimPrefix(catalogID, "gemini/")
			if alt := geminiconfig.ResolveMappedCatalogID(norm, req.ImageSize); alt != "" {
				catalogID = alt
			}
		}
		subProv, modelInfo, ok := provider.ProviderForModel(catalogID)
		if !ok || subProv == nil || subProv.Name() == p.Name() {
			lastErr = pkgerrors.New(http.StatusInternalServerError, pkgerrors.TypeInternal, "xiangyun_backend", "model "+catalogID+" is not registered")
			continue
		}
		b := subProv.Name()
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
			if out.EffectiveUpstreamModel == "" {
				out.EffectiveUpstreamModel = upstreamWire
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
			log.Warn("xiangyun_try_upstream_failed", "provider", b, "catalog", catalogID, "err", err)
		} else {
			log.Info("xiangyun_try_upstream_failed", "provider", b, "catalog", catalogID)
		}
	}
	if lastErr == nil {
		return nil, pkgerrors.BadRequest("xiangyun_no_backend", "祥云未配置可用的模型列表")
	}
	return nil, lastErr
}

// DefaultModels is used when config leaves models empty.
var DefaultModels = []string{
	"gemini/gemini-3.1-flash-image-preview",
	"openai/gpt-image-2",
	"wan/wan2.7-image",
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)
