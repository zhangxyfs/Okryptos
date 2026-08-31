package oksrv

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordAndRoot(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse") || CheckPassword(h, "wrong") {
		t.Fatal("bcrypt check")
	}

	s, _ := OpenStore(t.TempDir())
	defer s.Close()
	pw, created, err := s.EnsureRoot()
	if err != nil || !created {
		t.Fatalf("EnsureRoot: %v %v", created, err)
	}
	if len(pw) != 32 {
		t.Fatalf("root password length = %d", len(pw))
	}
	// 二次调用不再生成
	_, created2, _ := s.EnsureRoot()
	if created2 {
		t.Fatal("second EnsureRoot must not create")
	}
	// root 可登录
	u := s.VerifyLogin("root", pw)
	if u == nil || u.Role != "root" {
		t.Fatal("root login failed")
	}
	if s.VerifyLogin("root", "bad") != nil {
		t.Fatal("bad password must fail")
	}
	// 初始 root 密码强制首登改密
	if !s.GetUser("root").MustChangePassword {
		t.Fatal("初始 root 密码必须置强制改密标记")
	}
	// 禁用用户不可登录
	if _, err := s.CreateUser("bob", "member", mustHash(t, "pw1")); err != nil {
		t.Fatal(err)
	}
	if s.VerifyLogin("bob", "pw1") == nil {
		t.Fatal("bob should login")
	}
	s.SetUserDisabled("bob", true)
	if s.VerifyLogin("bob", "pw1") != nil {
		t.Fatal("disabled must not login")
	}
}

func mustHash(t *testing.T, pw string) string {
	t.Helper()
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestSession(t *testing.T) {
	s, _ := OpenStore(t.TempDir())
	defer s.Close()
	pw, _, _ := s.EnsureRoot()
	_ = pw
	root := s.GetUser("root")
	tok, err := s.CreateSession(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 40 {
		t.Fatalf("token too short: %d", len(tok))
	}
	u := s.SessionUser(tok)
	if u == nil || u.Username != "root" {
		t.Fatal("session lookup")
	}
	if s.SessionUser("garbage") != nil {
		t.Fatal("bad token must fail")
	}
	if s.SessionUser(tok[:len(tok)-2] + "xx") != nil {
		t.Fatal("tampered token must fail")
	}
	// token 落库的是哈希不是明文
	row := s.db.QueryRow("SELECT token_hash FROM sessions")
	var th string
	if err := row.Scan(&th); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(th, tok) {
		t.Fatal("plaintext token in db")
	}
}

func TestLoginLimiter(t *testing.T) {
	l := NewLoginLimiter()
	l.failWindow = 50 * time.Millisecond // 测试调小（包级可调字段）
	for i := 0; i < 5; i++ {
		if !l.Allow("u") {
			t.Fatalf("attempt %d should be allowed", i)
		}
		l.Fail("u")
	}
	if l.Allow("u") {
		t.Fatal("6th should be locked")
	}
	if !l.Allow("other") {
		t.Fatal("other user unaffected")
	}
	time.Sleep(60 * time.Millisecond)
	if !l.Allow("u") {
		t.Fatal("lock should expire")
	}
	l.Reset("u") // 成功登录后清零
}
