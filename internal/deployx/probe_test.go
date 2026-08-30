package deployx

import (
	"context"
	"strings"
	"testing"
)

// 干净机器：docker+compose 有，端口空，无 gitea，无 okserver。
func TestProbeFreshMachine(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "uname -m", code: 0, stdout: "x86_64"},
		{match: "command -v docker", code: 0, stdout: "/usr/bin/docker"},
		{match: "docker version", code: 0, stdout: "24.0.7"},
		{match: "docker compose version", code: 0, stdout: "2.23.0"},
		{match: "ss -ltn", code: 0, stdout: "State  Recv-Q Send-Q Local Address:Port Peer Address:Port\nLISTEN 0      4096       0.0.0.0:22    0.0.0.0:*"},
		{match: "docker ps -a", code: 0, stdout: ""},
	}}
	r, err := Probe(context.Background(), fx)
	if err != nil {
		t.Fatal(err)
	}
	if !r.DockerCLI || !r.DockerOK || !r.ComposeOK || r.Arch != "x86_64" {
		t.Fatalf("%+v", r)
	}
	if r.PortGiteaBusy != "" || r.PortOKBusy != "" || r.GiteaFound || r.Existing {
		t.Fatalf("%+v", r)
	}
	fx.Done()
}

// 无 Docker 机器：CLI 不存在，跳过 daemon 检查，结果全 false，不返回 err。
func TestProbeNoDocker(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "uname -m", code: 0, stdout: "aarch64"},
		{match: "command -v docker", code: 127},
		{match: "docker compose version", code: 127},
		{match: "ss -ltn", code: 0, stdout: ""},
		{match: "docker ps -a", code: 127},
	}}
	r, err := Probe(context.Background(), fx)
	if err != nil {
		t.Fatal(err)
	}
	if r.DockerCLI || r.DockerOK || r.ComposeOK {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.DockerDetail, "PATH") {
		t.Fatalf("DockerDetail = %q", r.DockerDetail)
	}
	fx.Done()
}

// NAS 经典状况：CLI 装了但 SSH 用户不在 docker 组（daemon socket 权限不足）。
// DockerCLI=true、DockerOK=false、Detail 带回真实 stderr——前端据此给加组指引。
func TestProbeDockerPermissionDenied(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "uname -m", code: 0, stdout: "x86_64"},
		{match: "command -v docker", code: 0, stdout: "/usr/local/bin/docker"},
		{match: "docker version", code: 1, stderr: "permission denied while trying to connect to the Docker daemon socket"},
		{match: "docker compose version", code: 0, stdout: "2.23.0"},
		{match: "ss -ltn", code: 0, stdout: ""},
		{match: "docker ps -a", code: 1},
	}}
	r, err := Probe(context.Background(), fx)
	if err != nil {
		t.Fatal(err)
	}
	if !r.DockerCLI || r.DockerOK || !r.ComposeOK {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.DockerDetail, "permission denied") {
		t.Fatalf("DockerDetail = %q", r.DockerDetail)
	}
	fx.Done()
}

// 已有 Gitea 容器且 3000 被它占用。
func TestProbeGiteaPresent(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "uname -m", code: 0, stdout: "x86_64"},
		{match: "command -v docker", code: 0, stdout: "/usr/bin/docker"},
		{match: "docker version", code: 0, stdout: "24.0.7"},
		{match: "docker compose version", code: 0, stdout: "2.23.0"},
		{match: "ss -ltn", code: 0, stdout: "LISTEN 0 4096 0.0.0.0:3000 0.0.0.0:*"},
		{match: "docker ps -a", code: 0, stdout: "mygitea|gitea/gitea:1.21|Up 3 days|0.0.0.0:3000->3000/tcp"},
	}}
	r, err := Probe(context.Background(), fx)
	if err != nil {
		t.Fatal(err)
	}
	if !r.GiteaFound || r.PortGiteaBusy == "" {
		t.Fatalf("%+v", r)
	}
	fx.Done()
}

// 已有 okserver 部署 → Existing + DeployDir。
func TestProbeExistingDeploy(t *testing.T) {
	fx := &fakeExec{t: t, steps: []fakeStep{
		{match: "uname -m", code: 0, stdout: "x86_64"},
		{match: "command -v docker", code: 0, stdout: "/usr/bin/docker"},
		{match: "docker version", code: 0, stdout: "24.0.7"},
		{match: "docker compose version", code: 0, stdout: "2.23.0"},
		{match: "ss -ltn", code: 0, stdout: "LISTEN 0 4096 0.0.0.0:3100 0.0.0.0:*"},
		{match: "docker ps -a", code: 0, stdout: "okserver|openknowledge/okserver:v2.23.0|Up 2 hours|0.0.0.0:3100->3100/tcp\ngitea|gitea/gitea:1.22|Up 2 hours|0.0.0.0:3000->3000/tcp"},
		{match: "docker inspect okserver", code: 0, stdout: "/root/openknowledge"},
	}}
	r, err := Probe(context.Background(), fx)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Existing || r.DeployDir != "/root/openknowledge" || !r.GiteaFound {
		t.Fatalf("%+v", r)
	}
	fx.Done()
}
