// 备份/恢复是停机一致性语义：先 stop 容器再打包/解压，与
// server/nas/backup/backup.sh 的在线备份语义不同；停机窗口通常数秒，v1 从简取安全。
package deployx

import (
	"bytes"
	"context"
	"fmt"
	"time"
)

// BackupTimeout 是备份/恢复流传输的超时（数据卷可能很大）。
const BackupTimeout = 30 * time.Minute

// BackupCmd 产出 tar 流的远端命令（调用前容器已停，保证一致性）。
// dir 已过 ValidateDir，原样使用。数据目录可能不存在其一（external 模式无
// gitea-data），用 --ignore-failed-read 容错。
func BackupCmd(dir string) string {
	return "cd " + dir + " && tar --ignore-failed-read -cf - okserver-data gitea-data"
}

// BuildBackupStopTask 备份前半段：停容器（HTTP 层随后用 BackupCmd 流式下载）。
func BuildBackupStopTask(dir string) Task {
	return Task{Name: "备份（停容器）", Steps: []Step{
		{Name: "停止容器", Run: func(ctx context.Context, e *Env) error {
			_, err := runCmd(ctx, e, "停止容器", composeCmd(dir, "stop"))
			return err
		}},
	}}
}

// BuildStartTask 备份/恢复后半段：起容器 + 健康检查。
func BuildStartTask(dir string) Task {
	return Task{Name: "启动服务", Steps: []Step{
		newReadOKPortStep(dir),
		{Name: "启动容器", Run: func(ctx context.Context, e *Env) error {
			_, err := runCmd(ctx, e, "启动容器", composeCmd(dir, "up -d"))
			return err
		}},
		healthStep(dir),
	}}
}

// BuildRestoreTask 恢复：停 → 清旧数据 → stdin 解压备份包 → 起 → 健康检查。
func BuildRestoreTask(dir string, data []byte) Task {
	return Task{Name: "恢复备份", Steps: []Step{
		{Name: "停止容器", Run: func(ctx context.Context, e *Env) error {
			_, err := runCmd(ctx, e, "停止容器", composeCmd(dir, "stop"))
			return err
		}},
		{Name: "清理旧数据", Run: func(ctx context.Context, e *Env) error {
			_, err := runCmd(ctx, e, "清理旧数据",
				"rm -rf "+dir+"/okserver-data "+dir+"/gitea-data")
			return err
		}},
		{Name: "解压备份包", Run: func(ctx context.Context, e *Env) error {
			ctxT, cancel := context.WithTimeout(ctx, BackupTimeout)
			defer cancel()
			e.Hub.Publish("解压备份包", "info", "$ tar -xf -（上传 "+dir+"）")
			code, err := e.Ex.Run(ctxT, "tar -xf - -C "+dir, bytes.NewReader(data),
				func(stream, line string) { e.Hub.Publish("解压备份包", "info", line) })
			if err != nil {
				return err
			}
			if code != 0 {
				return fmt.Errorf("解压失败，退出码 %d", code)
			}
			return nil
		}},
		newReadOKPortStep(dir),
		{Name: "启动容器", Run: func(ctx context.Context, e *Env) error {
			_, err := runCmd(ctx, e, "启动容器", composeCmd(dir, "up -d"))
			return err
		}},
		healthStep(dir),
	}}
}
