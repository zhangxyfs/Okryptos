//go:build windows

package main

// OkManager 窗口宿主（Windows）：确保 okd 在线后创建原生窗口并嵌入 WebView2，
// 驻留消息循环至窗口关闭。不用 webview2.NewWithOptions——其内部"建窗→立即
// SW_SHOW→才初始化 WebView2"会让用户先看到未最大化的白窗口再跳变（像瞬间打开
// 两个页面）。这里自建窗口（初始不显示）+ 直接 edge.Chromium 嵌入 + 首次导航
// 完成后才显示：第一帧即渲染好的页面，白屏与跳变全消。

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	edge "github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"

	"openknowledge/internal/daemon"
	"openknowledge/internal/gui"
	"openknowledge/internal/registry"
)

// 库的 internal/w32 不可 import（internal 包限制），所需 proc 与结构体在此自行声明。
var (
	hostUser32                 = windows.NewLazySystemDLL("user32.dll")
	procHostShowWindow         = hostUser32.NewProc("ShowWindow")
	procHostUpdateWindow       = hostUser32.NewProc("UpdateWindow")
	procGetWindowPlacement     = hostUser32.NewProc("GetWindowPlacement")
	procSetWindowPlacement     = hostUser32.NewProc("SetWindowPlacement")
	procHostGetSystemMetrics   = hostUser32.NewProc("GetSystemMetrics")
	procRegisterClassExW       = hostUser32.NewProc("RegisterClassExW")
	procCreateWindowExW        = hostUser32.NewProc("CreateWindowExW")
	procDestroyWindow          = hostUser32.NewProc("DestroyWindow")
	procDefWindowProcW         = hostUser32.NewProc("DefWindowProcW")
	procGetMessageW            = hostUser32.NewProc("GetMessageW")
	procTranslateMessage       = hostUser32.NewProc("TranslateMessage")
	procDispatchMessageW       = hostUser32.NewProc("DispatchMessageW")
	procPostQuitMessage        = hostUser32.NewProc("PostQuitMessage")
	procSetFocus               = hostUser32.NewProc("SetFocus")
	hostShell32                = windows.NewLazySystemDLL("shell32.dll")
	procExtractIconW           = hostShell32.NewProc("ExtractIconW")
	hostGdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	procCreateSolidBrush       = hostGdi32.NewProc("CreateSolidBrush")
	hostKernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procHostGetModuleHandleExW = hostKernel32.NewProc("GetModuleHandleExW")
)

const (
	swShowNormal = 1
	swMaximize   = 3

	cwUseDefault       = 0x80000000
	wsOverlappedWindow = 0xCF0000 // 不带 WS_VISIBLE：首帧前窗口不可见

	wmDestroy       = 0x0002
	wmMove          = 0x0003
	wmSize          = 0x0005
	wmActivate      = 0x0006
	wmClose         = 0x0010
	wmGetMinMaxInfo = 0x0024
	wmNCLButtonDown = 0x00A1
)

// 镜像 Win32 结构体（仅用到的字段完整保留布局）。
type wndClassExW struct {
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

type point struct {
	X, Y int32
}

type minMaxInfo struct {
	PtReserved     point
	PtMaxSize      point
	PtMaxPosition  point
	PtMinTrackSize point
	PtMaxTrackSize point
}

type msg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

// windowPlacement 镜像 Win32 WINDOWPLACEMENT（仅用到的字段）。
type windowPlacement struct {
	Length           uint32
	Flags            uint32
	ShowCmd          uint32
	PtMinPosition    [2]int32
	PtMaxPosition    [2]int32
	RcNormalPosition [4]int32 // left, top, right, bottom
}

func placementFromState(s *gui.WindowState) windowPlacement {
	wp := windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{})), ShowCmd: swShowNormal}
	if s.Maximized {
		wp.ShowCmd = swMaximize
	}
	wp.RcNormalPosition = [4]int32{s.Left, s.Top, s.Right, s.Bottom}
	return wp
}

func stateFromPlacement(wp *windowPlacement) *gui.WindowState {
	r := wp.RcNormalPosition
	return &gui.WindowState{
		Maximized: wp.ShowCmd == swMaximize,
		Left:      r[0], Top: r[1], Right: r[2], Bottom: r[3],
	}
}

func primaryScreenSize() (uint, uint) {
	w, _, _ := procHostGetSystemMetrics.Call(0) // SM_CXSCREEN
	h, _, _ := procHostGetSystemMetrics.Call(1) // SM_CYSCREEN
	if w == 0 || h == 0 {
		return 1280, 800
	}
	return uint(w), uint(h)
}

// trackPlacement 每 2s 采样窗口 placement 到 last（窗口销毁后查询会失败，
// 退出时落盘最后一次有效值）。
func trackPlacement(hwnd uintptr, last *atomic.Value, stop <-chan struct{}) {
	sample := func() {
		wp := windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
		if r, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp))); r != 0 {
			last.Store(stateFromPlacement(&wp))
		}
	}
	sample()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			sample()
		}
	}
}

// loadingPageHTML 是 okd 页面加载前的本地过渡页：无网络依赖、首帧即渲染，
// 底色与 GUI 一致（#f3f4f6），替代"白屏等待"。token 注入脚本按 origin 门控，
// 本地页不会拿到 token。
const loadingPageHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><style>
  html,body{margin:0;height:100%;background:#f3f4f6;display:flex;align-items:center;justify-content:center;
    font-family:"Segoe UI","Microsoft YaHei",sans-serif;color:#6b7280}
  .box{text-align:center}
  .logo{width:56px;height:56px;margin:0 auto 14px;border-radius:14px;background:#2563eb;color:#fff;
    font-size:26px;font-weight:700;display:flex;align-items:center;justify-content:center}
  .t{font-size:15px;letter-spacing:.5px}
  .d{margin-top:10px;font-size:12px;color:#9ca3af}
</style></head><body>
  <div class="box"><div class="logo">ok</div>
  <div class="t">OpenKnowledge 配置中心</div>
  <div class="d">正在加载…</div></div>
</body></html>`

// hostCtx 是 hwnd 关联的窗口上下文（WndProc 经 hostContexts 查取，
// 参照库 webview.go 的 windowContext 模式）。
type hostCtx struct {
	hwnd     uintptr
	chromium *edge.Chromium
	saved    *gui.WindowState // 有保存状态时显示走 SetWindowPlacement
}

var (
	hostContexts   = map[uintptr]*hostCtx{}
	hostContextsMu sync.RWMutex
)

// show 首次显示窗口：有保存状态恢复 placement，无则直接最大化；随后聚焦 WebView2。
func (c *hostCtx) show() {
	if c.saved != nil {
		wp := placementFromState(c.saved)
		procSetWindowPlacement.Call(c.hwnd, uintptr(unsafe.Pointer(&wp)))
	} else {
		procHostShowWindow.Call(c.hwnd, swMaximize)
	}
	procHostUpdateWindow.Call(c.hwnd)
	c.chromium.Focus()
}

func hostWndProc(hwnd, message, wp, lp uintptr) uintptr {
	hostContextsMu.RLock()
	c := hostContexts[hwnd]
	hostContextsMu.RUnlock()
	if c == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, message, wp, lp)
		return r
	}
	switch message {
	case wmSize:
		c.chromium.Resize()
	case wmMove:
		_ = c.chromium.NotifyParentWindowPositionChanged()
	case wmActivate:
		if wp != 0 { // 非 WA_INACTIVE：Alt-Tab 切回等场景把键盘焦点交还 WebView2
			c.chromium.Focus()
		}
	case wmNCLButtonDown:
		procSetFocus.Call(c.hwnd)
		r, _, _ := procDefWindowProcW.Call(hwnd, message, wp, lp)
		return r
	case wmClose:
		procDestroyWindow.Call(hwnd)
	case wmDestroy:
		procPostQuitMessage.Call(0)
	case wmGetMinMaxInfo:
		mmi := *(**minMaxInfo)(unsafe.Pointer(&lp)) // lParam 即 *MINMAXINFO
		mmi.PtMinTrackSize = point{X: 960, Y: 600}
	default:
		r, _, _ := procDefWindowProcW.Call(hwnd, message, wp, lp)
		return r
	}
	return 0
}

var registerHostClassOnce sync.Once

// registerHostClass 注册宿主窗口类（重复注册返回"类已存在"，无碍，仅注册一次）。
func registerHostClass(className *uint16) {
	registerHostClassOnce.Do(func() {
		var hinstance windows.Handle
		// GetModuleHandleExW(0, NULL, &hinstance)
		procHostGetModuleHandleExW.Call(0, 0, uintptr(unsafe.Pointer(&hinstance)))
		// 任务栏/Alt-Tab 图标：取 exe 自身（winres 嵌入）首个图标；
		// 句柄进程生命周期内不释放。失败留 0（系统默认图标），不影响注册。
		var icon uintptr
		if exe, err := os.Executable(); err == nil {
			if exePath, err := windows.UTF16PtrFromString(exe); err == nil {
				icon, _, _ = procExtractIconW.Call(0, uintptr(unsafe.Pointer(exePath)), 0)
				if icon <= 1 { // ExtractIconW 失败返回 0/1
					icon = 0
				}
			}
		}
		// 背景刷与 GUI 底色一致（#f3f4f6 → COLORREF 0x00F6F4F3）：WebView2 首绘
		// 之前窗框也不是白底。句柄进程生命周期内不释放。
		bg, _, _ := procCreateSolidBrush.Call(0x00F6F4F3)
		wc := wndClassExW{
			CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
			HInstance:     hinstance,
			LpszClassName: className,
			HIcon:         windows.Handle(icon),
			HIconSm:       windows.Handle(icon),
			HbrBackground: windows.Handle(bg),
			LpfnWndProc:   windows.NewCallback(hostWndProc),
		}
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	})
}

func runHost(stdout, stderr io.Writer) int {
	info, ok := daemon.EnsureCurrent()
	if !ok { // daemon 正在后台拉起：与 daemon.OpenGUI 同款轮询（最长 3s）
		for i := 0; i < 30 && !ok; i++ {
			time.Sleep(100 * time.Millisecond)
			info, ok = daemon.EnsureCurrent()
		}
		if !ok {
			fmt.Fprintln(stderr, "daemon 未就绪，请稍后重试")
			return 1
		}
	}

	screenW, screenH := primaryScreenSize()
	className, _ := windows.UTF16PtrFromString("OkManagerHost")
	registerHostClass(className)
	title, _ := windows.UTF16PtrFromString(gui.WindowTitle)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow, // 不带 WS_VISIBLE：导航完成前不显示
		cwUseDefault, cwUseDefault,
		uintptr(screenW), uintptr(screenH),
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		fmt.Fprintln(stderr, "窗口创建失败，回退浏览器打开")
		gui.OpenBrowser(info.URL() + "/#token=" + info.Token)
		return 0
	}

	chromium := edge.NewChromium()
	chromium.DataPath = filepath.Join(registry.Home(), "webview2-data")
	ctx := &hostCtx{hwnd: hwnd, chromium: chromium}
	if s, ok := gui.LoadWindowState(); ok {
		ctx.saved = s
	}
	hostContextsMu.Lock()
	hostContexts[hwnd] = ctx
	hostContextsMu.Unlock()

	// 创建即显示（最终形态一次到位：有状态恢复 placement，无则最大化，之后
	// 不再有任何窗口状态跳变）。实测：WebView2 在隐藏父窗口上完成初始化后
	// 内容不再渲染（白屏），故不能延迟到导航完成再显示——加载期短暂白屏
	// 属所有浏览器/内嵌 WebView 的正常行为。
	ctx.show()

	if !chromium.Embed(hwnd) { // WebView2 初始化失败：直开浏览器（不经 OpenPreferred，防回退重入内嵌路径）
		fmt.Fprintln(stderr, "WebView2 初始化失败，回退浏览器打开")
		hostContextsMu.Lock()
		delete(hostContexts, hwnd)
		hostContextsMu.Unlock()
		procDestroyWindow.Call(hwnd)
		gui.OpenBrowser(info.URL() + "/#token=" + info.Token)
		return 0
	}

	if settings, err := chromium.GetSettings(); err == nil {
		_ = settings.PutAreDefaultContextMenusEnabled(false)
		_ = settings.PutAreDevToolsEnabled(false)
	}
	chromium.Init(gui.TokenInitScript(info.URL(), info.Token))
	chromium.Resize()
	// 先渲染本地加载页（零网络、首帧即出），再导航——加载期用户看到品牌页
	// 而非白底。okd 未启动时 EnsureCurrent 已在建窗前完成拉起等待。
	chromium.NavigateToString(loadingPageHTML)
	chromium.Navigate(info.URL() + "/")

	var last atomic.Value
	stop := make(chan struct{})
	go trackPlacement(hwnd, &last, stop)

	var m msg
	for { // 消息循环直至 WM_QUIT
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	close(stop)
	if v := last.Load(); v != nil {
		_ = gui.SaveWindowState(v.(*gui.WindowState))
	}
	hostContextsMu.Lock()
	delete(hostContexts, hwnd)
	hostContextsMu.Unlock()
	return 0
}
