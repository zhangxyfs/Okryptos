package agentx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupDSH 隔离 DSH 家目录与 OK_HOME（HookTimeoutSec 读全局配置），返回 DSHHome。
func setupDSH(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("OK_DSH_HOME", home)
	t.Setenv("OK_HOME", t.TempDir())
	return home
}

func TestDSHHome(t *testing.T) {
	t.Setenv("OK_DSH_HOME", "")
	t.Setenv("DSH_HOME", `D:\dsh-official`)
	if got := DSHHome(); got != `D:\dsh-official` {
		t.Fatalf("DSH_HOME 优先于默认目录: %q", got)
	}
	t.Setenv("OK_DSH_HOME", `D:\dsh-ok`)
	t.Setenv("DSH_HOME", `D:\dsh-official`)
	if got := DSHHome(); got != `D:\dsh-ok` {
		t.Fatalf("OK_DSH_HOME 应最优先: %q", got)
	}
}

func TestDSHDetect(t *testing.T) {
	setupDSH(t)
	if !(dshAgent{}).Detect() {
		t.Fatal("DSHHome 存在应检测为真")
	}
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "nonexistent"))
	if (dshAgent{}).Detect() {
		t.Fatal("DSHHome 不存在应检测为假")
	}
}

func TestDSHSkillsDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_SKILLS_HOME", dir)
	setupDSH(t)
	if got := (dshAgent{}).SkillsDir(); got != dir {
		t.Fatalf("SkillsDir 应为共享 SkillsHome: %q", got)
	}
}

func TestDSHRegistered(t *testing.T) {
	if _, ok := Find("dsh"); !ok {
		t.Fatal("dsh 适配器应已注册")
	}
}

func TestDSHInstallWritesPlugin(t *testing.T) {
	setupDSH(t)
	exe := currentExe(t)
	if err := (dshAgent{}).InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dshPluginPath())
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, dshPluginMarker) {
		t.Fatal("插件应含头标记")
	}
	if !strings.Contains(content, "// fingerprint: "+dshTemplateFingerprint()) {
		t.Fatal("插件应含当前指纹")
	}
	if !strings.Contains(content, filepath.ToSlash(exe)) {
		t.Fatal("插件应烘焙 exe 正斜杠路径")
	}
	for _, want := range []string{`ctx.on("agent/pre-step"`, `ctx.on("tools/post-execute"`, `ctx.on("agent/turn-stopping"`, `["hook", "prompt"]`, `["hook", "post-tool"]`, `["hook", "stop"]`} {
		if !strings.Contains(content, want) {
			t.Fatalf("插件缺少 %q", want)
		}
	}
}

func TestDSHInstallBacksUpForeignPlugin(t *testing.T) {
	setupDSH(t)
	if err := os.MkdirAll(filepath.Dir(dshPluginPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// someone else's plugin\n"
	if err := os.WriteFile(dshPluginPath(), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (dshAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(dshPluginPath() + ".bak-openknowledge")
	if err != nil || string(bak) != foreign {
		t.Fatalf("应备份既有非自家插件: %v %q", err, bak)
	}
}

func readDSHPatch(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(dshPatchPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDSHInstallWritesPatchLine(t *testing.T) {
	setupDSH(t)
	if err := (dshAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	patch := readDSHPatch(t)
	if !strings.Contains(patch, MarkerBegin) || !strings.Contains(patch, MarkerEnd) {
		t.Fatalf("patch 应含标记块: %q", patch)
	}
	if !strings.Contains(patch, "id: ok-hooks") {
		t.Fatalf("patch 应含 ok-hooks 行: %q", patch)
	}
	slash := filepath.ToSlash(dshPluginPath())
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	if !strings.Contains(patch, "name: 'file://"+slash+"'") {
		t.Fatalf("patch 应含插件 file:// URL（单引号）: %q", patch)
	}
	if !(dshAgent{}).HooksInstalled() {
		t.Fatal("安装后 HooksInstalled 应为真")
	}
}

func TestDSHInstallIdempotent(t *testing.T) {
	setupDSH(t)
	exe := currentExe(t)
	a := dshAgent{}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(exe); err != nil {
		t.Fatal(err)
	}
	patch := readDSHPatch(t)
	if n := strings.Count(patch, "id: ok-hooks"); n != 1 {
		t.Fatalf("重复安装后应有 1 条 ok-hooks 行, got %d", n)
	}
}

func TestDSHInstallPreservesForeignPatch(t *testing.T) {
	setupDSH(t)
	pre := "# 用户自己的 patch\n- insert:\n    - id: my-plugin\n      name: 'D:/x/my.js'\n"
	if err := os.WriteFile(dshPatchPath(), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (dshAgent{}).InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	patch := readDSHPatch(t)
	if !strings.Contains(patch, "id: my-plugin") || !strings.Contains(patch, "# 用户自己的 patch") {
		t.Fatalf("第三方行与注释应保留: %q", patch)
	}
	if !strings.Contains(patch, "id: ok-hooks") {
		t.Fatalf("ok 行应追加: %q", patch)
	}
	if _, err := os.Stat(dshPatchPath() + ".bak-openknowledge"); err != nil {
		t.Fatal("应生成 .bak-openknowledge 备份")
	}
}

func TestDSHRemoveHooks(t *testing.T) {
	setupDSH(t)
	a := dshAgent{}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("无配置 RemoveHooks = %v, %v", removed, err)
	}
	pre := "- insert:\n    - id: my-plugin\n      name: 'D:/x/my.js'\n"
	if err := os.WriteFile(dshPatchPath(), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.InstallHooks(currentExe(t)); err != nil {
		t.Fatal(err)
	}
	removed, err := a.RemoveHooks()
	if err != nil || !removed {
		t.Fatalf("RemoveHooks = %v, %v", removed, err)
	}
	if _, err := os.Stat(dshPluginPath()); !os.IsNotExist(err) {
		t.Fatal("插件文件应被删除")
	}
	patch := readDSHPatch(t)
	if strings.Contains(patch, "ok-hooks") {
		t.Fatalf("patch 不应残留 ok 行: %q", patch)
	}
	if !strings.Contains(patch, "id: my-plugin") {
		t.Fatalf("第三方行应保留: %q", patch)
	}
	if a.HooksInstalled() {
		t.Fatal("移除后 HooksInstalled 应为假")
	}
	if removed, err := a.RemoveHooks(); err != nil || removed {
		t.Fatalf("重复 RemoveHooks = %v, %v", removed, err)
	}
}

func TestDSHRemoveKeepsForeignPlugin(t *testing.T) {
	setupDSH(t)
	if err := os.MkdirAll(filepath.Dir(dshPluginPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "// someone else's plugin\n"
	if err := os.WriteFile(dshPluginPath(), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := (dshAgent{}).RemoveHooks()
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dshPluginPath()); string(data) != foreign {
		t.Fatal("非自家插件不应被删")
	}
	_ = removed
}

func TestDSHEnsureHooks(t *testing.T) {
	setupDSH(t)
	exe := currentExe(t)
	a := dshAgent{}

	// 从未安装 → no-op（不创建任何文件）
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dshPluginPath()); !os.IsNotExist(err) {
		t.Fatal("从未安装时 EnsureHooks 不应创建插件")
	}
	if _, err := os.Stat(dshPatchPath()); !os.IsNotExist(err) {
		t.Fatal("从未安装时 EnsureHooks 不应创建 patch")
	}

	// 过期（旧 exe 路径）→ 重写为当前
	if err := a.InstallHooks(`D:\old\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if a.HooksInstalled() {
		t.Fatal("旧 exe 路径应判为未安装/过期")
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if !a.HooksInstalled() {
		t.Fatal("自愈后 HooksInstalled 应为真")
	}

	// 当前 → no-op
	beforePlugin, _ := os.ReadFile(dshPluginPath())
	beforePatch, _ := os.ReadFile(dshPatchPath())
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	afterPlugin, _ := os.ReadFile(dshPluginPath())
	afterPatch, _ := os.ReadFile(dshPatchPath())
	if string(beforePlugin) != string(afterPlugin) || string(beforePatch) != string(afterPatch) {
		t.Fatal("内容当前时 EnsureHooks 应 no-op")
	}

	// 显式移除 → 不复活
	if _, err := a.RemoveHooks(); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureHooks(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dshPluginPath()); !os.IsNotExist(err) {
		t.Fatal("显式移除后 EnsureHooks 不应复活插件")
	}
}

// TestDSHYAMLSingleQuoted 回归 L-04：YAML 单引号标量内单引号必须双写（''）——
// 路径含 ' 时不转义会截断标量、file URL 断裂、插件挂载失效。
func TestDSHYAMLSingleQuoted(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "'plain'"},
		{"o'brien", "'o''brien'"},
		{"''", "''''''"},
	}
	for _, c := range cases {
		if got := dshYAMLSingleQuoted(c.in); got != c.want {
			t.Errorf("dshYAMLSingleQuoted(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDSHPatchBlockQuoteRoundTrip 回归 L-04：家目录路径含单引号时，patch 行的
// name 标量按 YAML 规则还原（'' → '）后必须逐字等于 file URL。
func TestDSHPatchBlockQuoteRoundTrip(t *testing.T) {
	t.Setenv("OK_DSH_HOME", filepath.Join(t.TempDir(), "o'brien"))
	t.Setenv("OK_HOME", t.TempDir())
	block := dshPatchBlock()
	want := dshPluginFileURL()
	const prefix = "name: '"
	i := strings.Index(block, prefix)
	if i < 0 || !strings.HasSuffix(block, "'\n") {
		t.Fatalf("patch 行 name 标量形态异常: %q", block)
	}
	raw := block[i+len(prefix) : len(block)-2]
	// 标量内部不得存在未转义的孤立单引号（YAML 单引号串内 ' 必成对出现）。
	for j := 0; j < len(raw); j++ {
		if raw[j] == '\'' && (j+1 >= len(raw) || raw[j+1] != '\'') {
			t.Fatalf("name 标量含未转义单引号（截断 file URL）: %q", raw)
		}
		if raw[j] == '\'' {
			j++
		}
	}
	if got := strings.ReplaceAll(raw, "''", "'"); got != want {
		t.Errorf("YAML 还原后 name = %q, want %q", got, want)
	}
}

// TestDSHLegacyMigration（2.25.0 改名迁移）：旧插件目录 plugins/openknowledge/
// + 旧品牌 patch 标记块，在 EnsureHooks 窗口整体迁移——插件写新目录
// plugins/okryptos/、patch 块换为新标记与新 file URL、旧目录删除。
func TestDSHLegacyMigration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_DSH_HOME", home)
	// 旧版遗留：插件目录 + patch 旧标记块
	legacyDir := filepath.Join(home, "plugins", "openknowledge")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "index.js"),
		[]byte(legacyDSHPluginMarker+"\n// fingerprint: 000000000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := filepath.Join(home, "cordis.patch.yml")
	legacyPatch := "# 别的内容: 1\n" + LegacyMarkerBegin + "\n- insert:\n    - id: ok-hooks\n      name: 'file:///D:/old/plugins/openknowledge/index.js'\n" + LegacyMarkerEnd + "\n"
	if err := os.WriteFile(patch, []byte(legacyPatch), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := (dshAgent{}).EnsureHooks(`D:\new\ok.exe`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatal("旧插件目录应被清除")
	}
	if _, err := os.Stat(filepath.Join(home, "plugins", "okryptos", "index.js")); err != nil {
		t.Fatalf("新插件应就位: %v", err)
	}
	data, _ := os.ReadFile(patch)
	s := string(data)
	if strings.Contains(s, "openknowledge") {
		t.Fatalf("patch 不应残留旧品牌引用: %q", s)
	}
	if !strings.Contains(s, MarkerBegin) || !strings.Contains(s, "plugins/okryptos/index.js") {
		t.Fatalf("patch 应含新标记块与新 file URL: %q", s)
	}
	if !strings.Contains(s, "# 别的内容: 1") {
		t.Fatal("块外内容不应受损")
	}
}
