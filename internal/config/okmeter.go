package config

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"okryptos/internal/fsx"
)

// SetOKMeterEnabled upsert config.toml 顶层键 okmeter_enabled。顶层键必须位于首个
// [section] 之前，否则会被 TOML 归属到上一个小节——与 upsertTomlKey 的小节内
// upsert 同款纪律，只是目标区是"首个小节头之前的顶层区"。
// 读-改-写包在 fsx.WithFileLock 内（原因同 SetCapture）。
func SetOKMeterEnabled(path string, enabled bool) error {
	keyLine := "okmeter_enabled = " + strconv.FormatBool(enabled)
	return fsx.WithFileLock(path, func() error {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return fsx.WriteFile(path, []byte(keyLine+"\n"), 0o644)
		}
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		// 顶层区终点 = 首个小节头（含 [retrieve.gate] 子表与 [[enforce]] 数组表）
		firstSection := len(lines)
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "[") {
				firstSection = i
				break
			}
		}
		// 顶层区内命中键行则原位替换（键名边界判定同 upsertTomlKeyLocked）
		for i := 0; i < firstSection; i++ {
			t := strings.TrimSpace(lines[i])
			if strings.HasPrefix(t, "okmeter_enabled") &&
				strings.HasPrefix(strings.TrimSpace(t[len("okmeter_enabled"):]), "=") {
				lines[i] = keyLine
				return fsx.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
			}
		}
		// 未命中：插到顶层区末尾（首个小节头之前），与下文空行分隔
		if firstSection == len(lines) {
			if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) != "" {
				lines = append(lines, "")
			}
			lines = append(lines, keyLine)
		} else {
			tail := append([]string{keyLine, ""}, lines[firstSection:]...)
			lines = append(lines[:firstSection], tail...)
		}
		return fsx.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
	})
}
