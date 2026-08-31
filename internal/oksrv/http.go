// http.go：okserver 管理面 HTTP API（设计文档 §9.3）。
// 鉴权 Bearer；无 cookie/静态页 → CSRF 结构免疫，不做 Origin/Host 白名单（LAN 服务）。
package oksrv

import (
	"net/http"
	"regexp"
	"strconv"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)
var projectRe = regexp.MustCompile(`^[a-zA-Z0-9_-][a-zA-Z0-9_.-]{0,63}$`)

type server struct {
	st      *Store
	backend GitBackend
	version string
	limiter *LoginLimiter
}

// NewMux 返回完整管理面 handler。
func NewMux(st *Store, backend GitBackend, version string) http.Handler {
	s := &server{st: st, backend: backend, version: version, limiter: NewLoginLimiter()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/meta", s.apiMeta)
	mux.HandleFunc("POST /api/v1/login", s.apiLogin)
	// 强制改密白名单：me 与 change-password 不套 gate，其余已认证端点全拦
	mux.HandleFunc("GET /api/v1/me", s.auth(s.apiMe))
	mux.HandleFunc("POST /api/v1/change-password", s.auth(s.apiChangePassword))
	mux.HandleFunc("POST /api/v1/repos/personal", s.auth(s.gate(s.apiPersonalRepo)))
	mux.HandleFunc("POST /api/v1/git-token", s.auth(s.gate(s.apiGitToken)))
	mux.HandleFunc("GET /api/v1/users", s.auth(s.gate(s.admin(s.apiUsers))))
	mux.HandleFunc("POST /api/v1/users", s.auth(s.gate(s.admin(s.apiUserCreate))))
	mux.HandleFunc("POST /api/v1/users/{name}/reset-password", s.auth(s.gate(s.admin(s.apiUserResetPassword))))
	mux.HandleFunc("DELETE /api/v1/users/{name}", s.auth(s.gate(s.admin(s.apiUserDelete))))
	mux.HandleFunc("POST /api/v1/users/{name}/disable", s.auth(s.gate(s.admin(s.apiUserDisable(true)))))
	mux.HandleFunc("POST /api/v1/users/{name}/enable", s.auth(s.gate(s.admin(s.apiUserDisable(false)))))
	mux.HandleFunc("GET /api/v1/orgs", s.auth(s.gate(s.admin(s.apiOrgs))))
	mux.HandleFunc("POST /api/v1/orgs", s.auth(s.gate(s.admin(s.apiOrgCreate))))
	mux.HandleFunc("POST /api/v1/orgs/{org}/members", s.auth(s.gate(s.admin(s.apiOrgMemberAdd))))
	mux.HandleFunc("DELETE /api/v1/orgs/{org}/members/{username}", s.auth(s.gate(s.admin(s.apiOrgMemberRemove))))
	mux.HandleFunc("POST /api/v1/repos/team", s.auth(s.gate(s.admin(s.apiTeamRepo))))
	mux.HandleFunc("GET /api/v1/repos", s.auth(s.gate(s.admin(s.apiRepos))))
	mux.HandleFunc("GET /api/v1/audit", s.auth(s.gate(s.admin(s.apiAudit))))
	return mux
}

// auth：Bearer → User；失败 401。
func (s *server) auth(fn func(http.ResponseWriter, *http.Request, *User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := ""
		if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
			tok = h[7:]
		}
		u := s.st.SessionUser(tok)
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "未认证")
			return
		}
		fn(w, r, u)
	}
}

// admin：root/admin 门控。
func (s *server) admin(fn func(http.ResponseWriter, *http.Request, *User)) func(http.ResponseWriter, *http.Request, *User) {
	return func(w http.ResponseWriter, r *http.Request, u *User) {
		if u.Role != "root" && u.Role != "admin" {
			writeErr(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		fn(w, r, u)
	}
}

// gate：强制改密拦截——带 must_change_password 标记的会话只放行白名单端点
// （me/change-password 注册时不套 gate），其余一律 403，客户端据此弹强制改密框。
func (s *server) gate(fn func(http.ResponseWriter, *http.Request, *User)) func(http.ResponseWriter, *http.Request, *User) {
	return func(w http.ResponseWriter, r *http.Request, u *User) {
		if u.MustChangePassword {
			writeErr(w, http.StatusForbidden, "must_change_password")
			return
		}
		fn(w, r, u)
	}
}

// backendErr：GitBackend 错误一律 502（ErrBackendDown 的消息本身就是"git 后端不可用"）。
func backendErr(w http.ResponseWriter, err error) {
	writeErr(w, http.StatusBadGateway, err.Error())
}

// backendType 报告后端实现名（meta 端点用）。
func backendType(b GitBackend) string {
	switch b.(type) {
	case *FakeBackend:
		return "fake"
	case *GiteaBackend:
		return "gitea"
	default:
		return "unknown"
	}
}

// gitRepoJSON 是建仓响应的 repo 形状（{owner, name, clone_url}）。
func gitRepoJSON(r GitRepo) map[string]any {
	return map[string]any{"owner": r.Owner, "name": r.Name, "clone_url": r.CloneURL}
}

// repoJSON 是仓库列表/me 的 repo 形状（含登记元数据）。
func repoJSON(r Repo) map[string]any {
	return map[string]any{
		"layer": r.Layer, "owner": r.Owner, "project": r.Project,
		"clone_url": r.CloneURL, "created_by": r.CreatedBy, "created_at": r.CreatedAt,
	}
}

// ---------- 公开端点 ----------

func (s *server) apiMeta(w http.ResponseWriter, r *http.Request) {
	_, err := s.backend.Ping(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     s.version,
		"initialized": s.st.HasRoot(),
		"git_backend": map[string]any{"type": backendType(s.backend), "ok": err == nil},
	})
}

func (s *server) apiLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.limiter.Allow(in.Username) {
		writeErr(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
		return
	}
	u := s.st.VerifyLogin(in.Username, in.Password)
	if u == nil {
		s.limiter.Fail(in.Username)
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	tok, err := s.st.CreateSession(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.limiter.Reset(in.Username)
	writeJSON(w, http.StatusOK, map[string]any{
		"token": tok,
		"user":  map[string]any{"name": u.Username, "role": u.Role, "must_change_password": u.MustChangePassword},
	})
}

// ---------- 成员端点 ----------

func (s *server) apiMe(w http.ResponseWriter, _ *http.Request, u *User) {
	orgs := s.st.ListUserOrgs(u.Username)
	if orgs == nil {
		orgs = []string{}
	}
	repos := []map[string]any{}
	for _, rp := range s.st.ListReposByOwner("personal", u.Username) {
		repos = append(repos, repoJSON(rp))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": u.Username, "role": u.Role, "orgs": orgs, "repos": repos,
		"must_change_password": u.MustChangePassword,
	})
}

// apiChangePassword 自助改密：已认证用户凭旧密码换新密码。成功后清强制改密标记、
// 踢掉除当前会话外的全部会话（当前会话保留，客户端无需重登）。
// 旧密码失败计入登录限流（与 apiLogin 共用同一 LoginLimiter，防在线爆破）。
func (s *server) apiChangePassword(w http.ResponseWriter, r *http.Request, u *User) {
	var in struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.limiter.Allow(u.Username) {
		writeErr(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
		return
	}
	if s.st.VerifyLogin(u.Username, in.OldPassword) == nil {
		s.limiter.Fail(u.Username)
		writeErr(w, http.StatusUnauthorized, "旧密码错误")
		return
	}
	if len(in.NewPassword) < 8 {
		writeErr(w, http.StatusBadRequest, "密码至少 8 位")
		return
	}
	if in.NewPassword == in.OldPassword {
		writeErr(w, http.StatusBadRequest, "新密码不能与旧密码相同")
		return
	}
	hash, err := HashPassword(in.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.SetUserPasswordHash(u.Username, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.SetMustChangePassword(u.Username, false); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.DeleteUserSessionsExcept(u.ID, bearerTokenHash(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.limiter.Reset(u.Username)
	s.st.Audit(u.Username, "change-password", u.Username, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiPersonalRepo 建个人仓 ok-<project>（幂等：已存在返回现有记录且 git_token 空串，
// token 只在首次建仓下发；丢失走 reset-password 联动重发——v1.1）。
func (s *server) apiPersonalRepo(w http.ResponseWriter, r *http.Request, u *User) {
	var in struct {
		Project string `json:"project"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !projectRe.MatchString(in.Project) {
		writeErr(w, http.StatusBadRequest, "项目名非法（^[a-zA-Z0-9_-][a-zA-Z0-9_.-]{0,63}$）")
		return
	}
	if existing := s.st.GetRepo("personal", u.Username, in.Project); existing != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"repo":      map[string]any{"owner": existing.Owner, "name": "ok-" + existing.Project, "clone_url": existing.CloneURL},
			"git_token": "",
		})
		return
	}
	repo, err := s.backend.CreatePersonalRepo(r.Context(), u.Username, "ok-"+in.Project)
	if err != nil {
		backendErr(w, err)
		return
	}
	if err := s.st.UpsertRepo(Repo{
		Layer: "personal", Owner: u.Username, Project: in.Project,
		CloneURL: repo.CloneURL, CreatedBy: u.Username,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 审计随事实落库：仓已建成，与下文 token 下发成败无关。
	s.st.Audit(u.Username, "create-personal-repo", u.Username+"/"+in.Project, repo.CloneURL)
	// token 失败不阻断建仓结果（仓已登记，幂等重试不再补发——同上 v1.1 语义）。
	token, err := s.backend.CreateUserToken(r.Context(), u.Username, "ok-sync-"+in.Project)
	if err != nil {
		backendErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repo": gitRepoJSON(repo), "git_token": token})
}

// apiGitToken 自助重发 git token：删旧建新（规避同名撞名），明文返回一次。
// 解决新机器拉取已有仓时拿不到凭据的断链（token 原本只在建仓首发）。
func (s *server) apiGitToken(w http.ResponseWriter, r *http.Request, u *User) {
	const tokenName = "ok-sync-reissue"
	_ = s.backend.DeleteUserToken(r.Context(), u.Username, tokenName) // 尽力而为，以建为准
	token, err := s.backend.CreateUserToken(r.Context(), u.Username, tokenName)
	if err != nil {
		backendErr(w, err)
		return
	}
	s.st.Audit(u.Username, "reissue-git-token", u.Username, "")
	writeJSON(w, http.StatusOK, map[string]string{"git_token": token})
}

// ---------- 用户管理 ----------

func (s *server) apiUsers(w http.ResponseWriter, _ *http.Request, _ *User) {
	users := []map[string]any{}
	for _, x := range s.st.ListUsers() {
		users = append(users, map[string]any{
			"name": x.Username, "role": x.Role, "disabled": x.Disabled, "created_at": x.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// apiUserCreate 建用户（一次性返回明文初始密码 + git token）；
// 可选 role:"admin" 仅 root 可授（admin 只能建 member）。
// 可选 password：管理员自选初始密码（≥8 位）；留空则服务端生成一次性随机密码。
func (s *server) apiUserCreate(w http.ResponseWriter, r *http.Request, u *User) {
	var in struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !usernameRe.MatchString(in.Username) {
		writeErr(w, http.StatusBadRequest, "用户名非法（^[a-z0-9_-]{2,32}$）")
		return
	}
	role := "member"
	switch in.Role {
	case "", "member":
	case "admin":
		if u.Role != "root" {
			writeErr(w, http.StatusForbidden, "仅 root 可授予 admin 角色")
			return
		}
		role = "admin"
	default:
		writeErr(w, http.StatusBadRequest, "非法角色: "+in.Role)
		return
	}
	if s.st.GetUser(in.Username) != nil {
		writeErr(w, http.StatusConflict, "用户已存在")
		return
	}
	pw := in.Password
	if pw == "" {
		pw = GenerateSecret(16) // 16 字节 → 22 字符
	} else if len(pw) < 8 {
		writeErr(w, http.StatusBadRequest, "密码至少 8 位")
		return
	}
	hash, err := HashPassword(pw)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 顺序：Gitea 用户 → Gitea token → 本地用户（最后）。任一步失败回滚 Gitea 侧，
	// 本地用户最后建——保证"用户已存在"闸门（本地库）前的失败不留半截状态，重试可收敛。
	if err := s.backend.CreateUser(r.Context(), in.Username, pw); err != nil {
		backendErr(w, err)
		return
	}
	token, err := s.backend.CreateUserToken(r.Context(), in.Username, "ok-sync")
	if err != nil {
		if rbErr := s.backend.DeleteUser(r.Context(), in.Username); rbErr != nil {
			s.st.Audit(u.Username, "create-user-rollback-failed", in.Username, rbErr.Error())
		}
		backendErr(w, err)
		return
	}
	if _, err := s.st.CreateUser(in.Username, role, hash); err != nil {
		if rbErr := s.backend.DeleteUser(r.Context(), in.Username); rbErr != nil {
			s.st.Audit(u.Username, "create-user-rollback-failed", in.Username, rbErr.Error())
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 他人代设的初始密码（自选或随机）一律强制首登改密
	if err := s.st.SetMustChangePassword(in.Username, true); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.st.Audit(u.Username, "create-user", in.Username, "role="+role)
	writeJSON(w, http.StatusOK, map[string]any{
		"username": in.Username, "password": pw, "git_token": token,
	})
}

// apiUserResetPassword 重置密码（一次性明文；只改 oksrv 侧——Gitea 密码用户不持有，
// 不需要同步）。root 保护：admin 不可重置 root。
func (s *server) apiUserResetPassword(w http.ResponseWriter, r *http.Request, u *User) {
	name := r.PathValue("name")
	target := s.st.GetUser(name)
	if target == nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	if target.Role == "root" && u.Role != "root" {
		writeErr(w, http.StatusForbidden, "root 不可被重置")
		return
	}
	pw := GenerateSecret(16)
	hash, err := HashPassword(pw)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.SetUserPasswordHash(name, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 重置 = 强制下次登录改密 + 踢掉该用户全部旧会话
	if err := s.st.SetMustChangePassword(name, true); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.DeleteUserSessionsExcept(target.ID, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.st.Audit(u.Username, "reset-password", name, "")
	writeJSON(w, http.StatusOK, map[string]any{"password": pw})
}

// apiUserDisable 禁用/启用账号（root 不可禁用——store 层另有最后防线）。
// 双写顺序 fail-closed：先断 git 侧再写本库；本库失败残留"能登录不能推"，重试可自愈。
func (s *server) apiUserDisable(disable bool) func(http.ResponseWriter, *http.Request, *User) {
	return func(w http.ResponseWriter, r *http.Request, u *User) {
		name := r.PathValue("name")
		target := s.st.GetUser(name)
		if target == nil {
			writeErr(w, http.StatusNotFound, "用户不存在")
			return
		}
		if disable && target.Role == "root" {
			writeErr(w, http.StatusForbidden, "root 不可禁用")
			return
		}
		if err := s.backend.SetUserActive(r.Context(), name, !disable); err != nil {
			backendErr(w, err)
			return
		}
		if err := s.st.SetUserDisabled(name, disable); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		action := "enable-user"
		if disable {
			action = "disable-user"
		}
		s.st.Audit(u.Username, action, name, "")
		w.WriteHeader(http.StatusNoContent)
	}
}

// apiUserDelete 删账号：root 不可删；admin 只能删 member（对齐建 admin 的角色门控）。
// 双写顺序 fail-closed 同 disable：先删 Gitea 侧再删本库；本库失败残留可重试删除收敛。
// Gitea 侧若因用户持有仓库等拒绝删除，错误原样透传给管理员。
func (s *server) apiUserDelete(w http.ResponseWriter, r *http.Request, u *User) {
	name := r.PathValue("name")
	target := s.st.GetUser(name)
	if target == nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	if target.Role == "root" {
		writeErr(w, http.StatusForbidden, "root 不可删除")
		return
	}
	if target.Role == "admin" && u.Role != "root" {
		writeErr(w, http.StatusForbidden, "仅 root 可删除 admin")
		return
	}
	if err := s.backend.DeleteUser(r.Context(), name); err != nil {
		backendErr(w, err)
		return
	}
	if err := s.st.DeleteUser(name); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.st.Audit(u.Username, "delete-user", name, "")
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 组织管理 ----------

func (s *server) apiOrgs(w http.ResponseWriter, _ *http.Request, _ *User) {
	orgs := []map[string]any{}
	for _, o := range s.st.ListOrgs() {
		members := []map[string]any{}
		for _, m := range s.st.ListOrgMembers(o.Name) {
			members = append(members, map[string]any{"username": m.Username, "role": m.Role})
		}
		orgs = append(orgs, map[string]any{"name": o.Name, "description": o.Desc, "members": members})
	}
	writeJSON(w, http.StatusOK, map[string]any{"orgs": orgs})
}

// apiOrgCreate 建组织：Gitea 侧名为 ok-<name>，本库登记原名。
func (s *server) apiOrgCreate(w http.ResponseWriter, r *http.Request, u *User) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !usernameRe.MatchString(in.Name) { // Gitea 组织名规则同用户名
		writeErr(w, http.StatusBadRequest, "组织名非法（^[a-z0-9_-]{2,32}$）")
		return
	}
	if s.st.OrgExists(in.Name) {
		writeErr(w, http.StatusConflict, "组织已存在")
		return
	}
	if err := s.backend.CreateOrg(r.Context(), "ok-"+in.Name, in.Name); err != nil {
		backendErr(w, err)
		return
	}
	if err := s.st.CreateOrg(in.Name, in.Description); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.st.Audit(u.Username, "create-org", in.Name, in.Description)
	writeJSON(w, http.StatusCreated, map[string]any{"name": in.Name, "description": in.Description})
}

// apiOrgMemberAdd 加组织成员（重复添加幂等，兼作改角色——store 层 INSERT OR REPLACE）。
func (s *server) apiOrgMemberAdd(w http.ResponseWriter, r *http.Request, u *User) {
	org := r.PathValue("org")
	var in struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.st.OrgExists(org) {
		writeErr(w, http.StatusNotFound, "组织不存在")
		return
	}
	if s.st.GetUser(in.Username) == nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	role := in.Role
	if role == "" {
		role = "member"
	}
	if role != "member" && role != "owner" {
		writeErr(w, http.StatusBadRequest, "非法组织角色: "+role)
		return
	}
	if err := s.backend.AddOrgMember(r.Context(), "ok-"+org, in.Username); err != nil {
		backendErr(w, err)
		return
	}
	if err := s.st.AddOrgMember(org, in.Username, role); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.st.Audit(u.Username, "add-org-member", org+"/"+in.Username, "role="+role)
	w.WriteHeader(http.StatusNoContent)
}

// apiOrgMemberRemove 移除组织成员；本库非成员先 404（避免无意义的后端调用）。
func (s *server) apiOrgMemberRemove(w http.ResponseWriter, r *http.Request, u *User) {
	org := r.PathValue("org")
	username := r.PathValue("username")
	if !s.st.OrgExists(org) {
		writeErr(w, http.StatusNotFound, "组织不存在")
		return
	}
	member := false
	for _, m := range s.st.ListOrgMembers(org) {
		if m.Username == username {
			member = true
			break
		}
	}
	if !member {
		writeErr(w, http.StatusNotFound, "用户不是组织成员")
		return
	}
	if err := s.backend.RemoveOrgMember(r.Context(), "ok-"+org, username); err != nil {
		backendErr(w, err)
		return
	}
	if err := s.st.RemoveOrgMember(org, username); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.st.Audit(u.Username, "remove-org-member", org+"/"+username, "")
	w.WriteHeader(http.StatusNoContent)
}

// ---------- 仓库与审计 ----------

// apiTeamRepo 建团队仓：org 映射 ok-<org>，仓名即项目名（重复建 409）。
func (s *server) apiTeamRepo(w http.ResponseWriter, r *http.Request, u *User) {
	var in struct {
		Org     string `json:"org"`
		Project string `json:"project"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !projectRe.MatchString(in.Project) {
		writeErr(w, http.StatusBadRequest, "项目名非法（^[a-zA-Z0-9_-][a-zA-Z0-9_.-]{0,63}$）")
		return
	}
	if !s.st.OrgExists(in.Org) {
		writeErr(w, http.StatusNotFound, "组织不存在")
		return
	}
	owner := "ok-" + in.Org
	if s.st.GetRepo("team", owner, in.Project) != nil {
		writeErr(w, http.StatusConflict, "仓库已存在")
		return
	}
	repo, err := s.backend.CreateOrgRepo(r.Context(), owner, in.Project)
	if err != nil {
		backendErr(w, err)
		return
	}
	if err := s.st.UpsertRepo(Repo{
		Layer: "team", Owner: owner, Project: in.Project,
		CloneURL: repo.CloneURL, CreatedBy: u.Username,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.st.Audit(u.Username, "create-team-repo", owner+"/"+in.Project, repo.CloneURL)
	writeJSON(w, http.StatusOK, map[string]any{"repo": gitRepoJSON(repo)})
}

func (s *server) apiRepos(w http.ResponseWriter, _ *http.Request, _ *User) {
	repos := []map[string]any{}
	for _, rp := range s.st.ListRepos() {
		repos = append(repos, repoJSON(rp))
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

func (s *server) apiAudit(w http.ResponseWriter, r *http.Request, _ *User) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	entries := []map[string]any{}
	for _, e := range s.st.ListAudit(limit, offset) {
		entries = append(entries, map[string]any{
			"id": e.ID, "actor": e.Actor, "action": e.Action, "target": e.Target,
			"detail": e.Detail, "created_at": e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}
