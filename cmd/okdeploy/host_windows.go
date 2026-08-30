//go:build windows

package main

// okdeploy 窗口宿主（Windows）：自建 Win32 窗口 + 内嵌 WebView2，固定 972×686
// 居中窗口——Edge app 模式的几何记忆/最大化开关都不可控，内嵌窗口尺寸标题全自主。
// 模式参照 cmd/okmanager/host_windows.go（离屏可见创建 + 导航完成才入屏，零白帧），
// 裁掉 okdeploy 用不到的部分：无 placement 持久化（每次固定尺寸）、无过渡页
// （服务已在跑，直接导航）、无 token 注入脚本（#token= fragment 自带）。

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	edge "github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"

	"openknowledge/internal/gui"
	"openknowledge/internal/registry"
)

var (
	dUser32              = windows.NewLazySystemDLL("user32.dll")
	procShowWindow       = dUser32.NewProc("ShowWindow")
	procUpdateWindow     = dUser32.NewProc("UpdateWindow")
	procGetSystemMetrics = dUser32.NewProc("GetSystemMetrics")
	procRegisterClassExW = dUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = dUser32.NewProc("CreateWindowExW")
	procDestroyWindow    = dUser32.NewProc("DestroyWindow")
	procDefWindowProcW   = dUser32.NewProc("DefWindowProcW")
	procGetMessageW      = dUser32.NewProc("GetMessageW")
	procTranslateMessage = dUser32.NewProc("TranslateMessage")
	procDispatchMessageW = dUser32.NewProc("DispatchMessageW")
	procPostQuitMessage  = dUser32.NewProc("PostQuitMessage")
	procMoveWindow       = dUser32.NewProc("MoveWindow")
	procGetWindowRect    = dUser32.NewProc("GetWindowRect")
	dShell32             = windows.NewLazySystemDLL("shell32.dll")
	procExtractIconW     = dShell32.NewProc("ExtractIconW")
	dGdi32               = windows.NewLazySystemDLL("gdi32.dll")
	procCreateSolidBrush = dGdi32.NewProc("CreateSolidBrush")
	dKernel32            = windows.NewLazySystemDLL("kernel32.dll")
	procGetModuleHandle  = dKernel32.NewProc("GetModuleHandleExW")
)

const (
	dCwUseDefault       = 0x80000000
	dWsOverlappedWindow = 0xCF0000
	dWsVisible          = 0x10000000

	dWmSize    = 0x0005
	dWmMove    = 0x0003
	dWmClose   = 0x0010
	dWmDestroy = 0x0002

	deployWinW = 972
	deployWinH = 686
)

// deployPageSizes 各页窗口尺寸（前端 route 切页时 postMessage 通知宿主换尺寸）；
// 未列出的页用默认 deployWinW×deployWinH。
var deployPageSizes = map[string][2]int{
	"probe": {958, 793},
}

type dWndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CnClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type dPoint struct{ X, Y int32 }

type dMsg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       dPoint
	LPrivate uint32
}

// deployCtx 是 hwnd 关联的窗口上下文。
type deployCtx struct {
	hwnd     uintptr
	chromium *edge.Chromium
	url      string
	showOnce sync.Once
	curW     int
	curH     int
}

type dRect struct{ Left, Top, Right, Bottom int32 }

// onPageMessage 前端 route() 切页通知：按 deployPageSizes 换窗口尺寸，保持窗口中心不动。
func (c *deployCtx) onPageMessage(msg string) {
	var m struct {
		Type string `json:"type"`
		Page string `json:"page"`
	}
	if err := json.Unmarshal([]byte(msg), &m); err != nil || m.Type != "page" {
		return
	}
	w, h := deployWinW, deployWinH
	if s, ok := deployPageSizes[m.Page]; ok {
		w, h = s[0], s[1]
	}
	if w == c.curW && h == c.curH {
		return
	}
	var r dRect
	procGetWindowRect.Call(c.hwnd, uintptr(unsafe.Pointer(&r)))
	x := (r.Left + r.Right - int32(w)) / 2
	y := (r.Top + r.Bottom - int32(h)) / 2
	c.curW, c.curH = w, h
	procMoveWindow.Call(c.hwnd, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 1)
}

var (
	deployContexts   = map[uintptr]*deployCtx{}
	deployContextsMu sync.RWMutex
)

// show 首次入屏：把离屏窗口搬到屏内居中位置并刷新（导航在 Embed 后只发一次）。
func (c *deployCtx) show() {
	sw, _, _ := procGetSystemMetrics.Call(0)
	sh, _, _ := procGetSystemMetrics.Call(1)
	x := (int32(sw) - deployWinW) / 2
	y := (int32(sh) - deployWinH) / 2
	procMoveWindow.Call(c.hwnd, uintptr(x), uintptr(y), deployWinW, deployWinH, 1)
	procShowWindow.Call(c.hwnd, 1) // SW_SHOWNORMAL
	procUpdateWindow.Call(c.hwnd)
	c.chromium.Focus()
}

func deployWndProc(hwnd, message, wp, lp uintptr) uintptr {
	deployContextsMu.RLock()
	c := deployContexts[hwnd]
	deployContextsMu.RUnlock()
	if c == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, message, wp, lp)
		return r
	}
	switch message {
	case dWmSize:
		c.chromium.Resize()
	case dWmMove:
		_ = c.chromium.NotifyParentWindowPositionChanged()
	case dWmClose:
		procDestroyWindow.Call(hwnd)
	case dWmDestroy:
		procPostQuitMessage.Call(0)
	default:
		r, _, _ := procDefWindowProcW.Call(hwnd, message, wp, lp)
		return r
	}
	return 0
}

var registerDeployClassOnce sync.Once

func registerDeployClass(className *uint16) {
	registerDeployClassOnce.Do(func() {
		var hinstance windows.Handle
		procGetModuleHandle.Call(0, 0, uintptr(unsafe.Pointer(&hinstance)))
		// 任务栏/标题栏图标：取 exe 自身（winres 嵌入的 logo-deploy）首个图标。
		var icon uintptr
		if exe, err := os.Executable(); err == nil {
			if exePath, err := windows.UTF16PtrFromString(exe); err == nil {
				icon, _, _ = procExtractIconW.Call(0, uintptr(unsafe.Pointer(exePath)), 0)
				if icon <= 1 {
					icon = 0
				}
			}
		}
		// 背景刷与页底色一致（#f5f6f8 → COLORREF 0x00F8F6F5），首绘前窗框不闪白。
		bg, _, _ := procCreateSolidBrush.Call(0x00F8F6F5)
		wc := dWndClassExW{
			CbSize:        uint32(unsafe.Sizeof(dWndClassExW{})),
			HInstance:     hinstance,
			LpszClassName: className,
			HIcon:         windows.Handle(icon),
			HIconSm:       windows.Handle(icon),
			HbrBackground: windows.Handle(bg),
			LpfnWndProc:   windows.NewCallback(deployWndProc),
		}
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	})
}

// openUI Windows 版：WebView2 固定尺寸窗口，驻留消息循环直至窗口关闭（返回后进程退出）。
// 任何一步失败都回退浏览器并继续驻留 HTTP 服务。
func openUI(url string, serveErr chan error, stderr io.Writer) int {
	className, _ := windows.UTF16PtrFromString("OkDeployHost")
	registerDeployClass(className)
	title, _ := windows.UTF16PtrFromString("OpenKnowledge 服务端部署")
	offscreen := int32(-32000)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		dWsOverlappedWindow|dWsVisible, // 离屏可见：WebView2 要求父窗口可见才渲染
		uintptr(uint32(offscreen)), uintptr(uint32(offscreen)),
		deployWinW, deployWinH,
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		fmt.Fprintln(stderr, "窗口创建失败，回退浏览器打开")
		gui.OpenBrowserOpt(url, browserOpts)
		return waitServe(serveErr, stderr)
	}

	chromium := edge.NewChromium()
	chromium.DataPath = filepath.Join(registry.Home(), "webview2-data-okdeploy")
	ctx := &deployCtx{hwnd: hwnd, chromium: chromium, url: url, curW: deployWinW, curH: deployWinH}
	deployContextsMu.Lock()
	deployContexts[hwnd] = ctx
	deployContextsMu.Unlock()

	show := func() {
		ctx.showOnce.Do(func() {
			ctx.show()
		})
	}
	chromium.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		show()
	}
	go func() { // 4s 兜底防导航回调不到窗口永不出现
		time.Sleep(4 * time.Second)
		show()
	}()

	if !chromium.Embed(hwnd) {
		fmt.Fprintln(stderr, "WebView2 初始化失败，回退浏览器打开")
		deployContextsMu.Lock()
		delete(deployContexts, hwnd)
		deployContextsMu.Unlock()
		procDestroyWindow.Call(hwnd)
		gui.OpenBrowserOpt(url, browserOpts)
		return waitServe(serveErr, stderr)
	}
	if settings, err := chromium.GetSettings(); err == nil {
		_ = settings.PutAreDefaultContextMenusEnabled(false)
		_ = settings.PutAreDevToolsEnabled(false)
	}
	chromium.MessageCallback = ctx.onPageMessage // 前端切页时按需换窗口尺寸
	chromium.Resize()
	chromium.Navigate(ctx.url)

	var m dMsg
	for { // 消息循环直至 WM_QUIT（窗口关闭）
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	deployContextsMu.Lock()
	delete(deployContexts, hwnd)
	deployContextsMu.Unlock()
	return 0 // 窗口关闭即退出进程（HTTP 服务随之结束）
}
