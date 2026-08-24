//go:build windows

package tray

import (
	"context"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"openknowledge/internal/gui"
)

const (
	wmTrayIcon       = 0x0400 + 1 // WM_USER + 1
	wmQuit           = 0x0012
	wmDestroy        = 0x0002
	wmLButtonDblClk  = 0x0203
	wmRButtonUp      = 0x0205
	nimAdd           = 0x00000000
	nimDelete        = 0x00000002
	nifMessage       = 0x00000001
	nifIcon          = 0x00000002
	nifTip           = 0x00000004
	mfGrayed         = 0x00000001
	mfString         = 0x00000000
	mfSeparator      = 0x00000800
	tpmReturnCmd     = 0x0100
	tpmNoNotify      = 0x0080
	imageIcon        = 1
	lrDefaultSize    = 0x0040
	lrShared         = 0x8000
	idiApplication   = 32512
	idMenuQuit       = 1001
	hwndMessage      = ^uintptr(2) // (HWND)-3
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procUnregisterClassW    = user32.NewProc("UnregisterClassW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procLoadImageW          = user32.NewProc("LoadImageW")
	procShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadId  = kernel32.NewProc("GetCurrentThreadId")
)

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msgStruct struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type pointStruct struct{ X, Y int32 }

type notifyIconDataW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

// Tray 持托盘运行时状态；全局唯一（daemon 单实例）。
type Tray struct {
	version  string
	openGUI  func() uintptr
	onQuit   func()
	hwnd     uintptr // 消息窗口
	threadID uint32

	mu      sync.Mutex
	guiHwnd uintptr // 最近打开的 GUI 窗口（openGUI 异步回写，见 openOrFocus）
	opening bool    // 一次浏览器拉起进行中：连击防抖，避免重复拉起
}

var current *Tray

// Run 创建托盘图标并跑消息循环，阻塞至 ctx 取消（daemon 退出链路）。
// openGUI 打开浏览器 GUI 并返回新窗口 hwnd；onQuit 由菜单"退出"触发。
func Run(ctx context.Context, version string, openGUI func() uintptr, onQuit func()) {
	runtime.LockOSThread() // 消息队列绑定线程：锁定防 goroutine 迁移，不解锁（线程专职消息循环）
	t := &Tray{version: version, openGUI: openGUI, onQuit: onQuit}
	current = t
	defer func() { current = nil }()
	if err := t.init(); err != nil {
		return // 图标创建失败不影响 daemon（fail-open）
	}
	defer t.cleanup()
	tid, _, _ := procGetCurrentThreadId.Call()
	t.threadID = uint32(tid)
	go func() {
		<-ctx.Done()
		procPostThreadMessageW.Call(uintptr(t.threadID), wmQuit, 0, 0)
	}()
	var m msgStruct
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 { // 0=WM_QUIT；-1=错误（Windows ABI 只保证 eax 有效）
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (t *Tray) init() error {
	className, _ := windows.UTF16PtrFromString("OpenKnowledgeTrayMsg")
	hinst, _, _ := procGetModuleHandleW.Call(0)
	cb := windows.NewCallback(trayWndProc)
	wcx := wndClassExW{
		WndProc:   cb,
		Instance:  hinst,
		ClassName: className,
	}
	wcx.Size = uint32(unsafe.Sizeof(wcx))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx))); r == 0 {
		return err
	}
	title, _ := windows.UTF16PtrFromString("OpenKnowledgeTray")
	hwnd, _, err := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, hwndMessage, 0, hinst, 0)
	if hwnd == 0 {
		return err
	}
	t.hwnd = hwnd

	icon := t.loadIcon(hinst)
	nid := notifyIconDataW{
		HWnd:             t.hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTrayIcon,
		HIcon:            icon,
	}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	tip, _ := windows.UTF16FromString("OpenKnowledge v" + t.version)
	copy(nid.SzTip[:], tip)
	if r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return err
	}
	return nil
}

// loadIcon 加载 exe 内嵌的 APP 图标（go-winres 资源名），失败退回系统默认图标。
func (t *Tray) loadIcon(hinst uintptr) uintptr {
	name, _ := windows.UTF16PtrFromString("APP")
	if h, _, _ := procLoadImageW.Call(hinst, uintptr(unsafe.Pointer(name)),
		imageIcon, 0, 0, lrDefaultSize|lrShared); h != 0 {
		return h
	}
	h, _, _ := procLoadIconW.Call(0, idiApplication)
	return h
}

func (t *Tray) cleanup() {
	nid := notifyIconDataW{HWnd: t.hwnd, UID: 1}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	procDestroyWindow.Call(t.hwnd)
	className, _ := windows.UTF16PtrFromString("OpenKnowledgeTrayMsg")
	hinst, _, _ := procGetModuleHandleW.Call(0)
	procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), hinst)
}

func trayWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	t := current
	if t == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return r
	}
	switch msg {
	case wmTrayIcon:
		switch lParam {
		case wmRButtonUp:
			t.showMenu()
		case wmLButtonDblClk:
			t.openOrFocus()
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

// showMenu 右键弹菜单：灰化版本项 + 分隔线 + 退出。
// 弹出前 SetForegroundWindow 自身窗口，保证点击他处菜单正常消失（Win32 惯例）。
func (t *Tray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	ver, _ := windows.UTF16PtrFromString("OpenKnowledge v" + t.version)
	procAppendMenuW.Call(menu, mfString|mfGrayed, 0, uintptr(unsafe.Pointer(ver)))
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	quit, _ := windows.UTF16PtrFromString("退出")
	procAppendMenuW.Call(menu, mfString, idMenuQuit, uintptr(unsafe.Pointer(quit)))

	var pt pointStruct
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(t.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmReturnCmd|tpmNoNotify,
		uintptr(pt.X), uintptr(pt.Y), 0, t.hwnd, 0)
	procPostMessageW.Call(t.hwnd, 0, 0, 0) // WM_NULL：保证点击他处菜单可靠收起（KB Q135788）
	if cmd == idMenuQuit && t.onQuit != nil {
		t.onQuit()
	}
}

// openOrFocus 双击：既有 GUI 窗口有效则聚焦，否则拉起浏览器。
// openGUI 内含最长约 20s 的窗口轮询（gui.OpenBrowser），必须异步——在消息线程
// 同步执行会卡死整个托盘消息循环，daemon 退出时 2s 的 trayDone 等待超时跳过
// cleanup（NIM_DELETE），留下幽灵图标（M-05）。此异步化不影响 `ok gui` 路径
// （其仍同步调用 OpenBrowser，见 browser_windows.go 注释）。
func (t *Tray) openOrFocus() {
	t.mu.Lock()
	hwnd := t.guiHwnd
	t.mu.Unlock()
	if decideReuseGUI(hwnd, gui.IsWindow) {
		gui.FocusWindow(hwnd)
		return
	}
	t.mu.Lock()
	if t.openGUI == nil || t.opening {
		t.mu.Unlock()
		return
	}
	t.opening = true
	t.mu.Unlock()
	go func() {
		h := t.openGUI()
		t.mu.Lock()
		t.guiHwnd = h
		t.opening = false
		t.mu.Unlock()
	}()
}
