package deployx

import (
	"context"
	"strings"
	"testing"
)

func TestQueryStatus(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "docker compose", code: 0, stdout: "gitea|Up 2 hours\nokserver|Up 2 hours"},
		{match: "OKSERVER_IMAGE", code: 0, stdout: "z7dream/openknowledge-okserver:v2.23.0"},
		{match: "OKSERVER_PORT", code: 0, stdout: "3100"},
		{match: "du -sh", code: 0, stdout: "128M\t/home/u/openknowledge"},
	}}
	st, err := QueryStatus(context.Background(), fx, "/home/u/openknowledge")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Containers) != 2 || st.Containers[0].Name != "gitea" {
		t.Fatalf("%+v", st)
	}
	if st.Image != "z7dream/openknowledge-okserver:v2.23.0" || st.DiskUsage != "128M" || st.OKPort != "3100" {
		t.Fatalf("%+v", st)
	}
	fx.Done()
}

func TestUpgradeTask(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "OKSERVER_PORT", code: 0, stdout: "3100"},
		{match: "OKSERVER_IMAGE", code: 0}, // sed 改 .env
		{match: "pull", code: 0},
		{match: "up -d", code: 0},
		{match: "api/v1/meta", code: 0, stdout: "{}"},
	}}
	task := BuildUpgradeTask("/home/u/openknowledge", "v9.9.9")
	e := &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}}
	if err := task.Execute(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	// 断言 sed 命令含新 tag
	found := false
	for _, c := range fx.Cmds {
		if strings.Contains(c, "z7dream/openknowledge-okserver:v9.9.9") {
			found = true
		}
	}
	if !found {
		t.Fatalf("未找到改镜像 tag 的命令：%v", fx.Cmds)
	}
	fx.Done()
}

func TestUninstallTaskKeepsDataByDefault(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "down --rmi all", code: 0},
	}}
	task := BuildUninstallTask("/home/u/openknowledge", false)
	if err := task.Execute(context.Background(), &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range fx.Cmds {
		if strings.Contains(c, "rm -rf") {
			t.Fatalf("默认不应删数据目录：%v", fx.Cmds)
		}
	}
	fx.Done()
}

func TestUninstallTaskDeleteData(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "down --rmi all", code: 0},
		{match: "rm -rf", code: 0},
	}}
	task := BuildUninstallTask("/home/u/openknowledge", true)
	if err := task.Execute(context.Background(), &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	fx.Done()
}

// 非法部署目录：BuildUpgradeTask 第一步即失败，不执行任何远程命令。
func TestUpgradeTaskRejectsBadDir(t *testing.T) {
	fx := &fakeExec{t: t}
	task := BuildUpgradeTask("/a;id", "v9.9.9")
	err := task.Execute(context.Background(), &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}})
	if err == nil {
		t.Fatal("非法目录应报错")
	}
	if len(fx.Cmds) != 0 {
		t.Fatalf("不应执行任何远程命令：%v", fx.Cmds)
	}
}

// 非法部署目录：BuildUninstallTask 第一步即失败，不执行任何远程命令。
func TestUninstallTaskRejectsBadDir(t *testing.T) {
	fx := &fakeExec{t: t}
	task := BuildUninstallTask("/a;id", true)
	err := task.Execute(context.Background(), &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}})
	if err == nil {
		t.Fatal("非法目录应报错")
	}
	if len(fx.Cmds) != 0 {
		t.Fatalf("不应执行任何远程命令：%v", fx.Cmds)
	}
}

func TestQueryRemoteLogs(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "logs --tail=200", code: 0, stdout: "okserver  | 启动完成"},
	}}
	logs, err := QueryRemoteLogs(context.Background(), fx, "/home/u/openknowledge", 0)
	if err != nil || !strings.Contains(logs, "启动完成") {
		t.Fatalf("logs=%q err=%v", logs, err)
	}
	fx.Done()
}

func TestResetRootTask(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "reset-root", code: 0, stdout: "newpw32chars"},
	}}
	task := BuildResetRootTask("/home/u/openknowledge")
	e := &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}}
	if err := task.Execute(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if e.Vars["root_password"] != "newpw32chars" {
		t.Fatalf("root_password = %q", e.Vars["root_password"])
	}
	for _, ev := range e.Hub.History() {
		if strings.Contains(ev.Text, "newpw32chars") {
			t.Fatalf("新密码进了日志：%+v", ev)
		}
	}
	fx.Done()
}
