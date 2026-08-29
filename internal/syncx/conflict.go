// conflict.go：冲突解决原语（设计文档 §5，供 GUI 冲突页使用，CLI 不消费）。
// 纪律（知识库条目）：pull --rebase 冲突期间 stage 语义反转——
// :1:=base（共同祖先）、:2:=远端（刚拉下来的）、:3:=本地（正在 replay 的提交）。
// 本文件对外只暴露用户语义（local/remote），严禁直译 git ours/theirs。
package syncx

import (
	"os"
	"path/filepath"
	"strings"
)

// ConflictFiles 返回当前冲突文件列表；非冲突态返回 nil。
func (r *Repo) ConflictFiles() []string {
	out, err := execGit(r.Dir, localTimeout, "diff", "--name-only", "--diff-filter=U")
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// ConflictVersions 取冲突文件的三版本（用户语义）：
// base=共同祖先，local=本机改动（stage :3:），remote=远端改动（stage :2:）。
func (r *Repo) ConflictVersions(file string) (base, local, remote string, err error) {
	show := func(stage string) (string, error) {
		return execGit(r.Dir, localTimeout, "show", ":"+stage+":"+file)
	}
	if base, err = show("1"); err != nil {
		return "", "", "", err
	}
	if remote, err = show("2"); err != nil {
		return "", "", "", err
	}
	if local, err = show("3"); err != nil {
		return "", "", "", err
	}
	return base, local, remote, nil
}

// ResolveFile 写入解决结果并 git add。
func (r *Repo) ResolveFile(file, content string) error {
	path := filepath.Join(r.Dir, file)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	_, err := execGit(r.Dir, localTimeout, "add", "--", file)
	return err
}

// MergeInProgress 报告 rebase 或 merge 是否进行在半途。
// 注意：不能用 rev-parse --verify REBASE_HEAD——rebase 成功结束后该引用仍残留
// （仅 --abort 会清除），会误判为仍在进行中。rebase 以 rebase-merge/rebase-apply
// 状态目录为准，merge 以 MERGE_HEAD 为准。
// 状态路径经 rev-parse --git-path 取（linked worktree 下 .git 是文件，硬拼
// .git/<name> 会假阴）；git 可能返回绝对路径，需 IsAbs 判断后再决定是否 Join。
func (r *Repo) MergeInProgress() bool {
	for _, name := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD"} {
		out, err := execGit(r.Dir, localTimeout, "rev-parse", "--git-path", name)
		if err != nil || out == "" {
			continue
		}
		path := out
		if !filepath.IsAbs(path) {
			path = filepath.Join(r.Dir, path)
		}
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// ContinueRebase 全部解决后续推。GIT_EDITOR=true 防唤起编辑器挂起。
func (r *Repo) ContinueRebase() error {
	_, err := execGit(r.Dir, localTimeout, "-c", "core.editor=true", "rebase", "--continue")
	return err
}

// AbortRebase 回滚到同步前状态（本地内容原样保留）。
func (r *Repo) AbortRebase() error {
	_, err := execGit(r.Dir, localTimeout, "rebase", "--abort")
	return err
}
