package deployx

import (
	"context"
	"errors"
	"io"
	"strings"
)

// sudoExecutor 把 Executor 的所有命令包装成 sudo 执行（NAS SSH 用户不在
// docker 组的场景：CLI 装了但 daemon socket 权限不足）。密码经 stdin 首行
// 喂给 sudo -S，只存内存、不进日志。
type sudoExecutor struct {
	inner Executor
	pw    string
}

var _ Executor = (*sudoExecutor)(nil)

// WrapSudo 返回 sudo 包装版 Executor；pw 为空报错。
func WrapSudo(inner Executor, pw string) (Executor, error) {
	if pw == "" {
		return nil, errors.New("sudo 密码不能为空")
	}
	return &sudoExecutor{inner: inner, pw: pw}, nil
}

// sudoCmd 包装命令：-k 清缓存防误用既有时间戳，-S stdin 读密码，-p '' 关提示符。
// 命令经 shellQuote 进 sh -c，sudo 只读走 stdin 首行，余下 stdin 留给命令本身
// （上传文件的 cat 通道不受影响）。
func sudoCmd(cmd string) string {
	return "sudo -kS -p '' -- sh -c " + shellQuote(cmd)
}

func (s *sudoExecutor) Run(ctx context.Context, cmd string, stdin io.Reader, onLine func(string, string)) (int, error) {
	in := io.Reader(strings.NewReader(s.pw + "\n"))
	if stdin != nil {
		in = io.MultiReader(in, stdin)
	}
	return s.inner.Run(ctx, sudoCmd(cmd), in, onLine)
}

// Download 没有 stdin 参数，密码用 echo 管道喂入。该命令串不经过 runCmd/runCmdMask
//（备份下载只向 Hub 发进度行），密码不会进日志。
func (s *sudoExecutor) Download(ctx context.Context, cmd string, w io.Writer, onProgress func(int64)) error {
	return s.inner.Download(ctx, "echo "+shellQuote(s.pw)+" | sudo -kS -p '' -- sh -c "+shellQuote(cmd), w, onProgress)
}

func (s *sudoExecutor) Close() error { return s.inner.Close() }
