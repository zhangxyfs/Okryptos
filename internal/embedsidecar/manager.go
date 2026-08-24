package embedsidecar

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/embed"
	"openknowledge/internal/logx"
)

// ServerCommand 是 spawn 接缝：测试替换为 helper 进程。生产即 exec.Command。
var ServerCommand = func(path string, args ...string) *exec.Cmd { return exec.Command(path, args...) }

// serverExeName 随平台（windows=llama-server.exe）。
var serverExeName = map[bool]string{true: "llama-server.exe", false: "llama-server"}[runtime.GOOS == "windows"]

// RuntimeServerPath 返回 <runtimeDir>/llama-server[.exe]；缺失时报错（裸 exe
// 便携形态无 runtime 目录 → 内置模式不可用）。
func RuntimeServerPath(runtimeDir string) (string, error) {
	p := filepath.Join(runtimeDir, serverExeName)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("推理运行时缺失（%s）——内置模式仅安装版可用", p)
	}
	return p, nil
}

// DefaultRuntimeDir 返回 <exe 所在目录>/runtime。
func DefaultRuntimeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Join(filepath.Dir(exe), "runtime")
}

// DefaultModelsDir 返回 <exe 所在目录>/models（安装版即安装目录下）。
func DefaultModelsDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Join(filepath.Dir(exe), "models")
}

// ModelsDir 解析生效的模型目录：配置优先，空则默认。
func ModelsDir(cfg config.Config) string {
	if cfg.Embedding.ModelsDir != "" {
		return cfg.Embedding.ModelsDir
	}
	return DefaultModelsDir()
}

// Manager 托管 sidecar 生命周期。仅 daemon 持有。
type Manager struct {
	RuntimeDir    string
	ModelsDir     string
	HealthTimeout time.Duration // Ensure 就绪等待上限（建议 90s）
	IdleTimeout   time.Duration // 空闲回收阈值（建议 10min）

	mu              sync.Mutex
	cmd             *exec.Cmd
	lastDesired     string
	failCount       int
	unhealthyStreak int
}

// Ensure 保证 model 对应 sidecar 在线（幂等）；返回可用 State。
// 锁纪律：mu 只覆盖状态检查与 spawn（快进快出）；就绪等待在锁外——daemon 退出时
// Stop 须能立即拿锁杀进程，若锁横跨最长 HealthTimeout 的就绪等待，shutdown 会被
// 卡到超时（M-11）。就绪等待只读本地 cmd/st/waitCh，并发 Stop 杀进程后经 waitCh
// 以"提前退出"返回，无共享状态竞态。
func (m *Manager) Ensure(model embed.BuiltinModel) (*State, error) {
	m.mu.Lock()
	if st := LoadState(); st != nil && st.ModelID == model.ID && st.Healthy() {
		m.failCount = 0
		m.mu.Unlock()
		return st, nil
	}
	m.stopLocked()
	cmd, st, waitCh, err := m.spawnLocked(model)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(m.HealthTimeout)
	for {
		if st.Healthy() {
			if err := writeState(st); err != nil {
				return nil, err
			}
			m.mu.Lock()
			m.failCount = 0
			m.mu.Unlock()
			return st, nil
		}
		select {
		case err := <-waitCh:
			return nil, fmt.Errorf("llama-server 提前退出: %v（日志 %s）", err, logPath())
		default:
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("llama-server 就绪超时（%s）", m.HealthTimeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// spawnLocked 校验并拉起 llama-server（调用方须持 mu）；返回进程、初始 State 与
// 看护结果通道，就绪探测由调用方在锁外进行（见 Ensure 锁纪律注释）。
func (m *Manager) spawnLocked(model embed.BuiltinModel) (*exec.Cmd, *State, chan error, error) {
	server, err := RuntimeServerPath(m.RuntimeDir)
	if err != nil {
		return nil, nil, nil, err
	}
	modelPath := model.InstalledPath(m.ModelsDir)
	if _, err := os.Stat(modelPath); err != nil {
		return nil, nil, nil, fmt.Errorf("模型文件缺失: %s", modelPath)
	}
	port, err := freePort()
	if err != nil {
		return nil, nil, nil, err
	}
	args := []string{
		"-m", modelPath,
		"--port", strconv.Itoa(port),
		"--host", "127.0.0.1",
		"--embeddings",
		"--pooling", model.Pooling,
	}
	cmd := ServerCommand(server, args...)
	hideWindow(cmd)
	logx.RotateIfOversize(logPath()) // sidecar 重启窗口轮替：上一进程已退出，句柄已释放
	logF, err := os.OpenFile(logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	var logW *logx.Writer
	if err == nil {
		// 输出经 logx 按行加时间戳再落盘：llama-server 自身不打时间戳，
		// 多次启动的输出交织后无法对时间线。io.Writer 模式下 exec 用管道 +
		// 复制 goroutine 转发，句柄不继承给子进程，改在 Wait 返回后关闭。
		logW = logx.New(logF)
		cmd.Stdout = logW
		cmd.Stderr = logW
	}
	if err := cmd.Start(); err != nil {
		if logF != nil {
			_ = logF.Close()
		}
		return nil, nil, nil, err
	}
	if logW != nil {
		fmt.Fprintf(logW, "=== llama-server 启动 model=%s port=%d pid=%d ===\n", model.ID, port, cmd.Process.Pid)
	}
	m.cmd = cmd
	st := &State{PID: cmd.Process.Pid, Port: port, ModelID: model.ID, StartedAt: time.Now(), LastUsed: time.Now()}
	waitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if logW != nil {
			fmt.Fprintf(logW, "=== llama-server 退出: %v ===\n", err)
		}
		if logF != nil {
			_ = logF.Close()
		}
		waitCh <- err
	}()
	return cmd, st, waitCh, nil
}

// Stop 杀 sidecar 并删状态文件（幂等）。
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) stopLocked() {
	// 崩溃计数随实例生灭归零（M-10）：判死 Stop 后计数若停在阈值上，新 sidecar
	// 首次探测的瞬时失败会立即凑满"连续两轮"被杀，两轮判定退化为一轮。
	m.unhealthyStreak = 0
	if m.cmd != nil && m.cmd.Process != nil {
		// 只 Kill 不 Wait：回收由 Ensure 的看护 goroutine 独占（它同时负责写退出
		// 日志、关闭日志文件），此处再 Wait 会与之竞态，误报 "no child processes"。
		_ = m.cmd.Process.Kill()
		m.cmd = nil
	} else if st := LoadState(); st != nil {
		// 跨进程残留（daemon 重启后 m.cmd 为空）：按 PID 杀。仅当端口仍在正常
		// 应答（Healthy）才杀——sidecar 已死而 Windows 复用了该 PID 时，盲杀会
		// 误伤无关进程；不健康/不应答说明原进程大概率已退出，只清状态文件。
		if st.Healthy() {
			if p, err := os.FindProcess(st.PID); err == nil {
				_ = p.Kill()
			}
		}
	}
	_ = os.Remove(statePath())
}

// Reconcile 调和一次：desired=期望模型（nil=不需要 sidecar）。
// 拉起条件：desired 就绪 且（激活刚变化 或 want 标记 pending）；
// 停止条件：不需要/未就绪/模型切换/空闲超时/连续两轮不健康（进程崩溃）。
// 连续 3 次拉起失败进入冷却（直到 desired 变化重试）。
func (m *Manager) Reconcile(desired *embed.BuiltinModel, now time.Time) {
	desiredID := ""
	if desired != nil {
		desiredID = desired.ID
	}
	changed := desiredID != m.lastDesired
	m.lastDesired = desiredID
	if changed {
		m.failCount = 0
	}
	if desiredID == "" || !desired.Installed(m.ModelsDir) {
		m.Stop()
		ClearWant()
		return
	}
	st := LoadState()
	if st != nil && st.ModelID != desiredID {
		m.Stop()
		st = nil
	}
	if st == nil {
		if (changed || WantPending()) && m.failCount < 3 {
			if _, err := m.Ensure(*desired); err != nil {
				m.failCount++
			} else {
				ClearWant()
			}
		}
		return
	}
	if !st.Healthy() {
		m.unhealthyStreak++
		if m.unhealthyStreak >= 2 {
			m.Stop() // 连续两轮（≥10s）不健康判定死亡；want/激活门控下轮自然重拉
			return
		}
	} else {
		m.unhealthyStreak = 0
	}
	if now.Sub(st.LastUsed) > m.IdleTimeout {
		m.Stop()
	}
}

// freePort 先占后放取空闲端口。Close 与子进程 bind 之间存在 TOCTOU 窗口，端口
// 可能被其他进程抢占 → llama-server bind 失败提前退出。不加锁不重试：Ensure 失败
// 后状态文件不落盘，下轮 Reconcile（want 标记在失败时保留，embedx 调用失败也会重写
// want，failCount<3 门控内）自然重拉，spawnLocked 每次重新取端口——抢占是瞬时的，
// 重试即自愈（L-19）。
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
