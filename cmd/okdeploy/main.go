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
	// 先起服务再开浏览器：OpenBrowser 内部会同步轮询窗口标题做最大化兜底，
	// 若先开浏览器，轮询期间页面无人应答，用户会看到长时间白屏。
	serveErr := make(chan error, 1)
	go func() { serveErr <- http.Serve(ln, srv.Handler(webui.WebFS())) }()
	gui.BrowserWindowTitle = "OpenKnowledge 服务端部署" // 页面 <title>（尺寸模式下不轮询，仅兜底语义）
	gui.BrowserWindowSize = "972,686"                   // 部署向导是窄表单，固定尺寸比最大化更合适
	gui.OpenBrowser(url)
	if err := <-serveErr; err != nil {
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
