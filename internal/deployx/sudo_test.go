package deployx

import (
	"context"
	"io"
	"strings"
	"testing"
)

// recExec 记录最后一次命令与 stdin 内容。
type recExec struct {
	cmd   string
	stdin string
}

func (r *recExec) Run(_ context.Context, cmd string, stdin io.Reader, _ func(string, string)) (int, error) {
	r.cmd = cmd
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		r.stdin = string(b)
	}
	return 0, nil
}

func (r *recExec) Download(_ context.Context, cmd string, _ io.Writer, _ func(int64)) error {
	r.cmd = cmd
	return nil
}

func (r *recExec) Close() error { return nil }

func TestWrapSudoEmptyPassword(t *testing.T) {
	if _, err := WrapSudo(&recExec{}, ""); err == nil {
		t.Fatal("空密码应报错")
	}
}

// Run 包装：命令进 sh -c（shellQuote），stdin 首行是密码、余下内容原样跟随（上传通道）。
func TestSudoRunWrapsCommandAndStdin(t *testing.T) {
	rec := &recExec{}
	sx, err := WrapSudo(rec, "p@ss'w")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sx.Run(context.Background(), "docker ps", strings.NewReader("FILE-CONTENT"), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.cmd, "sudo -kS") || !strings.Contains(rec.cmd, "sh -c 'docker ps'") {
		t.Fatalf("cmd = %q", rec.cmd)
	}
	if !strings.HasPrefix(rec.stdin, "p@ss'w\n") || !strings.HasSuffix(rec.stdin, "FILE-CONTENT") {
		t.Fatalf("stdin = %q", rec.stdin)
	}
}

// Download 包装：密码经 echo 管道喂入（无 stdin 参数）。
func TestSudoDownloadWraps(t *testing.T) {
	rec := &recExec{}
	sx, _ := WrapSudo(rec, "pw")
	if err := sx.Download(context.Background(), "tar -cf - x", io.Discard, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.cmd, "sudo -kS") || !strings.Contains(rec.cmd, "tar -cf - x") {
		t.Fatalf("cmd = %q", rec.cmd)
	}
}
