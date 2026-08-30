package gui

import (
	"net/url"
	"strings"
)

// BrowserOptions 控制 OpenBrowserOpt 的 app 模式窗口形态（目前仅 Windows 版消费）。
type BrowserOptions struct {
	// WindowTitle 是最大化兜底匹配的窗口标题子串（app 模式窗口标题即页面标题）；
	// 空 = "OkManager"（web/index.html 的 <title>）。okdeploy 等内嵌 Web UI 的
	// 程序必须传自己的页面标题，否则 maximizeWindowByTitle 会空轮询满 10s——
	// 且调用方若先开浏览器再起 HTTP 服务，用户会看到长时间白屏。
	WindowTitle string
	// WindowSize 非空时（形如 "980,700"）：app 窗口按该尺寸打开且不做最大化
	// （跳过 --start-maximized 与 ShowWindow 兜底）；空 = 默认最大化行为（ok gui）。
	WindowSize string
}

// OpenBrowser 以默认形态（最大化、标题 "OkManager"）打开，等价
// OpenBrowserOpt(url, BrowserOptions{})。客户端各入口保持用这个。
func OpenBrowser(url string) uintptr { return OpenBrowserOpt(url, BrowserOptions{}) }

// OpenBrowserOpt 带窗口形态选项打开浏览器（实现分平台：browser_windows/unix.go）。
func OpenBrowserOpt(url string, o BrowserOptions) uintptr {
	if o.WindowTitle == "" {
		o.WindowTitle = "OkManager"
	}
	return openBrowser(url, o)
}

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
