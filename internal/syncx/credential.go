// credential.go：git 凭据写入系统 credential helper（设计文档 §9.4）。
// 无 helper 时返回 ErrNoCredentialHelper，调用方回退 URL 内嵌（v1 取舍）。
package syncx

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"openknowledge/internal/procx"
)

// ErrNoCredentialHelper 表示系统/仓库未配置 git credential.helper——
// approve 会静默丢弃凭据（git 语义），必须显式告知调用方走回退。
var ErrNoCredentialHelper = errors.New("系统未配置 git credential helper")

// StoreCredential 把 remote URL 的凭据写入系统 credential helper。
// 在仓库目录 dir 上下文执行（仓级 helper 配置可见）。
func StoreCredential(dir, remoteURL, username, password string) error {
	// 先探测 helper（credential.helper 为空则 approve 静默丢弃——git 语义）
	out, err := execGit(dir, localTimeout, "config", "--get", "credential.helper")
	if err != nil || strings.TrimSpace(out) == "" {
		return ErrNoCredentialHelper
	}
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "credential", "approve")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	procx.HideWindow(cmd)
	cmd.Stdin = strings.NewReader("url=" + remoteURL + "\nusername=" + username + "\npassword=" + password + "\n\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		var ee *exec.ExitError
		code := -1
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return &ExitError{Code: code, Output: string(out)}
	}
	return nil
}

// CredentialURLWithAuth 回退：把凭据嵌进 remote URL（无 helper 环境）。
// token 经 url.PathEscape（UserPassword 内部处理转义）。
func CredentialURLWithAuth(remoteURL, username, token string) string {
	u, err := url.Parse(remoteURL)
	if err != nil || u.Host == "" {
		return remoteURL
	}
	u.User = url.UserPassword(username, token)
	return u.String()
}
