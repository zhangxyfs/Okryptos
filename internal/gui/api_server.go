// api_server.go：GUI 服务器页的本地端点——配置读写 + 连接测试 + 登录 + 转发 okserver。
// SSRF 裁决（设计文档）：服务器地址来自用户自己的配置/输入，本地 GUI + X-Ok-Token
// 鉴权，目标是用户要连的 LAN 服务器——不做回环限制（与 ollama 探测端点不同）。
package gui

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"openknowledge/internal/config"
	"openknowledge/internal/registry"
	"openknowledge/internal/serverx"
	"openknowledge/internal/store"
	"openknowledge/internal/syncx"
)

func (h *Handler) registerServerAPI(api func(string, http.HandlerFunc)) {
	api("GET /api/server/config", h.apiServerConfigGet)
	api("PUT /api/server/config", h.apiServerConfigPut)
	api("POST /api/server/test", h.apiServerTest)
	api("POST /api/server/login", h.apiServerLogin)
	api("POST /api/server/logout", h.apiServerLogout)
	api("POST /api/server/change-password", h.fwdChangePassword)
	api("GET /api/server/me", h.apiServerMe)
	api("POST /api/server/repos", h.apiServerRepos)
	api("POST /api/server/pull", h.apiServerPull)
	// 管理类透传（角色门控在服务端）
	api("GET /api/server/users", h.fwdUsers)
	api("POST /api/server/users", h.fwdUserCreate)
	api("POST /api/server/users/{name}/reset-password", h.fwdUserResetPassword)
	api("DELETE /api/server/users/{name}", h.fwdUserDelete)
	api("POST /api/server/users/{name}/disable", h.fwdUserDisable(true))
	api("POST /api/server/users/{name}/enable", h.fwdUserDisable(false))
	api("GET /api/server/orgs", h.fwdOrgs)
	api("POST /api/server/orgs", h.fwdOrgCreate)
	api("POST /api/server/orgs/{org}/members", h.fwdOrgMemberAdd)
	api("DELETE /api/server/orgs/{org}/members/{username}", h.fwdOrgMemberRemove)
	api("POST /api/server/repos/team", h.fwdTeamRepo)
	api("GET /api/server/repos-all", h.fwdReposAll)
	api("GET /api/server/audit", h.fwdAudit)
	api("GET /api/server/tokens", h.fwdTokens)
	api("DELETE /api/server/tokens/{name}", h.fwdTokenDelete)
	api("GET /api/server/users/{name}/tokens", h.fwdUserTokens)
	api("DELETE /api/server/users/{name}/tokens/{token}", h.fwdUserTokenDelete)
}

// serverClient 从全局配置构造 serverx 客户端；未配置返回 nil。
func (h *Handler) serverClient() (*serverx.Client, config.Server, error) {
	cfg, err := config.Load(globalConfigPath())
	if err != nil {
		return nil, config.Server{}, err
	}
	s := cfg.Server
	if s.URL == "" {
		return nil, s, nil
	}
	return serverx.New(s.URL, s.Token), s, nil
}

// writeServerErr 把 serverx.Error 透传为同状态码；其他错误 502。
func writeServerErr(w http.ResponseWriter, err error) {
	var se *serverx.Error
	if errors.As(err, &se) {
		writeJSON(w, se.Code, map[string]string{"error": se.Msg})
		return
	}
	writeErr(w, http.StatusBadGateway, err.Error())
}

func (h *Handler) apiServerConfigGet(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.Load(globalConfigPath())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":       cfg.Server.URL,
		"username":  cfg.Server.Username,
		"has_token": cfg.Server.Token != "",
		"logged_in": cfg.Server.Token != "",
	})
}

func (h *Handler) apiServerConfigPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string `json:"url"`
		Username string `json:"username"`
		Token    string `json:"token"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := config.SetServer(globalConfigPath(), config.Server{URL: req.URL, Username: req.Username, Token: req.Token}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) apiServerTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.URL) == "" {
		writeErr(w, http.StatusBadRequest, "缺少服务器地址")
		return
	}
	meta, err := serverx.New(req.URL, "").Meta(r.Context())
	out := map[string]any{"ok": err == nil}
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["version"] = meta.Version
	out["git_backend_ok"] = meta.GitBackend.Ok
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) apiServerLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) || req.URL == "" || req.Username == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "地址/用户名/密码不能为空")
		return
	}
	tok, role, err := serverx.New(req.URL, "").Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	if err := config.SetServer(globalConfigPath(), config.Server{URL: req.URL, Username: req.Username, Token: tok}); err != nil {
		writeErr(w, http.StatusInternalServerError, "登录成功但配置写入失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token_saved": true,
		"user":        map[string]string{"name": req.Username, "role": role},
	})
}

// apiServerLogout 清空 [server] 段（SetServer 的空值语义：三键全省略 → 空段，加载回零值）。
func (h *Handler) apiServerLogout(w http.ResponseWriter, _ *http.Request) {
	if err := config.SetServer(globalConfigPath(), config.Server{}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) apiServerMe(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_configured"})
		return
	}
	me, err := c.Me(r.Context())
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
}

type serverReposRequest struct {
	Project string `json:"project"`
}

// errProjectExists 哨兵：apiServerPull 注册空壳时区分"项目已存在"（409）与锁/IO 错误（500）。
var errProjectExists = errors.New("项目已存在")

// apiServerRepos 建仓一条龙：已注册项目直接进共用绑定管线。
func (h *Handler) apiServerRepos(w http.ResponseWriter, r *http.Request) {
	var req serverReposRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	h.serveBindRepo(w, r, st, req.Project)
}

// apiServerPull 新机器拉取：服务器有仓、本机未注册的项目——注册空 Paths 壳
// （备份恢复同款先例）后走共用绑定管线（本地无内容 → InitForSync 自动 clone）。
func (h *Handler) apiServerPull(w http.ResponseWriter, r *http.Request) {
	var req serverReposRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validProjectName(req.Project) {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("非法项目名: %q", req.Project))
		return
	}
	// 未配置/未登录服务器时不注册空壳——宁可 409 也不留孤儿项目（终审 #3）
	if c, _, err := h.serverClient(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	} else if c == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_configured"})
		return
	}
	_, _, found, err := findProject(req.Project)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if found {
		writeErr(w, http.StatusConflict, "项目已注册："+req.Project)
		return
	}
	// 锁内读-改-写注册空壳（并发 ok init / GUI 删除互斥，与 cli.go init 同口径）；
	// 二次检查放锁内，防并发双注册。哨兵 errProjectExists 区分 409（已存在）/500（锁/IO）。
	if err := registry.Update(func(reg *registry.Registry) error {
		for _, p := range reg.Projects {
			if p.Name == req.Project {
				return errProjectExists
			}
		}
		reg.Projects = append(reg.Projects, registry.Project{Name: req.Project})
		return nil
	}); err != nil {
		if errors.Is(err, errProjectExists) {
			writeErr(w, http.StatusConflict, "项目已注册："+req.Project)
		} else {
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	st := store.New(filepath.Join(registry.Home(), "projects", req.Project))
	if err := st.EnsureDirs(); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建项目目录失败："+err.Error())
		return
	}
	h.serveBindRepo(w, r, st, req.Project)
}

// serveBindRepo 绑定管线（apiServerRepos 与 apiServerPull 共用）：
// provision → 凭据（空 token 时查本机已存凭据，没有则自助重发）→ init/clone → SetSync → 首次同步。
func (h *Handler) serveBindRepo(w http.ResponseWriter, r *http.Request, st *store.Store, project string) {
	c, cfgServer, err := h.serverClient()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_configured"})
		return
	}
	pr, err := c.ProvisionPersonalRepo(r.Context(), project)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	// 凭据：优先系统 credential helper；仓已存在（token 仅建仓首发）且本机未存凭据时
	// 自助重发（v2.25 服务端起）；无 helper 回退 URL 内嵌（credNote 警告）。
	remote := pr.Repo.CloneURL
	credNote := ""
	gitToken := pr.GitToken
	if gitToken == "" && !syncx.HasStoredCredential(st.Root, pr.Repo.CloneURL, cfgServer.Username) {
		host, _ := os.Hostname()
		tok, terr := c.GitToken(r.Context(), host)
		if terr != nil {
			var se *serverx.Error
			if errors.As(terr, &se) && se.Code == http.StatusNotFound {
				writeJSON(w, http.StatusOK, map[string]any{"status": "error", "clone_url": pr.Repo.CloneURL,
					"message": "仓已存在但本机无 git 凭据，且服务端版本过旧（不支持自助重发 token）：请升级 okserver，或联系管理员重置密码重发 token"})
				return
			}
			writeServerErr(w, terr)
			return
		}
		gitToken = tok
	}
	if gitToken != "" {
		if cerr := syncx.StoreCredential(st.Root, pr.Repo.CloneURL, cfgServer.Username, gitToken); cerr != nil {
			if errors.Is(cerr, syncx.ErrNoCredentialHelper) {
				remote = syncx.CredentialURLWithAuth(pr.Repo.CloneURL, cfgServer.Username, gitToken)
				credNote = "（凭据已内嵌 remote URL（本机无 git credential helper）——注意：项目 config.toml 会随仓同步，凭据将进入远端 git 历史。建议配置 credential helper 后重新绑定）"
			} else {
				writeErr(w, http.StatusInternalServerError, "写入 git 凭据失败："+cerr.Error())
				return
			}
		}
	} else if !syncx.HasCredentialHelper(st.Root) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "error", "clone_url": pr.Repo.CloneURL,
			"message": "仓已存在但本机无 git 凭据（token 仅建仓首发、本机无 credential helper）：请在终端手工绑定或联系管理员重置密码重发 token"})
		return
	}
	repo := syncx.Open(st.Root)
	if !repo.IsRepo() {
		kind, ierr := repo.InitForSync(remote, hasKnowledgeContent(st), "sync: init "+syncCommitMsg())
		switch {
		case errors.Is(ierr, syncx.ErrRemoteNotEmpty):
			writeErr(w, http.StatusConflict, "远端仓已有内容，需手动合并一次（git pull --rebase origin main 或 merge --allow-unrelated-histories）后重试")
			return
		case ierr != nil:
			writeErr(w, http.StatusInternalServerError, "初始化失败："+ierr.Error())
			return
		}
		_ = kind
	} else if err := repo.SetRemote(remote); err != nil {
		writeErr(w, http.StatusInternalServerError, "绑定远端失败："+err.Error())
		return
	}
	cfg, _ := config.LoadMerged(st.ConfigPath(), "")
	if err := config.SetSync(st.ConfigPath(), config.Sync{Enabled: true, Remote: remote, AutoIntervalMin: cfg.Sync.AutoIntervalMin}); err != nil {
		writeErr(w, http.StatusInternalServerError, "同步配置写入失败："+err.Error())
		return
	}
	o := syncx.SyncOnce(st.Root, syncCommitMsg())
	syncx.RecordOutcome(st.Root, st.StateDir(), o)
	msg := describeOutcome(o) + credNote
	switch {
	case o.Err != nil:
		writeJSON(w, http.StatusOK, map[string]any{"status": "error", "clone_url": pr.Repo.CloneURL, "message": msg})
	case len(o.Conflicts) > 0:
		writeJSON(w, http.StatusOK, map[string]any{"status": "conflict", "clone_url": pr.Repo.CloneURL, "message": msg})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "clone_url": pr.Repo.CloneURL, "message": msg})
	}
}

// —— 管理类透传 ——

func (h *Handler) fwdUsers(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	users, err := c.ListUsers(r.Context())
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (h *Handler) fwdUserCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	u, err := c.CreateUser(r.Context(), req.Username, req.Role, req.Password)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) fwdUserDelete(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	if err := c.DeleteUser(r.Context(), r.PathValue("name")); err != nil {
		writeServerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) fwdUserResetPassword(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	pw, err := c.ResetPassword(r.Context(), r.PathValue("name"))
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"password": pw})
}

// fwdChangePassword 透传自助改密；成功 204；401 旧密码错误 / 403 must_change_password
// 等经 writeServerErr 原样透传状态码与 message（前端据 403 弹强制改密框）。
func (h *Handler) fwdChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	if err := c.ChangePassword(r.Context(), req.OldPassword, req.NewPassword); err != nil {
		writeServerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) fwdUserDisable(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, _, err := h.serverClient()
		if err != nil || c == nil {
			writeNotConfigured(w, err)
			return
		}
		if err := c.SetUserDisabled(r.Context(), r.PathValue("name"), disabled); err != nil {
			writeServerErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) fwdOrgs(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	orgs, err := c.ListOrgs(r.Context())
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orgs": orgs})
}

func (h *Handler) fwdOrgCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	if err := c.CreateOrg(r.Context(), req.Name, req.Description); err != nil {
		writeServerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) fwdOrgMemberAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	if err := c.AddOrgMember(r.Context(), r.PathValue("org"), req.Username, req.Role); err != nil {
		writeServerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) fwdOrgMemberRemove(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	if err := c.RemoveOrgMember(r.Context(), r.PathValue("org"), r.PathValue("username")); err != nil {
		writeServerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) fwdTeamRepo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Org     string `json:"org"`
		Project string `json:"project"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	repo, err := c.CreateTeamRepo(r.Context(), req.Org, req.Project)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repo": repo})
}

func (h *Handler) fwdReposAll(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	repos, err := c.ListRepos(r.Context())
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

func (h *Handler) fwdAudit(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	entries, err := c.ListAudit(r.Context(), limit, offset)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// fwdTokens 自助列凭证（tokens 列/删走已登录用户自己，角色门控在服务端）。
func (h *Handler) fwdTokens(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	tokens, err := c.ListTokens(r.Context())
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

func (h *Handler) fwdTokenDelete(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	if err := c.DeleteToken(r.Context(), r.PathValue("name")); err != nil {
		writeServerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fwdUserTokens 管理员列指定用户凭证。
func (h *Handler) fwdUserTokens(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	tokens, err := c.ListUserTokens(r.Context(), r.PathValue("name"))
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

func (h *Handler) fwdUserTokenDelete(w http.ResponseWriter, r *http.Request) {
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	if err := c.DeleteUserToken(r.Context(), r.PathValue("name"), r.PathValue("token")); err != nil {
		writeServerErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeNotConfigured(w http.ResponseWriter, err error) {
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusConflict, map[string]string{"error": "not_configured"})
}
