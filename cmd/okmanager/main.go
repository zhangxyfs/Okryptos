package main

import "os"

// OkManager：OpenKnowledge 配置中心入口。Windows 托管 WebView2 原生窗口
// （host_windows.go），其余平台回退浏览器（host_other.go）。不含业务逻辑，
// 全部功能走 okd HTTP API。见 docs/superpowers/specs/2026-08-25-gui-embedded-window-design.md。
func main() { os.Exit(runHost(os.Stdout, os.Stderr)) }
