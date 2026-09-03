package gui

// 窗口状态持久化（机器本地，不进任何同步面）：记录 normal 矩形与最大化标志，
// 供内嵌窗口启动时原样恢复。平台无关纯逻辑；Win32 placement 转换在
// cmd/okmanager/host_windows.go。

import (
	"encoding/json"
	"os"
	"path/filepath"

	"okryptos/internal/fsx"
	"okryptos/internal/registry"
)

type WindowState struct {
	Maximized bool  `json:"maximized"`
	Left      int32 `json:"left"`
	Top       int32 `json:"top"`
	Right     int32 `json:"right"`
	Bottom    int32 `json:"bottom"`
}

func windowStatePath() string { return filepath.Join(registry.Home(), "gui-state.json") }

// LoadWindowState 读窗口状态；文件缺失/损坏/矩形非法一律 (nil,false)，调用方按
// "无状态"处理（首启最大化）。
func LoadWindowState() (*WindowState, bool) {
	data, err := os.ReadFile(windowStatePath())
	if err != nil {
		return nil, false
	}
	var s WindowState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, false
	}
	if s.Right <= s.Left || s.Bottom <= s.Top {
		return nil, false
	}
	if s.Right < 0 || s.Bottom < 0 { // 整体位于屏外（如离屏创建阶段的 -32000 垃圾值）：按无状态处理
		return nil, false
	}
	return &s, true
}

func SaveWindowState(s *WindowState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return fsx.WriteFile(windowStatePath(), data, 0o644)
}
