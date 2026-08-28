package syncx

import (
	"fmt"
	"os"
	"path/filepath"
)

// Repo 是一个项目数据目录对应的 git 仓视图。
type Repo struct {
	Dir string
}

// Open 返回 dir 的仓视图（不校验是否为仓，校验用 IsRepo）。
func Open(dir string) *Repo { return &Repo{Dir: dir} }

// IsRepo 报告 dir 是否已是 git 仓。
func (r *Repo) IsRepo() bool {
	_, err := execGit(r.Dir, localTimeout, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

// gitignoreContent 随 git init 生成（设计文档 §3）：索引/状态/日志不入仓。
const gitignoreContent = "kb.db\nkb.db-*\nstate/\n*.log\n"

// Init 原地成仓：git init -b main + 写 .gitignore + 初始提交（仓保持干净，文件不挪动）。
func (r *Repo) Init() error {
	if _, err := execGit(r.Dir, localTimeout, "init", "-b", "main"); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.Dir, ".gitignore"), []byte(gitignoreContent), 0o644); err != nil {
		return err
	}
	if _, err := execGit(r.Dir, localTimeout, "add", ".gitignore"); err != nil {
		return err
	}
	_, err := execGit(r.Dir, localTimeout,
		"-c", "user.name=OpenKnowledge Sync", "-c", "user.email=sync@openknowledge.local",
		"commit", "-m", "sync: init")
	return err
}

// RemoteURL 返回 origin 的 URL；未配置返回 ""。
func (r *Repo) RemoteURL() string {
	out, err := execGit(r.Dir, localTimeout, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return out
}

// SetRemote 配置 origin（不存在则 add，存在则改地址）。
func (r *Repo) SetRemote(url string) error {
	if r.RemoteURL() == "" {
		_, err := execGit(r.Dir, localTimeout, "remote", "add", "origin", url)
		return err
	}
	_, err := execGit(r.Dir, localTimeout, "remote", "set-url", "origin", url)
	return err
}

// CurrentBranch 返回当前分支名；尚无提交（unborn HEAD）时返回 main。
func (r *Repo) CurrentBranch() string {
	out, err := execGit(r.Dir, localTimeout, "branch", "--show-current")
	if err != nil || out == "" {
		return "main"
	}
	return out
}

// Status 返回工作区状态：dirty = 有未提交变更；ahead/behind 仅在有 upstream 时非零。
// 失败一律 fail-open 零值（状态展示不影响主链路）。
func (r *Repo) Status() (dirty bool, ahead, behind int) {
	out, err := execGit(r.Dir, localTimeout, "status", "--porcelain")
	dirty = err == nil && out != ""
	ab, err := execGit(r.Dir, localTimeout, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	if err != nil {
		return dirty, 0, 0
	}
	// rev-list --left-right：左 = 仅 upstream 有的提交（behind），右 = 仅本地有的提交（ahead）。
	var b, a int
	if _, err := fmt.Sscanf(ab, "%d\t%d", &b, &a); err == nil {
		ahead, behind = a, b
	}
	return dirty, ahead, behind
}

// CommitAll 暂存全部变更；有内容才提交（身份内置，不依赖用户全局配置）。
func (r *Repo) CommitAll(msg string) (bool, error) {
	if _, err := execGit(r.Dir, localTimeout, "add", "-A"); err != nil {
		return false, err
	}
	if _, err := execGit(r.Dir, localTimeout, "diff", "--cached", "--quiet"); err == nil {
		return false, nil // 无暂存变更
	}
	if _, err := execGit(r.Dir, localTimeout,
		"-c", "user.name=OpenKnowledge Sync", "-c", "user.email=sync@openknowledge.local",
		"commit", "-m", msg); err != nil {
		return false, err
	}
	return true, nil
}

// Push 推送当前分支；尚无 upstream 时建立跟踪。
func (r *Repo) Push() error {
	if _, err := execGit(r.Dir, localTimeout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err != nil {
		_, err = execGit(r.Dir, networkTimeout, "push", "-u", "origin", r.CurrentBranch())
		return err
	}
	_, err := execGit(r.Dir, networkTimeout, "push")
	return err
}

// CloneToDir 把 url 克进 Dir——Dir 允许含骨架文件（config.toml/state/ 等）：
// clone 到临时目录 → 移 .git 进来 → checkout 覆盖工作区（设计文档 §14"无知识内容"情形）。
func (r *Repo) CloneToDir(url string) error {
	tmp, err := os.MkdirTemp("", "ok-sync-clone")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := execGit("", networkTimeout, "-c", "core.autocrlf=false", "clone", url, tmp); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(tmp, ".git"), filepath.Join(r.Dir, ".git")); err != nil {
		return err
	}
	// 强制 autocrlf=false：库内 blob 是 LF，checkout 不做 CRLF 转换（Windows 全局 autocrlf=true 时也能逐字节还原）。
	_, err = execGit(r.Dir, localTimeout, "-c", "core.autocrlf=false", "checkout", "--", ".")
	return err
}
