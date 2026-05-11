# Daily 冒烟测试

需真实上游网络；默认 **`go test ./...` 不会执行**（使用 build tag `integration`）。

## Gemini 3.1 Flash Image Preview

环境变量：

| 变量 | 含义 |
|------|------|
| `GENPIC_DAILY_UPSTREAM_BASE_URL` | OpenAI 兼容聚合根地址（如 NewAPI），与浏览器里 Base URL 一致 |
| `GENPIC_DAILY_UPSTREAM_API_KEY` | 调用聚合站的密钥 |

可选：

| 变量 | 含义 |
|------|------|
| `GENPIC_DAILY_MVPLITE_URL` | 若设置（如 `http://127.0.0.1:8080`），则改为请求本地 **MVP Lite** 的 `POST /api/generate`，不再直连聚合站 |

执行：

```bash
cd /path/to/genpic
go test ./daily -tags=integration -count=1 -timeout 5m
```

成功标准：HTTP 200 且响应中含至少一张图（`b64_json` 或 `url`）。

浏览器手工页：`/daily/gemini-3-1-preview.html`（需先启动 **`go run ./cmd/genpic`** 或 MVP Lite，且配置 Gemini 上游）。
