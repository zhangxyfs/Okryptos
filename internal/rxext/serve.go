// Package rxext 实现 ok 的 Reasonix Extension Protocol sidecar：
// Reasonix 宿主以 exec form 拉起 `ok extension-serve`，经 NDJSON/JSON-RPC
// 下发 input.receive / tool.after 拦截。fail-open：任何内部错误一律 Continue。
package rxext

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"openknowledge/internal/agentx"
	"openknowledge/internal/daemonx"
	"openknowledge/internal/hook"
	"openknowledge/internal/project"
	"openknowledge/internal/registry"
	extension "openknowledge/internal/rxext/sdk"
	"openknowledge/internal/setupx"
	"openknowledge/internal/state"
	"openknowledge/internal/version"
)

// Serve 是 `ok extension-serve` 的入口：跑 SDK 的 stdio 服务循环直到宿主关闭。
func Serve(ctx context.Context) error {
	h := &handler{}
	return extension.Serve(ctx, h, extension.Options{
		Name:    "openknowledge",
		Version: version.Version,
		Interceptors: map[string]extension.InterceptorFunc{
			"input.receive":      h.onInput,
			"tool.after":         h.onToolAfter,
			"compaction.complete": h.onCompaction,
		},
	})
}

// handler 持有 initialize 握手确定的单会话上下文（一个 Reasonix 会话一个 sidecar 进程）。
// mu 串行化拦截器回调：SDK 并发派发（wire.go maxConcurrentHandlers=32），
// state 的读-改-写（TrackTouched/CheckStop）无锁会丢更新。
// healMu/lastHeal 是 selfHealHooks 的进程内节流，独立于 mu：自愈是全 agent
// 检测/重写 I/O，不触碰会话状态，进会话锁会把串行化的 I/O 压进 input.receive
// 热路径（逼近 manifest timeoutMillis，M-12）。
type handler struct {
	mu        sync.Mutex
	sessionID string
	cwd       string
	healMu    sync.Mutex
	lastHeal  time.Time
}

func (h *handler) Initialize(_ context.Context, p extension.InitializeParams) (*extension.InitializeResult, error) {
	h.sessionID = p.Session.SessionID
	h.cwd = p.Session.WorkspaceRoot
	return &extension.InitializeResult{
		Subscriptions: []string{"input.receive", "tool.after", "compaction.complete"},
	}, nil
}

// onInput input.receive 拦截器：先 enforce 检查（block 优先于注入），再检索注入。
// 三档分流（[reasonix] enforce_mode）：mixed（默认）=自省软提醒/规则硬阻断；
// soft=全软提示；hard=全硬阻断。软路径把提醒与注入合并为一个 <ok-context> 块
// （提醒在前、注入在后）。fail-open：panic/错误一律 Continue。
func (h *handler) onInput(_ context.Context, _ string, payload json.RawMessage) (res *extension.InterceptResult, err error) {
	defer continueOnPanic(&res, &err)
	res = extension.Continue()
	h.maybeSelfHeal() // 会话锁外 + 进程内节流（M-12）
	h.mu.Lock()
	defer h.mu.Unlock()
	var in struct {
		Text string `json:"text"`
	}
	if uerr := json.Unmarshal(payload, &in); uerr != nil {
		// 宿主载荷变化会让注入静默失联（L-18）：记 ok.log，与 hook 包同类分支一致
		logErr("rxext input.receive: bad payload: %v", uerr)
		return res, nil
	}
	if strings.TrimSpace(in.Text) == "" {
		return res, nil
	}
	pc, err := project.FromCwd(h.cwd)
	if err != nil {
		return res, nil
	}
	// 宿主合成回合（goal 自动续跑等）不是真实用户回合：照常评估 enforce/提醒，
	// 但不推进 auto 自省的回合计数（L-21，提醒口径不被输入次数稀释）
	reason, blockedRule := hook.CheckStopTurn(pc, h.sessionID, !isSyntheticTurn(in.Text))
	mode := enforceMode()
	isBlock := blockedRule != ""
	if reason != "" {
		switch {
		case isBlock && mode != "soft":
			// 规则硬阻断：mixed/hard 直接 Block；Block 生效前落每会话防重标记
			// （soft 路径不标记 → 下条输入 CheckStop 重复命中，提醒逐条重复）
			markBlocked(pc, h.sessionID, blockedRule)
			return extension.Block(reason), nil
		case !isBlock && mode == "hard":
			// auto 自省软提醒：hard 档升级为硬阻断（blockedRule 为空，无规则可标）
			return extension.Block(reason), nil
		}
	}
	prefix := hook.InjectForPrompt(pc, h.sessionID, h.cwd, in.Text)
	var parts []string
	if reason != "" { // 软路径：提醒与注入合并为一个 ok-context 块（提醒在前）
		parts = append(parts, reason)
	}
	if strings.TrimSpace(prefix) != "" {
		parts = append(parts, prefix)
	}
	if len(parts) == 0 {
		return res, nil
	}
	rep, err := buildInputReplacement(in.Text, parts)
	if err != nil {
		return extension.Continue(), nil // fail-open：组装失败不阻断输入
	}
	return rep, nil
}

// enforceMode 读全局三档配置 [reasonix] enforce_mode（soft|hard|mixed，缺省/非法
// 按 mixed），生产路径收编在 setupx.ReasonixEnforceMode()；OK_RX_ENFORCE_TEST_MODE
// 是测试注入口（生产不设置）。
func enforceMode() string {
	if m := os.Getenv("OK_RX_ENFORCE_TEST_MODE"); m != "" {
		return normalizeEnforceMode(m)
	}
	return setupx.ReasonixEnforceMode()
}

func normalizeEnforceMode(m string) string {
	switch m {
	case "soft", "hard":
		return m
	default:
		return "mixed"
	}
}

// buildInputReplacement 把若干注入片段合并为一个 <ok-context> 块前缀进原输入。
func buildInputReplacement(original string, parts []string) (*extension.InterceptResult, error) {
	var b strings.Builder
	b.WriteString("<ok-context>\n")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		b.WriteString(strings.TrimRight(p, "\n"))
		b.WriteString("\n\n")
	}
	b.WriteString("</ok-context>\n\n")
	b.WriteString(original)
	return extension.Replace(map[string]string{"text": b.String()})
}

// maybeSelfHeal 按 hook.SelfHealMinInterval 进程内节流执行 selfHealHooks
// （与 hook 包 HandlePrompt 同口径）。healMu 持锁跨过 I/O：并发回调下同一时刻
// 最多一个自愈在跑；会话锁 mu 不参与，input.receive 不被自愈 I/O 串行化。
func (h *handler) maybeSelfHeal() {
	h.healMu.Lock()
	defer h.healMu.Unlock()
	if time.Since(h.lastHeal) < hook.SelfHealMinInterval {
		return
	}
	h.lastHeal = time.Now()
	selfHealHooks()
}

// reasonixSyntheticTurnMarkers 镜像 Reasonix 宿主经 input.receive 下发的合成回合
// 指令文本（上游 internal/control/goal.go goalContinueTurn、formatIncompleteTodos
// 与 readiness 催促）：这些回合由宿主自动注入、无用户动作，不应推进 auto 自省
// "每 turn_interval 回合"的时钟（L-21）。宿主 intercept 回合允许携带模型生成的
// nextAction 文本（无法识别，仍会计数）——残余漂移有界；宿主文案变更漏判的代价
// 同样仅是合成回合计入计数，不错杀真实用户输入。
var reasonixSyntheticTurnMarkers = []string{
	"Continue pursuing the active goal",
	"Goal signaled complete but issues remain:",
	"The agent signaled goal completion and all tasks are marked done.",
	"No tool calls in recent turns.",
}

// isSyntheticTurn 按宿主合成回合指令文本识别 input.receive 里的非用户回合。
// 合成文本前可能拼有 <active-goal> 等宿主注入块，故用 Contains 而非前缀匹配。
func isSyntheticTurn(text string) bool {
	for _, m := range reasonixSyntheticTurnMarkers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// logErr 追加诊断到 ok.log（与 hook 包 logErr 同一文件同一格式；日志本身失败
// 不再上报）。sidecar 的 stderr 由宿主脱敏留尾，协议类故障（如宿主载荷漂移）
// 需进 ok.log 才能在 GUI 日志页诊断。
func logErr(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(registry.Home(), "ok.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("2006-01-02 15:04:05 ")+format+"\n", args...)
}

// selfHealHooks 逐 agent 自检 hooks/插件集成（如 ok.exe 迁移后重写登记）。fail-open。
// sidecar 由 okd 拉起时 os.Executable 可能是 okd.exe，必须经 CliTargetFor 换算成
// CLI 入口，否则会把各 agent hooks 改写成指向 okd（无子命令的失效命令）；换算失败
// 放弃本次自愈。
func selfHealHooks() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if exe, err = daemonx.CliTargetFor(exe); err != nil {
		return
	}
	for _, a := range agentx.Detected() {
		_ = a.EnsureHooks(exe)
	}
}

// markBlocked 硬阻断生效前落每会话防重标记（fail-open：失败仅记日志）。
func markBlocked(pc *project.Context, sessionID, ruleType string) {
	if err := state.Update(pc.Store.StateDir(), sessionID, func(st *state.Session) {
		st.MarkBlocked(ruleType)
	}); err != nil {
		fmt.Fprintf(os.Stderr, "rxext markBlocked: %v\n", err)
	}
}

// onToolAfter tool.after 拦截器：写文件工具成功执行后记录 touched。恒 Continue。
func (h *handler) onToolAfter(_ context.Context, _ string, payload json.RawMessage) (res *extension.InterceptResult, err error) {
	defer continueOnPanic(&res, &err)
	h.mu.Lock()
	defer h.mu.Unlock()
	res = extension.Continue()
	var p struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		IsError   bool   `json:"isError"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.IsError {
		return res, nil
	}
	switch p.Name {
	case "write_file", "edit_file", "multi_edit", "notebook_edit":
	default:
		return res, nil
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(p.Arguments), &args); err != nil || args.Path == "" {
		return res, nil
	}
	pc, err := project.FromCwd(h.cwd)
	if err != nil {
		return res, nil
	}
	hook.TrackTouched(pc, h.sessionID, p.Name, args.Path)
	return res, nil
}

// onCompaction compaction.complete 拦截器：宿主压缩上下文后，首轮注入的 mandatory
// 全文已被摘要/丢弃，但 BaseInjected 仍为 true 会阻止重注入。这里重置标记，下一次
// input.receive 即重新注入基础段（mandatory 全文 + 索引）。恒 Continue。
func (h *handler) onCompaction(_ context.Context, _ string, _ json.RawMessage) (res *extension.InterceptResult, err error) {
	defer continueOnPanic(&res, &err)
	h.mu.Lock()
	defer h.mu.Unlock()
	res = extension.Continue()
	pc, err := project.FromCwd(h.cwd)
	if err != nil {
		return res, nil
	}
	hook.ResetBaseInjection(pc, h.sessionID)
	return res, nil
}

// continueOnPanic 把 panic 折叠为 Continue（fail-open 铁律）。供拦截器 defer 使用：
//
//	func (h *handler) onInput(...) (res *extension.InterceptResult, err error) {
//		defer continueOnPanic(&res, &err)
//		...
//	}
//
// panic 时 err 必须置 nil：SDK 对非 nil err 会丢弃 result 改回 JSON-RPC 错误，
// 而 fail-open 要的是 Continue。panic 痕迹写 stderr（由宿主脱敏留尾）。
func continueOnPanic(res **extension.InterceptResult, err *error) {
	if r := recover(); r != nil {
		*res = extension.Continue()
		*err = nil
		fmt.Fprintf(os.Stderr, "rxext panic: %v\n", r)
	}
}
