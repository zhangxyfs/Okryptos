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

func TestOpenEmbeddedDefaultNoRuntime(t *testing.T) {
	defer func(f func() bool) { webView2RuntimeAvailable = f }(webView2RuntimeAvailable)
	webView2RuntimeAvailable = func() bool { return false }
	if h := openEmbeddedDefault("x"); h != 0 {
		t.Fatalf("got %d, want 0 when WebView2 runtime missing", h)
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
