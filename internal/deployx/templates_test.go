package deployx

import (
	"strings"
	"testing"
)

func TestRenderComposeFull(t *testing.T) {
	c, err := RenderCompose("full")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gitea/gitea:1.22", "${OKSERVER_IMAGE}", "DISABLE_REGISTRATION=true",
		"MAX_CREATION_LIMIT=0", "DEFAULT_PRIVATE=private", "GITEA__security__INSTALL_LOCK=true", "${GITEA_HTTP_PORT}:3000", "${OKSERVER_PORT}:3100"} {
		if !strings.Contains(c, want) {
			t.Fatalf("full compose 缺 %q", want)
		}
	}
}

func TestRenderComposeExternal(t *testing.T) {
	c, err := RenderCompose("external")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(c, "gitea/gitea") {
		t.Fatal("external 模式不应含 gitea 服务")
	}
	if !strings.Contains(c, "OKSERVER_GITEA_URL=${EXTERNAL_GITEA_URL}") {
		t.Fatal("external 模式缺外部 Gitea URL")
	}
	if _, err := RenderCompose("bogus"); err == nil {
		t.Fatal("非法 mode 应报错")
	}
}

func TestRenderEnvFull(t *testing.T) {
	env := RenderEnv(DeploySpec{
		Mode: "full", GiteaPort: 3000, OKPort: 3100,
		Tag: "v9.9.9", RootURL: "http://192.168.1.10:3000/", AdminToken: "tok123",
	})
	for _, want := range []string{"GITEA_HTTP_PORT=3000", "OKSERVER_PORT=3100",
		"OKSERVER_IMAGE=z7dream/openknowledge-okserver:v9.9.9", "GITEA_ROOT_URL=http://192.168.1.10:3000/", "GITEA_ADMIN_TOKEN=tok123"} {
		if !strings.Contains(env, want) {
			t.Fatalf(".env 缺 %q", want)
		}
	}
}

func TestRenderEnvExternal(t *testing.T) {
	env := RenderEnv(DeploySpec{Mode: "external", OKPort: 3101, Tag: "v9.9.9",
		GiteaURL: "http://192.168.1.10:3000", AdminToken: "tok123"})
	if strings.Contains(env, "GITEA_HTTP_PORT") {
		t.Fatal("external 模式不应有 Gitea 端口")
	}
	if !strings.Contains(env, "EXTERNAL_GITEA_URL=http://192.168.1.10:3000") {
		t.Fatal("external 模式缺 EXTERNAL_GITEA_URL")
	}
}

func TestNewGiteaAdminToken(t *testing.T) {
	a, b := NewGiteaAdminToken(), NewGiteaAdminToken()
	if len(a) != 40 || a == b {
		t.Fatalf("token 长度/随机性异常：%q", a)
	}
}
