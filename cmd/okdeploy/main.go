// okdeploy：OpenKnowledge 服务端一键部署器（独立程序，不进客户端安装包）。
// 双击启动 → 监听 127.0.0.1 随机端口 → 自动开浏览器（同 ok gui 模式）。
// 设计文档：docs/superpowers/specs/2026-08-28-okdeploy-design.md
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
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
	// 先起服务再开窗口（OpenBrowser 内部会同步轮询窗口标题做最大化兜底，
	// 若先开窗口，轮询期间页面无人应答，用户会看到长时间白屏）。
	serveErr := make(chan error, 1)
	go func() { serveErr <- http.Serve(ln, srv.Handler(webui.WebFS())) }()
	return openUI(url, serveErr, stderr)
}

// browserOpts 是回退浏览器路径（WebView2 不可用时）的窗口形态：
// 固定尺寸不最大化，标题匹配部署页 <title>。
var browserOpts = gui.BrowserOptions{WindowTitle: "OpenKnowledge 服务端部署", WindowSize: "972,686"}

// waitServe 回退路径：浏览器打开后驻留 HTTP 服务直至出错或进程被杀。
func waitServe(serveErr chan error, stderr io.Writer) int {
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
