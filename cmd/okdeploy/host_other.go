//go:build !windows

package main

// openUI 非 Windows 版：无 WebView2 内嵌宿主，开浏览器并驻留 HTTP 服务。

import (
	"io"

	"openknowledge/internal/gui"
)

func openUI(url string, serveErr chan error, _ io.Writer) int {
	gui.OpenBrowser(url)
	return waitServe(serveErr, io.Discard)
}
