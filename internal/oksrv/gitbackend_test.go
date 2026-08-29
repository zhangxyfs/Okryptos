package oksrv

import (
	"context"
	"errors"
	"testing"
)

func TestFakeBackend(t *testing.T) {
	b := NewFakeBackend()
	ctx := context.Background()

	v, err := b.Ping(ctx)
	if err != nil || v == "" {
		t.Fatalf("ping: %v %q", err, v)
	}
	if err := b.CreateUser(ctx, "alice", "pw"); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := b.CreateUser(ctx, "alice", "pw"); err == nil {
		t.Fatal("duplicate user must fail")
	}
	tok, err := b.CreateUserToken(ctx, "alice", "ok-sync")
	if err != nil || tok == "" {
		t.Fatalf("token: %v %q", err, tok)
	}
	r, err := b.CreatePersonalRepo(ctx, "alice", "ok-demo")
	if err != nil || r.Owner != "alice" || r.Name != "ok-demo" || r.CloneURL == "" {
		t.Fatalf("repo: %+v %v", r, err)
	}
	if _, err := b.CreatePersonalRepo(ctx, "alice", "ok-demo"); err == nil {
		t.Fatal("duplicate repo must fail")
	}
	ok, err := b.RepoExists(ctx, "alice", "ok-demo")
	if err != nil || !ok {
		t.Fatalf("exists: %v %v", ok, err)
	}
	if err := b.CreateOrg(ctx, "ok-acme", "Acme"); err != nil {
		t.Fatalf("org: %v", err)
	}
	if err := b.AddOrgMember(ctx, "ok-acme", "alice"); err != nil {
		t.Fatalf("member: %v", err)
	}
	if err := b.AddOrgMember(ctx, "ok-acme", "ghost"); err == nil {
		t.Fatal("member of unknown user must fail")
	}
	r2, err := b.CreateOrgRepo(ctx, "ok-acme", "demo")
	if err != nil || r2.Owner != "ok-acme" {
		t.Fatalf("org repo: %+v %v", r2, err)
	}
	if err := b.SetUserActive(ctx, "alice", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	// down 模式：全部响亮失败
	b.SetDown(true)
	if _, err := b.Ping(ctx); !errors.Is(err, ErrBackendDown) {
		t.Fatalf("down ping: %v", err)
	}
	if _, err := b.CreatePersonalRepo(ctx, "alice", "ok-x"); !errors.Is(err, ErrBackendDown) {
		t.Fatalf("down create: %v", err)
	}
}
