
### 测试拉取第三方外链图片（上游模型返回的 URL）

Go 的 `-run` 是正则且**区分大小写**；第三方拉取测试函数名以 `TestIntegration_` 开头，因此可用 `-run Integration` 选中。

```bash
GENPIC_TEST_REMOTE_IMAGE_FETCH=1 go test ./internal/api/... -run Integration -count=1 -v
```

仅跑第三方拉取相关用例（不含本地 `httptest`）：

```bash
GENPIC_TEST_REMOTE_IMAGE_FETCH=1 go test ./internal/api/... -run 'TestIntegration_ThirdParty' -count=1 -v
```
