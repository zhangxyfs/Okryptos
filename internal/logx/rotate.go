package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxLogBytes 单个在役日志的大小上限：超过即在下次打开/拉起窗口轮替归档。
// 包级变量供测试调小。
var maxLogBytes = int64(8 << 20)

// archiveKeepDays 归档日志保留天数：过期归档在轮替发生时与 daemon 启动时清理。
var archiveKeepDays = 7

// RotateIfOversize 把超限日志改名归档到同级 logs/ 目录（<名>-<日期>_<时分秒>.log），
// 之后调用方以 O_CREATE 重开即是新文件。必须在句柄释放窗口调用（daemon/sidecar
// 拉起前、ok.log append 打开前）——Windows 上被占用的文件改不了名，改名失败
// 跳过本轮、日志继续增长，下次窗口再轮替。fail-open：任何失败都不影响主流程。
// 发生轮替时顺带清理过期归档。
func RotateIfOversize(path string) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= maxLogBytes {
		return
	}
	dir := filepath.Join(filepath.Dir(path), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	stamp := time.Now().Format("2006-01-02_150405")
	name := base + "-" + stamp + ".log"
	for i := 2; ; i++ { // 同一秒多次轮替撞名：追加序号
		if _, err := os.Lstat(filepath.Join(dir, name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s-%s_%d.log", base, stamp, i)
	}
	if err := os.Rename(path, filepath.Join(dir, name)); err != nil {
		return
	}
	CleanArchives(dir)
}

// CleanArchives 删除 logs 目录里超过保留期的归档 .log（按文件修改时间）。
// 只在低频次时机调用（轮替发生、daemon 启动），不做热路径调用。
func CleanArchives(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Duration(archiveKeepDays) * 24 * time.Hour)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
