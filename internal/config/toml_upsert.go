package config

import (
	"errors"
	"os"
	"strings"

	"openknowledge/internal/fsx"
)

// upsertTomlKey 在 config.toml 的指定小节内 upsert 单个键：小节已存在则只
// 替换/追加该键行（其余键、注释与子表原样保留），小节不存在则文件尾追加整块。
// 多人共用笔触的小节（[inject]/[retrieve]）不能整段覆盖，GUI 配置写路径统一
// 走这里。落盘经 fsx.WriteFile 原子写；读-改-写包在 fsx.WithFileLock 内——
// 行级写基于读时快照整体写回，与锁内整档写者交错时裸跑会回滚对方的写入。
func upsertTomlKey(path, section, key, keyLine string) error {
	return fsx.WithFileLock(path, func() error {
		return upsertTomlKeyLocked(path, section, key, keyLine)
	})
}

func upsertTomlKeyLocked(path, section, key, keyLine string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fsx.WriteFile(path, []byte("["+section+"]\n"+keyLine+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if start < 0 {
			if t == "["+section+"]" {
				start = i
			}
			continue
		}
		// 任何后续小节头（含 [retrieve.gate] 这类子表与 [[enforce]] 数组表）
		// 都是本小节边界
		if strings.HasPrefix(t, "[") {
			end = i
			break
		}
	}
	if start < 0 {
		if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "["+section+"]", keyLine)
		return fsx.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
	}
	// 小节内 upsert：命中键行替换，未命中插到小节末尾
	hit := false
	for i := start + 1; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		// 键名边界判定：前缀命中后下一个非空字符必须是 "="，否则是
		// dedup_turns_v2 之类前缀相同的自定义键，不得误替换
		if strings.HasPrefix(t, key) && strings.HasPrefix(strings.TrimSpace(t[len(key):]), "=") {
			lines[i] = keyLine
			hit = true
			break
		}
	}
	if !hit {
		tail := append([]string{keyLine}, lines[end:]...)
		lines = append(lines[:end], tail...)
	}
	return fsx.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
