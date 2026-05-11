# Genpic — 视觉工厂生图平台

Image generation platform supporting OpenAI GPT Image 2, Google Gemini Banana
series, and Aliyun Tongyi Wanxiang 2.7 via an OpenAI-compatible API.

**新手（MVP Lite 传参、链接、配置）先看：[how-to-use.md](how-to-use.md)**  
**正式服部署（编译、systemd、Nginx、宝塔）：[docs/deploy-production.md](docs/deploy-production.md)**

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

`go run ./cmd/genpic` 提供 **`POST /api/generate`**（与嵌入式主页一致）：请求 JSON 必须包含 **`base_url`**、**`api_key`**，由服务端转发到第三方；**无需**事先 `export GEMINI_*` 等环境变量即可在网页里生图。每次 `/api/generate` 会在运行服务的 **终端 stderr** 打印请求与响应 JSON（超长 **base64**、**thoughtSignature** 会脱敏为占位符）。

与 **MVP Lite** 相同，全平台会读取模块根 **`config.yaml`** 中的 **`mvp_lite.default_base_url`**（可选 **`-config /path/to/config.yaml`**），通过 **`GET /api/public-config`** 提供给浏览器默认 Base URL；**监听端口** 使用 **`server.port`**，可被环境变量 **`PORT`** 覆盖（**`mvp_lite.port`** 仅用于 **MVP Lite**）。

可选：若使用 **`POST /v1/images/generations`**，仍可通过进程环境变量配置默认上游地址与密钥（见 [docs/runbook.md](docs/runbook.md)）。

```bash
go run ./cmd/genpic
# Open http://localhost:8080 (or server.port / PORT)
```

```bash
go run ./cmd/genpic -config /etc/genpic/config.yaml
```

See `docs/runbook.md` for all environment variables and troubleshooting.

## API (OpenAI-compatible)

| Endpoint | Description |
|---|---|
| `GET  /v1/models` | List available models |
| `POST /v1/images/generations` | Generate images |
| `GET  /v1/jobs/{id}` | Job status (async; in-memory in `cmd/genpic`) |
| `GET  /health` | Liveness check |

Full spec: [`openapi.yaml`](openapi.yaml) — render with [Scalar](https://scalar.com/) or Redoc.

## NewAPI integration

In NewAPI "chat application integration", set **Address** (`{address}`) to your
platform's public origin, e.g. `https://imgapi.example.com` (the service's `/v1`
is OpenAI-compatible and is not authenticated by this repo — protect it at the
network or gateway if needed).

The web UI still uses **`?address=`** and **`?key=`** for the **upstream**
aggregator URL and key (stored in the browser); that is separate from `/v1`.

## Repository layout

```
cmd/mvplite/      MVP Lite binary
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
| M1 — async `/v1` jobs (in-memory); DB + object storage | Partial (async only) |
| M2 — Gemini chat completions path | 🔲 Planned |
| M3 — Wan editing + multi-image | 🔲 Planned |
| M4 — credit accounts, admin UI | 🔲 Planned |
| M5 — community feed, paid SKUs | 🔲 Planned |
