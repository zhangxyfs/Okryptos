package deployx

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strings"
)

//go:embed templates/compose-full.yaml
var composeFull string

//go:embed templates/compose-external.yaml
var composeExternal string

// DeploySpec 是一次部署的全部参数。
type DeploySpec struct {
	Mode       string `json:"mode"`        // "full" | "external"
	Dir        string `json:"dir"`         // 远端部署目录（默认 ~/openknowledge）
	GiteaPort  int    `json:"gitea_port"`  // full 模式
	OKPort     int    `json:"ok_port"`
	Tag        string `json:"tag"`         // okserver 镜像 tag
	RootURL    string `json:"root_url"`    // full 模式：http://<nas>:<giteaPort>/
	GiteaURL   string `json:"gitea_url"`   // external 模式：已有 Gitea 地址
	AdminToken string `json:"admin_token"` // external 必填；full 由部署流程生成后回填
}

// RenderCompose 返回内嵌 compose 模板。
func RenderCompose(mode string) (string, error) {
	switch mode {
	case "full":
		return composeFull, nil
	case "external":
		return composeExternal, nil
	}
	return "", fmt.Errorf("未知部署模式 %q", mode)
}

// RenderEnv 渲染 .env（含秘密值，上传权限 0600，永不进日志）。
func RenderEnv(s DeploySpec) string {
	var b strings.Builder
	if s.Mode == "full" {
		fmt.Fprintf(&b, "GITEA_HTTP_PORT=%d\n", s.GiteaPort)
		fmt.Fprintf(&b, "GITEA_ROOT_URL=%s\n", s.RootURL)
	} else {
		fmt.Fprintf(&b, "EXTERNAL_GITEA_URL=%s\n", s.GiteaURL)
	}
	fmt.Fprintf(&b, "OKSERVER_PORT=%d\n", s.OKPort)
	fmt.Fprintf(&b, "OKSERVER_IMAGE=z7dream/openknowledge-okserver:%s\n", s.Tag)
	fmt.Fprintf(&b, "GITEA_ADMIN_TOKEN=%s\n", s.AdminToken)
	return b.String()
}

// NewGiteaAdminToken 生成 40 字符 hex token（full 模式建仓后注入 .env）。
func NewGiteaAdminToken() string {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand 失败是不可恢复的系统故障（同 internal/oksrv/auth.go 惯例）
	}
	return hex.EncodeToString(buf)
}
