package oksrv

import (
	"context"
	"errors"
	"time"
)

// TokenInfo 是 git token 列表项（UpdatedAt = Gitea 1.24+ 的 last_used_at，每次使用时刷新；
// Gitea ≤1.23 无时间字段，两者均为零值，输出层转空串由前端回落/置空）。
type TokenInfo struct {
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// GitRepo 描述一个 git 仓库的最小信息。
type GitRepo struct {
	Owner    string
	Name     string
	CloneURL string
}

// GitBackend 抽象 git 托管后端（Gitea 等）的管理操作。
type GitBackend interface {
	Ping(ctx context.Context) (version string, err error)
	CreateUser(ctx context.Context, username, password string) error
	CreateUserToken(ctx context.Context, username, tokenName string) (string, error)
	// DeleteUserToken 删除同名 token（自助重发前的撞名清理；不存在时返回 nil 或
	// 可忽略错误——调用方尽力而为语义，成败以随后的 CreateUserToken 为准）。
	DeleteUserToken(ctx context.Context, username, tokenName string) error
	// ListUserTokens 列用户全部 token（凭证管理页数据源）。
	ListUserTokens(ctx context.Context, username string) ([]TokenInfo, error)
	// DeleteUser 用于建用户流程半途失败的回滚（尽力而为，错误只记审计不阻断）。
	DeleteUser(ctx context.Context, username string) error
	SetUserActive(ctx context.Context, username string, active bool) error
	CreatePersonalRepo(ctx context.Context, username, repoName string) (GitRepo, error)
	CreateOrg(ctx context.Context, name, displayName string) error
	AddOrgMember(ctx context.Context, org, username string) error
	RemoveOrgMember(ctx context.Context, org, username string) error
	CreateOrgRepo(ctx context.Context, org, repoName string) (GitRepo, error)
	RepoExists(ctx context.Context, owner, repoName string) (bool, error)
}

// ErrBackendDown 表示 git 后端不可达/故障（响亮失败，不静默 fallback）。
var ErrBackendDown = errors.New("git 后端不可用")
