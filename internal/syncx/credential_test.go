package syncx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreCredential(t *testing.T) {
	// 用 store helper + 临时文件验证 approve 真的落了凭据
	storeFile := filepath.Join(t.TempDir(), "creds")
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	// 仓级配 store helper 指向临时文件
	if _, err := execGit(dir, localTimeout, "config", "credential.helper", "store --file "+filepath.ToSlash(storeFile)); err != nil {
		t.Fatalf("config helper: %v", err)
	}
	if err := StoreCredential(dir, "http://nas:3000/alice/ok-demo.git", "alice", "tok-1"); err != nil {
		t.Fatalf("store: %v", err)
	}
	data, err := os.ReadFile(storeFile)
	if err != nil {
		t.Fatalf("store file: %v", err)
	}
	if !strings.Contains(string(data), "alice:tok-1@nas") {
		t.Fatalf("cred content: %q", data)
	}
}

func TestStoreCredentialNoHelper(t *testing.T) {
	// GIT_CONFIG_GLOBAL/SYSTEM 指向空文件隔离全局与系统配置（须在 Init 前生效）
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-sys"))
	// 仓级也确保无 helper（防 Init 继承）——显式置空一次
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := execGit(dir, localTimeout, "config", "--local", "credential.helper", ""); err != nil {
		t.Fatalf("clear helper: %v", err)
	}
	err := StoreCredential(dir, "http://nas/x.git", "u", "p")
	if !errors.Is(err, ErrNoCredentialHelper) {
		t.Fatalf("want ErrNoCredentialHelper, got %v", err)
	}
}

// TestHasStoredCredential 无 helper → false；helper 指向测试脚本后可分辨有/无存凭据。
func TestHasStoredCredential(t *testing.T) {
	dir := t.TempDir()
	// 隔离配置：无 helper
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-sys"))
	if HasStoredCredential(dir, "http://h/u/r.git", "alice") {
		t.Fatal("no helper should be false")
	}
	// 配一个永远返回固定凭据的 helper（shell 脚本），验证 true 分支
	gitconfig := filepath.Join(t.TempDir(), "gitconfig2")
	helper := filepath.Join(t.TempDir(), "helper.sh")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho username=alice\necho password=secret\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[credential]\n\thelper = !sh " + filepath.ToSlash(helper) + "\n"
	if err := os.WriteFile(gitconfig, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	if !HasStoredCredential(dir, "http://h/u/r.git", "alice") {
		t.Fatal("helper with stored cred should be true")
	}
}

func TestCredentialURLWithAuth(t *testing.T) {
	got := CredentialURLWithAuth("http://nas:3000/alice/ok-demo.git", "alice", "tok/with special")
	if !strings.Contains(got, "alice:tok") || !strings.Contains(got, "@nas:3000") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, " ") {
		t.Fatalf("space must be escaped: %q", got)
	}
}
