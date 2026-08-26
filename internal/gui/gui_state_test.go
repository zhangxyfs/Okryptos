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
