# Genpic Platform — Operations Runbook

**正式服部署（MVP Lite）：[deploy-production.md](deploy-production.md)**

## Starting the service

### MVP Lite (development / minimal)

最短步骤与 **URL 传参**：仓库根目录 **[how-to-use.md](../how-to-use.md)**。

Copy `config.example.yaml` to `config.yaml` and set at least `mvp_lite` (see
example file). The browser loads defaults from `GET /api/public-config`.

```bash
go run ./cmd/mvplite
# or:
go run ./cmd/mvplite -config /path/to/config.yaml
```

Optional: `PORT=9090` overrides the listen port from yaml (`mvp_lite.port` for MVP Lite, `server.port` for Full platform) / default.

NewAPI-style deep link (query params are read once then stripped from the address bar):

`http://localhost:8080/?address=https%3A%2F%2Fapi.example.com&key=sk-...`

- **`address`** — optional if `mvp_lite.default_base_url` is set in `config.yaml`.
- **`key`** — may be masked (`sk****…`); the UI warns you to paste the full key.

Base URL + API Key are stored encrypted in **localStorage** (Web Crypto AES-GCM when available).

Without `config.yaml`, the server still starts; the UI has no default Base URL until you add the file or open a link with `?address=`.

### Full platform

与 **MVP Lite** 一样，可在模块根放置 **`config.yaml`**（或 **`go run ./cmd/genpic -config /path/to/config.yaml`**）：**`mvp_lite.default_base_url`** 由 **`GET /api/public-config`** 暴露给嵌入式主页；**`server.port`** 为全平台默认监听端口（**`mvp_lite.port`** 仅给 **MVP Lite** 用），**`PORT`** 环境变量优先。无配置文件时进程照常启动，浏览器需自行填写 Base URL 或使用 **`?address=`**。

`GEMINI_BASE_URL` 为可选：仅当你使用 **`POST /v1/images/generations`** 且希望由服务端持有上游密钥时才需要。嵌入式主页走 **`POST /api/generate`** 时，请在请求 JSON 里传 **`base_url`** 与 **`api_key`**（与浏览器表单一致）；每次调用会在运行 `go run ./cmd/genpic` 的终端 **stderr** 打印发往第三方的请求 JSON 与响应 JSON（其中超长 **base64**、**thoughtSignature** 会被替换为占位符，避免刷屏）。

`GEMINI_BASE_URL` 应为**仅含协议与主机**的地址（不要带 `/v1` 等路径）。Gemini 适配器会请求 `POST {GEMINI_BASE_URL}/v1beta/models/{model}:generateContent`（与 `model-fingers/gemini-image.md` 一致）。

```bash
export OPENAI_BASE_URL="https://your-aggregator.example.com"
export OPENAI_API_KEY="sk-..."
export GEMINI_BASE_URL="https://your-aggregator.example.com"
export GEMINI_API_KEY="sk-..."
export WAN_BASE_URL="https://dashscope.aliyuncs.com"
export WAN_API_KEY="sk-..."
export LOG_FORMAT=json
export PORT=8080

go run ./cmd/genpic
```

Providers without `*_BASE_URL` set are silently disabled at startup. A warning
is logged for each missing provider.

## Health check

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `PORT` | No | `8080` | Listen port |
| `LOG_FORMAT` | No | `text` | `text` or `json` |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, `error` |
| `GENPIC_DEV` | No | — | `1` / `true` / `yes` / `on`: log Gemini `generateContent` URL, redacted key, request/response JSON previews (large `inlineData` redacted), and on **model not found** dump registered catalog/upstream ids |
| `OPENAI_BASE_URL` | No | — | OpenAI-compatible aggregator base URL |
| `OPENAI_API_KEY` | No | — | Server-side key for OpenAI channel |
| `GEMINI_BASE_URL` | No | — | 可选；仅 `POST /v1/images/generations` 使用。`POST /api/generate` 用 JSON 里的 `base_url` |
| `GEMINI_API_KEY` | No | — | 可选；仅 `POST /v1/images/generations`。`POST /api/generate` 用 JSON 里的 `api_key` |
| `WAN_BASE_URL` | No | — | DashScope endpoint (CN or AP) |
| `WAN_API_KEY` | No | — | Server-side DashScope key |

## Troubleshooting

### 502 on `/v1/images/generations`

1. Check the structured log for `"upstream_error"` — it includes the upstream
   HTTP status and message.
2. Verify the `*_BASE_URL` and `*_API_KEY` for the failing provider are correct.
3. If the upstream returns 429, the client is being rate-limited. Check quota
   on the aggregator dashboard.

### Model not found (404)

The `model` field in the request does not match any registered model ID.
Run `GET /v1/models` to see what is currently registered, and compare against
`contracts/providers.yaml`.

For Gemini image models, this often means the **model string does not match**
the built-in catalog (see `GET /v1/models`). When using **`POST /api/generate`**, ensure **`base_url` and `api_key`** are present in the JSON body.

With **`GENPIC_DEV=1`**, startup logs list registered models when resolution fails.

### Provider not appearing in `/v1/models`

The provider's `*_BASE_URL` environment variable is missing. Check startup
logs for `"provider disabled"` warnings.

### Upstream timeout (504)

The default timeout for each model is set in `contracts/providers.yaml`
(`timeout_s`). If the upstream is consistently timing out, increase the
timeout via provider config or contact the aggregator operator.

## Upstream failure switchover (M0)

In M0 there is no automatic failover. If an upstream is down:

1. Remove or clear the failing provider's `*_BASE_URL` env var.
2. Restart the service — the provider will be excluded from `/v1/models`.
3. Notify users of the outage via status page.

Full automatic failover is planned for M4.

## Quota exhausted

If an upstream returns `402` or a quota-exhaustion message:

1. Contact the NewAPI aggregator operator to top up the channel quota.
2. The model will continue returning errors until the quota is restored.
3. In M4+, platform-side quota tracking will catch this before the upstream does.

## Rolling update (systemd)

```bash
# Build new binary
GOOS=linux GOARCH=amd64 go build -o /opt/genpic/genpic ./cmd/genpic

# Reload systemd (zero-downtime with socket activation, or restart)
systemctl restart genpic
```
