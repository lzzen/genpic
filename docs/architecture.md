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
                              │  GET  /v1/models      │
                              │  POST /v1/images/     │
                              │       generations     │
                              │  GET  /v1/jobs/{id}   │
                              │  GET  /health         │
                              │  GET  /  (static UI)  │
                              │                       │
                              │  pkg/auth   (bearer)  │
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
│   ├── mvplite/      # Zero-dep MVP binary (stdlib only)
│   └── genpic/       # Full platform binary
├── internal/
│   ├── api/          # HTTP handlers, DTOs, response helpers
│   ├── provider/
│   │   ├── openai/   # GPT Image adapter
│   │   ├── gemini/   # Gemini Banana adapter
│   │   └── wan/      # Wan2.7 DashScope adapter
│   ├── auth/         # (M1) DB-backed API key validation
│   ├── billing/      # (M1) Job billing wiring
│   └── storage/      # (M1) Object storage upload helpers
├── pkg/
│   ├── auth/         # API key interface + bearer middleware
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
├── openapi.yaml       # OpenAPI 3.1 contract (external v1 surface)
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

## Authentication model (Mode A)

Callers authenticate with a **platform-issued API key** (`Authorization: Bearer sk-...`).
The platform's server holds the upstream provider keys; callers never see them.

See design §2.1 and `pkg/auth/auth.go` for the implementation contract.

## Provider routing

`POST /v1/images/generations` dispatches to a provider based on `model`:

| Model prefix | Provider | Upstream shape |
|---|---|---|
| `openai/*`  | OpenAI adapter | `POST {base}/v1/images/generations` (OpenAI Images API) |
| `gemini/*`  | Gemini adapter | `POST {base}/v1/chat/completions` (single-turn, image in assistant) |
| `wan/*`     | Wan adapter    | `POST {base}/api/v1/services/aigc/multimodal-generation/generation` |

All providers implement `pkg/provider.Provider` and are registered in `cmd/genpic/main.go`.

## Milestone map

| Milestone | Scope |
|---|---|
| **MVP Lite** | `cmd/mvplite` — single binary, no DB, no auth, direct proxy |
| **M0**       | `cmd/genpic` — all three providers, static auth (env key), in-memory rate limit |
| **M1**       | Async job queue (Redis), DB-backed jobs + billing, object storage |
| **M2**       | Gemini full integration, `/v1/chat/completions` |
| **M3**       | Wan sub-pages (image editing, multi-image) |
| **M4**       | Credit account management, admin UI, NewAPI integration wizard |
| **M5**       | Community feed, artwork visibility, paid SKUs |
