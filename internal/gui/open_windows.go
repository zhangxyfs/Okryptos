//go:build windows

package gui

// OpenPreferred：打开 GUI 的首选路径——WebView2 运行时在位且 OkManager.exe 同目录
// 则拉起内嵌原生窗口；任一失败回退浏览器。返回窗口句柄（0=未找到，调用方仅作
// 聚焦缓存用，不视为错误）。

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"openknowledge/internal/procx"
)

// WindowTitle 内嵌窗口标题；托盘聚焦与 OkManager 拉起后的找窗都用它。
const WindowTitle = "OpenKnowledge 配置中心"

// 包级变量以便测试替换。
var (
	embeddedOpener = openEmbeddedDefault
	browserOpener  = OpenBrowser
)

func OpenPreferred(url string) uintptr {
	if h := embeddedOpener(url); h != 0 {
		return h
	}
	return browserOpener(url)
}

// openEmbeddedDefault 拉起同目录 OkManager.exe（其自行读 daemon.json 取 URL/token，
// url 参数仅保持签名一致），轮询其窗口出现；运行时缺失/exe 不在/超时返回 0。
func openEmbeddedDefault(_ string) uintptr {
	if !webView2RuntimeAvailable() {
		return 0
	}
	if h := FindWindowByTitleAny(WindowTitle); h != 0 {
		return h // 已在运行：复用
	}
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	mgr := filepath.Join(filepath.Dir(exe), "OkManager.exe")
	if _, err := os.Stat(mgr); err != nil {
		return 0
	}
	cmd := exec.Command(mgr)
	procx.HideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return 0
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h := FindWindowByTitleAny(WindowTitle); h != 0 {
			return h
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0
}

// webView2RuntimeAvailable 检测 WebView2 常青运行时：EdgeUpdate Clients 下
// {F3017226-...} 的 pv 值非空且非 0.0.0.0（HKLM 两个视图 + HKCU 兜底）。
var webView2RuntimeAvailable = func() bool {
	const clsid = `{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	roots := []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER}
	subs := []string{
		`SOFTWARE\Microsoft\EdgeUpdate\Clients\` + clsid,
		`SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\` + clsid,
	}
	for _, root := range roots {
		for _, sub := range subs {
			k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			pv, _, err := k.GetStringValue("pv")
			k.Close()
			if err == nil && pv != "" && pv != "0.0.0.0" {
				return true
			}
		}
	}
	return false
}

// FindWindowByTitleAny 按标题子串找任意进程的可见顶层窗口（Z 序首个），未找到 0。
// 与 findWindowsByTitle 的差别仅在不校验浏览器进程白名单——内嵌窗口属 OkManager.exe。
func FindWindowByTitleAny(substr string) uintptr {
	var found uintptr
	cb := windows.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		if found != 0 {
			return 0 // 已命中，停止枚举
		}
		if r, _, _ := procIsWindowVisible.Call(hwnd); r == 0 {
			return 1
		}
		n, _, _ := procGetWindowTextLenW.Call(hwnd)
		if n == 0 {
			return 1
		}
		buf := make([]uint16, n+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
		if containsIgnoreCase(windows.UTF16ToString(buf), substr) {
			found = hwnd
			return 0
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found
}
