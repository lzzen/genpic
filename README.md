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

`go run ./cmd/genpic` 提供 **`POST /api/generate`**（与嵌入式主页一致）：请求 JSON 必须包含 **`base_url`**、**`api_key`**，由服务端转发到第三方；**无需**事先 `export GEMINI_*` 等环境变量即可在网页里生图。全平台始终启用任务存储：该接口返回 **`202 Accepted`** 与任务对象，客户端需轮询 **`GET /jobs/{id}`** 直至成功或失败；终端 **stderr** 仍会打印上游请求/响应（超长 **base64**、**thoughtSignature** 会脱敏）。

与 **MVP Lite** 相同，全平台会读取模块根 **`config.yaml`** 中的 **`mvp_lite.default_base_url`**（可选 **`-config /path/to/config.yaml`**），通过 **`GET /api/public-config`** 提供给浏览器默认 Base URL；**监听端口** 使用 **`server.port`**，可被环境变量 **`PORT`** 覆盖（**`mvp_lite.port`** 仅用于 **MVP Lite**）。

适配器默认上游地址与密钥仍可在 **`config.yaml` / 环境变量** 中配置（见 [docs/runbook.md](docs/runbook.md)）；**网页生图**以请求 JSON 里的 **`base_url` / `api_key`** 为准。

```bash
go run ./cmd/genpic
# Open http://localhost:8080 (or server.port / PORT)
```

```bash
go run ./cmd/genpic -config /etc/genpic/config.yaml
```

See `docs/runbook.md` for all environment variables and troubleshooting.

## API

| Endpoint | Description |
|---|---|
| `POST /api/generate` | Enqueue generation (`202` + job); poll `GET /jobs/{id}` (embedded UI does this automatically) |
| `GET  /models` | List available models |
| `GET  /jobs/{id}` | Job status and image `data` when succeeded |
| `GET  /jobs` | List recent jobs for the caller's session / user headers |
| `GET  /health` | Liveness check |

Full spec: [`openapi.yaml`](openapi.yaml) — render with [Scalar](https://scalar.com/) or Redoc.

## NewAPI integration

In NewAPI "chat application integration", set **Address** (`{address}`) to your
platform's public origin, e.g. `https://imgapi.example.com`. Generation is
**`POST {address}/api/generate`** for generation. **`GET {address}/models`**
and **`GET {address}/jobs/...`** cover model discovery and job polling.
These routes are not authenticated by this repo — protect them at the network
or gateway if needed.

The web UI still uses **`?address=`** and **`?key=`** for the **upstream**
aggregator URL and key (stored in the browser); that is separate from platform job APIs.

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
| M1 — async `POST /api/generate` + jobs (`/jobs`); DB + object storage | Partial (async + poll) |
| M2 — Gemini chat completions path | 🔲 Planned |
| M3 — Wan editing + multi-image | 🔲 Planned |
| M4 — credit accounts, admin UI | 🔲 Planned |
| M5 — community feed, paid SKUs | 🔲 Planned |
