// api_sync.go：个人多端同步的 GUI 端点（设计文档 §11.4）。
// 全部走 syncx single-flight；sync-conflict.json 由 syncx 独占写，这里只读。
package gui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/llmx"
	"openknowledge/internal/store"
	"openknowledge/internal/syncx"
)

func (h *Handler) registerSyncAPI(api func(string, http.HandlerFunc)) {
	api("POST /api/project/sync", h.apiProjectSync)
	api("GET /api/project/sync/status", h.apiSyncStatus)
	api("GET /api/project/sync/conflict-file", h.apiSyncConflictFile)
	api("POST /api/project/sync/resolve", h.apiSyncResolve)
	api("POST /api/project/sync/finish", h.apiSyncFinish)
	api("POST /api/project/sync/abort", h.apiSyncAbort)
	api("POST /api/project/sync/ai-merge", h.apiSyncAIMerge)
}

type syncRequest struct {
	Project string `json:"project"`
	Remote  string `json:"remote"`
	File    string `json:"file"`
	Action  string `json:"action"`
	Content string `json:"content"`
}

// validSyncFileParam 校验冲突文件参数：拒绝空串、".." 穿越与绝对路径（conflict-file/resolve 共用）。
// 纵深加固：Clean 后首段为 .git（大小写不敏感，正反斜杠都算）同样拒绝。
func validSyncFileParam(file string) bool {
	if file == "" || strings.Contains(file, "..") || filepath.IsAbs(file) {
		return false
	}
	first := strings.SplitN(filepath.ToSlash(filepath.Clean(file)), "/", 2)[0]
	return !strings.EqualFold(first, ".git")
}

func syncCommitMsg() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("sync: %s %s", host, time.Now().Format(time.RFC3339))
}

// hasKnowledgeContent 判定"knowledge/ 下有 .md"（三情形之二的分界，设计文档 §14）。
func hasKnowledgeContent(st *store.Store) bool {
	entries, _ := os.ReadDir(st.KnowledgeDir())
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			return true
		}
	}
	return false
}

// knowledgeDirty：knowledge/ 下最新 .md 的 mtime 晚于 lastSync = 有未同步变更。
// 纯文件系统检查（一次 ReadDir），不起 git 子进程；LastSync 零值（从未同步）且有内容即为 dirty。
func knowledgeDirty(st *store.Store, lastSync time.Time) bool {
	entries, err := os.ReadDir(st.KnowledgeDir())
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().After(lastSync) {
			return true
		}
	}
	return false
}

// describeOutcome 人话状态（与 CLI 文案同族，供 toast 展示）。
func describeOutcome(o syncx.Outcome) string {
	switch {
	case o.NotRepo:
		return "项目未初始化同步"
	case len(o.Conflicts) > 0:
		return fmt.Sprintf("%d 个文件冲突，待解决", len(o.Conflicts))
	case o.NoRemote:
		if o.Committed {
			return "已本地提交（无远端，仅本地历史）"
		}
		return "已是最新（无远端，仅本地历史）"
	}
	var parts []string
	if o.Pulled > 0 {
		parts = append(parts, fmt.Sprintf("拉取 %d 个提交", o.Pulled))
	}
	if o.Pushed > 0 {
		parts = append(parts, fmt.Sprintf("推送 %d 个提交", o.Pushed))
	}
	if len(parts) == 0 {
		if o.Committed {
			return "本地提交已记录"
		}
		return "已是最新"
	}
	return strings.Join(parts, "，")
}

// apiProjectSync 触发一次同步；未 init 且带 remote 则先走三情形初始化（§14）。
func (h *Handler) apiProjectSync(w http.ResponseWriter, r *http.Request) {
	var req syncRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	repo := syncx.Open(st.Root)
	if !repo.IsRepo() {
		if req.Remote == "" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "not_repo", "message": "项目未初始化同步：请先建仓绑定或提供 remote"})
			return
		}
		kind, err := repo.InitForSync(req.Remote, hasKnowledgeContent(st), "sync: init "+syncCommitMsg())
		switch {
		case errors.Is(err, syncx.ErrRemoteNotEmpty):
			writeErr(w, http.StatusConflict, "远端仓已有内容，需手动合并一次（git pull --rebase origin main 或 merge --allow-unrelated-histories）后重试")
			return
		case err != nil:
			writeErr(w, http.StatusInternalServerError, "初始化失败："+err.Error())
			return
		}
		_ = kind
		cfg, _ := config.LoadMerged(st.ConfigPath(), "")
		if err := config.SetSync(st.ConfigPath(), config.Sync{Enabled: true, Remote: req.Remote, AutoIntervalMin: cfg.Sync.AutoIntervalMin}); err != nil {
			writeErr(w, http.StatusInternalServerError, "同步配置写入失败："+err.Error())
			return
		}
	}
	o := syncx.SyncOnce(st.Root, syncCommitMsg())
	syncx.RecordOutcome(st.Root, st.StateDir(), o)
	switch {
	case o.Err != nil:
		writeJSON(w, http.StatusOK, map[string]any{"status": "error", "message": o.Err.Error()})
	case len(o.Conflicts) > 0:
		writeJSON(w, http.StatusOK, map[string]any{"status": "conflict", "conflicts": o.Conflicts, "message": describeOutcome(o)})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "pushed": o.Pushed, "pulled": o.Pulled, "message": describeOutcome(o)})
	}
}

// apiSyncStatus 返回该项目 personal 层状态 + 冲突文件列表（行内状态点与冲突页数据源）。
func (h *Handler) apiSyncStatus(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	cfg, err := config.LoadMerged(st.ConfigPath(), "")
	// LoadStatus 读文件失败会返回 nil（fail-open：状态展示不受影响）
	l := &syncx.LayerStatus{}
	if sf, serr := syncx.LoadStatus(st.StateDir()); serr == nil {
		l = sf.Layer("personal")
	}
	conflicts, _ := syncx.ReadConflictFiles(st.StateDir())
	repo := syncx.Open(st.Root)
	// rebase 半途以仓态为准：conflicts 返回动态未决列表（仍未解决的），与 sync-conflict.json 解耦。
	// 部分解决后只剩未解决项——fresh 加载的冲突页不会拿到已解决文件（其 conflict-file 必 409）。
	if repo.MergeInProgress() {
		l.Conflict = true
		conflicts = repo.ConflictFiles()
	}
	// 归一化 null → []：前端 conflicts.map 依赖数组
	if conflicts == nil {
		conflicts = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":    err == nil && cfg.Sync.Enabled,
		"is_repo":    repo.IsRepo(),
		"ahead":      l.Ahead,
		"behind":     l.Behind,
		"conflict":   l.Conflict,
		"last_sync":  l.LastSync,
		"last_error": l.LastError,
		"conflicts":  conflicts,
	})
}

// apiSyncConflictFile 返回 {base, local, remote, working} 四版本全文（三向编辑器数据源）。
func (h *Handler) apiSyncConflictFile(w http.ResponseWriter, r *http.Request) {
	st := resolveProject(w, r.URL.Query().Get("project"))
	if st == nil {
		return
	}
	file := r.URL.Query().Get("file")
	if !validSyncFileParam(file) {
		writeErr(w, http.StatusBadRequest, "非法文件参数")
		return
	}
	base, local, remote, err := syncx.Open(st.Root).ConflictVersions(file)
	if err != nil {
		writeErr(w, http.StatusConflict, "取冲突版本失败（冲突可能已解决）："+err.Error())
		return
	}
	working, _ := os.ReadFile(filepath.Join(st.Root, file))
	writeJSON(w, http.StatusOK, map[string]string{
		"base": base, "local": local, "remote": remote, "working": string(working),
	})
}

// apiSyncResolve 落盘 + git add。action: me/theirs 取对应 stage 版本；merged 用提交的 content。
func (h *Handler) apiSyncResolve(w http.ResponseWriter, r *http.Request) {
	var req syncRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	if !validSyncFileParam(req.File) {
		writeErr(w, http.StatusBadRequest, "非法文件参数")
		return
	}
	repo := syncx.Open(st.Root)
	var content string
	switch req.Action {
	case "me", "theirs":
		_, local, remote, err := repo.ConflictVersions(req.File)
		if err != nil {
			writeErr(w, http.StatusConflict, "取冲突版本失败："+err.Error())
			return
		}
		if req.Action == "me" {
			content = local
		} else {
			content = remote
		}
	case "merged":
		content = req.Content
	default:
		writeErr(w, http.StatusBadRequest, "非法 action")
		return
	}
	if err := repo.ResolveFile(req.File, content); err != nil {
		writeErr(w, http.StatusInternalServerError, "写入失败："+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiSyncFinish 校验全部解决 → rebase --continue + push。
func (h *Handler) apiSyncFinish(w http.ResponseWriter, r *http.Request) {
	var req syncRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	repo := syncx.Open(st.Root)
	if files := repo.ConflictFiles(); len(files) > 0 {
		writeErr(w, http.StatusConflict, fmt.Sprintf("还有 %d 个文件未解决", len(files)))
		return
	}
	if !repo.MergeInProgress() {
		writeErr(w, http.StatusConflict, "没有进行中的同步冲突")
		return
	}
	if err := repo.ContinueRebase(); err != nil {
		writeErr(w, http.StatusInternalServerError, "rebase --continue 失败："+err.Error())
		return
	}
	var o syncx.Outcome
	if err := repo.Push(); err != nil {
		o.Err = err
	}
	syncx.RecordOutcome(st.Root, st.StateDir(), o)
	if o.Err != nil {
		writeErr(w, http.StatusInternalServerError, "推送失败："+o.Err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "message": "冲突已解决，同步完成"})
}

// apiSyncAbort rebase --abort 回滚到同步前状态（本地内容不丢）。
func (h *Handler) apiSyncAbort(w http.ResponseWriter, r *http.Request) {
	var req syncRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	repo := syncx.Open(st.Root)
	if !repo.MergeInProgress() {
		writeErr(w, http.StatusConflict, "没有进行中的同步冲突")
		return
	}
	if err := repo.AbortRebase(); err != nil {
		writeErr(w, http.StatusInternalServerError, "回滚失败："+err.Error())
		return
	}
	if sf, err := syncx.LoadStatus(st.StateDir()); err == nil {
		sf.Layer("personal").Conflict = false
		_ = sf.Save(st.StateDir())
	}
	_ = syncx.ClearConflictFiles(st.StateDir())
	w.WriteHeader(http.StatusNoContent)
}

// ai-merge：LLM 按三版本语义合并正文（front matter 不让 LLM 碰，取 local 版）。
// 纪律：只返回不落盘（落盘走 resolve）；超时按场景区分；temperature 缺省不传。

const aiMergeSystemPrompt = `你是合并助手。给定同一条知识笔记正文的三个版本——base（共同祖先）、local（本机修改）、remote（远端修改）——输出语义合并后的正文。规则：两边的信息都尽量保留；同义近重复只留一份；冲突表述取较具体者；只输出合并后的正文本身，不要任何解释、不要用 markdown 代码围栏包裹。`

const aiMergeMaxTokens = 4096

// splitFrontMatter 拆 "---\n...\n---\n" 头部。无头部时 fm=""。
// fm 连同规范空行分隔（Serialize 的 "---\n\n<body>" 形态）一起带走，
// 拼回时 fm+body 仍保持规范格式。
func splitFrontMatter(content string) (fm, body string) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content
	}
	rest := content[4:]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return "", content
	}
	end := idx + 5 // "\n---\n" 之后
	if end < len(rest) && rest[end] == '\n' {
		end++ // 吞掉分隔空行
	}
	return content[:4+end], rest[end:]
}

// llmAssistMode 判定 ai-merge 可用性（设计文档 §12）：off/server 档明确 409；
// local/空(auto) 看全局 LLM 配置。
func (h *Handler) llmAssistMode(st *store.Store) (client *llmx.Client, timeout time.Duration, errCode string) {
	cfg, _ := config.LoadMerged(st.ConfigPath(), "")
	switch cfg.Sync.LLMAssist {
	case "off":
		return nil, 0, "no_llm"
	case "server":
		return nil, 0, "server_not_available"
	}
	gcfg, err := loadGlobalConfig()
	if err != nil {
		return nil, 0, "no_llm"
	}
	prof := gcfg.LLM.ActiveProfile()
	if prof == nil {
		return nil, 0, "no_llm"
	}
	timeout = time.Duration(gcfg.LLM.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second // GUI 交互场景
	}
	return llmx.New(*prof, timeout), timeout, ""
}

func (h *Handler) apiSyncAIMerge(w http.ResponseWriter, r *http.Request) {
	var req syncRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st := resolveProject(w, req.Project)
	if st == nil {
		return
	}
	if !validSyncFileParam(req.File) {
		writeErr(w, http.StatusBadRequest, "非法文件参数")
		return
	}
	client, timeout, errCode := h.llmAssistMode(st)
	if errCode != "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": errCode})
		return
	}
	base, local, remote, err := syncx.Open(st.Root).ConflictVersions(req.File)
	if err != nil {
		writeErr(w, http.StatusConflict, "取冲突版本失败："+err.Error())
		return
	}
	// 三版本剥离 front matter；fm 取 local（本机最新），LLM 只合并正文
	fmLocal, bodyLocal := splitFrontMatter(local)
	_, bodyBase := splitFrontMatter(base)
	_, bodyRemote := splitFrontMatter(remote)
	user := "【base】\n" + bodyBase + "\n【local】\n" + bodyLocal + "\n【remote】\n" + bodyRemote
	ctx, cancel := context.WithTimeout(r.Context(), timeout+15*time.Second)
	defer cancel()
	rep, err := client.Chat(ctx, aiMergeSystemPrompt, user, aiMergeMaxTokens)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "LLM 合并失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"merged": fmLocal + rep.Text})
}
