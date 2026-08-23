package daemon

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestIsCLISubcommand(t *testing.T) {
	for _, yes := range []string{"hook", "on", "off", "setup", "gui", "init", "propose", "doctor"} {
		if !IsCLISubcommand(yes) {
			t.Errorf("%q should be a CLI subcommand", yes)
		}
	}
	for _, no := range []string{"", "stop", "daemon", "--help", "-v", "unknown"} {
		if IsCLISubcommand(no) {
			t.Errorf("%q should not be a CLI subcommand", no)
		}
	}
}

// 旧形态命令（okd hook ...）必须转发给同目录 ok，参数与 stdio 原样透传。
func TestForwardCLIRoutesToSiblingOk(t *testing.T) {
	dir := t.TempDir()
	okd := filepath.Join(dir, exeName("okd"))
	ok := filepath.Join(dir, exeName("ok"))
	for _, p := range []string{okd, ok} {
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	restoreSelf(t, okd)
	var gotPath string
	var gotArgs []string
	var gotStdin, gotStdout, gotStderr any
	restoreRun(t, func(c *exec.Cmd) error {
		gotPath, gotArgs = c.Path, c.Args
		gotStdin, gotStdout, gotStderr = c.Stdin, c.Stdout, c.Stderr
		return nil
	})
	var in, out, errOut bytes.Buffer
	in.WriteString("hook-payload")
	if code := ForwardCLI([]string{"hook", "prompt", "claude"}, &in, &out, &errOut); code != 0 {
		t.Fatalf("code=%d, want 0", code)
	}
	if gotPath != ok {
		t.Fatalf("exe=%q, want sibling ok %q", gotPath, ok)
	}
	wantArgs := append([]string{ok}, "hook", "prompt", "claude")
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("args=%v, want %v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Fatalf("args=%v, want %v", gotArgs, wantArgs)
		}
	}
	if gotStdin == nil || gotStdout == nil || gotStderr == nil {
		t.Fatal("stdio must be wired through")
	}
}

// 子进程退出码原样透传；其他执行错误按 1。
func TestForwardCLIExitCode(t *testing.T) {
	restoreSelf(t, fakeSelfNonOkd(t))
	restoreRun(t, func(c *exec.Cmd) error { return nil })
	if code := ForwardCLI([]string{"on"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("success: code=%d, want 0", code)
	}
	restoreRun(t, func(c *exec.Cmd) error { return errors.New("boom") })
	if code := ForwardCLI([]string{"on"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); code != 1 {
		t.Fatalf("generic error: code=%d, want 1", code)
	}
	restoreRun(t, func(c *exec.Cmd) error { return exitErrWithCode(t, 3) })
	if code := ForwardCLI([]string{"off"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); code != 3 {
		t.Fatalf("exit error: code=%d, want 3", code)
	}
}

// 孤儿 okd（无同目录 ok）：报错退出 1，stderr 有可读信息。
func TestForwardCLIOrphanOkdFails(t *testing.T) {
	dir := t.TempDir()
	okd := filepath.Join(dir, exeName("okd"))
	if err := os.WriteFile(okd, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreSelf(t, okd)
	var errOut bytes.Buffer
	if code := ForwardCLI([]string{"hook", "prompt", "claude"}, &bytes.Buffer{}, &bytes.Buffer{}, &errOut); code != 1 {
		t.Fatalf("code=%d, want 1", code)
	}
	if errOut.Len() == 0 {
		t.Fatal("stderr should explain the failure")
	}
}

// exitErrWithCode 构造带指定退出码的 *exec.ExitError（借真实子进程退出）。
func exitErrWithCode(t *testing.T, code int) error {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", "exit "+strconv.Itoa(code))
	} else {
		cmd = exec.Command("/bin/sh", "-c", "exit "+strconv.Itoa(code))
	}
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee
	}
	t.Fatalf("want ExitError, got %v", err)
	return nil
}

func fakeSelfNonOkd(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "agent.test")
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func restoreSelf(t *testing.T, self string) {
	t.Helper()
	old := cliSelfPath
	cliSelfPath = func() string { return self }
	t.Cleanup(func() { cliSelfPath = old })
}

func restoreRun(t *testing.T, f func(*exec.Cmd) error) {
	t.Helper()
	old := runCLIProc
	runCLIProc = f
	t.Cleanup(func() { runCLIProc = old })
}
