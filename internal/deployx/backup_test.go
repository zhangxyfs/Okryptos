package deployx

import (
	"context"
	"strings"
	"testing"
)

func TestBackupCmd(t *testing.T) {
	cmd := BackupCmd("/home/u/openknowledge")
	if !strings.Contains(cmd, "tar") || !strings.Contains(cmd, "okserver-data") {
		t.Fatalf("BackupCmd = %q", cmd)
	}
}

func TestBackupStopAndStartTasks(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "stop", code: 0},
		{match: "OKSERVER_PORT", code: 0, stdout: "3100"},
		{match: "up -d", code: 0},
		{match: "api/v1/meta", code: 0, stdout: "{}"},
	}}
	e := &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}}
	if err := BuildBackupStopTask("/d").Execute(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if err := BuildStartTask("/d").Execute(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	fx.Done()
}

func TestRestoreTask(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "stop", code: 0},
		{match: "rm -rf", code: 0},        // 清旧数据目录
		{match: "tar -xf -", code: 0},     // stdin 解压备份包
		{match: "OKSERVER_PORT", code: 0, stdout: "3100"},
		{match: "up -d", code: 0},
		{match: "api/v1/meta", code: 0, stdout: "{}"},
	}}
	task := BuildRestoreTask("/home/u/openknowledge", []byte("fake-tar-bytes"))
	e := &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}}
	if err := task.Execute(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	fx.Done()
}
