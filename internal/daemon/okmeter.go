package daemon

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"okryptos/internal/config"
	"okryptos/internal/registry"
)

// decideLaunchOKMeter 保活判定纯函数：开关开 + OkMeter.exe 在位 + 未运行 → 拉起一次。
// 开关关 → 不拉起（已在运行的不杀，OkMeter 生命周期归用户/自身管理）。
func decideLaunchOKMeter(enabled, exeExists, running bool) bool {
	return enabled && exeExists && !running
}

// okmeterJanitor 以自省周期保活 OkMeter（token 统计工具）：全局开关
// okmeter_enabled 开、okd 同目录 OkMeter.exe 在位且未运行 → 拉起一次
//（单实例守卫在 OkMeter 侧，重复拉起静默退出不叠实例）。fail-open：
// 任何失败只写 stderr。配置变更经周期轮询自然生效（GUI 写全局配置）。
func okmeterJanitor(stderr io.Writer) {
	okmeterKeepAlive(stderr) // 启动即查一轮，不等首个 tick
	ticker := time.NewTicker(selfCheckInterval)
	defer ticker.Stop()
	for range ticker.C {
		okmeterKeepAlive(stderr)
	}
}

func okmeterKeepAlive(stderr io.Writer) {
	cfg, err := config.LoadMerged("", filepath.Join(registry.Home(), "config.toml"))
	if err != nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	om := filepath.Join(filepath.Dir(exe), "OkMeter.exe")
	_, statErr := os.Stat(om)
	if !decideLaunchOKMeter(cfg.OKMeterEnabled, statErr == nil, okmeterRunning()) {
		return
	}
	if err := exec.Command(om).Start(); err != nil {
		fmt.Fprintf(stderr, "okmeter: 拉起失败 %v\n", err)
		return
	}
	fmt.Fprintln(stderr, "okmeter: 已随 okd 拉起")
}
