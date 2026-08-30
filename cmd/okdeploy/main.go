// okdeploy：OpenKnowledge 服务端一键部署器（独立程序，不进客户端安装包）。
// 双击启动 → 监听 127.0.0.1 随机端口 → 自动开浏览器（同 ok gui 模式）。
// 设计文档：docs/superpowers/specs/2026-08-28-okdeploy-design.md
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"

	"openknowledge/internal/deployx"
	"openknowledge/internal/deployx/webui"
	"openknowledge/internal/gui"
	"openknowledge/internal/version"
)

func main() {
	os.Exit(run(os.Stderr))
}

func run(stderr *os.File) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(stderr, "监听失败：", err)
		return 1
	}
	token, err := newToken()
	if err != nil {
		fmt.Fprintln(stderr, "生成令牌失败：", err)
		return 1
	}
	srv := deployx.NewServer(token)
	port := ln.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d/#token=%s", port, token)
	fmt.Fprintf(stderr, "okdeploy %s 管理界面：%s\n", version.Version, url)
	gui.OpenBrowser(url)
	if err := http.Serve(ln, srv.Handler(webui.WebFS())); err != nil {
		fmt.Fprintln(stderr, "服务退出：", err)
		return 1
	}
	return 0
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
