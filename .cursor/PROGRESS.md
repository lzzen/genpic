# Genpic 开发进度

> 最后更新：2026-05-15

## 里程碑状态

| 里程碑 | 状态 | 说明 |
|--------|------|------|
| MVP Lite | ✅ 完成 | 单 Go 服务 + HTML/JS；base_url + api_key + model + prompt 即可生图 |
| M0 — multi-provider 骨架, OpenAPI, pkg/ | ✅ 完成 | chi-free net/http; pkg/provider, httpclient, errors, logger, ratelimit, refimages, modelmap, mvpconfig |
| M1 — async POST /api/generate + jobs; MySQL job store + 图片 artifacts | ✅ 完成 | 202 + 轮询 GET /jobs/{id}; in-memory fallback; artifacts 写磁盘 |
| **M2 — Gemini chat completions path** | ✅ 完成 | POST /v1/chat/completions；OpenAI 兼容客户端直接生图 |
| **M3 — Wan editing + multi-image** | ✅ 完成 | Wan edit_type / bbox_list + Web UI 编辑类型与 bbox 区域 |
| **M4 — 用户系统 + 异步 UX** | ✅ 完成 | Cookie 会话注册/登录；PBKDF2 密码；匿名 jobs 登录迁移；提示词按归属隐藏；前端任务队列并行轮询 |
| **M5 — 社区 + 创作同款** | ✅ 完成 | job visibility；GET /api/community/feed；GET job 含 params；社区 UI + 隐私开关 + 创作同款 |
| **M6 — 生成模板** | ✅ 完成 | `generation_templates` 表；按模型列出公用/私有模板；从成功任务保存；管理员公用模板与 `is_admin` |

---

## M6 — 生成模板（预设）

**目标**：每个模型有可选模板列表；登录用户可将**自己的成功任务**保存为私有模板；**管理员**（`auth.admin_emails` / `GENPIC_ADMIN_EMAILS`）可发布**公用模板**；侧栏表单区横向卡片 +「立即使用」套用提示词、参数与参考图（参考图由客户端随保存请求提交）。

### 数据表

- **`generation_templates`**：`id`，`user_id`，`visibility`（`private`|`public`），`title`，`primary_model`，`models_json`（支持的模型 id 列表，JSON 数组），`prompt`，`params_json`（与任务一致的 JobParams），`reference_images_json`（可选，与生成 API 同结构的 base64 引用图），`result_image_url`（成品预览 URL，通常 `/api/artifacts/...`），`created_at` / `updated_at`。
- 迁移：`internal/dbmigrate/migrations/003_generation_templates.sql`（启动时自动执行）。

### API

- `GET /api/templates?model=` — 可选登录；未登录仅公用；已登录附加当前用户该模型的私有模板。
- `POST /api/templates` — 需登录；`job_id` + 可选 `reference_images`；`visibility: public` 仅管理员。
- `DELETE /api/templates/{id}` — 本人删私有；管理员可删任意公用模板。
- `GET /api/auth/me` 增加 `is_admin`（邮箱命中管理员列表时为 `true`）。

### 前端

- 侧栏在「提示词」与「图片比例」之间：**快速选择模板**横滑列表、计数、「查看更多」横向滚动、**立即使用**套用（逻辑对齐「创作同款」）。
- 生成详情 / 历史中成功任务：**保存为模板** /（管理员）**保存为公用模板**、历史卡片 **存为模板** / **公用模板**。

### 进度

- [x] DDL + dbmigrate
- [x] `internal/templatestore`（MySQL）
- [x] Handlers + 路由 + OpenAPI + `config.example.yaml` 说明
- [x] Web UI 模板区与保存入口

---

## M4 — 用户系统 + 异步任务 UX

**目标**：内置账号（MySQL）、HTTP-only `genpic_session`、匿名历史归属迁移、非作者看不到完整 prompt（除非策略允许）、前端可多任务后台轮询。

### 实现要点

- **密码**：`crypto/pbkdf2`（HMAC-SHA256），非 bcrypt（离线构建友好）。
- **迁移**：`internal/dbmigrate` 嵌入 goose 风格 SQL（`-- +goose Up`）；启动时 `dbmigrate.Up`。
- **API**：`POST /api/auth/register|login|logout`，`GET /api/auth/me`，`GET|PUT /api/user/settings`；`internal/auth` 中间件 `OptionalAuth` 注入 ctx。
- **`callerScope`**：`internal/api/caller.go` 优先会话用户 id，保留 header 匿名会话。
- **前端**：`web/index.html` 登录/注册/隐私设置、`#task-queue-bar`、202 后非阻塞轮询。

### 进度

- [x] users / user_sessions / user_settings DDL + 应用迁移
- [x] Auth 包 + handlers + main 路由
- [x] 登录/注册迁移匿名 `generation_jobs`
- [x] `toJobResponse` prompt/params 归属规则
- [x] 任务队列 UX

---

## M5 — 社区 + 创作同款

**目标**：作品公开/私密、`community_listed_at`、`GET /api/community/feed`、job JSON 带 `params`、社区列表与「创作同款」预填表单。

### 实现要点

- **`PUT /api/jobs/{job_id}/visibility`**：作者登录后可 `public` / `private`。
- **Feed**：分页 `limit` / `cursor`；匿名 prompt 受作者 `prompt_public` 约束；已登录可看他人 prompt；`params` 仅登录返回。
- **`community_auto_public`**：成功任务可按用户设置自动公开（见 `generate` 完成路径）。
- **OpenAPI**：`openapi.yaml` 已补充 auth / settings / visibility / community / `JobParams`。

### 进度

- [x] visibility 列 + ListPublic / SetVisibility
- [x] community handlers + 前端 feed + 历史「公开」toggle
- [x] 创作同款（params + prompt 预填）

---

## M2 — Gemini chat completions path

（摘要保留；详见历史提交与 `openapi.yaml`。）

### 进度

- [x] `internal/api/chatcompletions.go` + 测试 + 路由 + openapi

---

## M3 — Wan editing + multi-image

### 进度

- [x] GenerateRequest / Wan adapter / Web UI bbox + edit_type

---

## 后续可选（未列入上述里程碑）

| 方向 | 备注 |
|------|------|
| 算力账本 / api_keys / billing | 原 PROGRESS 中「M4 账本」构想；可与当前用户系统并行演进 |
| `pkg/objstore` | S3/OSS 工件存储 |
| 管理后台强化 | `GET /admin` 仍为占位级 UI |
| 模板管理后台 | 当前为侧栏 + API；无独立 CRUD 页 |

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
| `pkg/mvpconfig` | ✅ | config.yaml + env；含 `auth.session_ttl`、`auth.admin_emails` |
| `pkg/objstore` | 🔲 | 对象存储抽象 |
| `pkg/billing` | 🔲 | 计费 |
| `pkg/idempotency` | 🔲 | 幂等 |
