- 设置代理环境变量（当前 Terminal 会话）
```sh
export WIN_HOST=172.17.32.1
export HTTP_PROXY="http://${WIN_HOST}:17891"
export HTTPS_PROXY="http://${WIN_HOST}:17891"
export ALL_PROXY="socks5://${WIN_HOST}:17891"   # 若客户端提供 SOCKS 端口
export NO_PROXY="localhost,127.0.0.1,::1"
```

- 国内镜像：
`go env -w GOPROXY=https://goproxy.cn,direct`
