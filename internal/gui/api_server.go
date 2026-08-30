// api_server.go：GUI 服务器页的本地端点——配置读写 + 连接测试 + 登录 + 转发 okserver。
// SSRF 裁决（设计文档）：服务器地址来自用户自己的配置/输入，本地 GUI + X-Ok-Token
// 鉴权，目标是用户要连的 LAN 服务器——不做回环限制（与 ollama 探测端点不同）。
package gui

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"openknowledge/internal/config"
	"openknowledge/internal/serverx"
	"openknowledge/internal/syncx"
)

func (h *Handler) registerServerAPI(api func(string, http.HandlerFunc)) {
	api("GET /api/server/config", h.apiServerConfigGet)
	api("PUT /api/server/config", h.apiServerConfigPut)
	api("POST /api/server/test", h.apiServerTest)
	api("POST /api/server/login", h.apiServerLogin)
	api("POST /api/server/logout", h.apiServerLogout)
	api("GET /api/server/me", h.apiServerMe)
	api("POST /api/server/repos", h.apiServerRepos)
	// 管理类透传（角色门控在服务端）
	api("GET /api/server/users", h.fwdUsers)
	api("POST /api/server/users", h.fwdUserCreate)
	api("POST /api/server/users/{name}/reset-password", h.fwdUserResetPassword)
	api("POST /api/server/users/{name}/disable", h.fwdUserDisable(true))
	api("POST /api/server/users/{name}/enable", h.fwdUserDisable(false))
	api("GET /api/server/orgs", h.fwdOrgs)
	api("POST /api/server/orgs", h.fwdOrgCreate)
	api("POST /api/server/orgs/{org}/members", h.fwdOrgMemberAdd)
	api("DELETE /api/server/orgs/{org}/members/{username}", h.fwdOrgMemberRemove)
	api("POST /api/server/repos/team", h.fwdTeamRepo)
	api("GET /api/server/repos-all", h.fwdReposAll)
	api("GET /api/server/audit", h.fwdAudit)
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

// apiServerRepos 建仓一条龙：provision → 写凭据 → init/SetRemote → SetSync → 首次同步。
func (h *Handler) apiServerRepos(w http.ResponseWriter, r *http.Request) {
	var req serverReposRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	c, cfgServer, err := h.serverClient()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not_configured"})
		return
	}
	pr, err := c.ProvisionPersonalRepo(r.Context(), req.Project)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	// 凭据：优先系统 credential helper；无 helper（或本次未发 token）回退 URL 内嵌
	remote := pr.Repo.CloneURL
	credNote := ""
	if pr.GitToken != "" {
		if cerr := syncx.StoreCredential(st.Root, pr.Repo.CloneURL, cfgServer.Username, pr.GitToken); cerr != nil {
			if errors.Is(cerr, syncx.ErrNoCredentialHelper) {
				remote = syncx.CredentialURLWithAuth(pr.Repo.CloneURL, cfgServer.Username, pr.GitToken)
				credNote = "（凭据已内嵌 URL：本机未配置 git credential helper）"
			} else {
				writeErr(w, http.StatusInternalServerError, "写入 git 凭据失败："+cerr.Error())
				return
			}
		}
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
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	c, _, err := h.serverClient()
	if err != nil || c == nil {
		writeNotConfigured(w, err)
		return
	}
	u, err := c.CreateUser(r.Context(), req.Username, req.Role)
	if err != nil {
		writeServerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
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

func writeNotConfigured(w http.ResponseWriter, err error) {
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusConflict, map[string]string{"error": "not_configured"})
}
