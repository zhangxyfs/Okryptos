package config

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"okryptos/internal/fsx"
)

// SetCapture 重写 config.toml 的 [capture] 小节：已存在则整段替换（到下一个
// [section] 或文件尾），不存在则在文件尾追加；其余内容（含注释）原样保留。
// 文件不存在时以 header 为初始内容创建（调用方决定头部模板，可为空）。
// 读-改-写包在 fsx.WithFileLock 内：行级写基于读时快照整体写回，与锁内整档
// 写者（updateGlobalConfig 保存 LLM/embedding profile）交错时裸跑会把对方
// 刚写入的内容静默回滚。
func SetCapture(path, mode string, turnInterval int, header string) error {
	block := captureBlock(mode, turnInterval)
	return fsx.WithFileLock(path, func() error {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return fsx.WriteFile(path, []byte(header+block), 0o644)
		}
		if err != nil {
			return err
		}
		return fsx.WriteFile(path, []byte(replaceSection(string(data), "[capture]", block)), 0o644)
	})
}

func captureBlock(mode string, turnInterval int) string {
	return "[capture]\nmode = " + strconv.Quote(mode) + "\nturn_interval = " + strconv.Itoa(turnInterval) + "\n"
}

// replaceSection 返回 content 中整段替换 name 小节后的内容：小节已存在则替换到
// 下一个 [section] 或文件尾，不存在则文件尾追加（与上文保持空行分隔）；其余
// 内容（含注释）原样保留。纯函数，落盘与加锁由调用方负责。
func replaceSection(content, name, block string) string {
	lines := strings.Split(content, "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if t == name {
				start = i
			}
			continue
		}
		if strings.HasPrefix(t, "[") {
			end = i
			break
		}
	}
	var out []string
	if start >= 0 {
		out = append(out, lines[:start]...)
		out = append(out, strings.TrimSuffix(block, "\n"))
		out = append(out, lines[end:]...)
	} else {
		out = append(out, lines...)
		// 与上文保持空行分隔
		if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) != "" {
			out = append(out, "")
		}
		out = append(out, strings.TrimSuffix(block, "\n"))
	}
	return strings.Join(out, "\n")
}
