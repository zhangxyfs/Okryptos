package daemon

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"okryptos/internal/daemonx"
)

// stubSpawn 替换 SpawnDetached，返回调用次数。
func stubSpawn(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	old := SpawnDetached
	SpawnDetached = func(exe string, args []string, logPath string) error { *calls++; return nil }
	t.Cleanup(func() { SpawnDetached = old })
	return calls
}

func fakeDaemon(t *testing.T, fingerprint string, hr HookResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Ok-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/health" {
			json.NewEncoder(w).Encode(map[string]string{"fingerprint": fingerprint})
			return
		}
		json.NewEncoder(w).Encode(hr)
	}))
}

func saveInfo(t *testing.T, port int, fp string) {
	t.Helper()
	if err := daemonx.Save(&daemonx.Info{PID: os.Getpid(), Port: port, Token: "tok", Fingerprint: fp}); err != nil {
		t.Fatal(err)
	}
}

func TestForwardHookOK(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	_ = stubSpawn(t)
	fp, err := daemonx.ExeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	srv := fakeDaemon(t, fp, HookResponse{Stdout: "注入内容", Code: 0})
	defer srv.Close()
	saveInfo(t, srv.Listener.Addr().(*net.TCPAddr).Port, fp)
	var out, errOut bytes.Buffer
	handled, code := ForwardHook("prompt", "", []byte(`{}`), &out, &errOut)
	if !handled || code != 0 || out.String() != "注入内容" {
		t.Fatalf("handled=%v code=%d out=%q", handled, code, out.String())
	}
}

// daemon 健康时顺手清掉 .spawning 残留：防抖标记没有任何删除路径，一次拉起
// 后永久滞留，排障时会误读成"正在拉起中"（2026-08-22 实际踩过）。
func TestEnsureRemovesStaleSpawningMarkWhenHealthy(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	_ = stubSpawn(t)
	fp, err := daemonx.ExeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	srv := fakeDaemon(t, fp, HookResponse{})
	defer srv.Close()
	saveInfo(t, srv.Listener.Addr().(*net.TCPAddr).Port, fp)
	mark := daemonx.Path() + ".spawning"
	if err := os.WriteFile(mark, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	Ensure()
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Fatal("stale spawning mark should be removed when daemon is healthy")
	}
}

func TestForwardHookStaleDaemonFallsBack(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	calls := stubSpawn(t)
	saveInfo(t, 1, "fp") // 端口 1 必然不通
	var out bytes.Buffer
	handled, _ := ForwardHook("prompt", "", []byte(`{}`), &out, &out)
	if handled {
		t.Fatal("unreachable daemon should not handle")
	}
	if *calls != 1 {
		t.Fatalf("expected 1 spawn, got %d", *calls)
	}
	// 15s 防抖：第二次不再 spawn
	handled, _ = ForwardHook("prompt", "", []byte(`{}`), &out, &out)
	if handled || *calls != 1 {
		t.Fatalf("debounce broken: handled=%v calls=%d", handled, *calls)
	}
}

func TestForwardHookVersionMismatch(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	calls := stubSpawn(t)
	srv := fakeDaemon(t, "old-fingerprint", HookResponse{})
	defer srv.Close()
	saveInfo(t, srv.Listener.Addr().(*net.TCPAddr).Port, "old-fingerprint")
	var out bytes.Buffer
	handled, _ := ForwardHook("prompt", "", []byte(`{}`), &out, &out)
	if handled {
		t.Fatal("version mismatch should not handle")
	}
	if *calls != 1 {
		t.Fatalf("expected respawn, got %d", *calls)
	}
}

// 超时=daemon 已收到且处理不可取消 → handled=true：本地兜底会造成同一次事件
// 双执行（entry_events 双份、采纳双倍入账）。宁缺毋双，该轮注入缺失可接受。
func TestForwardHookTimeoutHandled(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	_ = stubSpawn(t)
	old := forwardTimeout
	forwardTimeout = 100 * time.Millisecond
	t.Cleanup(func() { forwardTimeout = old })
	fp, _ := daemonx.ExeFingerprint()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			json.NewEncoder(w).Encode(map[string]string{"fingerprint": fp})
			return
		}
		time.Sleep(500 * time.Millisecond)
	}))
	defer slow.Close()
	saveInfo(t, slow.Listener.Addr().(*net.TCPAddr).Port, fp)
	var out bytes.Buffer
	start := time.Now()
	handled, code := ForwardHook("prompt", "", []byte(`{}`), &out, &out)
	if !handled || code != 0 {
		t.Fatalf("timeout means daemon received it: handled=%v code=%d", handled, code)
	}
	if time.Since(start) > time.Second {
		t.Fatal("forward should time out promptly")
	}
}

// 升级熔断：~/.okryptos/update/.upgrading 存在时 Ensure/EnsureCurrent 一律不
// 拉起——此时旧 daemon 已被 GUI /api/update/apply 停掉、安装器正在覆盖 exe，拉起
// 只会启动即将被替换的旧二进制并与安装器抢文件锁。
func TestEnsureUpgradeCircuitBreaker(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	calls := stubSpawn(t)
	mark := filepath.Join(os.Getenv("OK_HOME"), "update", ".upgrading")
	if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mark, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	Ensure()
	if *calls != 0 {
		t.Fatalf("升级中 Ensure 不应拉起, spawn calls = %d", *calls)
	}
	info, ok := EnsureCurrent()
	if ok || info != nil {
		t.Fatalf("升级中 EnsureCurrent = (%v, %v), want (nil, false)", info, ok)
	}
	if *calls != 0 {
		t.Fatalf("升级中 EnsureCurrent 不应拉起, spawn calls = %d", *calls)
	}

	// 熔断解除后恢复正常路径：无凭证 → Ensure 拉起一次（stub 不真起进程）。
	if err := os.Remove(mark); err != nil {
		t.Fatal(err)
	}
	Ensure()
	if *calls != 1 {
		t.Fatalf("熔断解除后 Ensure 应拉起 1 次, calls = %d", *calls)
	}
}

// 熔断自愈 TTL：安装中止/崩溃留下过期 .upgrading（mtime 超过 upgradeMarkTTL）时
// upgradeInProgress 视为失效并删除——否则 Ensure 永久拒拉 daemon（v2.26.4 实踩死锁）。
func TestUpgradeCircuitBreakerExpiry(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	calls := stubSpawn(t)
	mark := filepath.Join(os.Getenv("OK_HOME"), "update", ".upgrading")
	if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mark, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-upgradeMarkTTL - time.Minute)
	if err := os.Chtimes(mark, stale, stale); err != nil {
		t.Fatal(err)
	}

	Ensure() // 过期熔断应失效并自删，走正常拉起（stub 不真起进程）
	if *calls != 1 {
		t.Fatalf("过期熔断不应挡住拉起, spawn calls = %d", *calls)
	}
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Fatalf("过期熔断应被删除, stat err = %v", err)
	}
}

// daemon.Run 启动自愈（clearUpgradeMark）：.upgrading 残留（升级收尾或安装中断）
// 在启动路径开头删除并记日志——否则 Ensure/EnsureCurrent 会永久拒拉 daemon。
func TestClearUpgradeMarkSelfHeal(t *testing.T) {
	t.Setenv("OK_HOME", t.TempDir())
	mark := filepath.Join(os.Getenv("OK_HOME"), "update", ".upgrading")
	if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mark, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	clearUpgradeMark(&buf)
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Fatalf("残留熔断应被自愈删除, stat err = %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("自愈应记日志")
	}
	// 无残留时静默不动
	buf.Reset()
	clearUpgradeMark(&buf)
	if buf.Len() != 0 {
		t.Fatalf("无残留时不应输出: %q", buf.String())
	}
}
