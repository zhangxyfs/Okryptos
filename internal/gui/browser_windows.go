//go:build windows

package gui

import (
	"fmt"
	"os/exec"
	"time"

	"okryptos/internal/procx"
)

// openBrowser 以最大化窗口打开 Edge/Chrome 应用模式，返回新窗口句柄（未找到返回 0）；
// 失败退回默认浏览器（不保证最大化）。调用方可用返回的 hwnd 做后续聚焦复用。
// 最大化三道保险：--start-maximized 浏览器开关 + Start-Process -WindowStyle Maximized
// + 启动后 ShowWindow 兜底——单实例浏览器会吞掉 WindowStyle，app 窗口还会按 profile
// 记忆还原尺寸，单一手段都不可靠。兜底按窗口标题子串 o.WindowTitle 匹配（调用方
// 经 OpenBrowserOpt 传入，空值已回填 "OkManager"）。
// 注：maximizeWindowByTitle 必须同步调用：ok gui 开浏览器即退（daemon 托管生命周期），
// 协程会随进程退出而被杀——v2.1 的"不自动最大化"回归正源于此。
func openBrowser(url string, o BrowserOptions) uintptr {
	if !safeAppURL(url) {
		return 0 // 非回环 http(s) 或含击穿引号的 URL：拒绝进入 PowerShell 命令串
	}
	for _, browser := range []string{"msedge", "chrome"} {
		var ps string
		if o.WindowSize != "" {
			// 固定窗口尺寸模式（okdeploy）：不最大化（-WindowStyle Maximized 会
			// 盖掉 --window-size），跳过标题轮询兜底
			ps = fmt.Sprintf("Start-Process %s -ArgumentList '--app=%s --window-size=%s'", browser, url, o.WindowSize)
		} else {
			ps = fmt.Sprintf("Start-Process %s -ArgumentList '--app=%s --start-maximized' -WindowStyle Maximized", browser, url)
		}
		cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
		procx.HideWindow(cmd)
		if err := cmd.Run(); err == nil {
			if o.WindowSize != "" {
				return 0
			}
			return maximizeWindowByTitle(o.WindowTitle, 10*time.Second)
		}
	}
	// 兜底经 explorer.exe 起默认浏览器：URL 作为参数直传，不经 cmd 解析——
	// cmd /c start 对 & ^ | % , ; = 等元字符的防御不完整，不如绕开 cmd。
	fallback := exec.Command("explorer.exe", url)
	procx.HideWindow(fallback)
	_ = fallback.Run()
	if o.WindowSize != "" {
		return 0
	}
	return maximizeWindowByTitle(o.WindowTitle, 10*time.Second)
}
