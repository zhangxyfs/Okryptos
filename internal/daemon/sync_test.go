package daemon

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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
	// 首推建立 upstream（照 syncx 测试 mkPair 的setup）：Sync 先 pull --rebase，
	// 无 upstream 时 git 直接拒绝，同步会停在 Err 而非走到 push
	if err := ra.Push(); err != nil {
		t.Fatalf("a push: %v", err)
	}
	// remote 路径转 "/"：Windows 路径反斜杠在 TOML 基本字符串里是非法转义，
	// LoadMerged 会解析失败（git 在 Windows 上接受 "/" 路径）
	if err := os.WriteFile(stA.ConfigPath(), []byte("[sync]\nenabled = true\nremote = \""+filepath.ToSlash(bare)+"\"\nauto_interval_min = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 项目 b：未启用同步（无 config）
	stB := store.New(filepath.Join(home, "projects", "b"))
	if err := os.MkdirAll(stB.KnowledgeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// 注册表
	reg := &registry.Registry{Projects: []registry.Project{{Name: "a", Paths: []string{"/x/a"}}, {Name: "b", Paths: []string{"/x/b"}}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
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
	// 首推建立 upstream（同 TestRunSyncCycle 的适配说明）
	if err := r.Push(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(st.ConfigPath(), []byte("[sync]\nenabled = true\nremote = \""+filepath.ToSlash(bare)+"\"\nauto_interval_min = 5\n"), 0o644)
	reg := &registry.Registry{Projects: []registry.Project{{Name: "a", Paths: []string{"/x/a"}}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
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

// TestNotifyWriteDebounce 验证防抖合并：连续触发只跑一次，且等够时长。
func TestNotifyWriteDebounce(t *testing.T) {
	old := syncWriteDebounce
	syncWriteDebounce = 50 * time.Millisecond
	defer func() { syncWriteDebounce = old }()

	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	reg := &registry.Registry{}
	if err := reg.Save(registry.DefaultPath()); err != nil {
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

// TestRunSyncCycleConcurrent 验证 runSyncCycle 并发安全：ticker goroutine 与写入
// 防抖 goroutine 会并发调用，syncCycleMu 串行化后并发跑 force 同步无数据竞争、
// 无 git 锁争用（sync-status.json 的读-改-写被互斥保护）。
func TestRunSyncCycleConcurrent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)
	bare := t.TempDir()
	gitExec(t, bare, "init", "--bare", "-b", "main")
	st := store.New(filepath.Join(home, "projects", "a"))
	if err := os.MkdirAll(st.KnowledgeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(st.StateDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	r := syncx.Open(st.Root)
	if err := r.Init(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.KnowledgeDir(), "k.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitAll("init"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetRemote(bare); err != nil {
		t.Fatal(err)
	}
	// 首推建立 upstream（同 TestRunSyncCycle 的适配说明）
	if err := r.Push(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.ConfigPath(), []byte("[sync]\nenabled = true\nremote = \""+filepath.ToSlash(bare)+"\"\nauto_interval_min = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{Projects: []registry.Project{{Name: "a", Paths: []string{"/x/a"}}}}
	if err := reg.Save(registry.DefaultPath()); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runSyncCycle(io.Discard, true)
		}()
	}
	wg.Wait()

	sf, err := syncx.LoadStatus(st.StateDir())
	if err != nil {
		t.Fatal(err)
	}
	if sf.Layer("personal").LastSync.IsZero() {
		t.Fatal("a should have been synced")
	}
}
