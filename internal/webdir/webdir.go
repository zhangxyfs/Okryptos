// Package webdir 定位 Web GUI 静态资源目录（cmd/ok 与 cmd/okd 共用）。
// 叶子包：只依赖标准库。
package webdir

import (
	"fmt"
	"os"
	"path/filepath"
)

// Find 依次尝试 <exe目录>/web 与 <当前目录>/web。
func Find() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		if dir := filepath.Join(filepath.Dir(exe), "web"); isDir(dir) {
			return dir, nil
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if dir := filepath.Join(cwd, "web"); isDir(dir) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("未找到 web 资源目录（<exe目录>/web 或 <当前目录>/web）")
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
