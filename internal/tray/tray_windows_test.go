//go:build windows

package tray

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestOpenOrFocusAsync：openGUI 内含长轮询（gui.OpenBrowser 最长约 20s），双击
// 处理必须异步返回、不卡消息线程（M-05：消息线程被卡时 daemon 退出的 2s
// trayDone 等待超时，cleanup 跳过 NIM_DELETE 留下幽灵图标）。
func TestOpenOrFocusAsync(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	tr := &Tray{openGUI: func() uintptr {
		calls.Add(1)
		<-release // 模拟 maximizeWindowByTitle 长轮询
		return 42
	}}

	done := make(chan struct{})
	go func() { tr.openOrFocus(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("openOrFocus 被 openGUI 同步阻塞")
	}

	// 等异步 goroutine 真正进入 openGUI（openOrFocus 返回不代表其已调用）
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("openGUI 未被调用")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 拉起进行中再次双击：不得重复调 openGUI
	tr.openOrFocus()
	if n := calls.Load(); n != 1 {
		t.Fatalf("拉起进行中重复双击，openGUI 被调用 %d 次", n)
	}

	close(release)
	deadline = time.Now().Add(2 * time.Second)
	for {
		tr.mu.Lock()
		h, opening := tr.guiHwnd, tr.opening
		tr.mu.Unlock()
		if h == 42 && !opening {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("openGUI 结果未回写: hwnd=%d opening=%v", h, opening)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
