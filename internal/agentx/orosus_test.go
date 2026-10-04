package agentx

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// setupOrosus 隔离 Orosus 配置根与 OK_HOME（HookTimeoutSec 读全局配置），返回 OrosusHome。
func setupOrosus(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_OROSUS_HOME", home)
	t.Setenv("OK_HOME", t.TempDir())
	return home
}

// orosusHandwritten 首接 Orosus 时的真机手写件形态（2026-10-04）：TOML literal 串
// command（无转义引号）、name/product 显示元数据、post-tool 挂 PreToolUse（文档
// 补 PostToolUse tool_input 之前的旧挂载——刻意保留作存量治理夹具，安装须能识别
// 并替换为 PostToolUse 标记块，不留孤儿父表）。
const orosusHandwritten = `# Hooks —— Okryptos 知识注入
[hooks]
enabled = true
timeoutMs = 60000

# ── 提问即注入：知识索引 + 相关知识 ──
[[hooks.UserPromptSubmit]]
[[hooks.UserPromptSubmit.hooks]]
command = 'D:/software/OpenKnowledge/ok.exe hook prompt claude'
name = "知识注入"
product = "Okryptos"
timeout = 20

[[hooks.PreToolUse]]
matcher = "^tool-fs__(write|edit)$"
[[hooks.PreToolUse.hooks]]
command = 'D:/software/OpenKnowledge/ok.exe hook post-tool claude'
name = "触碰记账"
product = "Okryptos"
timeout = 20

[[hooks.Stop]]
[[hooks.Stop.hooks]]
command = 'D:/software/OpenKnowledge/ok.exe hook stop claude'
name = "规则评估"
product = "Okryptos"
timeout = 20
`

// orosusThirdParty 第三方钩子表：安装/卸载必须原样保留。
const orosusThirdParty = `[[hooks.PreToolUse]]
matcher = "^tool-shell__bash$"
[[hooks.PreToolUse.hooks]]
command = "bash /somewhere/guard.sh"
name = "危险命令守卫"
timeout = 10
`

func writeOrosusHooks(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(orosusHooksPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orosusHooksPath(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readOrosusHooks(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(orosusHooksPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// decodeOrosusTOML 校验文件是合法 TOML 并返回 hooks 节。
func decodeOrosusTOML(t *testing.T, content string) map[string]any {
	t.Helper()
	cfg := map[string]any{}
	if _, err := toml.Decode(content, &cfg); err != nil {
		t.Fatalf("hooks.toml 不是合法 TOML: %v", err)
	}
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		t.Fatal("缺 [hooks] 节")
	}
	return hooks
}

// orosusTablesOf 取某事件的表数组。
func orosusTablesOf(t *testing.T, hooks map[string]any, event string) []map[string]any {
	t.Helper()
	arr, _ := hooks[event].([]map[string]any)
	return arr
}

func TestOrosusHooksBlockForShape(t *testing.T) {
	// 平台双态约定：Linux 上 filepath.ToSlash 是 no-op（不转反斜杠）——期望值
	// 与函数走同一归一，否则 ubuntu CI 必红（Windows 断言 D:/、Linux 断言 D:\）
	slashExe := filepath.ToSlash(`D:\x\ok.exe`)
	block := OrosusHooksBlockFor(`D:\x\ok.exe`, 15)
	for _, want := range []string{
		"[[hooks.UserPromptSubmit]]",
		"[[hooks.PostToolUse]]",
		`matcher = "^tool-fs__(write|edit)$"`,
		`command = "\"` + slashExe + `\" hook prompt claude"`,
		`command = "\"` + slashExe + `\" hook post-tool claude"`,
		`command = "\"` + slashExe + `\" hook stop claude"`,
		`name = "知识注入"`, `name = "触碰记账"`, `name = "规则评估"`,
		`product = "Okryptos"`,
		"timeout = 15",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("块缺 %q:\n%s", want, block)
		}
	}
	// 块本身必须是合法 TOML（包一层 [hooks] 头验证表归属）
	if h := decodeOrosusTOML(t, "[hooks]\n"+block); len(orosusTablesOf(t, h, "PostToolUse")) != 1 {
		t.Error("PostToolUse 表数应为 1")
	}
	// 换行回归钉（2026-10-04 真机实证）：块必须以 \n 收尾——末行无换行会在拼
	// 标记时粘连成 `timeout = 15# <<< okryptos hooks <<<`，smol-toml 解析失败、
	// hooks 模块激活炸降级窗（BurntSushi 容忍该形态，只验解析拦不住）
	if !strings.HasSuffix(block, "\n") || strings.Contains(block, "15#") || strings.Contains(block, `"15"timeout`) {
		t.Fatalf("块尾必须换行收尾、任何行不得粘连:\n%q", block)
	}
	for _, l := range strings.Split(strings.TrimSuffix(block, "\n"), "\n") {
		if strings.Contains(l, "#") && !strings.HasPrefix(strings.TrimSpace(l), "#") {
			t.Fatalf("块内行尾不允许出现注释（换行丢失形态）: %q", l)
		}
	}
}

func TestOrosusDetect(t *testing.T) {
	setupOrosus(t)
	// 负例指向不存在的子路径（TempDir 本身已存在）
	t.Setenv("OK_OROSUS_HOME", filepath.Join(OrosusHome(), "nonexistent"))
	if (orosusAgent{}).Detect() {
		t.Fatal("目录不存在时 Detect 应为 false")
	}
	t.Setenv("OK_OROSUS_HOME", filepath.Join(t.TempDir(), "home"))
	if err := os.MkdirAll(OrosusHome(), 0o755); err != nil {
		t.Fatal(err)
	}
	if !(orosusAgent{}).Detect() {
		t.Fatal("目录存在时 Detect 应为 true")
	}
}

func TestOrosusInstallIdempotent(t *testing.T) {
	setupOrosus(t)
	a := orosusAgent{}
	exe := currentExe(t)
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	content := readOrosusHooks(t)
	if got := strings.Count(content, MarkerBegin); got != 1 {
		t.Fatalf("标记块应恰 1 个，得 %d", got)
	}
	hooks := decodeOrosusTOML(t, content)
	wantCmd := func(sub string) string {
		return "\"" + filepath.ToSlash(exe) + "\" hook " + sub + " claude"
	}
	ups := orosusTablesOf(t, hooks, "UserPromptSubmit")
	if len(ups) != 1 || len(ups[0]["hooks"].([]map[string]any)) != 1 {
		t.Fatalf("UserPromptSubmit 应 1 表 1 钩: %v", ups)
	}
	if got := ups[0]["hooks"].([]map[string]any)[0]["command"]; got != wantCmd("prompt") {
		t.Errorf("prompt command = %v", got)
	}
	post := orosusTablesOf(t, hooks, "PostToolUse")
	if len(post) != 1 || post[0]["matcher"] != "^tool-fs__(write|edit)$" {
		t.Fatalf("PostToolUse 表/matcher 不符: %v", post)
	}
	if got := post[0]["hooks"].([]map[string]any)[0]["timeout"].(int64); got != int64(HookTimeoutSec()) {
		t.Errorf("timeout = %v", got)
	}
	if !a.HooksInstalled() {
		t.Fatal("安装后 HooksInstalled 应为 true")
	}
	// 换行回归钉：结束标记必须独立成行、前行恰为 timeout 行
	lines := strings.Split(readOrosusHooks(t), "\n")
	for i, l := range lines {
		if l == MarkerEnd && (i == 0 || lines[i-1] != "timeout = "+strconv.Itoa(HookTimeoutSec())) {
			t.Fatalf("结束标记须独立成行且前行为 timeout 行，前行=%q", lines[i-1])
		}
	}
}

func TestOrosusInstallReplacesHandwrittenLegacy(t *testing.T) {
	setupOrosus(t)
	writeOrosusHooks(t, orosusHandwritten+orosusThirdParty)
	if err := (orosusAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	content := readOrosusHooks(t)
	if strings.Count(content, MarkerBegin) != 1 {
		t.Fatal("标记块应恰 1 个")
	}
	// ok 的 command 只允许出现在标记块内（遗留手写块已替换）
	inside := orosusBlockOf(content)
	for _, l := range strings.Split(content, "\n") {
		if orosusOKCommand.MatchString(l) && !strings.Contains(inside, l) {
			t.Fatalf("标记块外残留 ok command: %s", l)
		}
	}
	hooks := decodeOrosusTOML(t, content)
	// 旧挂载（PreToolUse）手写件被替换为 PostToolUse 标记块；第三方 PreToolUse 表保留
	pre := orosusTablesOf(t, hooks, "PreToolUse")
	if len(pre) != 1 || !strings.Contains(content, "guard.sh") {
		t.Fatalf("第三方 PreToolUse 表应保留 1 表: %v", pre)
	}
	post := orosusTablesOf(t, hooks, "PostToolUse")
	if len(post) != 1 || post[0]["matcher"] != "^tool-fs__(write|edit)$" {
		t.Fatalf("标记块应为 PostToolUse 挂载: %v", post)
	}
	if strings.Count(content, "guard.sh") != 1 {
		t.Error("第三方钩子被误删")
	}
}

func TestOrosusRemoveHooks(t *testing.T) {
	setupOrosus(t)
	a := orosusAgent{}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	content := readOrosusHooks(t)
	if orosusHasOKLine(content) || strings.Contains(content, MarkerBegin) {
		t.Fatalf("卸载后残留 ok 内容:\n%s", content)
	}
	if _, err := os.Stat(orosusHooksPath()); err != nil {
		t.Fatal("卸载不应删掉整个文件（用户可能有第三方钩子）")
	}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("二次卸载应为 no-op: %v, %v", removed, err)
	}
	// 文件不存在 = 无可移除
	if err := os.Remove(orosusHooksPath()); err != nil {
		t.Fatal(err)
	}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("无文件卸载应为 no-op: %v, %v", removed, err)
	}
}

func TestOrosusRemoveMixedKeepsThirdParty(t *testing.T) {
	setupOrosus(t)
	if err := (orosusAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	// 追加第三方表再卸载：第三方完好、ok 表与孤儿父表全清
	content := readOrosusHooks(t) + "\n" + orosusThirdParty
	writeOrosusHooks(t, content)
	if _, err := (orosusAgent{}).RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	after := readOrosusHooks(t)
	if !strings.Contains(after, "guard.sh") {
		t.Fatal("第三方钩子被误删")
	}
	if orosusHasOKLine(after) {
		t.Fatal("残留 ok command")
	}
	hooks := decodeOrosusTOML(t, after)
	pre := orosusTablesOf(t, hooks, "PreToolUse")
	if len(pre) != 1 || pre[0]["hooks"].([]map[string]any)[0]["command"] != "bash /somewhere/guard.sh" {
		t.Fatalf("第三方表形态被改: %v", pre)
	}
}

func TestOrosusHooksInstalledDrift(t *testing.T) {
	setupOrosus(t)
	a := orosusAgent{}
	// 旧 exe → 过期
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if a.HooksInstalled() {
		t.Fatal("旧 exe 形态应为过期")
	}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Fatal("当前形态应为已安装")
	}
	// 超时漂移 → 过期
	content := strings.Replace(readOrosusHooks(t), "timeout = "+strconv.Itoa(HookTimeoutSec()), "timeout = 99", 1)
	writeOrosusHooks(t, content)
	if a.HooksInstalled() {
		t.Fatal("超时漂移应为过期")
	}
}

func TestOrosusDisabledIsNotDrift(t *testing.T) {
	setupOrosus(t)
	a := orosusAgent{}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	// 用户经 /settings 停用（disabled = true）≠ 集成过期：不误报、自愈不复活停用
	content := strings.Replace(readOrosusHooks(t), `product = "Okryptos"`, `product = "Okryptos"`+"\ndisabled = true", 1)
	writeOrosusHooks(t, content)
	if !a.HooksInstalled() {
		t.Fatal("disabled 停用不应误报过期")
	}
	if err := a.EnsureHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readOrosusHooks(t), "disabled = true") {
		t.Fatal("自愈复活了用户的停用")
	}
	// 全局总闸 enabled = false → 未接入（全新安装件无 [hooks] 节，前置补节头再关闸）
	content = "[hooks]\nenabled = false\n\n" + readOrosusHooks(t)
	writeOrosusHooks(t, content)
	if a.HooksInstalled() {
		t.Fatal("总闸关闭应为未接入")
	}
}

func TestOrosusEnsureHooks(t *testing.T) {
	setupOrosus(t)
	a := orosusAgent{}
	// 文件不存在 → no-op（不凭空创建）
	if err := a.EnsureHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orosusHooksPath()); !os.IsNotExist(err) {
		t.Fatal("EnsureHooks 不应凭空创建文件")
	}
	// 无任何 ok 条目（显式卸载/从未安装）→ 不复活
	writeOrosusHooks(t, "[hooks]\nenabled = true\n\n"+orosusThirdParty)
	if err := a.EnsureHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if orosusHasOKLine(readOrosusHooks(t)) {
		t.Fatal("无 ok 条目时自愈不应复活集成")
	}
	// 遗留手写件（无标记）→ 备份后重写为标记块
	writeOrosusHooks(t, orosusHandwritten)
	if err := a.EnsureHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	content := readOrosusHooks(t)
	if strings.Count(content, MarkerBegin) != 1 || !a.HooksInstalled() {
		t.Fatalf("自愈应重建标记块:\n%s", content)
	}
	if _, err := os.Stat(orosusHooksPath() + ".bak-openknowledge"); err != nil {
		t.Fatal("孤儿重建应留备份")
	}
	// 标记块过期（旧 exe）→ 重写为当前形态
	writeOrosusHooks(t, MarkerBegin+"\n"+OrosusHooksBlockFor(`D:\old\ok.exe`, 10)+"\n"+MarkerEnd+"\n")
	if err := a.EnsureHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Fatal("过期标记块自愈后应为当前形态")
	}
}

func TestOrosusMixedParentOrphan(t *testing.T) {
	setupOrosus(t)
	// 同一父表下 ok 子表 + 第三方子表混装：剥离 ok 子表时保留父表与第三方子表
	mixed := `[[hooks.PreToolUse]]
matcher = "^tool-fs__"
[[hooks.PreToolUse.hooks]]
command = 'D:/software/OpenKnowledge/ok.exe hook post-tool claude'
timeout = 10
[[hooks.PreToolUse.hooks]]
command = "node /tools/lint.mjs"
timeout = 5
`
	writeOrosusHooks(t, mixed)
	got := stripLegacyOKHooksOrosus(mixed)
	if strings.Contains(got, "ok.exe") {
		t.Fatalf("ok 子表未剥: %s", got)
	}
	hooks := decodeOrosusTOML(t, got)
	pre := orosusTablesOf(t, hooks, "PreToolUse")
	if len(pre) != 1 || pre[0]["matcher"] != "^tool-fs__" {
		t.Fatalf("父表应保留（第三方子表仍在）: %v", pre)
	}
	if len(pre[0]["hooks"].([]map[string]any)) != 1 || pre[0]["hooks"].([]map[string]any)[0]["command"] != "node /tools/lint.mjs" {
		t.Fatalf("第三方子表应保留: %v", pre)
	}
}
