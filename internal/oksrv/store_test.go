package oksrv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreLifecycle(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer s.Close()

	if s.HasRoot() {
		t.Fatal("fresh store should have no root")
	}
	u, err := s.CreateUser("root", "root", "hash-x")
	if err != nil || u.Role != "root" {
		t.Fatalf("create root: %v %v", u, err)
	}
	if !s.HasRoot() {
		t.Fatal("should have root")
	}
	// 第二个 root 拒绝
	if _, err := s.CreateUser("root2", "root", "h"); err == nil {
		t.Fatal("second root must fail")
	}
	// 重名拒绝
	if _, err := s.CreateUser("root", "admin", "h"); err == nil {
		t.Fatal("duplicate username must fail")
	}
	if _, err := s.CreateUser("alice", "member", "h"); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if got := s.GetUser("alice"); got == nil || got.Username != "alice" {
		t.Fatalf("get: %+v", got)
	}
	if got := s.GetUser("nobody"); got != nil {
		t.Fatalf("ghost: %+v", got)
	}
	// 禁用/启用
	if err := s.SetUserDisabled("root", true); err == nil {
		t.Fatal("disable root must fail")
	}
	if err := s.SetUserDisabled("alice", true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !s.GetUser("alice").Disabled {
		t.Fatal("alice should be disabled")
	}
	// 改密码
	if err := s.SetUserPasswordHash("alice", "h2"); err != nil {
		t.Fatalf("setpw: %v", err)
	}

	// 组织
	if err := s.CreateOrg("acme", "Acme 公司"); err != nil {
		t.Fatalf("org: %v", err)
	}
	if !s.OrgExists("acme") || s.OrgExists("nope") {
		t.Fatal("org exists check")
	}
	if err := s.AddOrgMember("acme", "alice", "member"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := s.AddOrgMember("acme", "alice", "member"); err != nil {
		t.Fatalf("re-add should be idempotent: %v", err)
	}
	if ms := s.ListOrgMembers("acme"); len(ms) != 1 || ms[0].Username != "alice" {
		t.Fatalf("members: %+v", ms)
	}
	if orgs := s.ListUserOrgs("alice"); len(orgs) != 1 || orgs[0] != "acme" {
		t.Fatalf("user orgs: %v", orgs)
	}
	if err := s.RemoveOrgMember("acme", "alice"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(s.ListOrgMembers("acme")) != 0 {
		t.Fatal("member should be gone")
	}

	// 仓库
	if err := s.UpsertRepo(Repo{Layer: "personal", Owner: "alice", Project: "demo", CloneURL: "http://gitea/alice/ok-demo.git", CreatedBy: "alice"}); err != nil {
		t.Fatalf("repo: %v", err)
	}
	s.UpsertRepo(Repo{Layer: "personal", Owner: "alice", Project: "demo", CloneURL: "http://gitea/alice/ok-demo.git", CreatedBy: "alice"}) // 幂等
	if rs := s.ListReposByOwner("personal", "alice"); len(rs) != 1 || rs[0].Project != "demo" {
		t.Fatalf("repos: %+v", rs)
	}
	if got := s.GetRepo("personal", "alice", "demo"); got == nil {
		t.Fatal("get repo")
	}
	if got := s.GetRepo("personal", "alice", "nope"); got != nil {
		t.Fatal("ghost repo")
	}

	// 审计
	s.Audit("root", "user.create", "alice", "初始密码已下发")
	s.Audit("root", "org.create", "acme", "")
	entries := s.ListAudit(10, 0)
	if len(entries) != 2 || entries[0].Target != "acme" { // 倒序：新的在前
		t.Fatalf("audit: %+v", entries)
	}

	// meta
	if err := s.SetMeta("schema_version", "1"); err != nil {
		t.Fatal(err)
	}
	if s.GetMeta("schema_version") != "1" || s.GetMeta("missing") != "" {
		t.Fatal("meta")
	}

	// 重开持久化
	s.Close()
	s2, err := OpenStore(s.dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if !s2.HasRoot() || len(s2.ListUsers()) != 2 {
		t.Fatal("state should persist")
	}
}

// ResetRoot：新密码 32 位随机、可认证，旧密码失效，INITIAL_ROOT_PASSWORD 重写。
// （EnsureRoot 返回 (明文, created, err)、密码校验走 VerifyLogin、目录取 s.dir——
// 均以 auth.go/store.go 实际实现为准。）
func TestResetRoot(t *testing.T) {
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	old, created, err := st.EnsureRoot() // 首启生成
	if err != nil || !created {
		t.Fatalf("EnsureRoot: created=%v err=%v", created, err)
	}
	new1, err := st.ResetRoot()
	if err != nil {
		t.Fatal(err)
	}
	if len(new1) != 32 || new1 == old {
		t.Fatalf("新密码长度/随机性异常：%q", new1)
	}
	// 新密码可认证、旧密码不可（VerifyLogin 是 store 既有校验入口）
	if st.VerifyLogin("root", new1) == nil || st.VerifyLogin("root", old) != nil {
		t.Fatal("重置后新旧密码校验不符")
	}
	if _, err := os.Stat(filepath.Join(st.dir, "INITIAL_ROOT_PASSWORD")); err != nil {
		t.Fatal("应重写 INITIAL_ROOT_PASSWORD")
	}
}

// 未初始化 root 时 ResetRoot 应报错（root 行由首启 EnsureRoot 建立）。
func TestResetRootWithoutRoot(t *testing.T) {
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.ResetRoot(); err == nil {
		t.Fatal("root 未初始化时应报错")
	}
}
