package gui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowStateRoundTrip(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	p := windowStatePath()
	os.MkdirAll(filepath.Dir(p), 0o755)
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
	t.Setenv("OK_HOME", t.TempDir())
	p := windowStatePath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("{not json"), 0o644)
	if _, ok := LoadWindowState(); ok {
		t.Fatal("corrupt file should yield ok=false")
	}
}

func TestLoadWindowStateInvalidRect(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	p := windowStatePath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(`{"maximized":false,"left":100,"top":0,"right":50,"bottom":10}`), 0o644)
	if _, ok := LoadWindowState(); ok {
		t.Fatal("right<=left should yield ok=false")
	}
}

func TestLoadWindowStateOffscreenRect(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	p := windowStatePath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	// 离屏创建阶段采到的垃圾值（真实案例）：矩形有效但整体在屏外，必须按无状态处理
	os.WriteFile(p, []byte(`{"maximized":false,"left":-32000,"top":-32000,"right":-30080,"bottom":-30920}`), 0o644)
	if _, ok := LoadWindowState(); ok {
		t.Fatal("fully offscreen rect should yield ok=false")
	}
}
