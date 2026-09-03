// Package credmig 凭证统一（2026-09-01 设计）：机器级 git 凭证确保/迁移。
// 唯一发放通道 = okserver apiGitToken（ok-sync-r-<hostname>）。本包负责
// "本机持有一份新凭证 + 覆盖到所有已绑定项目 remote + 迁移标记"，
// 供 daemon 同步周期与 GUI 清理按钮两个入口共用。
package credmig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"okryptos/internal/config"
	"okryptos/internal/registry"
	"okryptos/internal/serverx"
	"okryptos/internal/store"
	"okryptos/internal/syncx"
)

const markerFile = "cred-migrated.json"

type marker struct {
	Username  string    `json:"username"`
	Hostname  string    `json:"hostname"`
	TokenName string    `json:"token_name"`
	At        time.Time `json:"at"`
}

// Done 报告本机该账号是否已完成凭证迁移（标记存在且用户名一致）。
func Done(home, username string) bool {
	data, err := os.ReadFile(filepath.Join(home, markerFile))
	if err != nil {
		return false
	}
	var m marker
	return json.Unmarshal(data, &m) == nil && m.Username == username
}

// Ensure 为本机申请/刷新 ok-sync-r-<hostname> 凭证并覆盖到所有已绑定项目的
// remote 凭据，成功后写迁移标记。幂等（服务端重发 = 删同名再建，无副作用堆积）。
// file:// remote 无认证语义，跳过凭据覆盖；无 credential helper 时回退 URL 内嵌
// （与绑定路径同语义）。
func Ensure(cfg config.Server) (string, error) {
	if cfg.URL == "" {
		return "", errors.New("未配置服务器")
	}
	host, _ := os.Hostname()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := serverx.New(cfg.URL, cfg.Token)
	tok, name, err := c.GitToken(ctx, host)
	if err != nil {
		return "", err
	}
	home := registry.Home()
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		return "", fmt.Errorf("加载注册表: %w", err)
	}
	for _, p := range reg.Projects {
		st := store.New(filepath.Join(home, "projects", p.Name))
		repo := syncx.Open(st.Root)
		if !repo.IsRepo() {
			continue
		}
		remote := repo.RemoteURL()
		if remote == "" || strings.HasPrefix(remote, "file://") {
			continue
		}
		if syncx.HasCredentialHelper(st.Root) {
			if err := syncx.StoreCredential(st.Root, remote, cfg.Username, tok); err != nil {
				return name, fmt.Errorf("%s 写入凭据: %w", p.Name, err)
			}
			continue
		}
		if err := repo.SetRemote(syncx.CredentialURLWithAuth(syncx.StripURLAuth(remote), cfg.Username, tok)); err != nil {
			return name, fmt.Errorf("%s 更新 remote: %w", p.Name, err)
		}
	}
	m := marker{Username: cfg.Username, Hostname: host, TokenName: name, At: time.Now()}
	data, _ := json.Marshal(m)
	// 标记写失败 fail-open：返回错误由调用方记日志，下轮重试（重发幂等）。
	if err := os.WriteFile(filepath.Join(home, markerFile), data, 0o600); err != nil {
		return name, fmt.Errorf("写迁移标记: %w", err)
	}
	return name, nil
}
