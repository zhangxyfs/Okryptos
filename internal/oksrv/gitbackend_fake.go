// 仅供测试与离线开发：内存态 GitBackend。镜像真实后端的失败语义（重名拒绝、未知用户拒绝）。
package oksrv

import (
	"context"
	"fmt"
	"sync"
)

type fakeRepo struct{ owner, name string }

type FakeBackend struct {
	mu         sync.Mutex
	down       bool
	failTokens bool // 测试注入：CreateUserToken 强制失败（建用户回滚路径用）
	users      map[string]bool // username → active
	tokens     map[string]int
	repos      map[fakeRepo]bool
	orgs       map[string]map[string]bool // org → members
}

func NewFakeBackend() *FakeBackend {
	return &FakeBackend{
		users:  map[string]bool{},
		tokens: map[string]int{},
		repos:  map[fakeRepo]bool{},
		orgs:   map[string]map[string]bool{},
	}
}

func (f *FakeBackend) SetDown(down bool) { f.mu.Lock(); f.down = down; f.mu.Unlock() }

// SetFailTokens 测试注入：CreateUserToken 强制失败（镜像 Gitea 1.22 缺 scope 的 400 场景）。
func (f *FakeBackend) SetFailTokens(fail bool) { f.mu.Lock(); f.failTokens = fail; f.mu.Unlock() }

func (f *FakeBackend) check() error {
	if f.down {
		return ErrBackendDown
	}
	return nil
}

func (f *FakeBackend) Ping(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return "", err
	}
	return "fake-gitea 0.0.0", nil
}

func (f *FakeBackend) CreateUser(ctx context.Context, username, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return err
	}
	if _, ok := f.users[username]; ok {
		return fmt.Errorf("用户 %q 已存在", username)
	}
	f.users[username] = true
	return nil
}

func (f *FakeBackend) CreateUserToken(ctx context.Context, username, tokenName string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return "", err
	}
	if f.failTokens {
		return "", fmt.Errorf("access token must have a scope")
	}
	if _, ok := f.users[username]; !ok {
		return "", fmt.Errorf("用户 %q 不存在", username)
	}
	f.tokens[username]++
	return fmt.Sprintf("fake-token-%s-%s-%d", username, tokenName, f.tokens[username]), nil
}

// DeleteUserToken _fake 语义：记数即可（撞名场景由 tokens 计数自然区分）。
func (f *FakeBackend) DeleteUserToken(_ context.Context, _, _ string) error { return nil }

// DeleteUser 从 users 删除（建用户回滚用）；不存在时报错（尽力而为场景被忽略）。
func (f *FakeBackend) DeleteUser(ctx context.Context, username string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return err
	}
	if _, ok := f.users[username]; !ok {
		return fmt.Errorf("用户 %q 不存在", username)
	}
	delete(f.users, username)
	return nil
}

func (f *FakeBackend) SetUserActive(ctx context.Context, username string, active bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return err
	}
	if _, ok := f.users[username]; !ok {
		return fmt.Errorf("用户 %q 不存在", username)
	}
	f.users[username] = active
	return nil
}

func (f *FakeBackend) CreatePersonalRepo(ctx context.Context, username, repoName string) (GitRepo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return GitRepo{}, err
	}
	if _, ok := f.users[username]; !ok {
		return GitRepo{}, fmt.Errorf("用户 %q 不存在", username)
	}
	key := fakeRepo{owner: username, name: repoName}
	if f.repos[key] {
		return GitRepo{}, fmt.Errorf("仓库 %s/%s 已存在", username, repoName)
	}
	f.repos[key] = true
	return GitRepo{Owner: username, Name: repoName, CloneURL: cloneURL(username, repoName)}, nil
}

func (f *FakeBackend) CreateOrg(ctx context.Context, name, displayName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return err
	}
	if _, ok := f.orgs[name]; ok {
		return fmt.Errorf("组织 %q 已存在", name)
	}
	f.orgs[name] = map[string]bool{}
	return nil
}

func (f *FakeBackend) AddOrgMember(ctx context.Context, org, username string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return err
	}
	members, ok := f.orgs[org]
	if !ok {
		return fmt.Errorf("组织 %q 不存在", org)
	}
	if _, ok := f.users[username]; !ok {
		return fmt.Errorf("用户 %q 不存在", username)
	}
	members[username] = true
	return nil
}

func (f *FakeBackend) RemoveOrgMember(ctx context.Context, org, username string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return err
	}
	members, ok := f.orgs[org]
	if !ok {
		return fmt.Errorf("组织 %q 不存在", org)
	}
	if !members[username] {
		return fmt.Errorf("用户 %q 不是组织 %q 的成员", username, org)
	}
	delete(members, username)
	return nil
}

func (f *FakeBackend) CreateOrgRepo(ctx context.Context, org, repoName string) (GitRepo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return GitRepo{}, err
	}
	if _, ok := f.orgs[org]; !ok {
		return GitRepo{}, fmt.Errorf("组织 %q 不存在", org)
	}
	key := fakeRepo{owner: org, name: repoName}
	if f.repos[key] {
		return GitRepo{}, fmt.Errorf("仓库 %s/%s 已存在", org, repoName)
	}
	f.repos[key] = true
	return GitRepo{Owner: org, Name: repoName, CloneURL: cloneURL(org, repoName)}, nil
}

func (f *FakeBackend) RepoExists(ctx context.Context, owner, repoName string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(); err != nil {
		return false, err
	}
	return f.repos[fakeRepo{owner: owner, name: repoName}], nil
}

func cloneURL(owner, name string) string {
	return fmt.Sprintf("http://gitea.fake/%s/%s.git", owner, name)
}

var _ GitBackend = (*FakeBackend)(nil)
