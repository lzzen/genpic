# Genpic MVP Lite — 正式服部署指南

面向：在 **Linux 服务器**（含 **宝塔**）上长期运行 `cmd/mvplite`，对外提供 HTTPS 与稳定进程管理。  
本地试用见根目录 [how-to-use.md](../how-to-use.md)。

---

## 1. 架构约定

| 组件 | 说明 |
|------|------|
| 进程 | 编译后的 **`mvplite` 二进制**（不要用 `go run` 跑生产） |
| 配置 | **`config.yaml`** 放在**模块根**（与 `go.mod` 同级），内含 `mvp_lite`；**勿把用户 API Key 写进该文件** |
| 用户 | 使用 **`www`**（或宝塔站点用户），**不要**长期 root |
| 对外 | 前面加 **Nginx / Caddy** 做 TLS 与反代；Go 进程监听 **本机回环 + 高端口** 即可 |

---

## 2. 服务器准备

1. 安装 **Go 1.22+**（不低于 `go.mod` 中的 `go` 版本）。
2. 克隆或上传项目到固定目录，例如：**`/www/wwwroot/ai-apps/genpic`**。
3. 在**模块根**创建 `config.yaml`（可从 `config.example.yaml` 复制），至少配置：

```yaml
mvp_lite:
  port: "18080"          # 本机监听，与 Nginx upstream 一致
  default_base_url: "https://你的上游OpenAI兼容网关"
```

4. 仓库中保留 **`go.sum`**；服务器能访问公网模块源（或已配置 `GOPROXY`）。

---

## 3. 编译（在模块根执行）

```bash
cd /www/wwwroot/ai-apps/genpic
CGO_ENABLED=0 go build -ldflags="-s -w" -o mvplite ./cmd/mvplite
```

- 产物：**`./mvplite`**（与 `go.mod` 同目录）。
- 交叉编译示例：  
  `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o mvplite ./cmd/mvplite`  
  ARM 服务器将 `GOARCH` 改为 `arm64`。

---

## 4. 权限与运行用户

```bash
chown www:www /www/wwwroot/ai-apps/genpic/mvplite
chmod 750 /www/wwwroot/ai-apps/genpic/mvplite
chown www:www /www/wwwroot/ai-apps/genpic/config.yaml
chmod 640 /www/wwwroot/ai-apps/genpic/config.yaml
```

以 **www** 试跑：

```bash
sudo -u www bash -lc 'cd /www/wwwroot/ai-apps/genpic && ./mvplite'
```

自检：

```bash
curl -fsS http://127.0.0.1:18080/health
curl -fsS http://127.0.0.1:18080/api/public-config
```

（端口以 `mvp_lite.port` 或下文 **`PORT` 环境变量**为准；若设置了 `PORT`，会**覆盖** yaml 中的端口。）

---

## 5. systemd（推荐）

创建 **`/etc/systemd/system/genpic-mvplite.service`**：

```ini
[Unit]
Description=Genpic MVP Lite
After=network.target

[Service]
Type=simple
User=www
Group=www
WorkingDirectory=/www/wwwroot/ai-apps/genpic
ExecStart=/www/wwwroot/ai-apps/genpic/mvplite -config /www/wwwroot/ai-apps/genpic/config.yaml
Restart=on-failure
RestartSec=5
# 可选：覆盖监听端口（优先于 config.yaml 中的 mvp_lite.port）
# Environment=PORT=18080

[Install]
WantedBy=multi-user.target
```

启用与日志：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now genpic-mvplite
sudo systemctl status genpic-mvplite
journalctl -u genpic-mvplite -f
```

发版：重新 `go build` 覆盖二进制后执行 `sudo systemctl restart genpic-mvplite`。

---

## 6. Nginx 反代 + HTTPS（示例）

Go 监听 `127.0.0.1:18080`，Nginx 对外 `443`：

```nginx
server {
    listen 443 ssl http2;
    server_name img.example.com;

    ssl_certificate     /path/to/fullchain.pem;
    ssl_certificate_key /path/to/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:18080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
```

生图较慢时 **`proxy_read_timeout`** 建议 ≥ **300s**。

---

## 7. 宝塔面板（避坑）

### 7.1 禁止路径叠成 `cmd/mvplite/cmd/mvplite`

- **运行目录 / 项目目录**：必须是 **模块根**  
  **`/www/wwwroot/ai-apps/genpic`**（含 `go.mod`、`config.yaml`、`mvplite`）。
- **不要**把运行目录设成 **`.../genpic/cmd/mvplite`**，又在别处填相对路径 **`cmd/mvplite`**，否则面板会拼成 **`.../cmd/mvplite/cmd/mvplite`** 并报 `directory not found`。

### 7.2 启动项与参数

- **启动 / 项目执行文件**：二进制绝对路径  
  **`/www/wwwroot/ai-apps/genpic/mvplite`**
- **执行命令 / 参数**（若单独一栏）：  
  **`-config /www/wwwroot/ai-apps/genpic/config.yaml`**  
  工作目录已是模块根且使用默认文件名 `config.yaml` 时，可省略 `-config`。
- 生产环境用 **`go build` 产物**；不要依赖 **`go run .../main.go`** 作为长期启动方式。

### 7.3 用户与端口

- 进程用户选 **www**；首次以该用户构建时模块缓存可能重新下载，属正常。
- 仅通过 Nginx 对外时，防火墙可只放行 **443**；**18080** 仅本机访问即可。

---

## 8. 排错简表

| 现象 | 排查 |
|------|------|
| 502 / 网关超时 | 加大 `proxy_read_timeout`；看 `journalctl` 是否 panic / OOM |
| 无默认 Base URL | `mvp_lite.default_base_url` 与 `config` 路径；`/api/public-config` 是否 200 |
| Permission denied | `chown` / `chmod`；工作目录是否可进入 |

---

## 9. 安全清单（最低限度）

- [ ] `config.yaml` 不含用户 API Key  
- [ ] 非 root 运行、权限最小  
- [ ] 对外 HTTPS，仅反代本机端口  
- [ ] 防火墙最小暴露；SSH 用密钥  
- [ ] 发版：拉代码 → **`go build`** → **重启** 服务  

---

## 10. 文档索引

| 文档 | 用途 |
|------|------|
| [how-to-use.md](../how-to-use.md) | 本地使用、URL 传参 |
| [runbook.md](runbook.md) | 全平台环境变量与运维 |
| 本文 | **正式服**：编译、systemd、Nginx、宝塔 |

**Full Platform（`cmd/genpic`）** 若需单独生产文档，可后续补充；本文以 **MVP Lite** 为主。
