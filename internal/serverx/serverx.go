// Package serverx 是本地（ok/okd/GUI）与 okserver 管理 API 打交道的客户端。
// 薄 Bearer HTTP 客户端（照 llmx 形态）；不依赖其他 internal 包。
package serverx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	base  string
	token string
	hc    *http.Client
}

// New 构造客户端；baseURL 去尾斜杠；默认 15s 超时。
func New(baseURL, token string) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), token: token, hc: &http.Client{Timeout: 15 * time.Second}}
}

// Error 是 okserver 的错误响应（{"error": msg}），Code 透传 HTTP 状态码。
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("okserver %d: %s", e.Code, e.Msg) }

// call 发请求：Bearer 头 + JSON 编解码 + 错误包装。
func (c *Client) call(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("连接服务器失败: %w", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		return &Error{Code: res.StatusCode, Msg: e.Error}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// —— 数据类型（与 oksrv 契约同形状，本地独立定义） ——

type Meta struct {
	Version    string `json:"version"`
	Initialized bool   `json:"initialized"`
	GitBackend struct {
		Type string `json:"type"`
		Ok   bool   `json:"ok"`
	} `json:"git_backend"`
}

type RepoInfo struct {
	Layer     string `json:"layer"`
	Owner     string `json:"owner"`
	Project   string `json:"project"`
	Name      string `json:"name"`
	CloneURL  string `json:"clone_url"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

type MeInfo struct {
	Name               string     `json:"name"`
	Role               string     `json:"role"`
	MustChangePassword bool       `json:"must_change_password"`
	Orgs               []string   `json:"orgs"`
	Repos              []RepoInfo `json:"repos"`
}

type ProvisionResult struct {
	Repo     RepoInfo `json:"repo"`
	GitToken string   `json:"git_token"`
}

type ServerUser struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Disabled  bool   `json:"disabled"`
	CreatedAt string `json:"created_at"`
}

type CreatedUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
	GitToken string `json:"git_token"`
}

type OrgMember struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type ServerOrg struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Members     []OrgMember `json:"members"`
}

type ServerRepo struct {
	Layer     string `json:"layer"`
	Owner     string `json:"owner"`
	Project   string `json:"project"`
	CloneURL  string `json:"clone_url"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
}

type ServerAudit struct {
	ID        int64  `json:"id"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"created_at"`
}

// —— 端点方法 ——

func (c *Client) Meta(ctx context.Context) (*Meta, error) {
	var m Meta
	if err := c.call(ctx, "GET", "/meta", nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (c *Client) Login(ctx context.Context, username, password string) (string, string, error) {
	var out struct {
		Token string `json:"token"`
		User  struct {
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"user"`
	}
	err := c.call(ctx, "POST", "/login", map[string]string{"username": username, "password": password}, &out)
	if err != nil {
		return "", "", err
	}
	return out.Token, out.User.Role, nil
}

func (c *Client) Me(ctx context.Context) (*MeInfo, error) {
	var m MeInfo
	if err := c.call(ctx, "GET", "/me", nil, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ChangePassword 自助改密（旧密码校验在服务端；401 旧密码错误、
// 403 must_change_password 等以 *Error 透传状态码）。
func (c *Client) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	return c.call(ctx, "POST", "/change-password", map[string]string{"old_password": oldPassword, "new_password": newPassword}, nil)
}

func (c *Client) ProvisionPersonalRepo(ctx context.Context, project string) (*ProvisionResult, error) {
	var out ProvisionResult
	if err := c.call(ctx, "POST", "/repos/personal", map[string]string{"project": project}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListUsers(ctx context.Context) ([]ServerUser, error) {
	var out struct {
		Users []ServerUser `json:"users"`
	}
	if err := c.call(ctx, "GET", "/users", nil, &out); err != nil {
		return nil, err
	}
	return out.Users, nil
}

func (c *Client) CreateUser(ctx context.Context, username, role, password string) (*CreatedUser, error) {
	var out CreatedUser
	if err := c.call(ctx, "POST", "/users", map[string]string{"username": username, "role": role, "password": password}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteUser 删账号（服务端连带删 Gitea 侧；root/越权由服务端门控）。
func (c *Client) DeleteUser(ctx context.Context, username string) error {
	return c.call(ctx, "DELETE", "/users/"+username, nil, nil)
}

func (c *Client) ResetPassword(ctx context.Context, username string) (string, error) {
	var out struct {
		Password string `json:"password"`
	}
	if err := c.call(ctx, "POST", "/users/"+username+"/reset-password", nil, &out); err != nil {
		return "", err
	}
	return out.Password, nil
}

func (c *Client) SetUserDisabled(ctx context.Context, username string, disabled bool) error {
	p := "/enable"
	if disabled {
		p = "/disable"
	}
	return c.call(ctx, "POST", "/users/"+username+p, nil, nil)
}

func (c *Client) ListOrgs(ctx context.Context) ([]ServerOrg, error) {
	var out struct {
		Orgs []ServerOrg `json:"orgs"`
	}
	if err := c.call(ctx, "GET", "/orgs", nil, &out); err != nil {
		return nil, err
	}
	return out.Orgs, nil
}

func (c *Client) CreateOrg(ctx context.Context, name, description string) error {
	return c.call(ctx, "POST", "/orgs", map[string]string{"name": name, "description": description}, nil)
}

func (c *Client) AddOrgMember(ctx context.Context, org, username, role string) error {
	return c.call(ctx, "POST", "/orgs/"+org+"/members", map[string]string{"username": username, "role": role}, nil)
}

func (c *Client) RemoveOrgMember(ctx context.Context, org, username string) error {
	return c.call(ctx, "DELETE", "/orgs/"+org+"/members/"+username, nil, nil)
}

func (c *Client) CreateTeamRepo(ctx context.Context, org, project string) (*RepoInfo, error) {
	var out struct {
		Repo RepoInfo `json:"repo"`
	}
	if err := c.call(ctx, "POST", "/repos/team", map[string]string{"org": org, "project": project}, &out); err != nil {
		return nil, err
	}
	return &out.Repo, nil
}

func (c *Client) ListRepos(ctx context.Context) ([]ServerRepo, error) {
	var out struct {
		Repos []ServerRepo `json:"repos"`
	}
	if err := c.call(ctx, "GET", "/repos", nil, &out); err != nil {
		return nil, err
	}
	return out.Repos, nil
}

func (c *Client) ListAudit(ctx context.Context, limit, offset int) ([]ServerAudit, error) {
	var out struct {
		Entries []ServerAudit `json:"entries"`
	}
	if err := c.call(ctx, "GET", fmt.Sprintf("/audit?limit=%d&offset=%d", limit, offset), nil, &out); err != nil {
		return nil, err
	}
	return out.Entries, nil
}
