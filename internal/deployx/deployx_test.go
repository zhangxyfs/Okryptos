package deployx

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// fakeStep 脚本化一条预期命令。
type fakeStep struct {
	match  string // 命令须包含的片段
	code   int
	stdout string
	err    error
}

// fakeExec 按脚本应答 Run/Download，并记录实际命令序列。
type fakeExec struct {
	t     *testing.T
	steps []fakeStep
	Cmds  []string
	dl    []byte // Download 时写出的字节
}

func (f *fakeExec) Run(_ context.Context, cmd string, stdin io.Reader, onLine func(string, string)) (int, error) {
	f.Cmds = append(f.Cmds, cmd)
	if len(f.steps) == 0 {
		f.t.Fatalf("意外命令：%s", cmd)
	}
	st := f.steps[0]
	f.steps = f.steps[1:]
	if !strings.Contains(cmd, st.match) {
		f.t.Fatalf("命令 %q 不含预期片段 %q", cmd, st.match)
	}
	for _, ln := range strings.Split(st.stdout, "\n") {
		if ln != "" && onLine != nil {
			onLine("stdout", ln)
		}
	}
	return st.code, st.err
}

func (f *fakeExec) Download(_ context.Context, cmd string, w io.Writer, onProgress func(int64)) error {
	f.Cmds = append(f.Cmds, cmd)
	if _, err := w.Write(f.dl); err != nil {
		return err
	}
	if onProgress != nil {
		onProgress(int64(len(f.dl)))
	}
	return nil
}

func (f *fakeExec) Close() error { return nil }

// Done 断言脚本已消费完。
func (f *fakeExec) Done() {
	f.t.Helper()
	if len(f.steps) != 0 {
		f.t.Fatalf("还有 %d 个预期命令未执行", len(f.steps))
	}
}

func TestTaskExecuteStopsOnFailure(t *testing.T) {
	hub := NewLogHub()
	var ran []string
	task := Task{Name: "t", Steps: []Step{
		{Name: "s1", Run: func(_ context.Context, _ *Env) error { ran = append(ran, "s1"); return nil }},
		{Name: "s2", Run: func(_ context.Context, _ *Env) error { return fmt.Errorf("boom") }},
		{Name: "s3", Run: func(_ context.Context, _ *Env) error { ran = append(ran, "s3"); return nil }},
	}}
	err := task.Execute(context.Background(), &Env{Hub: hub, Vars: map[string]string{}})
	if err == nil || !strings.Contains(err.Error(), "s2") {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(ran, ",") != "s1" {
		t.Fatalf("失败后仍执行了后续步骤：%v", ran)
	}
	hist := hub.History()
	if hist[len(hist)-1].Level != "err" {
		t.Fatalf("末条日志应为 err：%+v", hist[len(hist)-1])
	}
}

func TestLogHubSubscribeGetsLiveAndHistory(t *testing.T) {
	hub := NewLogHub()
	hub.Publish("a", "info", "old")
	ch, unsub := hub.Subscribe()
	defer unsub()
	hub.Publish("a", "ok", "new")
	select {
	case ev := <-ch:
		if ev.Text != "new" {
			t.Fatalf("ev = %+v", ev)
		}
	default:
		t.Fatal("订阅者没收到实时事件")
	}
	if len(hub.History()) != 2 {
		t.Fatalf("History = %d", len(hub.History()))
	}
}

func TestRunCmdCapturesStdoutAndFailsOnNonZero(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "echo hi", code: 0, stdout: "hi\nthere"},
		{match: "bad-cmd", code: 3},
	}}
	e := &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}}
	out, err := runCmd(context.Background(), e, "s", "echo hi")
	if err != nil || out != "hi\nthere" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, err := runCmd(context.Background(), e, "s", "bad-cmd"); err == nil || !strings.Contains(err.Error(), "退出码 3") {
		t.Fatalf("err = %v", err)
	}
	fx.Done()
}

func TestRunCmdMaskLogsDisplayNotSecret(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{{match: "tok SECRET123", code: 0}}}
	e := &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}}
	if _, err := runCmdMask(context.Background(), e, "s", "curl -u ****", "curl -u tok SECRET123"); err != nil {
		t.Fatal(err)
	}
	for _, ev := range e.Hub.History() {
		if strings.Contains(ev.Text, "SECRET123") {
			t.Fatalf("秘密值进了日志：%+v", ev)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("a'b"); got != `'a'"'"'b'` {
		t.Fatalf("shellQuote = %q", got)
	}
}

func TestValidateDir(t *testing.T) {
	for _, ok := range []string{"~/openknowledge", "$HOME/openknowledge", "/opt/ok", "/home/u/ok-1.2"} {
		if err := ValidateDir(ok); err != nil {
			t.Fatalf("%q 应合法：%v", ok, err)
		}
	}
	for _, bad := range []string{"", "/a b", "/a;rm -rf /", "/a`id`", "/a'b"} {
		if err := ValidateDir(bad); err == nil {
			t.Fatalf("%q 应非法", bad)
		}
	}
}
