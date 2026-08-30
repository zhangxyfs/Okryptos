package deployx

import (
	"context"
	"strings"
	"testing"
)

// 全新部署全流程：断言命令序列骨架与 root 密码处理。
func TestDeployTaskFull(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "mkdir -p", code: 0},
		{match: "chown -R", code: 0},
		{match: "cat > ", code: 0},                 // 上传 compose.yaml
		{match: "docker compose", code: 0},         // pull（含 --env-file 或 -f）
		{match: "up -d gitea", code: 0},
		{match: "api/v1/version", code: 0, stdout: "{\"version\":\"1.22.0\"}"},
		{match: "admin user", code: 0}, // 建管理员（grep || create 合并命令）
		{match: "generate-access-token", code: 0, stdout: "gtok_abc123"},
		{match: "cat > ", code: 0}, // 上传 .env
		{match: "up -d", code: 0},
		{match: "api/v1/meta", code: 0, stdout: "{}"},
		{match: "INITIAL_ROOT_PASSWORD", code: 0, stdout: "rootpw32chars"},
		{match: "rm -f", code: 0},
	}}
	spec := DeploySpec{Mode: "full", Dir: "/home/u/openknowledge", GiteaPort: 3000, OKPort: 3100, Tag: "v9.9.9", RootURL: "http://nas:3000/"}
	task, err := BuildDeployTask(spec)
	if err != nil {
		t.Fatal(err)
	}
	e := &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}}
	if err := task.Execute(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if e.Vars["root_password"] != "rootpw32chars" {
		t.Fatalf("root_password = %q", e.Vars["root_password"])
	}
	if e.Vars["admin_token"] != "gtok_abc123" {
		t.Fatalf("admin_token = %q", e.Vars["admin_token"])
	}
	for _, ev := range e.Hub.History() {
		if strings.Contains(ev.Text, "rootpw32chars") || strings.Contains(ev.Text, "gtok_abc123") {
			t.Fatalf("秘密值进了日志：%+v", ev)
		}
	}
	fx.Done()
}

// 参数校验。
func TestBuildDeployTaskValidates(t *testing.T) {
	if _, err := BuildDeployTask(DeploySpec{Mode: "bogus", Dir: "/x"}); err == nil {
		t.Fatal("非法 mode 应报错")
	}
	if _, err := BuildDeployTask(DeploySpec{Mode: "full"}); err == nil {
		t.Fatal("空 Dir 应报错")
	}
	if _, err := BuildDeployTask(DeploySpec{Mode: "external", Dir: "/x"}); err == nil {
		t.Fatal("external 缺 GiteaURL 应报错")
	}
}

// 接入已有 Gitea：冒烟 → 上传 → 启动 → 读 root 密码。
func TestDeployTaskExternal(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "api/v1/version", code: 0, stdout: "{\"version\":\"1.22.0\"}"}, // 冒烟①
		{match: "api/v1/user", code: 0, stdout: "{\"login\":\"okadmin\"}"},     // 冒烟② Basic+token
		{match: "mkdir -p", code: 0},
		{match: "chown -R", code: 0},
		{match: "cat > ", code: 0}, // compose.yaml
		{match: "cat > ", code: 0}, // .env（external 无需等 token 生成，直接写）
		{match: "pull", code: 0},
		{match: "up -d", code: 0},
		{match: "api/v1/meta", code: 0, stdout: "{}"},
		{match: "INITIAL_ROOT_PASSWORD", code: 0, stdout: "rootpw32chars"},
		{match: "rm -f", code: 0},
	}}
	spec := DeploySpec{Mode: "external", Dir: "/home/u/openknowledge", OKPort: 3100,
		Tag: "v9.9.9", GiteaURL: "http://192.168.1.10:3000", AdminToken: "usertok"}
	task, err := BuildDeployTask(spec)
	if err != nil {
		t.Fatal(err)
	}
	e := &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}}
	if err := task.Execute(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if e.Vars["root_password"] != "rootpw32chars" {
		t.Fatalf("root_password = %q", e.Vars["root_password"])
	}
	for _, ev := range e.Hub.History() {
		if strings.Contains(ev.Text, "usertok") || strings.Contains(ev.Text, "rootpw32chars") {
			t.Fatalf("秘密值进了日志：%+v", ev)
		}
	}
	fx.Done()
}

// 冒烟②失败（Gitea 版本不支持 token 当用户名）→ 任务终止，错误提示版本风险。
func TestExternalSmokeTokenAsUsernameFails(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "api/v1/version", code: 0, stdout: "{}"},
		{match: "api/v1/user", code: 22}, // curl -f 对 401/403 返回 22
	}}
	task, _ := BuildDeployTask(DeploySpec{Mode: "external", Dir: "/d", OKPort: 3100,
		Tag: "v1", GiteaURL: "http://gitea:3000", AdminToken: "tok"})
	err := task.Execute(context.Background(), &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}})
	if err == nil || !strings.Contains(err.Error(), "token 当用户名") {
		t.Fatalf("err = %v", err)
	}
	fx.Done()
}

func TestGovernanceChecklist(t *testing.T) {
	items := GovernanceChecklist()
	if len(items) != 4 {
		t.Fatalf("清单 = %v", items)
	}
}

// 中途失败即停：pull 失败后不得执行 up。
func TestDeployTaskStopsOnPullFailure(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "mkdir -p", code: 0},
		{match: "chown -R", code: 1}, // chown 失败只警告不终止
		{match: "cat > ", code: 0},
		{match: "pull", code: 1},
	}}
	task, _ := BuildDeployTask(DeploySpec{Mode: "full", Dir: "/d", GiteaPort: 3000, OKPort: 3100, Tag: "v1", RootURL: "http://x/"})
	err := task.Execute(context.Background(), &Env{Ex: fx, Hub: NewLogHub(), Vars: map[string]string{}})
	if err == nil {
		t.Fatal("pull 失败应终止任务")
	}
	fx.Done()
}
