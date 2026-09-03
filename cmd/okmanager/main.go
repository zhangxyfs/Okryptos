package main

import (
	"os"

	"okryptos/internal/registry"
)

// OkManager：Okryptos 配置中心入口。Windows 托管 WebView2 原生窗口
// （host_windows.go），其余平台回退浏览器（host_other.go）。不含业务逻辑，
// 全部功能走 okd HTTP API。见 docs/superpowers/specs/2026-08-25-gui-embedded-window-design.md。
func main() {
	// 2.25.0 改名版：双击配置中心也可能是升级后首个进程，同样触发数据根迁移（幂等）。
	registry.MigrateLegacyHome()
	os.Exit(runHost(os.Stdout, os.Stderr))
}
