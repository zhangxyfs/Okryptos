package syncx

import (
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

// Init 原地成仓：git init -b main + 写 .gitignore。文件不挪动。
func (r *Repo) Init() error {
	if _, err := execGit(r.Dir, localTimeout, "init", "-b", "main"); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.Dir, ".gitignore"), []byte(gitignoreContent), 0o644)
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
