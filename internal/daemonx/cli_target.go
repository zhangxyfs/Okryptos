package daemonx

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// okExeName 三 exe 部署中 CLI（ok）的可执行文件名。
func okExeName() string {
	if runtime.GOOS == "windows" {
		return "ok.exe"
	}
	return "ok"
}

// CliTargetFor 由 self（当前 exe 的绝对路径）推导 CLI（ok）入口：self 是 okd
// （daemon 进程内注册 hooks/技能、兼容转发的场景）→ 同目录 ok；其余（ok 自身、
// 测试二进制）→ self。okd 孤儿部署（无同目录 ok）报错而不是回落 okd——gui-split
// 后 okd 无子命令，回落会静默注册/执行失效命令。
func CliTargetFor(self string) (string, error) {
	base := filepath.Base(self)
	if trimmed := strings.TrimSuffix(base, ".exe"); trimmed != "okd" {
		return self, nil
	}
	sibling := filepath.Join(filepath.Dir(self), okExeName())
	if _, err := os.Stat(sibling); err != nil {
		return "", fmt.Errorf("okd 同目录未找到 CLI %s（%w）", okExeName(), err)
	}
	return sibling, nil
}
