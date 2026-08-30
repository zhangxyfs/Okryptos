package oksrv

import (
	"context"
	"errors"
)

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
