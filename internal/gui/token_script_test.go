package gui

import "testing"

func TestTokenInitScript(t *testing.T) {
	got := TokenInitScript("http://127.0.0.1:17888", `ab"cd`)
	want := `if(location.origin==="http://127.0.0.1:17888"){window.__okToken = "ab\"cd";}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTokenInitScriptOriginGated(t *testing.T) {
	got := TokenInitScript("http://127.0.0.1:17888", "tok")
	want := `if(location.origin==="http://127.0.0.1:17888"){window.__okToken = "tok";}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
