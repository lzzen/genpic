# Genpic — 视觉工厂生图平台

Image generation platform supporting OpenAI GPT Image 2, Google Gemini Banana
series, and Aliyun Tongyi Wanxiang 2.7 via an OpenAI-compatible API.

**新手（MVP Lite 传参、链接、配置）先看：[how-to-use.md](how-to-use.md)**

## Quick start — MVP Lite

MVP Lite reads **`config.yaml`** (same file shape as the full platform example;
see `mvp_lite` in `config.example.yaml`). Optional: `-config /path/to/config.yaml`.

- **`mvp_lite.default_base_url`**: default OpenAI-compatible Base URL for the web UI
  (exposed as `GET /api/public-config`; no secrets).
- **`mvp_lite.port`**: listen port unless overridden by `PORT` env.

The web UI supports **`?address=`** and **`?key=`** query parameters (NewAPI jump),
encrypts Base URL + API Key into **localStorage**, and warns on masked keys (`****`).

```bash
go run ./cmd/mvplite
# Open http://localhost:8080 (or the port from config / PORT env)
```

```bash
go run ./cmd/mvplite -config /etc/genpic/config.yaml
```

## Full platform

Requires upstream provider credentials (held server-side — never exposed to callers).

```bash
export OPENAI_BASE_URL="https://your-aggregator.example.com"
export OPENAI_API_KEY="sk-..."
export GEMINI_BASE_URL="https://your-aggregator.example.com"
export GEMINI_API_KEY="sk-..."
export WAN_BASE_URL="https://dashscope.aliyuncs.com"
export WAN_API_KEY="sk-..."

go run ./cmd/genpic
# Open http://localhost:8080
```

See `docs/runbook.md` for all environment variables and troubleshooting.

## API (OpenAI-compatible)

| Endpoint | Description |
|---|---|
| `GET  /v1/models` | List available models |
| `POST /v1/images/generations` | Generate images |
| `GET  /v1/jobs/{id}` | Job status (async, M1+) |
| `GET  /health` | Liveness check |

Full spec: [`openapi.yaml`](openapi.yaml) — render with [Scalar](https://scalar.com/) or Redoc.

## NewAPI integration

In NewAPI "chat application integration", set:

- **Address** (`{address}`): your platform's public origin, e.g. `https://imgapi.example.com`
- **Key** (`{key}`): a platform-issued API key

The platform's upstream credentials are held server-side and never exposed.

## Repository layout

```
cmd/mvplite/      MVP Lite binary (stdlib only)
cmd/genpic/       Full platform binary
internal/         Business logic (not importable externally)
pkg/              Reusable packages with stable interfaces
web/              Static frontend (embedded at build time)
contracts/        providers.yaml — model contract table
openapi.yaml      API contract
docs/             Architecture, runbook, ADRs
```

## Development

```bash
go test ./...
go vet ./...
gofmt -s -l .
```

See `.github/workflows/ci.yaml` for the full CI pipeline.

## Milestones

| Milestone | Status |
|---|---|
| MVP Lite | ✅ Done |
| M0 — multi-provider skeleton, OpenAPI, pkg/ | ✅ Done |
| M1 — async jobs, DB, object storage | 🔲 Planned |
| M2 — Gemini chat completions path | 🔲 Planned |
| M3 — Wan editing + multi-image | 🔲 Planned |
| M4 — credit accounts, admin UI | 🔲 Planned |
| M5 — community feed, paid SKUs | 🔲 Planned |
