package main

import (
	"os"

	"openknowledge/internal/daemon"
	"openknowledge/internal/logx"
	"openknowledge/internal/webdir"
)

func main() { os.Exit(run(os.Args)) }

// okd：OpenKnowledge 常驻 daemon——管理 API + 托盘 + sidecar 托管 + Web 静态页分发，
// 见 docs/2026-08-21-gui-split-design.md。无参数 → 常驻服务；stop → 经 API 停服；
// gui-split 前注册的旧形态 CLI 子命令（okd hook/on/off/setup ...）→ 转发同目录 ok。
// 后台拉起时 stdio 即 daemon.log：按行加时间戳，排查"何时发生"不再靠猜。
func run(argv []string) int {
	if len(argv) > 1 && argv[1] == "stop" {
		return daemon.Stop(os.Stdout, os.Stderr)
	}
	if len(argv) > 1 && daemon.IsCLISubcommand(argv[1]) {
		return daemon.ForwardCLI(argv[1:], os.Stdin, os.Stdout, os.Stderr)
	}
	webDir, _ := webdir.Find() // 找不到 web 目录也能跑（仅无 GUI 静态页）
	return daemon.Run(webDir, logx.New(os.Stdout), logx.New(os.Stderr))
}
