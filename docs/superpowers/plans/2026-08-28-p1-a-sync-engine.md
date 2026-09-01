# P1-A 同步引擎（syncx + ok sync + okd auto-sync）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 OpenKnowledge 增加个人多端同步引擎：`internal/syncx`（exec 系统 git）、`ok sync` / `ok sync init` 命令、okd 定时同步 + 写入防抖、状态文件与 `[sync]` 配置段，双设备 E2E 闭环。

**Architecture:** 叶子包 `internal/syncx` 以参数数组 exec 系统 git（不走 shell），提供仓库原语 + Sync 编排 + single-flight；CLI 与 okd janitor 共用同一编排；状态按层建模落 `state/sync-status.json`。GUI 冲突页、okserver 不在本计划（分别属 Plan B / Plan C）。

**Tech Stack:** Go 1.25 标准库（无新第三方依赖）、系统 git、既有 internal 包（fsx/config/registry/store/procx）。

## Global Constraints

- 规范源：`docs/superpowers/specs/2026-08-25-personal-sync-p1-design.md` §4-§8、§12、§14、§15；冲突时以规范源为准。
- **无新第三方依赖**（go.mod 不得新增 require）。
- exec git 一律参数数组不走 shell；`procx.HideWindow(cmd)`；env 追加 `GIT_TERMINAL_PROMPT=0`（防凭据提示挂起）；本地操作超时 10s、网络操作（clone/pull/push）超时 60s。
- commit 一律带 `-c user.name="OpenKnowledge Sync" -c user.email="sync@openknowledge.local"`，不依赖用户全局 git 配置。
- `git init` 统一 `git init -b main`（默认分支钉死 main）。
- 状态/配置文件写一律 `fsx.WriteFile` 原子写；registry 写必须 `registry.Update`。
- **新增 ok 子命令必须同步 `internal/daemon/forward_cli.go` 的 `cliSubcommands` map**（forward_cli.go:14-16 注释明示）。
- e2e 子进程必须复用 `tests/e2e/integration_test.go:44-60` 的 `runOK`（全量 agent home 隔离变量已内置，不得另起 runner 删减）。
- 同步失败 fail-open：仅记日志与状态文件，绝不影响 hook 注入等本地链路。
- commit 遵循仓库惯例 `feat(syncx): ...` / `feat(cli): ...` 等；最终任务统一补变更日志。
- 本计划不做：冲突解决原语（ConflictFiles/ConflictVersions/ResolveFile 等，Plan B）、`llm_assist` 配置（Plan B）、okserver/serverx（Plan C）。

---

### Task 1: syncx git 执行器

**Files:**
- Create: `internal/syncx/git.go`
- Test: `internal/syncx/git_test.go`

**Interfaces:**
- Consumes: `internal/procx.HideWindow(cmd *exec.Cmd)`（procx_windows.go:17，非 Windows 空实现）。
- Produces:
  - `func execGit(dir string, timeout time.Duration, args ...string) (string, error)` — 后续所有任务经它跑 git；
  - `var ErrGitNotFound = errors.New(...)`；`var ErrTimeout = errors.New(...)`；
  - `type ExitError struct{ Code int; Output string }`（实现 `Error() string`）；
  - `var localTimeout = 10 * time.Second`、`var networkTimeout = 60 * time.Second`（包级 var，测试可调小）。

- [ ] **Step 1: Write the failing test**

```go
package syncx

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExecGitVersion(t *testing.T) {
	out, err := execGit(t.TempDir(), localTimeout, "--version")
	if err != nil {
		t.Skipf("git 不可用，跳过：%v", err)
	}
	if !strings.Contains(out, "git version") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecGitNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // 空目录，git 必然找不到
	_, err := execGit(t.TempDir(), localTimeout, "--version")
	if !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("want ErrGitNotFound, got %v", err)
	}
}

func TestExecGitExitError(t *testing.T) {
	_, err := execGit(t.TempDir(), localTimeout, "status") // 非仓目录
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want *ExitError, got %T %v", err, err)
	}
	if ee.Code == 0 || !strings.Contains(ee.Output, "not a git repository") {
		t.Fatalf("unexpected ExitError: %+v", ee)
	}
}

func TestExecGitTimeout(t *testing.T) {
	old := localTimeout
	defer func() { _ = old }()
	_, err := execGit(t.TempDir(), time.Nanosecond, "status")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/syncx/ -run TestExecGit -v`
Expected: 编译失败 `undefined: execGit`（包不存在）。

- [ ] **Step 3: Write minimal implementation**

```go
// Package syncx 在项目数据目录里执行 git：个人多端同步的单机引擎。
// 叶子包——仅依赖 fsx（原子写状态），不依赖其他 internal 包。
// 纪律（设计文档 §4）：参数数组调系统 git，不走 shell；网络操作给足超时；
// GIT_TERMINAL_PROMPT=0 防凭据提示挂起。
package syncx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"openknowledge/internal/procx"
)

var (
	// ErrGitNotFound 表示系统未安装 git 或不在 PATH（同步禁用，本地功能不受影响）。
	ErrGitNotFound = errors.New("未找到 git（请安装并加入 PATH）")
	// ErrTimeout 表示 git 执行超时（网络盘/远端无响应等）。
	ErrTimeout = errors.New("git 执行超时")

	localTimeout   = 10 * time.Second // 本地操作上限
	networkTimeout = 60 * time.Second // clone/pull/push 上限
)

// ExitError 是 git 非零退出的结构化错误，Output 含 stdout+stderr 合并输出。
type ExitError struct {
	Code   int
	Output string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("git 退出码 %d: %s", e.Code, strings.TrimSpace(e.Output))
}

// execGit 执行 git -C dir <args>，返回合并输出。错误三分类：
// ErrGitNotFound / ErrTimeout / *ExitError。
func execGit(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	procx.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), ErrTimeout
	}
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), &ExitError{Code: ee.ExitCode(), Output: string(out)}
	}
	// 启动失败（找不到可执行文件等）
	return string(out), fmt.Errorf("%w: %v", ErrGitNotFound, err)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/syncx/ -v`
Expected: 4 个测试 PASS（无 git 的机器 TestExecGitVersion 为 SKIP）。

- [ ] **Step 5: Commit**

```bash
git add internal/syncx/git.go internal/syncx/git_test.go
git commit -m "feat(syncx): git 执行器与错误三分类"
```

---

### Task 2: Repo 基础原语

**Files:**
- Create: `internal/syncx/repo.go`
- Test: `internal/syncx/repo_test.go`

**Interfaces:**
- Consumes: Task 1 的 `execGit`、`localTimeout`。
- Produces（后续任务依赖的签名，不得改名）：
  - `type Repo struct{ Dir string }`
  - `func Open(dir string) *Repo`
  - `func (r *Repo) IsRepo() bool`
  - `func (r *Repo) Init() error` — `git init -b main` + 写 `.gitignore`
  - `func (r *Repo) RemoteURL() string` — 无 remote 返回 `""`
  - `func (r *Repo) SetRemote(url string) error` — 无则 add、有则 set-url
  - `func (r *Repo) CurrentBranch() string` — 无提交时（unborn HEAD）返回 `"main"`

- [ ] **Step 1: Write the failing test**

```go
package syncx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoLifecycle(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	if r.IsRepo() {
		t.Fatal("empty dir should not be a repo")
	}
	if err := r.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !r.IsRepo() {
		t.Fatal("after Init should be a repo")
	}
	if got := r.CurrentBranch(); got != "main" {
		t.Fatalf("branch = %q, want main", got)
	}
	ign, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf(".gitignore missing: %v", err)
	}
	for _, want := range []string{"kb.db", "kb.db-*", "state/", "*.log"} {
		if !strings.Contains(string(ign), want) {
			t.Fatalf(".gitignore missing %q:\n%s", want, ign)
		}
	}
	if got := r.RemoteURL(); got != "" {
		t.Fatalf("remote = %q, want empty", got)
	}
	if err := r.SetRemote("https://example.com/a/b.git"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}
	if got := r.RemoteURL(); got != "https://example.com/a/b.git" {
		t.Fatalf("remote = %q", got)
	}
	// 重复 SetRemote 应覆盖而非报错
	if err := r.SetRemote("https://example.com/a/c.git"); err != nil {
		t.Fatalf("SetRemote update: %v", err)
	}
	if got := r.RemoteURL(); got != "https://example.com/a/c.git" {
		t.Fatalf("remote after update = %q", got)
	}
}
```

（文件头加 `import "strings"`。）

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/syncx/ -run TestRepoLifecycle -v`
Expected: 编译失败 `undefined: Open`。

- [ ] **Step 3: Write minimal implementation**

```go
package syncx

import (
	"os"
	"path/filepath"
)

// Repo 是一个项目数据目录对应的 git 仓视图。
type Repo struct {
	Dir string
}

// Open 返回 dir 的仓视图（不校验是否为仓，校验用 IsRepo）。
func Open(dir string) *Repo { return &Repo{Dir: dir} }

// IsRepo 报告 dir 是否已是 git 仓。
func (r *Repo) IsRepo() bool {
	_, err := execGit(r.Dir, localTimeout, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

// gitignoreContent 随 git init 生成（设计文档 §3）：索引/状态/日志不入仓。
const gitignoreContent = "kb.db\nkb.db-*\nstate/\n*.log\n"

// Init 原地成仓：git init -b main + 写 .gitignore。文件不挪动。
func (r *Repo) Init() error {
	if _, err := execGit(r.Dir, localTimeout, "init", "-b", "main"); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.Dir, ".gitignore"), []byte(gitignoreContent), 0o644)
}

// RemoteURL 返回 origin 的 URL；未配置返回 ""。
func (r *Repo) RemoteURL() string {
	out, err := execGit(r.Dir, localTimeout, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return out
}

// SetRemote 配置 origin（不存在则 add，存在则改地址）。
func (r *Repo) SetRemote(url string) error {
	if r.RemoteURL() == "" {
		_, err := execGit(r.Dir, localTimeout, "remote", "add", "origin", url)
		return err
	}
	_, err := execGit(r.Dir, localTimeout, "remote", "set-url", "origin", url)
	return err
}

// CurrentBranch 返回当前分支名；尚无提交（unborn HEAD）时返回 main。
func (r *Repo) CurrentBranch() string {
	out, err := execGit(r.Dir, localTimeout, "branch", "--show-current")
	if err != nil || out == "" {
		return "main"
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/syncx/ -v`
Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/syncx/repo.go internal/syncx/repo_test.go
git commit -m "feat(syncx): Repo 基础原语（init/remote/branch）"
```

---

### Task 3: Status / CommitAll / Push / CloneToDir

**Files:**
- Modify: `internal/syncx/repo.go`（追加方法）
- Test: `internal/syncx/repo_test.go`（追加用例）

**Interfaces:**
- Consumes: Task 1-2 全部。
- Produces:
  - `func (r *Repo) Status() (dirty bool, ahead, behind int)` — fail-open 零值；ahead/behind 仅在存在 upstream 时非零
  - `func (r *Repo) CommitAll(msg string) (committed bool, err error)` — `add -A` 后有暂存变更才 commit
  - `func (r *Repo) Push() error` — 无 upstream 时 `push -u origin <branch>`
  - `func (r *Repo) CloneToDir(url string) error` — clone 到临时目录 → 移 `.git` 进 Dir → `checkout -- .`（允许 Dir 内有骨架文件）

- [ ] **Step 1: Write the failing test**

```go
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommitAllAndStatus(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// 无变更：committed=false
	committed, err := r.CommitAll("sync: test")
	if err != nil || committed {
		t.Fatalf("empty commit: committed=%v err=%v", committed, err)
	}
	writeFile(t, dir, "INDEX.md", "# index\n")
	dirty, _, _ := r.Status()
	if !dirty {
		t.Fatal("should be dirty after new file")
	}
	committed, err = r.CommitAll("sync: test")
	if err != nil || !committed {
		t.Fatalf("commit: committed=%v err=%v", committed, err)
	}
	dirty, ahead, behind := r.Status()
	if dirty {
		t.Fatal("should be clean after commit")
	}
	// 无 upstream：ahead/behind 为零值（fail-open）
	if ahead != 0 || behind != 0 {
		t.Fatalf("no upstream: ahead=%d behind=%d", ahead, behind)
	}
}

func TestPushPullRoundtrip(t *testing.T) {
	bare := t.TempDir()
	if _, err := execGit(bare, localTimeout, "init", "--bare", "-b", "main"); err != nil {
		t.Fatalf("bare init: %v", err)
	}
	// 设备 A：init + commit + push -u
	dirA := t.TempDir()
	ra := Open(dirA)
	if err := ra.Init(); err != nil {
		t.Fatalf("A Init: %v", err)
	}
	writeFile(t, dirA, "a.md", "v1\n")
	if _, err := ra.CommitAll("sync: a"); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	if err := ra.SetRemote(bare); err != nil {
		t.Fatalf("A SetRemote: %v", err)
	}
	if err := ra.Push(); err != nil {
		t.Fatalf("A Push: %v", err)
	}
	// 设备 B：目录内有骨架文件（模拟 ok init 生成的 config.toml/state/），CloneToDir 应覆盖
	dirB := t.TempDir()
	writeFile(t, dirB, "config.toml", "# local skeleton\n")
	rb := Open(dirB)
	if err := rb.CloneToDir(bare); err != nil {
		t.Fatalf("B CloneToDir: %v", err)
	}
	if !rb.IsRepo() {
		t.Fatal("B should be a repo after CloneToDir")
	}
	data, err := os.ReadFile(filepath.Join(dirB, "a.md"))
	if err != nil || string(data) != "v1\n" {
		t.Fatalf("B a.md: %v %q", err, data)
	}
	// A 再推一个提交，B 能 pull 到（PullRebase 属 Task 4，这里仅验证 ahead/behind 计数）
	writeFile(t, dirA, "b.md", "v2\n")
	if _, err := ra.CommitAll("sync: a2"); err != nil {
		t.Fatalf("A commit2: %v", err)
	}
	if err := ra.Push(); err != nil {
		t.Fatalf("A Push2: %v", err)
	}
	if _, err := rbCloneFetch(rb); err != nil {
		t.Fatalf("B fetch: %v", err)
	}
	_, _, behind := rb.Status()
	if behind != 1 {
		t.Fatalf("B behind = %d, want 1", behind)
	}
}

// rbCloneFetch 是测试内 helper：fetch 让 behind 计数可见。
func rbCloneFetch(r *Repo) (string, error) {
	return execGit(r.Dir, networkTimeout, "fetch", "origin")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/syncx/ -run 'TestCommitAllAndStatus|TestPushPullRoundtrip' -v`
Expected: 编译失败（`Status`/`CommitAll`/`Push`/`CloneToDir` 未定义）。

- [ ] **Step 3: Write minimal implementation**

追加到 `internal/syncx/repo.go`：

```go
// Status 返回工作区状态：dirty = 有未提交变更；ahead/behind 仅在有 upstream 时非零。
// 失败一律 fail-open 零值（状态展示不影响主链路）。
func (r *Repo) Status() (dirty bool, ahead, behind int) {
	out, err := execGit(r.Dir, localTimeout, "status", "--porcelain")
	dirty = err == nil && out != ""
	ab, err := execGit(r.Dir, localTimeout, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	if err != nil {
		return dirty, 0, 0
	}
	var a, b int
	if _, err := fmt.Sscanf(ab, "%d\t%d", &a, &b); err == nil {
		ahead, behind = a, b
	}
	return dirty, ahead, behind
}

// CommitAll 暂存全部变更；有内容才提交（身份内置，不依赖用户全局配置）。
func (r *Repo) CommitAll(msg string) (bool, error) {
	if _, err := execGit(r.Dir, localTimeout, "add", "-A"); err != nil {
		return false, err
	}
	if _, err := execGit(r.Dir, localTimeout, "diff", "--cached", "--quiet"); err == nil {
		return false, nil // 无暂存变更
	}
	if _, err := execGit(r.Dir, localTimeout,
		"-c", "user.name=OpenKnowledge Sync", "-c", "user.email=sync@openknowledge.local",
		"commit", "-m", msg); err != nil {
		return false, err
	}
	return true, nil
}

// Push 推送当前分支；尚无 upstream 时建立跟踪。
func (r *Repo) Push() error {
	if _, err := execGit(r.Dir, localTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err != nil {
		_, err = execGit(r.Dir, networkTimeout, "push", "-u", "origin", r.CurrentBranch())
		return err
	}
	_, err := execGit(r.Dir, networkTimeout, "push")
	return err
}

// CloneToDir 把 url 克进 Dir——Dir 允许含骨架文件（config.toml/state/ 等）：
// clone 到临时目录 → 移 .git 进来 → checkout 覆盖工作区（设计文档 §14"无知识内容"情形）。
func (r *Repo) CloneToDir(url string) error {
	tmp, err := os.MkdirTemp("", "ok-sync-clone")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := execGit("", networkTimeout, "clone", url, tmp); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(tmp, ".git"), filepath.Join(r.Dir, ".git")); err != nil {
		return err
	}
	_, err = execGit(r.Dir, localTimeout, "checkout", "--", ".")
	return err
}
```

（文件头 import 需加 `"fmt"`。）

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/syncx/ -v`
Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/syncx/repo.go internal/syncx/repo_test.go
git commit -m "feat(syncx): Status/CommitAll/Push/CloneToDir"
```

---

### Task 4: PullRebase 冲突检测 + Sync 编排 + single-flight

**Files:**
- Create: `internal/syncx/sync.go`
- Test: `internal/syncx/sync_test.go`

**Interfaces:**
- Consumes: Task 1-3 全部。
- Produces:
  - `func (r *Repo) PullRebase() (conflicts []string, err error)` — 冲突时返回文件列表且 err=nil（rebase 停在半途）
  - `type Outcome struct { Committed bool; Pulled, Pushed int; Conflicts []string; Err error; NoRemote, NotRepo bool }`
  - `func (r *Repo) Sync(msg string) Outcome` — add -A → commit → pull --rebase → push（设计文档 §7）
  - `func SyncOnce(dir, msg string) Outcome` — 包级 single-flight：同 dir 并发合并为一次执行，后到者等同结果

- [ ] **Step 1: Write the failing test**

```go
package syncx

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// mkPair 建 bare + 两台已关联设备仓，返回 (bare, dirA, dirB)。
func mkPair(t *testing.T) (string, string, string) {
	t.Helper()
	bare := t.TempDir()
	if _, err := execGit(bare, localTimeout, "init", "--bare", "-b", "main"); err != nil {
		t.Fatalf("bare: %v", err)
	}
	dirA := t.TempDir()
	ra := Open(dirA)
	if err := ra.Init(); err != nil {
		t.Fatalf("A init: %v", err)
	}
	writeFile(t, dirA, "k.md", "v1\n")
	if _, err := ra.CommitAll("init"); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	if err := ra.SetRemote(bare); err != nil {
		t.Fatalf("A remote: %v", err)
	}
	if err := ra.Push(); err != nil {
		t.Fatalf("A push: %v", err)
	}
	dirB := t.TempDir()
	if err := Open(dirB).CloneToDir(bare); err != nil {
		t.Fatalf("B clone: %v", err)
	}
	return bare, dirA, dirB
}

func TestSyncNoRemote(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	writeFile(t, dir, "k.md", "v1\n")
	o := r.Sync("sync: test")
	if o.Err != nil || !o.NoRemote || !o.Committed {
		t.Fatalf("outcome: %+v", o)
	}
}

func TestSyncNotRepo(t *testing.T) {
	o := Open(t.TempDir()).Sync("sync: test")
	if !o.NotRepo {
		t.Fatalf("want NotRepo: %+v", o)
	}
}

func TestSyncPushPullCounts(t *testing.T) {
	_, dirA, dirB := mkPair(t)
	// A 改并 sync → 推 1
	writeFile(t, dirA, "k.md", "v2\n")
	o := Open(dirA).Sync("sync: a")
	if o.Err != nil || o.Pushed != 1 || o.Pulled != 0 {
		t.Fatalf("A outcome: %+v err=%v", o, o.Err)
	}
	// B sync → 拉 1
	o = Open(dirB).Sync("sync: b")
	if o.Err != nil || o.Pulled != 1 || o.Pushed != 0 {
		t.Fatalf("B outcome: %+v err=%v", o, o.Err)
	}
	data, _ := os.ReadFile(filepath.Join(dirB, "k.md"))
	if string(data) != "v2\n" {
		t.Fatalf("B content: %q", data)
	}
	// 再 sync：已是最新
	o = Open(dirB).Sync("sync: b")
	if o.Err != nil || o.Pulled != 0 || o.Pushed != 0 || o.Committed {
		t.Fatalf("B idle outcome: %+v", o)
	}
}

func TestSyncConflictStopsPush(t *testing.T) {
	_, dirA, dirB := mkPair(t)
	// A 改同一行为 v2a 并推
	writeFile(t, dirA, "k.md", "v2a\n")
	if o := Open(dirA).Sync("sync: a"); o.Err != nil {
		t.Fatalf("A sync: %v", o.Err)
	}
	// B 改同一行为 v2b 并 sync → 冲突
	writeFile(t, dirB, "k.md", "v2b\n")
	o := Open(dirB).Sync("sync: b")
	if o.Err != nil {
		t.Fatalf("conflict should not be Err: %v", o.Err)
	}
	if len(o.Conflicts) != 1 || o.Conflicts[0] != "k.md" {
		t.Fatalf("conflicts: %v", o.Conflicts)
	}
	if o.Pushed != 0 {
		t.Fatalf("must not push on conflict: %+v", o)
	}
	// 内容不丢：工作区含 B 的改动（冲突标记内）
	data, _ := os.ReadFile(filepath.Join(dirB, "k.md"))
	if !strings.Contains(string(data), "v2b") {
		t.Fatalf("B content lost: %q", data)
	}
	// rebase 停在半途
	if _, err := execGit(dirB, localTimeout, "rev-parse", "--verify", "--quiet", "REBASE_HEAD"); err != nil {
		t.Fatal("rebase should be in progress")
	}
	// 清理
	_, _ = execGit(dirB, localTimeout, "rebase", "--abort")
}

func TestSyncOnceSingleFlight(t *testing.T) {
	_, dirA, _ := mkPair(t)
	writeFile(t, dirA, "k.md", "v3\n")
	const n = 8
	var wg sync.WaitGroup
	outs := make([]Outcome, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outs[i] = SyncOnce(dirA, "sync: concurrent")
		}(i)
	}
	wg.Wait()
	pushed := 0
	for _, o := range outs {
		if o.Err != nil {
			t.Fatalf("err: %v", o.Err)
		}
		pushed += o.Pushed
	}
	// 并发合并：最多一次执行真的推出 1 个提交；其余等同结果或零
	if pushed > 1 {
		t.Fatalf("single-flight violated: pushed total %d", pushed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/syncx/ -run 'TestSync' -v`
Expected: 编译失败（`Outcome`/`Sync`/`SyncOnce`/`PullRebase` 未定义）。

- [ ] **Step 3: Write minimal implementation**

```go
package syncx

import (
	"strings"
	"sync"
)

// Outcome 是 Sync 编排的结构化结果（设计文档 §7）。
// 冲突不是错误——Conflicts 非空且 Err 为 nil 表示停在 rebase 半途等人解决。
type Outcome struct {
	Committed bool     // 本次有新本地提交
	Pulled    int      // 拉取的提交数
	Pushed    int      // 推送的提交数
	Conflicts []string // 非空 = pull 冲突，已停止 push
	Err       error    // 真错误（git 缺失/网络/权限等）
	NoRemote  bool     // 未配置远端（仅本地历史）
	NotRepo   bool     // 目录不是 git 仓（需要先 ok sync init）
}

// PullRebase 拉取并变基。返回冲突文件列表（此时 rebase 停在半途，err=nil）。
func (r *Repo) PullRebase() ([]string, error) {
	if _, err := execGit(r.Dir, networkTimeout, "pull", "--rebase"); err != nil {
		// rebase 进行中 → 结构化冲突而非错误
		if _, rbErr := execGit(r.Dir, localTimeout, "rev-parse", "--verify", "--quiet", "REBASE_HEAD"); rbErr == nil {
			out, _ := execGit(r.Dir, localTimeout, "diff", "--name-only", "--diff-filter=U")
			var files []string
			if out != "" {
				files = strings.Split(out, "\n")
			}
			return files, nil
		}
		return nil, err
	}
	return nil, nil
}

// Sync 一次执行 = add -A → 有变更则 commit →（有远端）pull --rebase → push。
func (r *Repo) Sync(msg string) (o Outcome) {
	if !r.IsRepo() {
		o.NotRepo = true
		return o
	}
	_, _, behindBefore := r.Status()
	committed, err := r.CommitAll(msg)
	if err != nil {
		o.Err = err
		return o
	}
	o.Committed = committed
	if r.RemoteURL() == "" {
		o.NoRemote = true
		return o
	}
	conflicts, err := r.PullRebase()
	if err != nil {
		o.Err = err
		return o
	}
	if len(conflicts) > 0 {
		o.Conflicts = conflicts // 停止后续 push（设计文档 §7）
		return o
	}
	o.Pulled = behindBefore
	_, ahead, _ := r.Status()
	if err := r.Push(); err != nil {
		o.Err = err
		return o
	}
	o.Pushed = ahead
	return o
}

// single-flight：同 dir 并发请求合并为一次执行（设计文档 §4 串行化）。
var flights sync.Map // dir → *flight

type flight struct {
	done chan struct{}
	o    Outcome
}

// SyncOnce 对同 dir 的并发调用只执行一次，后到者等待并拿同一结果。
func SyncOnce(dir, msg string) Outcome {
	f := &flight{done: make(chan struct{})}
	actual, loaded := flights.LoadOrStore(dir, f)
	if loaded {
		af := actual.(*flight)
		<-af.done
		return af.o
	}
	defer flights.Delete(dir)
	f.o = Open(dir).Sync(msg)
	close(f.done)
	return f.o
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/syncx/ -v`
Expected: 全部 PASS（含 single-flight）。

- [ ] **Step 5: Commit**

```bash
git add internal/syncx/sync.go internal/syncx/sync_test.go
git commit -m "feat(syncx): Sync 编排、冲突检测与 single-flight"
```

---

### Task 5: 状态文件（sync-status.json 分层 + sync-conflict.json）

**Files:**
- Create: `internal/syncx/status.go`
- Test: `internal/syncx/status_test.go`

**Interfaces:**
- Consumes: `internal/fsx.WriteFile(path, data, perm)`（fsx.go:14，原子写）；Task 3 的 `Status()`、Task 4 的 `Outcome`。
- Produces:
  - `type LayerStatus struct { LastSync time.Time; Ahead, Behind int; Conflict bool; LastError string }`（json 标签见代码）
  - `type StatusFile struct { Layers map[string]*LayerStatus }`
  - `func LoadStatus(stateDir string) (*StatusFile, error)` — 不存在返回空骨架
  - `func (s *StatusFile) Layer(name string) *LayerStatus` — 不存在则创建
  - `func (s *StatusFile) Save(stateDir string) error`
  - `func WriteConflictFiles(stateDir string, files []string) error`
  - `func ReadConflictFiles(stateDir string) ([]string, error)`
  - `func ClearConflictFiles(stateDir string) error`
  - `func RecordOutcome(dir, stateDir string, o Outcome)` — CLI/daemon 共用的状态回写收敛函数

- [ ] **Step 1: Write the failing test**

```go
package syncx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatusRoundtrip(t *testing.T) {
	stateDir := t.TempDir()
	sf, err := LoadStatus(stateDir)
	if err != nil {
		t.Fatalf("LoadStatus empty: %v", err)
	}
	l := sf.Layer("personal")
	l.Ahead = 2
	l.LastError = ""
	if err := sf.Save(stateDir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	sf2, err := LoadStatus(stateDir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if sf2.Layer("personal").Ahead != 2 {
		t.Fatalf("ahead = %d", sf2.Layer("personal").Ahead)
	}
}

func TestConflictFilesLifecycle(t *testing.T) {
	stateDir := t.TempDir()
	if files, _ := ReadConflictFiles(stateDir); len(files) != 0 {
		t.Fatalf("initial: %v", files)
	}
	if err := WriteConflictFiles(stateDir, []string{"a.md", "b.md"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	files, err := ReadConflictFiles(stateDir)
	if err != nil || len(files) != 2 || files[0] != "a.md" {
		t.Fatalf("read: %v %v", files, err)
	}
	if err := ClearConflictFiles(stateDir); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sync-conflict.json")); !os.IsNotExist(err) {
		t.Fatalf("file should be gone: %v", err)
	}
}

func TestRecordOutcome(t *testing.T) {
	// 真仓（无远端）+ 状态目录
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	writeFile(t, dir, "k.md", "v1\n")
	stateDir := t.TempDir()

	// 成功：last_sync 落时间、conflict 清空
	o := r.Sync("sync: test")
	RecordOutcome(dir, stateDir, o)
	sf, _ := LoadStatus(stateDir)
	l := sf.Layer("personal")
	if l.LastSync.IsZero() || l.Conflict || l.LastError != "" {
		t.Fatalf("after success: %+v", l)
	}

	// 冲突：conflict=true 且写冲突文件
	RecordOutcome(dir, stateDir, Outcome{Conflicts: []string{"k.md"}})
	sf, _ = LoadStatus(stateDir)
	if !sf.Layer("personal").Conflict {
		t.Fatal("conflict not recorded")
	}
	files, _ := ReadConflictFiles(stateDir)
	if len(files) != 1 || files[0] != "k.md" {
		t.Fatalf("conflict files: %v", files)
	}

	// 错误：last_error 记录，不清 conflict
	RecordOutcome(dir, stateDir, Outcome{Err: os.ErrNotExist})
	sf, _ = LoadStatus(stateDir)
	if sf.Layer("personal").LastError == "" {
		t.Fatal("last_error not recorded")
	}

	// 再次成功：conflict 清除、冲突文件删除
	RecordOutcome(dir, stateDir, Outcome{})
	sf, _ = LoadStatus(stateDir)
	if sf.Layer("personal").Conflict || sf.Layer("personal").LastError != "" {
		t.Fatalf("recovery: %+v", sf.Layer("personal"))
	}
	if files, _ := ReadConflictFiles(stateDir); len(files) != 0 {
		t.Fatalf("conflict files should be cleared: %v", files)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/syncx/ -run 'TestStatus|TestConflict|TestRecordOutcome' -v`
Expected: 编译失败（`LayerStatus`/`StatusFile` 等未定义）。

- [ ] **Step 3: Write minimal implementation**

```go
package syncx

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"openknowledge/internal/fsx"
)

// LayerStatus 是一层（personal/team）的同步状态——从第一天就按层建模（设计文档 §8）。
type LayerStatus struct {
	LastSync  time.Time `json:"last_sync"`
	Ahead     int       `json:"ahead"`
	Behind    int       `json:"behind"`
	Conflict  bool      `json:"conflict"`
	LastError string    `json:"last_error"`
}

// StatusFile 对应 state/sync-status.json。
type StatusFile struct {
	Layers map[string]*LayerStatus `json:"layers"`
}

func statusPath(stateDir string) string  { return filepath.Join(stateDir, "sync-status.json") }
func conflictPath(stateDir string) string { return filepath.Join(stateDir, "sync-conflict.json") }

// LoadStatus 读状态文件；不存在或损坏返回空骨架（fail-open）。
func LoadStatus(stateDir string) (*StatusFile, error) {
	sf := &StatusFile{Layers: map[string]*LayerStatus{}}
	data, err := os.ReadFile(statusPath(stateDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sf, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, sf); err != nil {
		return &StatusFile{Layers: map[string]*LayerStatus{}}, nil
	}
	if sf.Layers == nil {
		sf.Layers = map[string]*LayerStatus{}
	}
	return sf, nil
}

// Layer 取层状态，不存在则创建。
func (s *StatusFile) Layer(name string) *LayerStatus {
	if s.Layers == nil {
		s.Layers = map[string]*LayerStatus{}
	}
	if s.Layers[name] == nil {
		s.Layers[name] = &LayerStatus{}
	}
	return s.Layers[name]
}

// Save 原子写状态文件。
func (s *StatusFile) Save(stateDir string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFile(statusPath(stateDir), data, 0o644)
}

type conflictFile struct {
	Files []string  `json:"files"`
	Since time.Time `json:"since"`
}

// WriteConflictFiles 写 state/sync-conflict.json（GUI 冲突页数据源，syncx 独占写）。
func WriteConflictFiles(stateDir string, files []string) error {
	data, err := json.MarshalIndent(conflictFile{Files: files, Since: time.Now()}, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFile(conflictPath(stateDir), data, 0o644)
}

// ReadConflictFiles 读冲突文件列表；不存在返回空。
func ReadConflictFiles(stateDir string) ([]string, error) {
	data, err := os.ReadFile(conflictPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cf conflictFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, nil
	}
	return cf.Files, nil
}

// ClearConflictFiles 删除冲突状态文件（解决完成后调）。
func ClearConflictFiles(stateDir string) error {
	if err := os.Remove(conflictPath(stateDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// RecordOutcome 是 CLI/daemon 共用的状态回写收敛：按 Outcome 更新
// sync-status.json 的 personal 层与 sync-conflict.json。失败仅尽力而为（fail-open）。
func RecordOutcome(dir, stateDir string, o Outcome) {
	sf, err := LoadStatus(stateDir)
	if err != nil {
		return
	}
	l := sf.Layer("personal")
	switch {
	case o.Err != nil:
		l.LastError = o.Err.Error()
	case len(o.Conflicts) > 0:
		l.Conflict = true
		l.LastError = ""
		_ = WriteConflictFiles(stateDir, o.Conflicts)
	default:
		l.LastSync = time.Now()
		l.Conflict = false
		l.LastError = ""
		_ = ClearConflictFiles(stateDir)
	}
	if Open(dir).IsRepo() {
		_, l.Ahead, l.Behind = Open(dir).Status()
	}
	_ = sf.Save(stateDir)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/syncx/ -v`
Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/syncx/status.go internal/syncx/status_test.go
git commit -m "feat(syncx): 分层同步状态与冲突状态文件"
```

---

### Task 6: config [sync] 段 + SetSync 写入

**Files:**
- Modify: `internal/config/config.go`（Config 结构体约 :260-272、Default() 约 :274-290，文件尾追加 SetSync）
- Test: `internal/config/config_test.go`（无则新建）

**Interfaces:**
- Consumes: 既有 `LoadMerged(projectPath, globalPath)`（config.go:315）、`fsx.WithFileLock`（lock.go:30）。
- Produces:
  - `type Sync struct { Enabled bool; Remote string; AutoIntervalMin int }`（toml 标签 `enabled`/`remote`/`auto_interval_min`）
  - `Config.Sync Sync ` + `Default()` 登记 `{Enabled: false, Remote: "", AutoIntervalMin: 5}`
  - `func SetSync(path string, s Sync) error` — 行级小节读-改-写（照 SetEnforceRules 先例 config.go:368）

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Sync.Enabled || cfg.Sync.Remote != "" || cfg.Sync.AutoIntervalMin != 5 {
		t.Fatalf("defaults: %+v", cfg.Sync)
	}
}

func TestSyncMerged(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	project := filepath.Join(dir, "project.toml")
	if err := os.WriteFile(global, []byte("[sync]\nenabled = true\nremote = \"http://nas:3000/u/ok-x.git\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte("[sync]\nauto_interval_min = 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadMerged(project, global)
	if err != nil {
		t.Fatalf("LoadMerged: %v", err)
	}
	// 项目未覆盖的字段继承 global；覆盖的字段取项目值
	if !cfg.Sync.Enabled || cfg.Sync.Remote != "http://nas:3000/u/ok-x.git" || cfg.Sync.AutoIntervalMin != 10 {
		t.Fatalf("merged: %+v", cfg.Sync)
	}
}

func TestSetSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("# 注释行\n[retrieve]\ntop_n = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetSync(path, Sync{Enabled: true, Remote: "http://nas/u/ok-x.git", AutoIntervalMin: 5}); err != nil {
		t.Fatalf("SetSync: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Sync.Enabled || cfg.Sync.Remote != "http://nas/u/ok-x.git" {
		t.Fatalf("after SetSync: %+v", cfg.Sync)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# 注释行") || !strings.Contains(string(data), "[retrieve]") {
		t.Fatalf("SetSync should preserve other content:\n%s", data)
	}
	// 幂等更新：改 remote 不重复追加段
	if err := SetSync(path, Sync{Enabled: true, Remote: "http://nas/v2/ok-x.git", AutoIntervalMin: 3}); err != nil {
		t.Fatalf("SetSync update: %v", err)
	}
	data, _ = os.ReadFile(path)
	if strings.Count(string(data), "[sync]") != 1 {
		t.Fatalf("duplicate [sync] section:\n%s", data)
	}
}
```

（文件头 import 需 `"strings"`；若 config_test.go 已存在则把三个用例追加进去并合并 import。）

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestSync -v`
Expected: 编译失败 `undefined: SetSync` / `cfg.Sync`。

- [ ] **Step 3: Write minimal implementation**

`internal/config/config.go` 三处修改：

a) Config 结构体加字段（紧跟 `LLM LLM \`toml:"llm"\`` 一行后）：

```go
	// Sync 同步配置。内部即按 personal 层建模——团队版演进为
	// [sync.personal]+[sync.team] 时本结构平移到层下（设计文档 §13 口子 1）。
	Sync Sync `toml:"sync"`
```

b) 类型定义 + Default() 登记：

```go
// Sync 是项目级 [sync] 段（设计文档 §12）。
type Sync struct {
	Enabled         bool   `toml:"enabled"`
	Remote          string `toml:"remote"`
	AutoIntervalMin int    `toml:"auto_interval_min"`
}
```

`Default()` 返回结构里加：

```go
		Sync: Sync{Enabled: false, Remote: "", AutoIntervalMin: 5},
```

c) 文件尾追加 SetSync（行级小节读-改-写，保留其他内容与注释）：

```go
// SetSync 行级更新 path 的 [sync] 段（enabled/remote/auto_interval_min 三行），
// 保留文件其余内容；照 SetEnforceRules 的读-改-写先例，fsx 锁内完成。
func SetSync(path string, s Sync) error {
	return fsx.WithFileLock(path, func() error {
		data, _ := os.ReadFile(path)
		lines := strings.Split(string(data), "\n")
		var out []string
		inSync := false
		wrote := false
		section := []string{
			"[sync]",
			fmt.Sprintf("enabled = %t", s.Enabled),
		}
		if s.Remote != "" {
			section = append(section, fmt.Sprintf("remote = %q", s.Remote))
		}
		section = append(section, fmt.Sprintf("auto_interval_min = %d", s.AutoIntervalMin))
		for _, line := range lines {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
				if inSync && !wrote {
					out = append(out, section...)
					wrote = true
				}
				inSync = trim == "[sync]"
				if inSync {
					continue // 跳过旧 [sync] 段头，由 section 重写
				}
			} else if inSync {
				continue // 丢弃旧 [sync] 段体
			}
			out = append(out, line)
		}
		if !wrote {
			if inSync {
				out = append(out, section...)
			} else {
				for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
					out = out[:len(out)-1]
				}
				out = append(out, "", section...)
			}
		}
		return fsx.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
	})
}
```

（文件头 import 需有 `"fmt"` `"strings"` `"openknowledge/internal/fsx"`——config.go 已 import fsx，检查并补齐另外两个。）

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: 全部 PASS（含既有用例不回归）。

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): [sync] 段与 SetSync 行级写入"
```

---

### Task 7: `ok sync` 命令

**Files:**
- Modify: `internal/cli/cli.go`（追加 Sync/syncInit 函数；syncInit 属 Task 8，本任务只留分发）
- Modify: `cmd/ok/main.go:54` 附近（注册 case）
- Modify: `internal/daemon/forward_cli.go:17-36`（cliSubcommands 同步）

**Interfaces:**
- Consumes: `resolveFromCwd(stderr)`（cli.go:47-54，返回 `pc *project.Context` — 含 `pc.Store *store.Store`、`pc.Config config.Config`；**先读 cli.go:47-54 与 internal/project/project.go:20-35 确认字段名，若实际不同以实际为准**）；`syncx.SyncOnce`/`Outcome`/`RecordOutcome`（Task 4/5）；`pc.Config.Sync`（Task 6）。
- Produces:
  - `func Sync(args []string, stdout, stderr io.Writer) int` — `args[0]=="init"` 分发到 Task 8 的 `syncInit(args[1:], stdout, stderr)`

- [ ] **Step 1: 验证路径（无单测，由 Task 11 E2E 覆盖）**

CLI 层是薄壳（解析 → 调 syncx → 格式化输出），单元测试要搭 registry/cwd 夹具，性价比低；本任务的验证 = 编译 + Task 11 的 E2E 用例（双设备闭环走真二进制）。先写实现。

- [ ] **Step 2: Write implementation**

`internal/cli/cli.go` 追加：

```go
// Sync 是 ok sync 入口（设计文档 §7）：一次执行 = commit → pull --rebase → push。
// args[0]=="init" 时分发到 syncInit。
func Sync(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "init" {
		return syncInit(args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	if !pc.Config.Sync.Enabled {
		fmt.Fprintln(stderr, "同步未启用：先 ok sync init [remote-url] 初始化（或在项目 config.toml 设 [sync] enabled = true）")
		return 1
	}
	host, _ := os.Hostname()
	msg := fmt.Sprintf("sync: %s %s", host, time.Now().Format(time.RFC3339))
	o := syncx.SyncOnce(pc.Store.Root, msg)
	syncx.RecordOutcome(pc.Store.Root, pc.Store.StateDir(), o)
	switch {
	case o.NotRepo:
		fmt.Fprintln(stderr, "项目目录还不是 git 仓：先 ok sync init [remote-url] 初始化")
		return 1
	case o.Err != nil:
		fmt.Fprintf(stderr, "同步失败：%v（本地功能不受影响）\n", o.Err)
		return 1
	case len(o.Conflicts) > 0:
		fmt.Fprintf(stderr, "同步冲突：拉取时 %d 个文件冲突，已停止推送：\n", len(o.Conflicts))
		for _, f := range o.Conflicts {
			fmt.Fprintf(stderr, "  - %s\n", f)
		}
		fmt.Fprintf(stderr, "请到 Web GUI 冲突解决页处理，或手动解决后执行 git -C %s rebase --continue && ok sync\n", pc.Store.Root)
		return 1
	case o.NoRemote:
		if o.Committed {
			fmt.Fprintln(stdout, "已本地提交（无远端，仅本地历史）")
		} else {
			fmt.Fprintln(stdout, "已是最新（无远端，仅本地历史）")
		}
		return 0
	default:
		var parts []string
		if o.Pulled > 0 {
			parts = append(parts, fmt.Sprintf("拉取 %d 个提交", o.Pulled))
		}
		if o.Pushed > 0 {
			parts = append(parts, fmt.Sprintf("推送 %d 个提交", o.Pushed))
		}
		if len(parts) == 0 {
			if o.Committed {
				parts = append(parts, "本地提交已记录")
			} else {
				parts = append(parts, "已是最新")
			}
		}
		fmt.Fprintln(stdout, strings.Join(parts, "，"))
		return 0
	}
}
```

（cli.go import 需加 `"time"`（若无）、`"openknowledge/internal/syncx"`；`strings` 已有。）

`cmd/ok/main.go` 在 `case "propose":` 附近加：

```go
	case "sync":
		return cli.Sync(argv[2:], os.Stdout, os.Stderr)
```

`internal/daemon/forward_cli.go` 的 `cliSubcommands` map 加一行（与现有条目同形）：

```go
	"sync":          {},
```

（map 字面量形式以 forward_cli.go:17-36 实际代码为准——照既有条目样式追加。）

- [ ] **Step 3: 编译验证**

Run: `go build ./...`
Expected: 编译通过（syncInit 此时 undefined——先把 Task 8 的函数签名空壳一并写下，或与 Task 8 同批提交。**本计划约定 Task 7/8 一次提交**，见 Task 8 Step 4）。

- [ ] **Step 4: Commit（与 Task 8 合并提交）**

见 Task 8 Step 5。

---

### Task 8: `ok sync init` 命令（三情形）

**Files:**
- Modify: `internal/cli/cli.go`（追加 syncInit）

**Interfaces:**
- Consumes: Task 7 分发；`syncx.Open/IsRepo/Init/CommitAll/SetRemote/Push/CloneToDir`；`config.SetSync`（Task 6）；`syncx.ExitError`（Task 1）。
- Produces: `func syncInit(args []string, stdout, stderr io.Writer) int`

三情形（设计文档 §14）：

| 情形 | 行为 |
|---|---|
| 非仓、无知识内容、remote 非空 | CloneToDir |
| 非仓、有内容、remote 非空 | Init + commit + remote + push；push 遭 non-fast-forward → 报错指引手动合并 |
| remote 为空 | Init + 首个 commit（仅本地历史） |

"无知识内容"判定：`knowledge/` 下无 `.md` 文件（骨架文件 config.toml/state/ 不算内容）。

- [ ] **Step 1: Write implementation**

`internal/cli/cli.go` 追加：

```go
// syncInit 实现 ok sync init [remote-url]（设计文档 §14 三情形）。
func syncInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	remote := fs.Arg(0)
	pc, code := resolveFromCwd(stderr)
	if pc == nil {
		return code
	}
	r := syncx.Open(pc.Store.Root)
	if r.IsRepo() {
		fmt.Fprintln(stderr, "项目目录已是 git 仓，无需初始化（直接 ok sync）")
		return 1
	}
	host, _ := os.Hostname()
	commitMsg := fmt.Sprintf("sync: init %s %s", host, time.Now().Format(time.RFC3339))

	hasContent := func() bool {
		entries, _ := os.ReadDir(pc.Store.KnowledgeDir())
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				return true
			}
		}
		return false
	}

	finish := func() int {
		if err := config.SetSync(pc.Store.ConfigPath(), config.Sync{
			Enabled: true, Remote: remote, AutoIntervalMin: pc.Config.Sync.AutoIntervalMin,
		}); err != nil {
			fmt.Fprintf(stderr, "同步配置写入失败：%v\n", err)
			return 1
		}
		return 0
	}

	// 情形 1：仅本地历史
	if remote == "" {
		if err := r.Init(); err != nil {
			fmt.Fprintf(stderr, "git init 失败：%v\n", err)
			return 1
		}
		if _, err := r.CommitAll(commitMsg); err != nil {
			fmt.Fprintf(stderr, "首次提交失败：%v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "已初始化本地历史（未配置远端，后续可 git remote add 或在 config.toml 写 [sync] remote）")
		return finish()
	}

	// 情形 2：无知识内容 → clone
	if !hasContent() {
		if err := r.CloneToDir(remote); err != nil {
			fmt.Fprintf(stderr, "clone 失败：%v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "已从远端克隆项目知识库")
		return finish()
	}

	// 情形 3：本地有内容 → init + commit + remote + push（首台设备路径）
	if err := r.Init(); err != nil {
		fmt.Fprintf(stderr, "git init 失败：%v\n", err)
		return 1
	}
	if _, err := r.CommitAll(commitMsg); err != nil {
		fmt.Fprintf(stderr, "首次提交失败：%v\n", err)
		return 1
	}
	if err := r.SetRemote(remote); err != nil {
		fmt.Fprintf(stderr, "关联远端失败：%v\n", err)
		return 1
	}
	if err := r.Push(); err != nil {
		var ee *syncx.ExitError
		if errors.As(err, &ee) && (strings.Contains(ee.Output, "non-fast-forward") || strings.Contains(ee.Output, "fetch first")) {
			fmt.Fprintf(stderr, "远端仓已有内容，不自动合并。请手动合并一次后重试：\n  git -C %s pull --rebase origin main\n  （或 git merge --allow-unrelated-histories）\n", pc.Store.Root)
			return 1
		}
		fmt.Fprintf(stderr, "首次推送失败：%v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "已初始化并推送首个提交到远端")
	return finish()
}
```

（cli.go import 需加 `"errors"`（若无）、`"openknowledge/internal/config"`（若无——resolveFromCwd 所在包很可能已 import，确认）。）

- [ ] **Step 2: 编译验证**

Run: `go build ./... && go vet ./internal/cli/ ./internal/syncx/`
Expected: 通过。

- [ ] **Step 3: 手动冒烟（可选，E2E 在 Task 11 覆盖）**

```bash
tmp=$(mktemp -d); export OK_HOME=$tmp/home
mkdir -p $tmp/proj && cd $tmp/proj
ok init demo
echo "正文" > /tmp/body.md && ok add --title 测试 --type note --file /tmp/body.md
git init --bare $tmp/bare.git
ok sync init $tmp/bare.git   # 期望：已初始化并推送首个提交到远端
ok sync                       # 期望：已是最新
```

Expected: 各步输出与注释一致。

- [ ] **Step 4: 全量回归**

Run: `go test ./...`
Expected: 全部 PASS（既有测试不回归）。

- [ ] **Step 5: Commit（Task 7+8 合并）**

```bash
git add internal/cli/cli.go cmd/ok/main.go internal/daemon/forward_cli.go
git commit -m "feat(cli): ok sync / ok sync init 命令"
```

---

### Task 9: okd 同步 ticker

**Files:**
- Create: `internal/daemon/sync.go`
- Modify: `internal/daemon/run.go:143-144`（挂载点）
- Test: `internal/daemon/sync_test.go`

**Interfaces:**
- Consumes: `registry.Load(registry.DefaultPath())`（registry.go:76）、`store.New`（store.go:11）、`config.LoadMerged`（config.go:315）、`syncx.SyncOnce/RecordOutcome/LoadStatus`（Task 4/5）。
- Produces:
  - `func startSyncJanitor(stdout io.Writer)` — 每分钟检查一轮，到点项目跑 SyncOnce
  - `func runSyncCycle(out io.Writer, force bool)` — force=true 跳过 interval 判断（Task 10 防抖复用）
  - `var syncCheckInterval = time.Minute`（包级 var，测试可调）

- [ ] **Step 1: Write the failing test**

```go
package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"openknowledge/internal/registry"
	"openknowledge/internal/store"
	"openknowledge/internal/syncx"
)

// TestRunSyncCycle 起两个隔离 OK_HOME 项目：一个启用同步、一个未启用，
// 验证 janitor 只同步启用的项目并回写状态文件。
func TestRunSyncCycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	// bare 远端
	bare := t.TempDir()
	gitExec(t, bare, "init", "--bare", "-b", "main")
	// 项目 a：启用同步
	stA := store.New(filepath.Join(home, "projects", "a"))
	if err := os.MkdirAll(stA.KnowledgeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stA.StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	ra := syncx.Open(stA.Root)
	if err := ra.Init(); err != nil {
		t.Fatalf("a init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stA.KnowledgeDir(), "k.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ra.CommitAll("init"); err != nil {
		t.Fatalf("a commit: %v", err)
	}
	if err := ra.SetRemote(bare); err != nil {
		t.Fatalf("a remote: %v", err)
	}
	if err := os.WriteFile(stA.ConfigPath(), []byte("[sync]\nenabled = true\nremote = \""+bare+"\"\nauto_interval_min = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 项目 b：未启用同步（无 config）
	stB := store.New(filepath.Join(home, "projects", "b"))
	if err := os.MkdirAll(stB.KnowledgeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// 注册表
	reg := &registry.Registry{Projects: []registry.Project{{Name: "a", Paths: []string{"/x/a"}}, {Name: "b", Paths: []string{"/x/b"}}}}
	if err := reg.Save(); err != nil {
		t.Fatalf("registry save: %v", err)
	}

	runSyncCycle(io.Discard, true)

	sf, err := syncx.LoadStatus(stA.StateDir())
	if err != nil {
		t.Fatalf("a status: %v", err)
	}
	if sf.Layer("personal").LastSync.IsZero() {
		t.Fatal("a should have been synced")
	}
	if _, err := os.Stat(filepath.Join(stB.StateDir(), "sync-status.json")); !os.IsNotExist(err) {
		t.Fatalf("b must not be synced: %v", err)
	}
	// a 的首个提交应已推到 bare
	out := gitExec(t, bare, "log", "--oneline", "main")
	if out == "" {
		t.Fatal("bare should have commits")
	}
}

// gitExec 是测试 helper：直接 exec git（经 syncx 未导出的 execGit 不可用时退化为 os/exec）。
func gitExec(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// TestSyncDueInterval 验证 interval 判断：未到点跳过、到点执行。
func TestSyncDueInterval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	bare := t.TempDir()
	gitExec(t, bare, "init", "--bare", "-b", "main")
	st := store.New(filepath.Join(home, "projects", "a"))
	_ = os.MkdirAll(st.KnowledgeDir(), 0o755)
	_ = os.MkdirAll(st.StateDir(), 0o755)
	r := syncx.Open(st.Root)
	_ = r.Init()
	_, _ = r.CommitAll("init")
	_ = r.SetRemote(bare)
	_ = os.WriteFile(st.ConfigPath(), []byte("[sync]\nenabled = true\nremote = \""+bare+"\"\nauto_interval_min = 5\n"), 0o644)
	reg := &registry.Registry{Projects: []registry.Project{{Name: "a", Paths: []string{"/x/a"}}}}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}

	// 状态文件的 last_sync 设为现在 → 未到点，不再同步
	sf, _ := syncx.LoadStatus(st.StateDir())
	sf.Layer("personal").LastSync = time.Now()
	if err := sf.Save(st.StateDir()); err != nil {
		t.Fatal(err)
	}
	before := sf.Layer("personal").LastSync
	runSyncCycle(io.Discard, false)
	sf2, _ := syncx.LoadStatus(st.StateDir())
	if !sf2.Layer("personal").LastSync.Equal(before) {
		t.Fatal("should skip when not due")
	}
	// force=true → 到点判断被跳过，状态文件被触碰（ahead/behind 刷新）
	runSyncCycle(io.Discard, true)
	// force 路径无错误即视为执行（具体由上一用例覆盖推送行为）
}
```

（import 需 `"io"`、`"os/exec"`；`registry.Project`/`Registry.Save` 字段名以 internal/registry/registry.go:17-24、:91-100 实际为准，若不同以实际为准。）

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run 'TestRunSyncCycle|TestSyncDueInterval' -v`
Expected: 编译失败 `undefined: runSyncCycle`。

- [ ] **Step 3: Write minimal implementation**

```go
package daemon

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/registry"
	"openknowledge/internal/store"
	"openknowledge/internal/syncx"
)

var syncCheckInterval = time.Minute // 检查周期（包级 var 供测试调小）

// startSyncJanitor 挂同步 ticker（照 run.go:96 自省 ticker 模式，随进程生命周期结束）。
// 每分钟检查一轮：启用同步且到点的项目跑 SyncOnce。失败仅记日志，绝不影响本地链路。
func startSyncJanitor(stdout io.Writer) {
	go func() {
		ticker := time.NewTicker(syncCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			runSyncCycle(stdout, false)
		}
	}()
}

// runSyncCycle 遍历注册项目，对启用同步且到点（或 force）的项目执行一次同步。
// force=true 跳过 interval 判断（写入防抖触发用，设计文档 §8）。
func runSyncCycle(out io.Writer, force bool) {
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		return
	}
	globalCfg := filepath.Join(registry.Home(), "config.toml")
	for _, p := range reg.Projects {
		st := store.New(filepath.Join(registry.Home(), "projects", p.Name))
		cfg, err := config.LoadMerged(st.ConfigPath(), globalCfg)
		if err != nil || !cfg.Sync.Enabled {
			continue
		}
		if !force && !syncDue(st, cfg.Sync.AutoIntervalMin) {
			continue
		}
		host, _ := os.Hostname()
		msg := fmt.Sprintf("sync: %s %s", host, time.Now().Format(time.RFC3339))
		o := syncx.SyncOnce(st.Root, msg)
		syncx.RecordOutcome(st.Root, st.StateDir(), o)
		switch {
		case o.Err != nil:
			fmt.Fprintf(out, "sync %s: %v\n", p.Name, o.Err)
		case len(o.Conflicts) > 0:
			fmt.Fprintf(out, "sync %s: %d 个文件冲突，待人工解决\n", p.Name, len(o.Conflicts))
		}
	}
}

// syncDue 判断项目是否到同步点（interval<=0 视为关闭自动同步）。
func syncDue(st *store.Store, intervalMin int) bool {
	if intervalMin <= 0 {
		return false
	}
	sf, err := syncx.LoadStatus(st.StateDir())
	if err != nil {
		return true
	}
	last := sf.Layer("personal").LastSync
	if last.IsZero() {
		return true
	}
	return time.Since(last) >= time.Duration(intervalMin)*time.Minute
}
```

`internal/daemon/run.go:143` 附近（`go sidecarJanitor(sidecarMgr)` 旁）加：

```go
	startSyncJanitor(stdout)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -v`
Expected: 全部 PASS（既有用例不回归）。

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/sync.go internal/daemon/sync_test.go internal/daemon/run.go
git commit -m "feat(daemon): 同步 ticker（每分钟检查，按项目 interval 触发）"
```

---

### Task 10: 写入后 30s 防抖触发

**Files:**
- Modify: `internal/daemon/sync.go`（追加 debouncer 与 NotifyWrite）
- Modify: `internal/gui/api.go`（Handler 加 OnWrite 字段；syncIndex :296-310、syncApprove :1143-1164 成功路径调 notifyWrite）
- Modify: `internal/daemon/run.go`（NewHandler 后注入回调）
- Test: `internal/daemon/sync_test.go`（追加 debouncer 用例）

**Interfaces:**
- Consumes: Task 9 的 `runSyncCycle`；`gui.NewHandler`（api.go:57-119 注册区，Handler 结构体定义在 api.go 前部——**先读 gui/api.go 的 Handler 定义确认字段位置**）。
- Produces:
  - `func NotifyWrite()`（daemon 包）— 30s 防抖后 `runSyncCycle(out, true)`
  - `var syncWriteDebounce = 30 * time.Second`（包级 var，测试可调）
  - gui 侧 `Handler.OnWrite func()`（nil 安全）

- [ ] **Step 1: Write the failing test**

```go
// 追加到 internal/daemon/sync_test.go

// TestNotifyWriteDebounce 验证防抖合并：连续触发只跑一次，且等够时长。
func TestNotifyWriteDebounce(t *testing.T) {
	old := syncWriteDebounce
	syncWriteDebounce = 50 * time.Millisecond
	defer func() { syncWriteDebounce = old }()

	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	reg := &registry.Registry{}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	// 计数器：替换 fire 函数
	var mu sync.Mutex
	fires := 0
	oldFire := syncFire
	syncFire = func() {
		mu.Lock()
		fires++
		mu.Unlock()
	}
	defer func() { syncFire = oldFire }()

	NotifyWrite()
	NotifyWrite()
	NotifyWrite()
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if fires != 1 {
		t.Fatalf("debounced fires = %d, want 1", fires)
	}
}
```

（import 需加 `"sync"`。）

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestNotifyWriteDebounce -v`
Expected: 编译失败 `undefined: NotifyWrite / syncFire`。

- [ ] **Step 3: Write minimal implementation**

`internal/daemon/sync.go` 追加：

```go
var syncWriteDebounce = 30 * time.Second // 写入后防抖时长（设计文档 §8）

// syncFire 是防抖到点后的动作（包级 var 供测试替换）。
var syncFire = func() {
	runSyncCycle(syncLogOut, true)
}

var syncLogOut io.Writer = io.Discard

var writeDeb = newDebouncer(syncWriteDebounce, func() { syncFire() })

// NotifyWrite 条目写入动作（approve、GUI 编辑保存等）的钩子：30s 防抖后
// 对启用同步的项目跑一轮同步，把未同步窗口压到分钟级。
func NotifyWrite() {
	writeDeb.d = syncWriteDebounce // 每次读取当前值（测试可调）
	writeDeb.Trigger()
}

type debouncer struct {
	mu sync.Mutex
	d  time.Duration
	fn func()
	t  *time.Timer
}

func newDebouncer(d time.Duration, fn func()) *debouncer {
	return &debouncer{d: d, fn: fn}
}

func (b *debouncer) Trigger() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.t != nil {
		b.t.Stop()
	}
	b.t = time.AfterFunc(b.d, b.fn)
}
```

（import 需加 `"sync"`。）

`startSyncJanitor` 函数体开头加一行（让防抖日志走 daemon stdout）：

```go
	syncLogOut = stdout
```

`internal/gui/api.go` 修改：

a) Handler 结构体加字段（位置照现有字段区）：

```go
	// OnWrite 条目写入成功后的回调（daemon 注入同步防抖触发），可为 nil。
	OnWrite func()
```

b) 加方法：

```go
func (h *Handler) notifyWrite() {
	if h.OnWrite != nil {
		h.OnWrite()
	}
}
```

c) `syncIndex`（api.go:296-310）成功返回前调 `h.notifyWrite()`——注意 syncIndex 是包级函数还是 Handler 方法，以 api.go 实际为准：若是包级函数 `func syncIndex(st *store.Store) error`，则改为 Handler 方法或在调用点（writeEntry/apiEntryArchive/apiEntryDelete/apiApprove 各自成功路径末尾）调 `h.notifyWrite()`。**以少改动为准：在各写入口 handler 的成功路径末尾加一行 `h.notifyWrite()`**——`writeEntry`（:788-832）、`apiApprove`（:1167-1227）、`apiEntryArchive`（:1231）、`apiEntryDelete`（:914-935）。

d) `internal/daemon/run.go` 在 `gh := gui.NewHandler(...)`（以实际变量名为准）之后、`NewMux` 之前加：

```go
	gh.OnWrite = NotifyWrite
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ ./internal/gui/ -v`
Expected: 全部 PASS（既有用例不回归）。

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/sync.go internal/daemon/sync_test.go internal/gui/api.go internal/daemon/run.go
git commit -m "feat(daemon): 写入后 30s 防抖触发同步"
```

---

### Task 11: E2E 双设备闭环 + 冲突用例

**Files:**
- Create: `tests/e2e/sync_test.go`

**Interfaces:**
- Consumes: 同包 `runOK(t, home, cwd, stdin, args...)`（integration_test.go:44-60）、`binPath`（TestMain 已构建）。**先读同包 wiki_test.go 确认是否已有可复用 helper，避免重复造轮子。**
- Produces: `TestSyncTwoDevices`、`TestSyncConflict` 两个 E2E 用例。

场景（设计文档 §15）：

- **双设备闭环**：设备 A 写条目 → sync → 设备 B sync init（clone）→ B 能检索到该条目（验证 mtime 重建链路）；
- **冲突路径**：双设备改同一文件 → 第二台 sync 报冲突、写 conflict 状态、内容不丢。

- [ ] **Step 1: Write the failing test**

```go
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitBare 建临时裸仓，返回路径。
func gitBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "-b", "main", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return dir
}

// mkHome 建一台"设备"：OK_HOME + kimi 目录 + 项目工作目录 + ok init 注册。
// 返回 (home, proj)。项目名固定 demo（两个 home 各自独立互不干扰）。
func mkHome(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "kimi"), 0o755); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(home, "demo")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if stdout, _, code := runOK(t, home, proj, "", "init", "demo"); code != 0 {
		t.Fatalf("init: code=%d out=%q", code, stdout)
	}
	return home, proj
}

// addEntry 用 ok add 写一条知识条目。
func addEntry(t *testing.T, home, proj, title, body string) {
	t.Helper()
	bodyFile := filepath.Join(proj, "body.md")
	if err := os.WriteFile(bodyFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if stdout, _, code := runOK(t, home, proj, "", "add", "--title", title, "--type", "note", "--file", bodyFile); code != 0 {
		t.Fatalf("add %s: code=%d out=%q", title, code, stdout)
	}
}

// overwriteEntry 直接改条目文件正文（模拟另一台设备的手工编辑，走 mtime 重建）。
func overwriteEntry(t *testing.T, home, title, newBody string) {
	t.Helper()
	kdir := filepath.Join(home, "projects", "demo", "knowledge")
	entries, err := os.ReadDir(kdir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), title) {
			data, err := os.ReadFile(filepath.Join(kdir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			// 保留 front matter（第一个 --- 到第二个 ---），替换正文
			s := string(data)
			idx := strings.Index(s[3:], "---")
			if idx < 0 {
				t.Fatalf("no front matter in %s", e.Name())
			}
			fm := s[:3+idx+3]
			if err := os.WriteFile(filepath.Join(kdir, e.Name()), []byte(fm+"\n"+newBody+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("entry %s not found in %s", title, kdir)
}

func TestSyncTwoDevices(t *testing.T) {
	bare := gitBare(t)
	homeA, projA := mkHome(t)
	homeB, projB := mkHome(t)

	// 设备 A：写条目 → sync init → 首推
	addEntry(t, homeA, projA, "部署清单", "上线前先跑回归测试套件。")
	stdout, stderr, code := runOK(t, homeA, projA, "", "sync", "init", bare)
	if code != 0 {
		t.Fatalf("A sync init: code=%d out=%q err=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "推送") {
		t.Fatalf("A sync init output: %q", stdout)
	}

	// 设备 B：sync init → clone → 能检索到 A 的条目
	stdout, stderr, code = runOK(t, homeB, projB, "", "sync", "init", bare)
	if code != 0 || !strings.Contains(stdout, "克隆") {
		t.Fatalf("B sync init: code=%d out=%q err=%q", code, stdout, stderr)
	}
	stdout, _, code = runOK(t, homeB, projB, "", "search", "部署清单")
	if code != 0 || !strings.Contains(stdout, "部署清单") {
		t.Fatalf("B search after clone: code=%d out=%q", code, stdout)
	}

	// 设备 A 再写一条 → sync；设备 B sync → 也能看到
	addEntry(t, homeA, projA, "发布清单", "发布前先跑 verify-deb。")
	stdout, _, code = runOK(t, homeA, projA, "", "sync")
	if code != 0 || !strings.Contains(stdout, "推送") {
		t.Fatalf("A sync: code=%d out=%q", code, stdout)
	}
	stdout, _, code = runOK(t, homeB, projB, "", "sync")
	if code != 0 || !strings.Contains(stdout, "拉取") {
		t.Fatalf("B sync: code=%d out=%q", code, stdout)
	}
	stdout, _, code = runOK(t, homeB, projB, "", "search", "发布清单")
	if code != 0 || !strings.Contains(stdout, "发布清单") {
		t.Fatalf("B search after sync: code=%d out=%q", code, stdout)
	}
	// 状态文件落盘且 conflict=false
	data, err := os.ReadFile(filepath.Join(homeB, "projects", "demo", "state", "sync-status.json"))
	if err != nil {
		t.Fatalf("B status file: %v", err)
	}
	if !strings.Contains(string(data), `"conflict": false`) {
		t.Fatalf("B status: %s", data)
	}
}

func TestSyncConflict(t *testing.T) {
	bare := gitBare(t)
	homeA, projA := mkHome(t)
	homeB, projB := mkHome(t)

	addEntry(t, homeA, projA, "冲突条目", "v1 原始内容")
	if _, _, code := runOK(t, homeA, projA, "", "sync", "init", bare); code != 0 {
		t.Fatalf("A sync init: code=%d", code)
	}
	if _, _, code := runOK(t, homeB, projB, "", "sync", "init", bare); code != 0 {
		t.Fatalf("B sync init: code=%d", code)
	}

	// A 改为 v2a 并同步推送
	overwriteEntry(t, homeA, "冲突条目", "v2a A 的修改")
	if stdout, _, code := runOK(t, homeA, projA, "", "sync"); code != 0 || !strings.Contains(stdout, "推送") {
		t.Fatalf("A sync: code=%d out=%q", code, stdout)
	}

	// B 改同一位置为 v2b → sync 必冲突
	overwriteEntry(t, homeB, "冲突条目", "v2b B 的修改")
	stdout, stderr, code := runOK(t, homeB, projB, "", "sync")
	if code == 0 {
		t.Fatalf("B sync should fail on conflict: out=%q", stdout)
	}
	if !strings.Contains(stderr, "冲突") {
		t.Fatalf("B sync stderr should mention conflict: %q", stderr)
	}
	// sync-conflict.json 落盘且含条目文件
	cfPath := filepath.Join(homeB, "projects", "demo", "state", "sync-conflict.json")
	data, err := os.ReadFile(cfPath)
	if err != nil {
		t.Fatalf("conflict file: %v", err)
	}
	var cf struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(data, &cf); err != nil || len(cf.Files) == 0 {
		t.Fatalf("conflict json: %v %s", err, data)
	}
	// 内容不丢：B 的改动仍在工作区（冲突标记内）
	kdir := filepath.Join(homeB, "projects", "demo", "knowledge")
	entries, _ := os.ReadDir(kdir)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "冲突条目") {
			body, _ := os.ReadFile(filepath.Join(kdir, e.Name()))
			if strings.Contains(string(body), "v2b B 的修改") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("B 的本地修改丢失")
	}
	// 清理：中止 rebase（不影响后续测试，TempDir 会删，但 Windows 上 git 进程须已退出）
	cmd := exec.Command("git", "-C", filepath.Join(homeB, "projects", "demo"), "rebase", "--abort")
	_ = cmd.Run()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests/e2e/ -run 'TestSyncTwoDevices|TestSyncConflict' -v`
Expected: FAIL——`sync` 子命令还不存在时 stderr 报未知命令（若 Task 7/8 已提交则直接 PASS，此时视为行为验证而非红绿循环，符合"行为变化类测试先对旧代码变红"纪律：先确认旧二进制跑这两个用例是红的）。

- [ ] **Step 3: 若旧代码意外变绿，停下来排查**

若 Task 7/8 之前的二进制能让用例通过，说明测试断言没有判别力（"行为变化类新测试必须先验证对旧代码变红"知识条目），修正断言后再继续。

- [ ] **Step 4: Run test to verify it passes（全部前置任务完成后）**

Run: `go test ./tests/e2e/ -run 'TestSyncTwoDevices|TestSyncConflict' -v`
Expected: 两个用例 PASS。

- [ ] **Step 5: 全量回归**

Run: `go test ./...`
Expected: 仓库全部测试 PASS。

- [ ] **Step 6: Commit**

```bash
git add tests/e2e/sync_test.go
git commit -m "test(e2e): 同步双设备闭环与冲突路径"
```

---

### Task 12: 变更日志与文档收尾

**Files:**
- Create: `docs/changelogs/2026-08-28-personal-sync-engine.md`
- Modify: `docs/superpowers/specs/2026-08-25-personal-sync-p1-design.md`（状态行）

- [ ] **Step 1: 写变更日志**

格式照 `docs/changelogs/2026-08-19-injection-cooldown.md`（`# 标题` + `日期：` + 正文段落）：

```markdown
# 个人多端同步引擎（P1-A）

日期：2026-08-28

新增个人多端同步引擎：项目知识库可经 git 在多设备间同步。`internal/syncx` 叶子包以参数数组 exec 系统 git（不走 shell、GIT_TERMINAL_PROMPT=0 防挂起、commit 身份内置），提供仓库原语与 Sync 编排（add -A → commit → pull --rebase → push），包级 single-flight 合并并发触发。`ok sync` / `ok sync init` 命令覆盖三情形初始化（clone / 首台设备推送 / 双有内容报错指引手动合并）；okd 每分钟检查一轮、按项目 `auto_interval_min` 到点同步，条目写入后 30s 防抖补一轮，把未同步窗口压到分钟级。冲突时停止推送、写 `state/sync-conflict.json` 并指引 GUI 冲突页或手动解决；同步状态按层建模落 `state/sync-status.json`（为团队版 `[sync.personal]` 预留）。项目级 `config.toml` 新增 `[sync]` 段（enabled/remote/auto_interval_min，`config.SetSync` 行级写入）。失败一律 fail-open，仅记日志与状态文件。E2E 覆盖双设备闭环（clone 后检索命中，验证 mtime 重建链路）与冲突路径（报冲突、写状态、内容不丢）。GUI 同步按钮/冲突解决页与 okserver 服务端为后续阶段。
```

- [ ] **Step 2: 更新设计文档状态**

`docs/superpowers/specs/2026-08-25-personal-sync-p1-design.md` 第 4 行状态改为：

```markdown
- 状态：P1-A 同步引擎已实施（2026-08-28）；GUI 同步面（Plan B）与服务端（Plan C）待实施
```

- [ ] **Step 3: Commit**

```bash
git add docs/changelogs/2026-08-28-personal-sync-engine.md docs/superpowers/specs/2026-08-25-personal-sync-p1-design.md
git commit -m "docs: P1-A 同步引擎变更日志与设计文档状态"
```

---

## 执行顺序与依赖

```
Task 1 → Task 2 → Task 3 → Task 4 → Task 5（syncx 全包）
Task 6（config，独立，可与 1-5 并行）
Task 7+8（CLI，依赖 1-6，合并一次提交）
Task 9（daemon ticker，依赖 1-6）
Task 10（防抖，依赖 9）
Task 11（E2E，依赖全部）
Task 12（收尾）
```

## Self-Review 记录

- **Spec 覆盖**：§4 syncx（T1-T5）✓；§7 ok sync（T7）✓；§8 auto-sync + 防抖 + 状态文件（T5/T9/T10）✓；§12 [sync] 配置（T6）✓；§14 init 三情形（T8）✓；§15 E2E（T11）✓。冲突解决原语/`llm_assist`/okserver 明确不在本计划（Plan B/C）。
- **Placeholder 扫描**：T7 的 pc 字段名、T9 的 registry.Project 字段名、T10 的 Handler 字段位置标注了"先读实际代码确认"——这是既有代码的事实对齐指令，非 TBD；实现代码均完整给出。
- **类型一致性**：Outcome/LayerStatus/Sync 字段名跨任务核对一致；`execGit`/`SyncOnce`/`RecordOutcome` 签名各处引用一致。
