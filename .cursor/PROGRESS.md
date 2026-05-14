# Genpic 开发进度

> 最后更新：2026-05-14

## 里程碑状态

| 里程碑 | 状态 | 说明 |
|--------|------|------|
| MVP Lite | ✅ 完成 | 单 Go 服务 + HTML/JS；base_url + api_key + model + prompt 即可生图 |
| M0 — multi-provider 骨架, OpenAPI, pkg/ | ✅ 完成 | chi-free net/http; pkg/provider, httpclient, errors, logger, ratelimit, refimages, modelmap, mvpconfig |
| M1 — async POST /api/generate + jobs; MySQL job store + 图片 artifacts | ✅ 完成 | 202 + 轮询 GET /jobs/{id}; in-memory fallback; artifacts 写磁盘 |
| **M2 — Gemini chat completions path** | ✅ 完成 | POST /v1/chat/completions；OpenAI 兼容客户端直接生图 |
| **M3 — Wan editing + multi-image** | ✅ 完成 | Wan edit_type / bbox_list + Web UI 编辑类型与 bbox 区域 |
| M4 — 算力账本、管理后台 | 🔲 计划中 | credit_ledger、api_keys 表、管理 UI |
| M5 — 社区 feed、付费 SKU | 🔲 计划中 | 作品可见性、订阅、按次解锁 |

---

## M2 — Gemini chat completions path

**目标**：实现 `POST /v1/chat/completions`，让 Cherry Studio / AI as Workspace / OpenAI-SDK 等客户端通过标准 Chat API 触发图片生成。

### 设计决策

- **认证**：`Authorization: Bearer <token>` 中的 token 作为上游 `api_key` 覆盖（与 POST /api/generate 的 JSON body 机制一致）。
- **base_url**：
  1. 优先读 `X-Base-Url` 请求头（明确指定上游）。
  2. fallback 到服务端 `config.yaml` 对应 provider 的 `base_url`（Mode A 正式部署场景）。
- **路由 model → provider**：与 `POST /api/generate` 完全相同的 `provider.ProviderForModel` 查找。
- **引用图片**：`messages[].content[]` 中 `type:"image_url"` 的 base64 data URL 转换为 `ReferenceImage`。
- **响应格式**：同步返回 OpenAI chat completions shape；`choices[0].message.content` 为 multimodal array，每张图一个 `image_url` part（`url: "data:image/png;base64,..."`）。
- **超时**：直接继承 provider `TimeoutSeconds`；不过 async job system（chat clients 无法轮询）。

### 实现文件

| 文件 | 变更 |
|------|------|
| `internal/api/chatcompletions.go` | 新建：HandleChatCompletions + 请求/响应类型 |
| `cmd/genpic/main.go` | 注册 `POST /v1/chat/completions` 路由 |
| `openapi.yaml` | 添加 `/v1/chat/completions` path + schema |

### 进度

- [x] 设计方案确认
- [x] `internal/api/chatcompletions.go` 编写完成
- [x] `internal/api/chatcompletions_test.go` 测试覆盖（6 个用例）
- [x] 路由注册 (`cmd/genpic/main.go`)
- [x] openapi.yaml 更新（`/v1/chat/completions` path + ChatCompletionsRequest/Response schema）
- [x] `pkg/provider/Unregister` 辅助函数（供测试清理使用）
- [x] go build 通过
- [x] go test 通过（全量）

---

## M3 — Wan editing + multi-image

**目标**：完善万相图像编辑能力（指定编辑类型、bbox 区域控制）和前端多图上传 UI。

### 设计决策

- **edit_type**：在 `GenerateRequest` 增加 `WanEditType string`（`text_to_image` | `image_edit` | `inpaint`）；Wan adapter 根据类型调整 DashScope body。
- **bbox_list**：在 `GenerateRequest` 增加 `WanBboxList []WanBbox`；Wan adapter 直接放入 `parameters.bbox_list`。
- **多图 UI**：web/index.html 的 Wan 面板增加「编辑类型」下拉 + bbox 输入区（仅 Wan 子页显示）。

### 实现文件

| 文件 | 变更 |
|------|------|
| `internal/api/generate.go` | `GenerateRequest` 增加 `WanEditType`、`WanBboxList` 字段 |
| `pkg/provider/provider.go` | `GenerateRequest` 增加 `WanEditType`、`WanBboxList` |
| `internal/provider/wan/wan.go` | `buildDashScopeRequest` 支持 edit_type + bbox_list |
| `web/index.html` | Wan 面板：编辑类型选择 + bbox 输入 |

### 进度

- [x] `pkg/provider.GenerateRequest` 增加 `WanEditType string` + `WanBboxList []WanBbox`
- [x] `pkg/provider.WanBbox` 类型定义 (x1/y1/x2/y2)
- [x] `internal/api.GenerateRequest` 增加 `wan_edit_type` + `wan_bbox_list` JSON 字段
- [x] Wan adapter `buildDashScopeRequest` — edit_type 验证 + bbox_list 构造
- [x] Web UI — 编辑类型下拉（文生图 / 图像编辑 / 局部重绘）
- [x] Web UI — bbox 添加/删除 UI（局部重绘模式下显示）
- [x] go build + test 通过

---

## 已完成细节

### M1 关键实现（参考）

- **Job store**：MySQL 持久化（`internal/jobstore/mysql.go`）+ in-memory fallback（24h TTL）。
- **Artifacts**：生成的 b64 图片写入 `data/genpic-artifacts/{job_id}/`，通过 `GET /api/artifacts/{job_id}/{name}` 提供服务。
- **Admin dashboard**：`GET /admin`（HTML）；`GET /admin/jobs`、`GET /admin/stats`（JSON API）。
- **Integration wizard**：`GET /integrate`（HTML）；一键复制 Cherry Studio / AI as Workspace 集成配置。

### 公共包状态

| 包 | 状态 | 备注 |
|----|------|------|
| `pkg/httpclient` | ✅ | 超时/重试/熔断骨架 |
| `pkg/provider` | ✅ | Provider 接口 + Registry + Fake |
| `pkg/errors` | ✅ | 统一错误码 + OpenAI 兼容错误体 |
| `pkg/logger` | ✅ | slog wrapper + 脱敏 |
| `pkg/ratelimit` | ✅ | 滑动窗口 in-memory |
| `pkg/modelmap` | ✅ | model ID 重映射 |
| `pkg/refimages` | ✅ | 引用图片 base64 解析 + 大小限制 |
| `pkg/compatctx` | ✅ | per-request upstream 凭证注入 |
| `pkg/mvpconfig` | ✅ | config.yaml + env var 读取 |
| `pkg/objstore` | 🔲 | M4/M5 时实现（S3/OSS） |
| `pkg/billing` | 🔲 | M4 时实现 |
| `pkg/auth` | 🔲 | M4 时实现（api_keys 表 + scope） |
| `pkg/idempotency` | 🔲 | M4 时实现 |
