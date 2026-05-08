# Genpic Platform — Operations Runbook

## Starting the service

### MVP Lite (development / minimal)

```bash
# No upstream auth — user supplies keys in the browser form.
go run ./cmd/mvplite
# or with a specific port:
PORT=9090 go run ./cmd/mvplite
```

### Full platform

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
| `OPENAI_BASE_URL` | No | — | OpenAI-compatible aggregator base URL |
| `OPENAI_API_KEY` | No | — | Server-side key for OpenAI channel |
| `GEMINI_BASE_URL` | No | — | Aggregator base URL for Gemini routing |
| `GEMINI_API_KEY` | No | — | Server-side key for Gemini channel |
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
