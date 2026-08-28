package daemon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"openknowledge/internal/daemonx"
)

// cliSubcommands 是 ok CLI 的全部子命令（cmd/ok/main.go run() 的分发面）。
// 新增 ok 子命令必须同步补这里，否则 okd 的兼容转发会漏——两处无法共享
// 常量（分发在 main 包），与 findWebDir 拷贝同款的"改动需同步"约定。
var cliSubcommands = map[string]struct{}{
	"gui":             {},
	"hook":            {},
	"extension-serve": {},
	"setup":           {},
	"init":            {},
	"add":             {},
	"propose":         {},
	"sync":            {},
	"approve":         {},
	"backfill-born":   {},
	"capture":         {},
	"wiki":            {},
	"search":          {},
	"index":           {},
	"archive":         {},
	"list":            {},
	"doctor":          {},
	"on":              {},
	"off":             {},
}

// IsCLISubcommand 报告 arg 是否 ok CLI 子命令（okd 兼容转发的判定依据）。
// stop/daemon 不在其中：okd 自有语义（停服/无此形态），其余未知参数维持
// "无参数启动 daemon" 的现状，绝不抢。
func IsCLISubcommand(arg string) bool {
	_, ok := cliSubcommands[arg]
	return ok
}

// cliSelfPath 当前进程 exe（解析符号链接）；var 供测试注入。
var cliSelfPath = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

// runCLIProc 执行转发子进程；var 供测试注入（捕获 argv / 模拟错误）。
var runCLIProc = func(c *exec.Cmd) error { return c.Run() }

// ForwardCLI 把旧形态命令转发给同目录 ok CLI：gui-split（2026-08-21）前
// hooks/技能注册的是 `okd.exe hook ...` 形态，拆分后 okd 只认 stop，不转发
// 会静默空转（stdout 打"daemon 已在运行"破坏 hook JSON 协议）。stdio 与
// 退出码原样透传。
func ForwardCLI(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	exe, err := daemonx.CliTargetFor(cliSelfPath())
	if err != nil {
		fmt.Fprintln(stderr, "okd 转发失败:", err)
		return 1
	}
	cmd := exec.Command(exe, argv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return exitCodeOf(runCLIProc(cmd))
}

// exitCodeOf 翻译子进程错误为退出码：nil→0；ExitError→真实码；
// 启动失败等其他错误→1。
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if c := ee.ExitCode(); c >= 0 {
			return c
		}
	}
	return 1
}
