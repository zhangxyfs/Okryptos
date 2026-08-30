package gui

import (
	"net/url"
	"strings"
)

// BrowserWindowTitle 是 app 模式窗口最大化兜底匹配的窗口标题子串（Windows）。
// 默认 "OkManager"（web/index.html 的 <title>）；其他内嵌 Web UI 的程序
// （如 okdeploy）启动前改成自己的页面标题子串，否则 maximizeWindowByTitle
// 会空轮询满 10s——且调用方若先开浏览器再起 HTTP 服务，用户会看到长时间白屏。
var BrowserWindowTitle = "OkManager"

// safeAppURL 判定可安全交给浏览器启动命令的应用 URL：仅 http/https、主机为
// 本机回环，且不含可击穿 Windows PowerShell 单引号包裹的字符（单双引号、
// 控制字符）。URL 由 daemon 自生成（http://127.0.0.1:<port>/?token=<hex>），
// 形状之外的输入一律拒绝——这是纵深防御，防未来调用方把外部输入传进来。
func safeAppURL(raw string) bool {
	if strings.ContainsAny(raw, "'\"\r\n\x00") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
