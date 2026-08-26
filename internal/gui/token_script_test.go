package gui

import "testing"

func TestTokenInitScript(t *testing.T) {
	got := TokenInitScript(`ab"cd`)
	want := `window.__okToken = "ab\"cd";`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
