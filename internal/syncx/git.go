// Package syncx 在项目数据目录里执行 git：个人多端同步的单机引擎。
// 叶子包——仅依赖 fsx（原子写状态），不依赖其他 internal 包。
// 纪律（设计文档 §4）：参数数组调系统 git，不走 shell；网络操作给足超时；
// GIT_TERMINAL_PROMPT=0 防凭据提示挂起；LC_ALL=C 固定 locale，防本地化输出影响解析；
// GIT_EDITOR=true 防唤起编辑器挂起（env 优先级高于 core.editor，daemon 继承 shell 导出的 GIT_EDITOR 也兜得住）。
package syncx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"okryptos/internal/procx"
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
// core.quotepath=false：路径按原始字节输出——默认 quotepath 会把非 ASCII 路径
// 转八进制转义（中文条目名变成 \345\206\262...），冲突文件列表将不可展示/不可用。
func execGit(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.quotepath=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "GIT_EDITOR=true")
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
