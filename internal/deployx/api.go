package deployx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server 是 okdeploy 的本地 HTTP 服务（浏览器 UI 的后端，只监听 127.0.0.1）。
type Server struct {
	token string
	hub   *LogHub

	mu       sync.Mutex
	ex       Executor          // 已连接的 SSH 会话（nil=未连接）
	running  bool              // 任务 single-flight
	lastVars map[string]string // 最近一次成功任务的 Vars
}

func NewServer(token string) *Server {
	return &Server{token: token, hub: NewLogHub()}
}

// Handler 注册静态资源与 API（Go 1.22 方法路由，同 internal/gui 模式）。
// 鉴权：API 一律 X-Ok-Token 头；/api/logs/stream 与 /api/backup 额外接受
// ?token=（浏览器 EventSource 与文件下载无法设请求头，仅绑定 127.0.0.1 可接受）。
func (s *Server) Handler(webFS fs.FS) http.Handler {
	mux := http.NewServeMux()
	for _, p := range []string{"/", "/index.html", "/app.js", "/style.css"} {
		p := p
		mux.HandleFunc("GET "+p, func(w http.ResponseWriter, r *http.Request) {
			name := strings.TrimPrefix(p, "/")
			if name == "" {
				name = "index.html"
			}
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, webFS, name)
		})
	}
	api := func(pattern string, fn http.HandlerFunc) {
		mux.HandleFunc(pattern, s.withAuth(fn))
	}
	api("POST /api/connect", s.apiConnect)
	api("POST /api/disconnect", s.apiDisconnect)
	api("GET /api/probe", s.apiProbe)
	api("POST /api/smoke-external", s.apiSmokeExternal)
	api("POST /api/deploy", s.apiDeploy)
	api("GET /api/deploy/result", s.apiDeployResult)
	api("GET /api/status", s.apiStatus)
	api("GET /api/remote-logs", s.apiRemoteLogs)
	api("POST /api/reset-root", s.apiResetRoot)
	api("POST /api/ls", s.apiLs)
	api("POST /api/upgrade", s.apiUpgrade)
	api("POST /api/uninstall", s.apiUninstall)
	api("POST /api/restore", s.apiRestore)
	mux.HandleFunc("GET /api/logs/stream", s.withAuthQuery(s.apiLogStream))
	mux.HandleFunc("GET /api/backup", s.withAuthQuery(s.apiBackup))
	return mux
}

func (s *Server) withAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ok-Token") != s.token {
			writeErr(w, http.StatusUnauthorized, "未授权")
			return
		}
		fn(w, r)
	}
}

func (s *Server) withAuthQuery(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ok-Token") != s.token && r.URL.Query().Get("token") != s.token {
			writeErr(w, http.StatusUnauthorized, "未授权")
			return
		}
		fn(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// session 取当前会话；未连接时写 412 并返回 nil。
func (s *Server) session(w http.ResponseWriter) Executor {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ex == nil {
		writeErr(w, http.StatusPreconditionFailed, "尚未连接服务器")
		return nil
	}
	return s.ex
}

// startTask single-flight 起后台任务；成功结束后 Vars 存 lastVars。
func (s *Server) startTask(w http.ResponseWriter, t Task) bool {
	s.mu.Lock()
	if s.ex == nil {
		s.mu.Unlock()
		writeErr(w, http.StatusPreconditionFailed, "尚未连接服务器")
		return false
	}
	if s.running {
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "已有任务在执行，请等待完成")
		return false
	}
	s.running = true
	ex := s.ex
	s.mu.Unlock()
	go func() {
		defer func() { s.mu.Lock(); s.running = false; s.mu.Unlock() }()
		env := &Env{Ex: ex, Hub: s.hub, Vars: map[string]string{}}
		if err := t.Execute(context.Background(), env); err != nil {
			return
		}
		s.mu.Lock()
		s.lastVars = env.Vars
		s.mu.Unlock()
	}()
	writeJSON(w, map[string]any{"started": true})
	return true
}

type connectReq struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	KeyPath  string `json:"key_path"`
}

func (s *Server) apiConnect(w http.ResponseWriter, r *http.Request) {
	var req connectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Host == "" || req.User == "" {
		writeErr(w, http.StatusBadRequest, "地址与用户名为必填")
		return
	}
	if req.Port == 0 {
		req.Port = 22
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ex, err := DialSSH(ctx, req.Host, req.Port, req.User, req.Password, req.KeyPath)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.mu.Lock()
	if s.ex != nil {
		s.ex.Close()
	}
	s.ex = ex
	s.mu.Unlock()
	s.hub.Publish("", "ok", fmt.Sprintf("已连接 %s@%s:%d", req.User, req.Host, req.Port))
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) apiDisconnect(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.ex != nil {
		s.ex.Close()
		s.ex = nil
	}
	s.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) apiProbe(w http.ResponseWriter, r *http.Request) {
	ex := s.session(w)
	if ex == nil {
		return
	}
	res, err := Probe(r.Context(), ex)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, res)
}

type smokeReq struct {
	GiteaURL   string `json:"gitea_url"`
	AdminToken string `json:"admin_token"`
}

func (s *Server) apiSmokeExternal(w http.ResponseWriter, r *http.Request) {
	ex := s.session(w)
	if ex == nil {
		return
	}
	var req smokeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	env := &Env{Ex: ex, Hub: s.hub, Vars: map[string]string{}}
	if err := SmokeExternalGitea(r.Context(), env, req.GiteaURL, req.AdminToken); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

type deployReq struct {
	DeploySpec
	ChecklistAck bool `json:"checklist_ack"`
}

func (s *Server) apiDeploy(w http.ResponseWriter, r *http.Request) {
	var req deployReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Mode == "external" && !req.ChecklistAck {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":          "接入已有 Gitea 需先逐项确认治理配置",
			"need_checklist": GovernanceChecklist(),
		})
		return
	}
	task, err := BuildDeployTask(req.DeploySpec)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.startTask(w, task)
}

func (s *Server) apiDeployResult(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	pw := ""
	if s.lastVars != nil {
		pw = s.lastVars["root_password"]
	}
	s.mu.Unlock()
	if pw == "" {
		writeErr(w, http.StatusNotFound, "暂无部署结果")
		return
	}
	writeJSON(w, map[string]string{"root_password": pw})
}

func (s *Server) apiStatus(w http.ResponseWriter, r *http.Request) {
	ex := s.session(w)
	if ex == nil {
		return
	}
	dir := r.URL.Query().Get("dir")
	if err := ValidateDir(dir); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := QueryStatus(r.Context(), ex, dir)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, st)
}

// apiLs 远端目录浏览：列出 path 下的子目录（部署表单的目录选择器用）。
// path 为空时列 $HOME；path 过 ValidateDir（允许 ~ 与 $HOME）。
func (s *Server) apiLs(w http.ResponseWriter, r *http.Request) {
	ex := s.session(w)
	if ex == nil {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	path := req.Path
	if path == "" {
		path = "$HOME"
	}
	if err := ValidateDir(path); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out, _ := runQuiet(r.Context(), ex, "ls -1d "+path+"/*/ 2>/dev/null || true")
	var dirs []string
	for _, ln := range strings.Split(out, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			dirs = append(dirs, ln)
		}
	}
	writeJSON(w, map[string]any{"path": path, "dirs": dirs})
}

// apiRemoteLogs 拉取远端容器日志（排障查看）。
func (s *Server) apiRemoteLogs(w http.ResponseWriter, r *http.Request) {
	ex := s.session(w)
	if ex == nil {
		return
	}
	dir := r.URL.Query().Get("dir")
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	logs, err := QueryRemoteLogs(r.Context(), ex, dir, tail)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]string{"logs": logs})
}

// apiResetRoot 重置 root 密码（确认词 RESET）；完成后经 /api/deploy/result 取新密码。
func (s *Server) apiResetRoot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir     string `json:"dir"`
		Confirm string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || ValidateDir(req.Dir) != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Confirm != "RESET" {
		writeErr(w, http.StatusBadRequest, "请输入 RESET 确认重置")
		return
	}
	s.startTask(w, BuildResetRootTask(req.Dir))
}

type upgradeReq struct {
	Dir string `json:"dir"`
	Tag string `json:"tag"`
}

func (s *Server) apiUpgrade(w http.ResponseWriter, r *http.Request) {
	var req upgradeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Tag == "" || ValidateDir(req.Dir) != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误（需合法 dir 与 tag）")
		return
	}
	s.startTask(w, BuildUpgradeTask(req.Dir, req.Tag))
}

type uninstallReq struct {
	Dir        string `json:"dir"`
	Confirm    string `json:"confirm"`
	DeleteData bool   `json:"delete_data"`
}

func (s *Server) apiUninstall(w http.ResponseWriter, r *http.Request) {
	var req uninstallReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || ValidateDir(req.Dir) != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Confirm != "DELETE" {
		writeErr(w, http.StatusBadRequest, "请输入 DELETE 确认卸载")
		return
	}
	s.startTask(w, BuildUninstallTask(req.Dir, req.DeleteData))
}

// apiRestore 恢复备份（确认词 RESTORE，与卸载 DELETE/重置 RESET 同级）。
// 校验顺序：确认词 → dir → 读文件，避免为大文件白付读取代价（台账 #19）。
func (s *Server) apiRestore(w http.ResponseWriter, r *http.Request) {
	if s.session(w) == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30) // 1GB 上限
	if r.FormValue("confirm") != "RESTORE" {
		writeErr(w, http.StatusBadRequest, "请输入 RESTORE 确认恢复")
		return
	}
	dir := r.FormValue("dir")
	if err := ValidateDir(dir); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺备份文件（file 字段，≤1GB）")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取备份文件失败："+err.Error())
		return
	}
	s.startTask(w, BuildRestoreTask(dir, data))
}

// apiBackup 停容器 → tar 流直接写响应（浏览器下载）→ 起容器（失败也要起）。
func (s *Server) apiBackup(w http.ResponseWriter, r *http.Request) {
	ex := s.session(w)
	if ex == nil {
		return
	}
	dir := r.URL.Query().Get("dir")
	if err := ValidateDir(dir); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "已有任务在执行，请等待完成")
		return
	}
	s.running = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.running = false; s.mu.Unlock() }()

	env := &Env{Ex: ex, Hub: s.hub, Vars: map[string]string{}}
	if err := BuildBackupStopTask(dir).Execute(r.Context(), env); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	// 无论下载成败，最后都要把服务起回来
	defer func() {
		env.Vars["ok_port"] = ""
		_ = BuildStartTask(dir).Execute(context.Background(), env)
	}()

	fname := "okbackup-" + time.Now().Format("20060102-150405") + ".tar"
	w.Header().Set("Content-Disposition", "attachment; filename="+fname)
	w.Header().Set("Content-Type", "application/x-tar")
	ctx, cancel := context.WithTimeout(context.Background(), BackupTimeout)
	defer cancel()
	var last int64
	err := ex.Download(ctx, BackupCmd(dir), w, func(n int64) {
		if n-last >= 8<<20 { // 每 8MB 报一次进度
			last = n
			s.hub.Publish("备份", "info", fmt.Sprintf("已传输 %.1f MB", float64(n)/1048576))
		}
	})
	if err != nil {
		// 响应头已发出，无法改状态码；日志里标错，前端据此提示文件可能不完整
		s.hub.Publish("备份", "err", "备份流传输中断："+err.Error()+"（下载的文件不可用）")
	}
}

// apiLogStream SSE：先补历史，再实时转发。
func (s *Server) apiLogStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "不支持流式响应", http.StatusInternalServerError)
		return
	}
	for _, ev := range s.hub.History() {
		writeSSE(w, ev)
	}
	fl.Flush()
	ch, unsub := s.hub.Subscribe()
	defer unsub()
	for {
		select {
		case ev := <-ch:
			writeSSE(w, ev)
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSE(w io.Writer, ev LogEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}
