# Genpic MVP Lite — 最短使用说明

面向：**只想尽快在浏览器里生图**的小白。完整平台见 `README.md`。

---

## 1. 准备配置文件

在**仓库根目录**（和 `go.mod` 同级）放一份 `config.yaml`。

最快做法：复制示例再改两行。

```bash
cp config.example.yaml config.yaml
```

用编辑器打开 `config.yaml`，至少确认有 **`mvp_lite`** 段，例如：

```yaml
mvp_lite:
  port: "8080"
  default_base_url: "https://你的OpenAI兼容网关地址"
```

- **`default_base_url`**：浏览器里「Base URL」的默认值；不配也可以，但就要自己在页面里填，或用下面链接里的 `address` 传进去。
- **不要把 API Key 写进这个文件**（Key 只放在浏览器 / 链接参数里）。

配置文件不在根目录时，再用：

`go run ./cmd/mvplite -config /绝对路径/config.yaml`

---

## 2. 启动

在**仓库根目录**执行（默认就会读当前目录下的 `config.yaml`）：

```bash
go run ./cmd/mvplite
```

浏览器打开终端里提示的地址，一般是：`http://localhost:8080`（端口以你 `config.yaml` 里为准，也可用环境变量 `PORT` 覆盖）。

---

## 3. 链接传参（NewAPI 跳转用）

在浏览器地址栏打开**带查询参数**的链接，页面会自动把参数填进「API 配置」，并**从地址栏删掉参数**（避免把 Key 留在书签/历史里）。

| 参数       | 含义 | 是否必填 |
|------------|------|----------|
| `address`  | OpenAI 兼容网关的根地址（如 `https://api.xxx.com`，一般**不要**带末尾 `/`，也**不要**写 `/v1`） | 否；不写则用 `config.yaml` 里的 `mvp_lite.default_base_url` |
| `key`      | 你的 API Key（`sk-...`） | 建议填；不写就要自己在页面里粘贴 |

**示例（请换成你自己的地址和 Key）：**

```
http://localhost:8080/?address=https%3A%2F%2Fapi.example.com&key=sk-your-real-secret-key
```

- 若地址或 Key 里含有 `&`、`=`、`#`、中文等，必须用 **URL 编码**后再拼进链接。  
  在浏览器控制台可快速编码：

  ```js
  encodeURIComponent("https://api.example.com")
  ```

**NewAPI 常见坑：** 控制台复制出来的 Key 经常是**脱敏**的（例如 `sk****xdf1`）。这种 **不能** 调接口。页面会橙色提示——请改填**完整** Key。

---

## 4. 填好以后

1. 展开侧边栏 **「API 配置」**，检查 Base URL 和 API Key。  
2. 选模型、写 **Prompt**，点 **「生成图片」**。

Base URL 和 Key 会加密存在本机 **localStorage**（换浏览器或清缓存要重新填或重新用链接带参打开一次）。

---

## 5. 对照清单

| 你想做的事 | 做法 |
|------------|------|
| 默认就有网关地址 | 在 `config.yaml` 里写 `mvp_lite.default_base_url` |
| 从 NewAPI 一键带地址和 Key 进来 | 打开 `http://你的Genpic地址/?address=编码后的地址&key=完整Key` |
| 只带 Key、地址用配置文件里的默认 | `http://你的Genpic地址/?key=完整Key` |
| 配置文件在别的路径 | `go run ./cmd/mvplite -config /路径/config.yaml` |

仍有问题：看 `docs/runbook.md` 或仓库 `README.md`。
