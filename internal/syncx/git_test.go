package syncx

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExecGitVersion(t *testing.T) {
	out, err := execGit(t.TempDir(), localTimeout, "--version")
	if err != nil {
		t.Skipf("git 不可用，跳过：%v", err)
	}
	if !strings.Contains(out, "git version") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecGitNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // 空目录，git 必然找不到
	_, err := execGit(t.TempDir(), localTimeout, "--version")
	if !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("want ErrGitNotFound, got %v", err)
	}
}

func TestExecGitExitError(t *testing.T) {
	_, err := execGit(t.TempDir(), localTimeout, "status") // 非仓目录
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want *ExitError, got %T %v", err, err)
	}
	if ee.Code == 0 || !strings.Contains(ee.Output, "not a git repository") {
		t.Fatalf("unexpected ExitError: %+v", ee)
	}
}

func TestExecGitTimeout(t *testing.T) {
	old := localTimeout
	defer func() { _ = old }()
	_, err := execGit(t.TempDir(), time.Nanosecond, "status")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
}
