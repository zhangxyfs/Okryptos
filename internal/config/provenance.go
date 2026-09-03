package config

import (
	"errors"
	"os"
	"strconv"

	"okryptos/internal/fsx"
)

// SetCaptureAndAutoBorn 在一次锁内读-改-写中同时重写 [capture] 小节与
// [provenance] 小节（auto_born 键），单次落盘；其余内容（含注释）原样保留。
// GUI capture 设置接口若拆成 SetCapture + 单独写 auto_born 两步独立落盘，
// 第二步失败时会留下 capture 已改而 auto_born 未改的中间态，故合并为一次锁内写。
// 文件不存在时以 header 为初始内容创建（调用方决定头部模板，可为空）。
// 锁纪律同 SetCapture。
func SetCaptureAndAutoBorn(path, mode string, turnInterval int, autoBorn bool, header string) error {
	capture := captureBlock(mode, turnInterval)
	provenance := "[provenance]\nauto_born = " + strconv.FormatBool(autoBorn) + "\n"
	return fsx.WithFileLock(path, func() error {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return fsx.WriteFile(path, []byte(header+capture+"\n"+provenance), 0o644)
		}
		if err != nil {
			return err
		}
		content := replaceSection(string(data), "[capture]", capture)
		content = replaceSection(content, "[provenance]", provenance)
		return fsx.WriteFile(path, []byte(content), 0o644)
	})
}
