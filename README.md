# Genpic

MVP Lite：单 Go 服务 + 原生 HTML，通过 OpenAI 兼容 `POST /v1/images/generations` 调用上游（如 NewAPI）生图。

## 运行

```bash
cd /path/to/genpic
go run ./cmd/mvplite
```

浏览器打开：<http://127.0.0.1:8080/>

可选环境变量：`PORT`（默认 `8080`，监听 `:{PORT}`）。

## 接口

- `GET /` — 静态页面
- `GET /health` — `{"ok":true}`
- `POST /api/generate` — JSON body：
  - `base_url`（必填）：如 `https://your-gateway.example.com/v1`
  - `api_key`（必填）：Bearer Token
  - `model_id`（必填）
  - `prompt`（必填）
  - `size`（可选，默认 `1024x1024`）
  - `response_format`（可选，`url` | `b64_json`，默认 `url`）
  - `n`（可选，默认 1，最大 10）
  - `quality`（可选，透传给上游）

成功响应：`{"success":true,"images":[{"url":"..."}]}` 或带 `b64_json`。

## 安全说明（MVP）

- 本版本允许在页面中填写上游 **Base URL** 与 **API Key**，用于快速联调；**不要在公网暴露且无鉴权地部署**。
- 服务端会对 `base_url` 解析 DNS，**拒绝解析到内网/回环地址**，以降低 SSRF 风险。
- 完整平台见 [docs/genpic_生图应用设计_74ad4c73.plan.md](docs/genpic_生图应用设计_74ad4c73.plan.md)。
