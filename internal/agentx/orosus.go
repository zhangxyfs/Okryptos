package agentx

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"okryptos/internal/fsx"
)

// OrosusHome 返回 Orosus 配置根目录（OK_OROSUS_HOME 优先——ok 自留的测试隔离口，
// Orosus 官方无环境重定位变量），否则 ~/.orosus。
// Orosus 是模块化 AI 编程助手 CLI，hooks 协议与 Claude 生态兼容（docs/hooks.md）。
func OrosusHome() string {
	if h := os.Getenv("OK_OROSUS_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".orosus")
}

func orosusHooksPath() string { return filepath.Join(OrosusHome(), "modules.d", "hooks.toml") }

// orosusHookEvents ok 接入的 Orosus hook 事件。post-tool 挂 PostToolUse：载荷带
// tool_input（工具实收参数、改参后为最终值，docs/hooks.md §3.2 cc 口径——事后
// 归因类钩子从这取 path），与 ZCode 的 Write|Edit 同位；事前（PreToolUse）跑会
// 记「未遂的写」——工具可能被审批拒绝或执行失败（文档 §12 示例四注意项）。
// 2026-10-04 文档补 PostToolUse tool_input 前曾挂 PreToolUse：存量 PreToolUse 表
// 由 stripLegacyOKHooksOrosus 治理（判定只看 command 行，与事件名无关）。
var orosusHookEvents = []hookEvent{
	{"UserPromptSubmit", "", "prompt"},
	{"PostToolUse", "^tool-fs__(write|edit)$", "post-tool"},
	{"Stop", "", "stop"},
}

// orosusHookNames ok 三链路在 Orosus 侧的显示名（hooks 条目 name 键——列表行/
// 状态行/阻断提示/注入折叠行显示用，纯元数据不进载荷）。
var orosusHookNames = map[string]string{
	"prompt":    "知识注入",
	"post-tool": "触碰记账",
	"stop":      "规则评估",
}

// OrosusHooksBlockFor 生成指向 exe 的 Orosus hooks 标记块内容（不含标记行）。
// command 引 exe（正斜杠+双引号，bash -c 与 cmd 都接受；路径含空格不断裂），
// 协议尾参 claude（Orosus 解析 Claude JSON 兼容壳）；三条统一 timeout 秒。
// 每行（含末行 timeout）必须 \n 收尾：末行无换行会在拼标记时粘连成
// `timeout = 20# <<< okryptos hooks <<<`——TOML 行尾注释前无空白，Orosus 的
// smol-toml 解析失败、hooks 模块激活炸降级窗（2026-10-04 真机实证；BurntSushi
// 容忍该形态，回归钉须直接断言换行而非只验解析）。
func OrosusHooksBlockFor(exe string, timeoutSec int) string {
	exe = filepath.ToSlash(exe)
	blocks := make([]string, 0, len(orosusHookEvents))
	for _, e := range orosusHookEvents {
		var b strings.Builder
		b.WriteString("[[hooks." + e.event + "]]\n")
		if e.matcher != "" {
			b.WriteString("matcher = \"" + e.matcher + "\"\n")
		}
		b.WriteString("[[hooks." + e.event + ".hooks]]\n")
		b.WriteString("command = \"\\\"" + exe + "\\\" hook " + e.okHook + " claude\"\n")
		b.WriteString("name = \"" + orosusHookNames[e.okHook] + "\"\n")
		b.WriteString("product = \"Okryptos\"\n")
		b.WriteString("timeout = " + strconv.Itoa(timeoutSec) + "\n")
		blocks = append(blocks, b.String())
	}
	return strings.Join(blocks, "\n") // 各块已 \n 收尾，join 补一个 = 块间恰一空行
}

// orosusGlobalDisabled 匹配 [hooks] 节的 enabled = false（允许行尾注释）。
var orosusGlobalDisabled = regexp.MustCompile(`^enabled\s*=\s*false\s*(?:#.*)?$`)

// orosusOKCommand 匹配 Orosus 格式 command 行（[[hooks.<E>.hooks]] 表内）：值指
// 向 ok/okd 的 hook prompt|post-tool|stop 命令（可带 claude 协议尾参）。双引号
// basic 串（exe 带引号 → 值内转义引号）与单引号 literal 串（手写配置常用——2026-10-04
// 首接 Orosus 时的手写件即 literal 形态）双形态；okd 存量形态同认（与 okHookCommand
// 口径一致：不认则存量块剥不掉，双注入）。
var orosusOKCommand = regexp.MustCompile(`(?i)^\s*command\s*=\s*(?:"(?:[^"\\]|\\.)*\bokd?(?:\.exe)?(?:\\")?\s+hook\s+(?:prompt|post-tool|stop)(?:\s+claude)?(?:\\")?"|'[^']*\bokd?(?:\.exe)?\s+hook\s+(?:prompt|post-tool|stop)(?:\s+claude)?')\s*$`)

// stripLegacyOKHooksOrosus 移除 Orosus 表格式（[[hooks.<E>]] / [[hooks.<E>.hooks]]）
// 中所有指向 ok hook 的无标记表：子表按 orosusOKCommand 判定整表删；子表被删空的
// 父表连带删——Orosus 表 schema 要求 hooks 键非空（至少 1 条），只剩 matcher 的
// 孤儿父表会让整份配置校验失败、模块激活炸降级窗。子表归属按 TOML 位置绑定：
// [[hooks.<E>.hooks]] 挂到最近一个 [[hooks.<E>]] 元素（与其后无关表头无关），
// 不能全文件搜同名子表认领——否则第三方子表会给孤儿父表续命。其它工具的表
// 原样保留；标记块内部由调用方保证不传入（与 StripLegacyOKHooks 同约束）。
func stripLegacyOKHooksOrosus(content string) string {
	lines := strings.Split(content, "\n")
	removed := make([]bool, len(lines))

	// 1) 表块划分：表头行 → 下一表头行（[ 或 [[ 都算表头边界）前
	type tbl struct {
		head, end int // [head, end) 行区间
		event     string
		child     bool // [[hooks.<E>.hooks]] 子表
		okHook    bool // 子表 body 命中 ok command
		owner     int  // 父表元素序（父表=自身序；子表=最近同事件父表，-1=隐式）
	}
	var tables []tbl
	parentCount := map[string]int{}
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, "[[hooks.") || !strings.HasSuffix(t, "]]") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(t, "[[hooks."), "]]")
		event := strings.TrimSuffix(name, ".hooks")
		tb := tbl{head: i, end: len(lines), event: event, child: strings.HasSuffix(name, ".hooks"), owner: -1}
		if tb.child {
			tb.owner = parentCount[event] - 1 // 归属最近一个同事件父表元素
		} else {
			tb.owner = parentCount[event]
			parentCount[event]++
		}
		j := i + 1
		for ; j < len(lines); j++ {
			if u := strings.TrimSpace(lines[j]); strings.HasPrefix(u, "[") {
				break
			}
			if tb.child && orosusOKCommand.MatchString(lines[j]) {
				tb.okHook = true
			}
		}
		tb.end = j
		tables = append(tables, tb)
	}

	// 2) 删 ok 子表；统计各父表元素名下保留的子表数
	removeBlock := func(head, end int) {
		for k := head; k < end; k++ {
			removed[k] = true
		}
		for k := end; k < len(lines) && strings.TrimSpace(lines[k]) == ""; k++ {
			removed[k] = true // 块间分隔空行随块走，避免留成串空行
		}
	}
	keptChildren := map[string]int{} // "事件#父表序" → 保留子表数（复合键防跨事件撞号）
	removedAnyChild := map[string]bool{}
	for _, tb := range tables {
		if !tb.child {
			continue
		}
		if tb.okHook {
			removeBlock(tb.head, tb.end)
			removedAnyChild[tb.event] = true
			continue
		}
		if tb.owner >= 0 {
			keptChildren[tb.event+"#"+strconv.Itoa(tb.owner)]++
		}
	}
	// 3) 子表被删空且确有 ok 子表被删事件的父表 → 孤儿，连带删
	for _, tb := range tables {
		if tb.child || !removedAnyChild[tb.event] {
			continue
		}
		if keptChildren[tb.event+"#"+strconv.Itoa(tb.owner)] == 0 {
			removeBlock(tb.head, tb.end)
		}
	}
	var out []string
	for i, l := range lines {
		if !removed[i] {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// orosusHasOKLine 报告内容里是否有任何 Orosus 格式 ok command 行（遗留判定）。
func orosusHasOKLine(content string) bool {
	for _, l := range strings.Split(content, "\n") {
		if orosusOKCommand.MatchString(l) {
			return true
		}
	}
	return false
}

// upsertOrosusHooksLocked 剥全部标记块（新旧品牌）与遗留无标记 ok 表后追加当前
// 标记块（位置无关紧要，恒尾追加；幂等）。锁内调用（WithFileLock 不可重入）。
func upsertOrosusHooksLocked(block string) error {
	data, err := os.ReadFile(orosusHooksPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content := string(data)
	if content, err = stripMarkerBlocks(content, orosusHooksPath()); err != nil {
		return err
	}
	content = stripLegacyOKHooksOrosus(content)
	wrapped := MarkerBegin + "\n" + block + MarkerEnd + "\n"
	sep := ""
	if strings.TrimSpace(content) != "" {
		sep = "\n"
		if !strings.HasSuffix(content, "\n") {
			sep = "\n\n"
		}
	}
	if err := os.MkdirAll(filepath.Dir(orosusHooksPath()), 0o755); err != nil {
		return err
	}
	return fsx.WriteFile(orosusHooksPath(), []byte(content+sep+wrapped), 0o644)
}

// orosusBlockOf 取标记块内容（两标记行之间，不含标记行）；无块/损坏返回 ""。
func orosusBlockOf(content string) string {
	i := strings.Index(content, MarkerBegin)
	if i < 0 {
		return ""
	}
	rest := content[i+len(MarkerBegin):]
	j := strings.Index(rest, MarkerEnd)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

// normalizeOrosusBlock 归一标记块内容用于新旧比对：剥 disabled 键行——用户经
// /settings → 钩子 → e 停用写的是 disabled = true，属用户意愿而非集成过期，
// 不归一会导致 HooksInstalled 误报 false、自愈把用户的停用复活。
func normalizeOrosusBlock(block string) string {
	var out []string
	for _, l := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "disabled") {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// orosusMarkerCurrent 标记块存在且归一后与当前期望形态一致（exe 迁移、超时变更 →
// 过期，自愈重写；仅 disabled 差异 → 视为当前，不复活用户停用）。
func orosusMarkerCurrent() bool {
	data, err := os.ReadFile(orosusHooksPath())
	if err != nil {
		return false
	}
	content := string(data)
	block := orosusBlockOf(content)
	if block == "" {
		return false
	}
	// [hooks] 总闸显式关闭 = 整份 hooks 不派发，视为未接入（与 zcode enabled 防御
	// 对称）。精确匹配防 enabled_xxx = "…false…" 之类键名前缀误命中。
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[[") {
			break // 总闸只可能在首个表头之前的 [hooks] 节里
		}
		if orosusGlobalDisabled.MatchString(t) {
			return false
		}
	}
	exe, err := currentCLIExe()
	if err != nil {
		return false
	}
	return normalizeOrosusBlock(block) == normalizeOrosusBlock(OrosusHooksBlockFor(exe, HookTimeoutSec()))
}

// orosusAgent Orosus 适配器：hook 集成 = 标记块 upsert ~/.orosus/modules.d/hooks.toml
// （kimi 同款标记块模式——TOML 保注释幂等写）；技能目录独立（Orosus 读 ~/.orosus/skills）。
type orosusAgent struct{}

func init() { Register(orosusAgent{}) }

func (orosusAgent) ID() string          { return "orosus" }
func (orosusAgent) DisplayName() string { return "Orosus" }
func (orosusAgent) HooksTarget() string { return orosusHooksPath() }
func (orosusAgent) SkillsDir() string   { return filepath.Join(OrosusHome(), "skills") }

func (orosusAgent) Detect() bool {
	info, err := os.Stat(OrosusHome())
	return err == nil && info.IsDir()
}

func (orosusAgent) InstallHooks(exe string) error {
	return fsx.WithFileLock(orosusHooksPath(), func() error {
		// 备份在锁内（同 kimi：改写宿主配置前留 .bak-openknowledge）
		if data, err := os.ReadFile(orosusHooksPath()); err == nil {
			_ = os.WriteFile(orosusHooksPath()+".bak-openknowledge", data, 0o644)
		}
		return upsertOrosusHooksLocked(OrosusHooksBlockFor(exe, HookTimeoutSec()))
	})
}

func (orosusAgent) RemoveHooks() (bool, error) {
	if _, err := os.Stat(orosusHooksPath()); os.IsNotExist(err) {
		return false, nil
	}
	removed := false
	err := fsx.WithFileLock(orosusHooksPath(), func() error {
		data, err := os.ReadFile(orosusHooksPath())
		if err != nil {
			return err
		}
		content := string(data)
		stripped, serr := stripMarkerBlocks(content, orosusHooksPath())
		if serr != nil {
			return serr
		}
		stripped = stripLegacyOKHooksOrosus(stripped)
		if stripped == content {
			return nil
		}
		removed = true
		return fsx.WriteFile(orosusHooksPath(), []byte(stripped), 0o644)
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

// EnsureHooks 自愈：标记块丢失但遗留 ok 表仍在（手动改写/首接手写件）→ 备份后
// 重写；标记块在但过期（exe 迁移、超时变更）→ 重写；完全无 ok 条目（显式卸载/
// 从未安装）不复活——与 kimi EnsureHooksBlock 同语义。
func (orosusAgent) EnsureHooks(exe string) error {
	if _, err := os.Stat(orosusHooksPath()); err != nil {
		return nil
	}
	return fsx.WithFileLock(orosusHooksPath(), func() error {
		if orosusMarkerCurrent() {
			return nil
		}
		data, err := os.ReadFile(orosusHooksPath())
		if err != nil {
			return err
		}
		if !strings.Contains(string(data), MarkerBegin) {
			if !orosusHasOKLine(string(data)) {
				return nil
			}
			_ = os.WriteFile(orosusHooksPath()+".bak-openknowledge", data, 0o644)
		}
		return upsertOrosusHooksLocked(OrosusHooksBlockFor(exe, HookTimeoutSec()))
	})
}

func (orosusAgent) HooksInstalled() bool {
	return orosusMarkerCurrent()
}
