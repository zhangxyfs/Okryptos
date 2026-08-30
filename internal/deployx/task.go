package deployx

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// CmdTimeout 是单条远程命令的默认超时；pull/备份/恢复类调用方自行放宽。
const CmdTimeout = 60 * time.Second

// Env 跨步骤共享的执行环境。
type Env struct {
	Ex   Executor
	Hub  *LogHub
	Vars map[string]string // 跨步骤传递（如 root_password）
}

// Step 是任务的一步；Run 返回 error 即终止整个任务。
type Step struct {
	Name string
	Run  func(ctx context.Context, e *Env) error
}

// Task 是有序步骤序列。
type Task struct {
	Name  string
	Steps []Step
}

// Execute 逐步执行；失败即停（日志标 err），返回 "步骤名: 原因"。
func (t Task) Execute(ctx context.Context, e *Env) error {
	e.Hub.Publish("", "info", "任务开始："+t.Name)
	for _, s := range t.Steps {
		e.Hub.Publish(s.Name, "info", "开始："+s.Name)
		if err := s.Run(ctx, e); err != nil {
			e.Hub.Publish(s.Name, "err", "失败："+s.Name+"："+err.Error())
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		e.Hub.Publish(s.Name, "ok", "完成："+s.Name)
	}
	e.Hub.Publish("", "ok", "任务完成："+t.Name)
	return nil
}

// runCmd 执行命令，行输出进日志（归属 step），收集 stdout 返回；
// 非零退出返回含 stderr 摘要的错误。
func runCmd(ctx context.Context, e *Env, step, cmd string) (string, error) {
	return runCmdMask(ctx, e, step, cmd, cmd)
}

// runCmdMask 同 runCmd，但日志里显示 display（用于掩去命令行中的 token/密码）。
// 超时策略：调用方已带 deadline（如 PullTimeout）就尊重它，否则兜底 CmdTimeout
// ——不能无条件再包一层 WithTimeout(CmdTimeout)，那会把 pull 类长操作也掐死在 60s。
func runCmdMask(ctx context.Context, e *Env, step, display, cmd string) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, CmdTimeout)
		defer cancel()
	}
	e.Hub.Publish(step, "info", "$ "+display)
	var out, errLines []string
	code, err := e.Ex.Run(ctx, cmd, nil, func(stream, line string) {
		if stream == "stdout" {
			out = append(out, line)
		} else {
			errLines = append(errLines, line)
		}
		e.Hub.Publish(step, "info", line)
	})
	if err != nil {
		return "", err
	}
	if code != 0 {
		tail := strings.Join(errLines, "; ")
		if tail == "" {
			tail = strings.Join(out, "; ")
		}
		return "", fmt.Errorf("退出码 %d：%s", code, tail)
	}
	return strings.Join(out, "\n"), nil
}

// shellQuote 把任意字符串安全嵌入 POSIX sh 单引号。
// 注意：单引号会阻止 $HOME / ~ 展开——只用于密码、token 等不可预测值；
// 目录路径走 ValidateDir 白名单校验后原样使用（见 deploy.go）。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// dirRe 是远端部署目录的允许字符集（含 ~ 与 $，供 ~/openknowledge、$HOME 展开）。
var dirRe = regexp.MustCompile(`^[~$A-Za-z0-9_./-]+$`)

// ValidateDir 校验远端目录：白名单字符集，且不含 ".."。
// 通过的目录在远程命令中原样使用（不加引号），~/$HOME 才能被远端 sh 展开。
func ValidateDir(dir string) error {
	if dir == "" || !dirRe.MatchString(dir) || strings.Contains(dir, "..") {
		return fmt.Errorf("部署目录含非法字符：%q（允许字母数字、._-/~$）", dir)
	}
	return nil
}
