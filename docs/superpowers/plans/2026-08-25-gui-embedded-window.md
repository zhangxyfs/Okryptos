# GUI 内嵌窗口化实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Windows 端 GUI 从"系统浏览器 `--app=` 窗口"改为 OkManager 托管的 WebView2 原生窗口，启动即最大化、窗口状态记忆、token 不走 URL；浏览器路径保留为全局回退，Linux 行为不变。

**Architecture:** 新增 `internal/gui` 的 openPreferred 决策层（内嵌优先、浏览器回退）与窗口状态持久化；`cmd/okmanager` 从薄启动器改为窗口宿主（`host_windows.go` / `host_other.go` build tag 分平台）；前端 `web/index.html` 的 token 获取加一级 `window.__okToken` 优先级。托盘与 okd API 零改动。

**Tech Stack:** Go 1.25（保持 `CGO_ENABLED=0`）、`github.com/jchv/go-webview2`（纯 Go，经 go-ole 调 COM，内嵌 WebView2Loader.dll）、`golang.org/x/sys/windows`（已有依赖 v0.46.0）。

**Spec:** `docs/superpowers/specs/2026-08-25-gui-embedded-window-design.md`

## Global Constraints

- 不引入 cgo：全部新代码纯 Go；`go.mod` 仅新增 `github.com/jchv/go-webview2` 及其传递依赖；
- 平台隔离：所有 Win32/WebView2 代码只在 `//go:build windows` 文件中；`GOOS=linux go build ./...` 必须始终通过；
- go-webview2 的 WebView 接口未暴露导航事件——不实现外部链接拦截（spec §4 已知限制）；
- 窗口标题常量 `gui.WindowTitle = "OpenKnowledge 配置中心"`，浏览器回退路径的页面 `<title>OkManager`（`web/index.html:6`）不动；
- 状态文件 `~/.openknowledge/gui-state.json` 用 `fsx.WriteFile` 原子写；损坏/非法一律按"无状态"处理；
- token 不下 URL（内嵌路径）；`web/index.html` 的 hash 解析逻辑保留给浏览器路径；
- OkManager 从 `daemon.json`（0600）自行读取 token，不经命令行传 token；
- 提交信息遵循仓库惯例（参考 `git log --oneline -10` 的中文简短风格）。

---

### Task 1: 窗口状态持久化 + token 注入脚本（跨平台纯逻辑）

**Files:**
- Create: `internal/gui/gui_state.go`
- Create: `internal/gui/token_script.go`
- Test: `internal/gui/gui_state_test.go`、`internal/gui/token_script_test.go`

**Interfaces:**
- Consumes: `fsx.WriteFile(path string, data []byte, perm os.FileMode) error`（`internal/fsx/fsx.go:14`）；`registry.Home() string`
- Produces:
  - `type WindowState struct { Maximized bool; Left, Top, Right, Bottom int32 }`（JSON 键 `maximized/left/top/right/bottom`）
  - `func LoadWindowState() (*WindowState, bool)`——文件缺失/损坏/矩形非法返回 `(nil, false)`
  - `func SaveWindowState(s *WindowState) error`
  - `func TokenInitScript(token string) string`——返回 `window.__okToken = "<json-quoted>";`

- [ ] **Step 1: 写失败测试**

`internal/gui/gui_state_test.go`：

```go
package gui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowStateRoundTrip(t *testing.T) {
	p := windowStatePath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	defer os.Remove(p)
	want := &WindowState{Maximized: true, Left: 10, Top: 20, Right: 1610, Bottom: 900}
	if err := SaveWindowState(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok := LoadWindowState()
	if !ok {
		t.Fatal("load: expected ok")
	}
	if *got != *want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLoadWindowStateCorrupt(t *testing.T) {
	p := windowStatePath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	defer os.Remove(p)
	os.WriteFile(p, []byte("{not json"), 0o644)
	if _, ok := LoadWindowState(); ok {
		t.Fatal("corrupt file should yield ok=false")
	}
}

func TestLoadWindowStateInvalidRect(t *testing.T) {
	p := windowStatePath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	defer os.Remove(p)
	os.WriteFile(p, []byte(`{"maximized":false,"left":100,"top":0,"right":50,"bottom":10}`), 0o644)
	if _, ok := LoadWindowState(); ok {
		t.Fatal("right<=left should yield ok=false")
	}
}
```

`internal/gui/token_script_test.go`：

```go
package gui

import "testing"

func TestTokenInitScript(t *testing.T) {
	got := TokenInitScript(`ab"cd`)
	want := `window.__okToken = "ab\"cd";`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/gui/ -run "TestWindowState|TestLoadWindowState|TestTokenInitScript" -v`
Expected: FAIL（`undefined: windowStatePath` 等编译错误）

- [ ] **Step 3: 实现**

`internal/gui/gui_state.go`：

```go
package gui

// 窗口状态持久化（机器本地，不进任何同步面）：记录 normal 矩形与最大化标志，
// 供内嵌窗口启动时原样恢复。平台无关纯逻辑；Win32 placement 转换在
// cmd/okmanager/host_windows.go。

import (
	"encoding/json"
	"os"
	"path/filepath"

	"openknowledge/internal/fsx"
	"openknowledge/internal/registry"
)

type WindowState struct {
	Maximized bool  `json:"maximized"`
	Left      int32 `json:"left"`
	Top       int32 `json:"top"`
	Right     int32 `json:"right"`
	Bottom    int32 `json:"bottom"`
}

func windowStatePath() string { return filepath.Join(registry.Home(), "gui-state.json") }

// LoadWindowState 读窗口状态；文件缺失/损坏/矩形非法一律 (nil,false)，调用方按
// "无状态"处理（首启最大化）。
func LoadWindowState() (*WindowState, bool) {
	data, err := os.ReadFile(windowStatePath())
	if err != nil {
		return nil, false
	}
	var s WindowState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, false
	}
	if s.Right <= s.Left || s.Bottom <= s.Top {
		return nil, false
	}
	return &s, true
}

func SaveWindowState(s *WindowState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return fsx.WriteFile(windowStatePath(), data, 0o644)
}
```

`internal/gui/token_script.go`：

```go
package gui

import "strconv"

// TokenInitScript 生成注入 WebView2 AddScriptToExecuteOnDocumentCreated 的脚本：
// 每次导航（含刷新）在页面脚本执行前写入 window.__okToken，替代 URL fragment。
func TokenInitScript(token string) string {
	return "window.__okToken = " + strconv.Quote(token) + ";"
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/gui/ -run "TestWindowState|TestLoadWindowState|TestTokenInitScript" -v`
Expected: PASS（3+1 个测试）

- [ ] **Step 5: Commit**

```bash
git add internal/gui/gui_state.go internal/gui/gui_state_test.go internal/gui/token_script.go internal/gui/token_script_test.go
git commit -m "feat(gui): 窗口状态持久化与 token 注入脚本（内嵌窗口前置）"
```

---

### Task 2: openPreferred 决策层与 OkManager 拉起（Windows）

**Files:**
- Create: `internal/gui/open_windows.go`
- Create: `internal/gui/open_other.go`
- Test: `internal/gui/open_windows_test.go`
- Modify: `go.mod` / `go.sum`（go get）

**Interfaces:**
- Consumes: `OpenBrowser(url string) uintptr`（`internal/gui/browser_windows.go:21`）；`findWindowsByTitle` 同包私有（`window_windows.go:103`，本任务不复用——它带浏览器进程白名单）；`procEnumWindows/procIsWindowVisible/procGetWindowTextW/procGetWindowTextLenW`（同包已声明，`window_windows.go:14-29`）；`containsIgnoreCase`（同包，`window_windows.go:147`）
- Produces:
  - `const WindowTitle = "OpenKnowledge 配置中心"`
  - `func OpenPreferred(url string) uintptr`（全平台；Windows 内嵌优先，其余=OpenBrowser）
  - `var embeddedOpener func(string) uintptr`、`var browserOpener = OpenBrowser`（包级变量，测试可替换）
  - `func FindWindowByTitleAny(substr string) uintptr`——按标题找**任意进程**的可见顶层窗口（无浏览器白名单），Z 序首个，未找到 0

- [ ] **Step 1: 引入 go-webview2 依赖**

Run: `go get github.com/jchv/go-webview2@latest && go mod tidy`
Expected: go.mod 新增 `github.com/jchv/go-webview2`（及传递依赖 `github.com/jchv/go-winloader`）；`go build ./...` 通过

- [ ] **Step 2: 写失败测试**

`internal/gui/open_windows_test.go`（`//go:build windows`）：

```go
//go:build windows

package gui

import "testing"

func TestOpenPreferredEmbeddedWins(t *testing.T) {
	defer func(e, b func(string) uintptr) { embeddedOpener, browserOpener = e, b }(embeddedOpener, browserOpener)
	embeddedOpener = func(string) uintptr { return 42 }
	browserCalled := false
	browserOpener = func(string) uintptr { browserCalled = true; return 7 }
	if h := OpenPreferred("http://127.0.0.1:1"); h != 42 {
		t.Fatalf("got %d, want 42", h)
	}
	if browserCalled {
		t.Fatal("browser opener should not be called when embedded succeeds")
	}
}

func TestOpenPreferredFallsBack(t *testing.T) {
	defer func(e, b func(string) uintptr) { embeddedOpener, browserOpener = e, b }(embeddedOpener, browserOpener)
	embeddedOpener = func(string) uintptr { return 0 }
	browserOpener = func(string) uintptr { return 7 }
	if h := OpenPreferred("http://127.0.0.1:1"); h != 7 {
		t.Fatalf("got %d, want browser fallback 7", h)
	}
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./internal/gui/ -run TestOpenPreferred -v`
Expected: FAIL（`undefined: OpenPreferred` 等）

- [ ] **Step 4: 实现**

`internal/gui/open_windows.go`（`//go:build windows`）：

```go
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
```

`internal/gui/open_other.go`（`//go:build !windows`）：

```go
//go:build !windows

package gui

// OpenPreferred 非 Windows 平台：内嵌窗口不可用，直接浏览器路径。
func OpenPreferred(url string) uintptr { return OpenBrowser(url) }
```

注意：`open_windows.go` 与既有 `window_windows.go` 同包，**不要**重复声明 `user32/procEnumWindows/procIsWindowVisible/procGetWindowTextW/procGetWindowTextLenW/swMaximize` 等——直接使用。

- [ ] **Step 5: 跑测试确认通过 + 全量构建**

Run: `go test ./internal/gui/ -run TestOpenPreferred -v && go build ./... && GOOS=linux GOARCH=amd64 go build ./...`
Expected: PASS；双平台构建均成功

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/gui/open_windows.go internal/gui/open_other.go internal/gui/open_windows_test.go
git commit -m "feat(gui): openPreferred 决策层——内嵌窗口优先、浏览器回退"
```

---

### Task 3: okd/CLI 打开路径切到 OpenPreferred

**Files:**
- Modify: `internal/daemon/run.go:25-26`

**Interfaces:**
- Consumes: `gui.OpenPreferred`（Task 2）
- Produces: `daemon.OpenBrowserFunc` 语义变为"首选路径打开"（托盘与 `ok gui` 经它自动获得内嵌窗口；托盘缓存 hwnd + `gui.FocusWindow` 与进程无关，零改动）

- [ ] **Step 1: 改一行**

`internal/daemon/run.go` 第 25-26 行：

```go
// OpenBrowserFunc 打开 GUI 首选路径（Windows 内嵌窗口优先、浏览器回退）并返回
// 窗口句柄；测试可替换。
var OpenBrowserFunc = gui.OpenPreferred
```

- [ ] **Step 2: 构建与既有测试**

Run: `go build ./... && go test ./internal/daemon/ ./internal/tray/ ./internal/gui/ -count=1`
Expected: 全部 PASS（行为变化：OpenBrowserFunc 指向；若 daemon/tray 测试有替换该变量的用例应仍绿）

注意本任务属"默认行为变化"——按项目教训需确认 e2e 兼容：`grep -rn "OpenBrowserFunc" --include="*.go" cmd/ internal/` 确认所有消费方语义仍成立（都是"打开并拿 hwnd"，无浏览器特化假设）。

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/run.go
git commit -m "feat(daemon): GUI 打开路径切换到 openPreferred（内嵌优先）"
```

---

### Task 4: OkManager 窗口宿主（Windows 实现 + 平台分发）

**Files:**
- Create: `cmd/okmanager/host_windows.go`
- Create: `cmd/okmanager/host_other.go`
- Modify: `cmd/okmanager/main.go`（整体替换为分发调用）
- Test: `cmd/okmanager/host_windows_test.go`

**Interfaces:**
- Consumes: `daemon.EnsureCurrent() (*daemonx.Info, bool)`（`internal/daemon/client.go:62`）；`daemon.OpenGUI(_, stderr io.Writer) int`（回退，`run.go:164`）；`gui.WindowTitle`、`gui.LoadWindowState/SaveWindowState/WindowState`、`gui.TokenInitScript`（Task 1/2）；`webview2.NewWithOptions / WebView.Run/Navigate/Init/SetSize/Destroy/Window`（go-webview2）
- Produces: `func runHost(stdout, stderr io.Writer) int`（两平台同名分发点，main.go 唯一调用）

- [ ] **Step 1: 写失败测试（token 脚本在 Task 1 已测，这里测 placement 转换纯逻辑）**

`cmd/okmanager/host_windows_test.go`（`//go:build windows`）：

```go
//go:build windows

package main

import (
	"testing"

	"openknowledge/internal/gui"
)

func TestPlacementFromStateMaximized(t *testing.T) {
	wp := placementFromState(&gui.WindowState{Maximized: true, Left: 1, Top: 2, Right: 801, Bottom: 601})
	if wp.ShowCmd != swMaximize {
		t.Fatalf("ShowCmd got %d, want %d", wp.ShowCmd, swMaximize)
	}
	if wp.RcNormalPosition != [4]int32{1, 2, 801, 601} {
		t.Fatalf("rect got %v", wp.RcNormalPosition)
	}
}

func TestStateFromPlacement(t *testing.T) {
	wp := windowPlacement{ShowCmd: swShowNormal, RcNormalPosition: [4]int32{5, 6, 805, 606}}
	s := stateFromPlacement(&wp)
	if s.Maximized || s.Left != 5 || s.Bottom != 606 {
		t.Fatalf("got %+v", s)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/okmanager/ -v`
Expected: FAIL（`undefined: placementFromState` 等）

- [ ] **Step 3: 实现**

`cmd/okmanager/host_windows.go`（`//go:build windows`）：

```go
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
	w, _, _ := procHostGetSystemMetrics.Call(0)  // SM_CXSCREEN
	h, _, _ := procHostGetSystemMetrics.Call(1)  // SM_CYSCREEN
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
```

`cmd/okmanager/host_other.go`（`//go:build !windows`）：

```go
//go:build !windows

package main

import (
	"io"

	"openknowledge/internal/daemon"
)

func runHost(stdout, stderr io.Writer) int { return daemon.OpenGUI(stdout, stderr) }
```

`cmd/okmanager/main.go`（整体替换）：

```go
package main

import "os"

// OkManager：OpenKnowledge 配置中心入口。Windows 托管 WebView2 原生窗口
// （host_windows.go），其余平台回退浏览器（host_other.go）。不含业务逻辑，
// 全部功能走 okd HTTP API。见 docs/superpowers/specs/2026-08-25-gui-embedded-window-design.md。
func main() { os.Exit(runHost(os.Stdout, os.Stderr)) }
```

- [ ] **Step 4: 跑测试 + 双平台构建**

Run: `go test ./cmd/okmanager/ -v && go build ./... && GOOS=linux GOARCH=amd64 go build ./cmd/okmanager`
Expected: PASS；构建成功（linux 编译选中 host_other.go）

- [ ] **Step 5: Commit**

```bash
git add cmd/okmanager/
git commit -m "feat(okmanager): WebView2 原生窗口宿主——启动即最大化、状态记忆、token 注入"
```

---

### Task 5: 前端 token 优先级（__okToken > sessionStorage）

**Files:**
- Modify: `web/index.html:9-23`

**Interfaces:**
- Consumes: `gui.TokenInitScript` 注入的 `window.__okToken`（Task 1/4）
- Produces: `window.OK_TOKEN` 取值顺序——`__okToken`（内嵌）→ sessionStorage（浏览器 hash 路径）

- [ ] **Step 1: 改 inline script**

`web/index.html` 第 16-18 行附近，把：

```js
  var t = "";
  try { t = sessionStorage.getItem("ok_token") || ""; } catch (e) {}
  window.OK_TOKEN = t;
```

改为：

```js
  var t = window.__okToken || "";
  if (!t) { try { t = sessionStorage.getItem("ok_token") || ""; } catch (e) {} }
  window.OK_TOKEN = t;
```

hash 解析段（其上数行）原样保留——浏览器路径仍靠它写 sessionStorage。

- [ ] **Step 2: 验证**

无自动化前端测试基建，手工验证并入 Task 6 清单。确认语法：`node --check web/index.html` 不可用（html），改为目检 + 浏览器开一次 GUI 正常加载（浏览器路径不受影响）。

- [ ] **Step 3: Commit**

```bash
git add web/index.html
git commit -m "feat(web): token 获取优先 window.__okToken（内嵌窗口注入）"
```

---

### Task 6: 全量验证与 Windows 实机手工清单

**Files:**
- Modify: 无（纯验证）

- [ ] **Step 1: 全量测试与双平台构建**

Run: `go test ./... -count=1 && go vet ./... && GOOS=linux GOARCH=amd64 go build ./...`
Expected: 全绿

- [ ] **Step 2: 按发布路径构建三 exe（含 winres）**

Run: `bash scripts/build-dist.sh`（Git Bash；winres 段若失败按脚本提示 `--skip-winres` 仅本地验证）
Expected: `dist/OkManager.exe` 等产物更新。注意项目坑：本地调试须用 dist 产物或同步 `dist/web`（`dist/okd.exe` serving 的页面要与 `web/` 一致）

- [ ] **Step 3: Windows 实机手工清单（逐项过）**

1. 双击 `OkManager.exe`：窗口**首帧即最大化**，无小窗口闪烁；标题"OpenKnowledge 配置中心"；任务栏/Alt-Tab 图标为 OkManager 图标（若显示默认图标，记录后续设 IconId）；
2. 取消最大化 → 拖动调整尺寸位置 → 关闭 → 重开：恢复上次状态；
3. 窗口内刷新（F5/Ctrl+R）：页面功能正常（token 未丢，`__okToken` 每次导航注入）；
4. 拖到最小尺寸限制：无法小于 960×600；
5. 托盘双击：聚焦内嵌窗口（非新开浏览器）；
6. `ok gui`：同样出内嵌窗口；
7. 临时重命名 WebView2 运行时注册表 pv 不便模拟时，改 `webView2RuntimeAvailable` 返回 false 重编验证：回退浏览器 `--app=` 正常；
8. 浏览器直接开 GUI URL（带 `#token=`）：正常（回退路径完整）；
9. 多显示器：副屏关闭窗口后重开，位置正确恢复。

- [ ] **Step 4: Commit（如有修复）+ 收尾**

确认无遗留后，向用户提示沉淀（内嵌窗口的平台坑/经验按项目约定走 propose 草稿）。

---

## Self-Review 记录

- Spec 覆盖：§3 进程模型→Task 3/4；§4 窗口行为→Task 4（状态记忆 Task 1）；§5 token 注入→Task 1/4/5；§6 代码结构→Task 2/4；§7 安全→Task 4（token 不走 URL/命令行）；§8 测试→各 Task + Task 6；回退语义→Task 2/4。已知限制（导航拦截不做）已写入 spec §4，计划无对应任务（有意）。
- 占位符：无 TBD/TODO；所有代码步骤含完整代码。
- 类型一致：`WindowState` 字段（Task 1）与 Task 4 的 `placementFromState/stateFromPlacement` 一致；`OpenPreferred/embeddedOpener/browserOpener`（Task 2）与 Task 3 消费一致；`gui.WindowTitle`（Task 2 定义，Task 4 使用）一致。
