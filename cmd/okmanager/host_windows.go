//go:build windows

package main

// OkManager 窗口宿主（Windows）：确保 okd 在线后创建原生窗口并嵌入 WebView2，
// 驻留消息循环至窗口关闭。窗口创建尺寸即主屏尺寸 + 立即最大化（或按
// gui-state.json 恢复），内容加载前已完成，无"小窗口闪一下再最大化"。

import (
	"fmt"
	"io"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"

	"openknowledge/internal/daemon"
	"openknowledge/internal/gui"
	"openknowledge/internal/registry"
)

var (
	hostUser32               = syscall.NewLazyDLL("user32.dll")
	procHostShowWindow       = hostUser32.NewProc("ShowWindow")
	procGetWindowPlacement   = hostUser32.NewProc("GetWindowPlacement")
	procSetWindowPlacement   = hostUser32.NewProc("SetWindowPlacement")
	procHostGetSystemMetrics = hostUser32.NewProc("GetSystemMetrics")
)

const (
	swShowNormal = 1
	swMaximize   = 3
)

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
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		DataPath:  filepath.Join(registry.Home(), "webview2-data"),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  gui.WindowTitle,
			Width:  screenW,
			Height: screenH,
		},
	})
	if w == nil { // WebView2 初始化失败：回退浏览器，GUI 永远打得开
		fmt.Fprintln(stderr, "WebView2 初始化失败，回退浏览器打开")
		return daemon.OpenGUI(stdout, stderr)
	}

	hwnd := uintptr(w.Window())
	if s, ok := gui.LoadWindowState(); ok {
		wp := placementFromState(s)
		procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp)))
	} else {
		procHostShowWindow.Call(hwnd, swMaximize) // 首启：内容加载前即最大化
	}
	w.SetSize(960, 600, webview2.HintMin) // 最小尺寸
	w.Init(gui.TokenInitScript(info.Token))
	w.Navigate(info.URL() + "/")

	var last atomic.Value
	stop := make(chan struct{})
	go trackPlacement(hwnd, &last, stop)

	w.Run()
	close(stop)
	if v := last.Load(); v != nil {
		_ = gui.SaveWindowState(v.(*gui.WindowState))
	}
	w.Destroy()
	return 0
}
