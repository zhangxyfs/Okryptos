// gitea.go：GitBackend 的 Gitea 实现（admin API）。
// 端点形状核实记录见 Task 4 报告（docs.gitea.com/api/next 对照）。
package oksrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GiteaBackend struct {
	base  string // 形如 http://gitea:3000（去尾斜杠）
	token string
	hc    *http.Client
}

func NewGitea(baseURL, adminToken string) *GiteaBackend {
	return &GiteaBackend{
		base:  strings.TrimRight(baseURL, "/"),
		token: adminToken,
		hc:    &http.Client{Timeout: 15 * time.Second},
	}
}

// do 发一次 API 调用；basic=true 时用 Basic 头（admin token 作用户名、空密码——
// Gitea Basic 校验支持 token 当用户名），否则用 "token <admin-token>" 头。
func (g *GiteaBackend) do(ctx context.Context, method, path string, body any, out any, basic bool) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	if basic {
		req.SetBasicAuth(g.token, "")
	} else {
		req.Header.Set("Authorization", "token "+g.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := g.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBackendDown, err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 400 {
		return &giteaError{Code: res.StatusCode, Body: string(data)}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (g *GiteaBackend) call(ctx context.Context, method, path string, body any, out any) error {
	return g.do(ctx, method, path, body, out, false)
}

type giteaError struct {
	Code int
	Body string
}

func (e *giteaError) Error() string { return fmt.Sprintf("gitea %d: %s", e.Code, e.Body) }

// Ping 核实：GET /version → 200 ServerVersion{version}。
func (g *GiteaBackend) Ping(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := g.call(ctx, http.MethodGet, "/version", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// CreateUser 核实：POST /admin/users，CreateUserOption 必填 username+email——
// 接口无邮箱语义，合成占位 <username>@okserver.invalid；重名 → 422 → 报错。
func (g *GiteaBackend) CreateUser(ctx context.Context, username, password string) error {
	return g.call(ctx, http.MethodPost, "/admin/users", map[string]any{
		"username":             username,
		"email":                username + "@okserver.invalid",
		"password":             password,
		"must_change_password": false,
	}, nil)
}

// CreateUserToken 核实偏差：简报表格的 /admin/users/{username}/tokens 不存在；
// 真实端点 POST /users/{username}/tokens（body {name}，201 → AccessToken{sha1}），
// 且路由强制 reqBasicOrRevProxyAuth——admin API token 直接调会 401，故走 Basic 头。
// body 必须带 scopes：1.22 起缺省空 scope 直接 400 "access token must have a scope"
// （2026-08-30 真机实证）；git push/pull 只需 write:repository。
func (g *GiteaBackend) CreateUserToken(ctx context.Context, username, tokenName string) (string, error) {
	var out struct {
		SHA1 string `json:"sha1"`
	}
	err := g.do(ctx, http.MethodPost, "/users/"+url.PathEscape(username)+"/tokens",
		map[string]any{"name": tokenName, "scopes": []string{"write:repository"}}, &out, true)
	if err != nil {
		return "", err
	}
	return out.SHA1, nil
}

// DeleteUser 核实：DELETE /admin/users/{username}（204）。用于建用户流程半途失败的回滚。
func (g *GiteaBackend) DeleteUser(ctx context.Context, username string) error {
	return g.call(ctx, http.MethodDelete, "/admin/users/"+url.PathEscape(username), nil, nil)
}

// SetUserActive 核实：PATCH /admin/users/{username}（EditUserOption 含 active；
// 1.22 中 source_id/login_name 必填，故一并带上；成功返回 200）。
func (g *GiteaBackend) SetUserActive(ctx context.Context, username string, active bool) error {
	return g.call(ctx, http.MethodPatch, "/admin/users/"+url.PathEscape(username), map[string]any{
		"active":     active,
		"source_id":  0,
		"login_name": username,
	}, nil)
}

// CreatePersonalRepo 核实：POST /admin/users/{username}/repos {name, private, auto_init:false}；
// 重名 → 409 → 报错；201 → Repository{clone_url}。
func (g *GiteaBackend) CreatePersonalRepo(ctx context.Context, username, repoName string) (GitRepo, error) {
	return g.createRepo(ctx, "/admin/users/"+url.PathEscape(username)+"/repos",
		map[string]any{"name": repoName, "private": true, "auto_init": false}, username, repoName)
}

// CreateOrg 核实：POST /orgs {username, full_name, visibility:"private"}（enum 含 private）；
// 重名 → 422 → 报错。建组织时 Gitea 自动创建 Owners 队（AddOrgMember 依赖）。
func (g *GiteaBackend) CreateOrg(ctx context.Context, name, displayName string) error {
	return g.call(ctx, http.MethodPost, "/orgs", map[string]any{
		"username":   name,
		"full_name":  displayName,
		"visibility": "private",
	}, nil)
}

// AddOrgMember 核实偏差：简报表格的 PUT /orgs/{org}/membership/{username} 不存在；
// Gitea 组织成员 = 队成员，须 GET /orgs/{org}/teams 拿 Owners 队 id（建组织自动创建），
// 再 PUT /teams/{id}/members/{username}（204）。
func (g *GiteaBackend) AddOrgMember(ctx context.Context, org, username string) error {
	var teams []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := g.call(ctx, http.MethodGet, "/orgs/"+url.PathEscape(org)+"/teams", nil, &teams); err != nil {
		return err
	}
	for _, t := range teams {
		if t.Name == "Owners" {
			return g.call(ctx, http.MethodPut,
				fmt.Sprintf("/teams/%d/members/%s", t.ID, url.PathEscape(username)), nil, nil)
		}
	}
	return fmt.Errorf("组织 %q 无 Owners 队", org)
}

// RemoveOrgMember 核实偏差：真实路径 DELETE /orgs/{org}/members/{username}（204；非成员 → 404）。
func (g *GiteaBackend) RemoveOrgMember(ctx context.Context, org, username string) error {
	return g.call(ctx, http.MethodDelete,
		"/orgs/"+url.PathEscape(org)+"/members/"+url.PathEscape(username), nil, nil)
}

// CreateOrgRepo 核实：POST /orgs/{org}/repos {name, private}；201 → Repository{clone_url}。
func (g *GiteaBackend) CreateOrgRepo(ctx context.Context, org, repoName string) (GitRepo, error) {
	return g.createRepo(ctx, "/orgs/"+url.PathEscape(org)+"/repos",
		map[string]any{"name": repoName, "private": true}, org, repoName)
}

func (g *GiteaBackend) createRepo(ctx context.Context, path string, body any, owner, name string) (GitRepo, error) {
	var out struct {
		CloneURL string `json:"clone_url"`
	}
	if err := g.call(ctx, http.MethodPost, path, body, &out); err != nil {
		return GitRepo{}, err
	}
	return GitRepo{Owner: owner, Name: name, CloneURL: out.CloneURL}, nil
}

// RepoExists 核实：GET /repos/{owner}/{repo} → 200 / 404。
func (g *GiteaBackend) RepoExists(ctx context.Context, owner, repoName string) (bool, error) {
	err := g.call(ctx, http.MethodGet,
		"/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repoName), nil, nil)
	if err == nil {
		return true, nil
	}
	var ge *giteaError
	if errors.As(err, &ge) && ge.Code == http.StatusNotFound {
		return false, nil
	}
	return false, err
}

var _ GitBackend = (*GiteaBackend)(nil)
