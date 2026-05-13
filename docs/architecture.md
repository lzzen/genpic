# Genpic Platform — Architecture Overview

> This is the condensed reference version of the full design document at
> `docs/genpic_生图应用设计_74ad4c73.plan.md`. For rationale and detailed
> trade-offs, see the ADRs in `docs/decisions/`.

## System diagram (M0 scope)

```
Clients
  ├── Web browser  ─────────────────────┐
  └── OpenAI-compatible client           │
       (Cherry Studio / AI Workspace)    │
                                         ▼
                              ┌──────────────────────┐
                              │  Genpic Platform      │
                              │                       │
                              │  GET  /models         │
                              │  POST /api/generate   │
                              │  GET  /jobs/{id}      │
                              │  GET  /health         │
                              │  GET  /  (static UI)  │
                              │                       │
                              │  pkg/ratelimit        │
                              │  pkg/billing          │
                              │  pkg/httpclient       │
                              └──────┬───────┬────────┘
                                     │       │
                  ┌──────────────────┼───────┼──────────────────────┐
                  ▼                  ▼       ▼
          OpenAI Images API    NewAPI/Gemini  DashScope (Wan2.7)
          (via aggregator)     aggregator
```

## Repository layout

```
genpic/
├── cmd/
│   ├── mvplite/      # MVP binary: stdlib net/http + config.yaml (mvp_lite)
│   └── genpic/       # Full platform binary
├── internal/
│   ├── api/          # HTTP handlers, DTOs, response helpers
│   ├── provider/
│   │   ├── openai/   # GPT Image adapter
│   │   ├── gemini/   # Gemini Banana adapter
│   │   └── wan/      # Wan2.7 DashScope adapter
│   └── jobstore/     # In-memory async job records (swap for DB later)
├── pkg/
│   ├── billing/      # Ledger interface + pricing table
│   ├── errors/       # OpenAI-compatible error types
│   ├── httpclient/   # Retry + logging HTTP client
│   ├── idempotency/  # Dedup store interface + in-memory impl
│   ├── logger/       # slog wrapper with redaction
│   ├── objstore/     # Object storage interface + Fake
│   ├── provider/     # Provider interface + registry + Fake
│   └── ratelimit/    # Sliding-window rate limiter
├── web/              # Static frontend (embedded at build time)
├── contracts/
│   └── providers.yaml # Machine-readable model contract table
├── openapi.yaml       # OpenAPI 3.1 contract (public HTTP surface)
├── config.example.yaml
├── .github/
│   ├── workflows/ci.yaml
│   └── pull_request_template.md
└── docs/
    ├── architecture.md   (this file)
    ├── runbook.md
    └── decisions/
        ├── ADR-001-stack-choice.md
        ├── ADR-002-credit-ledger.md
        └── ADR-003-token-storage-community.md
```

## Authentication and secrets

`cmd/genpic` does **not** implement platform-issued Bearer keys in-tree: **`/models`**
and **`/jobs`** are open at the application layer; use your edge or private network for access
control. Upstream provider defaults for adapters are read from server config /
environment variables; the browser still supplies `base_url` and `api_key` on
`POST /api/generate` for the live upstream call.

The embedded SPA (`POST /api/generate`) sends **per-request** `base_url` and
`api_key` for the upstream aggregator (same pattern as MVP Lite).

Product design §2.1 (Mode A) still describes the long-term “platform key at the
gateway” model; wire that at a reverse proxy or in a future release.

## Provider routing

`POST /api/generate` dispatches to a provider based on `model`:

| Model prefix | Provider | Upstream shape |
|---|---|---|
| `openai/*`  | OpenAI adapter | `POST {base}/v1/images/generations` (OpenAI Images API) |
| `gemini/*`  | Gemini adapter | `POST {base}/v1beta/models/{model}:generateContent` (IMAGE modality, `inlineData` in candidates) |
| `wan/*`     | Wan adapter    | `POST {base}/v1/images/generations` (multimodal body; images from `metadata.output` when present) |

All providers implement `pkg/provider.Provider` and are registered in `cmd/genpic/main.go`.

## Milestone map

| Milestone | Scope |
|---|---|
| **MVP Lite** | `cmd/mvplite` — single binary, no DB, no auth, direct proxy |
| **M0**       | `cmd/genpic` — all three providers, in-memory rate limit |
| **M1**       | **Current:** async `POST /api/generate` (`202` + poll `GET /jobs/{id}`). **Planned:** Redis/DB, billing, object storage |
| **M2**       | Gemini native generateContent path (`model-fingers/gemini-image.md`) |
| **M3**       | Wan sub-pages (image editing, multi-image) |
| **M4**       | Credit account management, admin UI, NewAPI integration wizard |
| **M5**       | Community feed, artwork visibility, paid SKUs |
