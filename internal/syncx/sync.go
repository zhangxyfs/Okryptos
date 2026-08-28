package syncx

import (
	"strings"
	"sync"
)

// Outcome 是 Sync 编排的结构化结果（设计文档 §7）。
// 冲突不是错误——Conflicts 非空且 Err 为 nil 表示停在 rebase 半途等人解决。
type Outcome struct {
	Committed bool     // 本次有新本地提交
	Pulled    int      // 拉取的提交数
	Pushed    int      // 推送的提交数
	Conflicts []string // 非空 = pull 冲突，已停止 push
	Err       error    // 真错误（git 缺失/网络/权限等）
	NoRemote  bool     // 未配置远端（仅本地历史）
	NotRepo   bool     // 目录不是 git 仓（需要先 ok sync init）
}

// PullRebase 拉取并变基。返回冲突文件列表（此时 rebase 停在半途，err=nil）。
func (r *Repo) PullRebase() ([]string, error) {
	if _, err := execGit(r.Dir, networkTimeout, "pull", "--rebase"); err != nil {
		// rebase 进行中 → 结构化冲突而非错误
		if _, rbErr := execGit(r.Dir, localTimeout, "rev-parse", "--verify", "--quiet", "REBASE_HEAD"); rbErr == nil {
			out, _ := execGit(r.Dir, localTimeout, "diff", "--name-only", "--diff-filter=U")
			var files []string
			if out != "" {
				files = strings.Split(out, "\n")
			}
			return files, nil
		}
		return nil, err
	}
	return nil, nil
}

// Sync 一次执行 = add -A → 有变更则 commit →（有远端）pull --rebase → push。
func (r *Repo) Sync(msg string) (o Outcome) {
	if !r.IsRepo() {
		o.NotRepo = true
		return o
	}
	// rebase 进行中 = 上次冲突未解决：直接报告未决冲突返回，不做任何提交/拉取——
	// 否则 CommitAll 会把冲突标记 add -A 提交进历史（冲突期应等人解决，设计文档 §7）。
	if _, err := execGit(r.Dir, localTimeout, "rev-parse", "--verify", "--quiet", "REBASE_HEAD"); err == nil {
		out, _ := execGit(r.Dir, localTimeout, "diff", "--name-only", "--diff-filter=U")
		if out != "" {
			o.Conflicts = strings.Split(out, "\n")
		}
		return o
	}
	// 有远端时先 fetch：Status 的 behind 读的是本地远程跟踪引用，
	// 不 fetch 永远看不到别人新推的提交（失败忽略——真网络错误由下面 pull 暴露）。
	if r.RemoteURL() != "" {
		_, _ = execGit(r.Dir, networkTimeout, "fetch", "origin")
	}
	_, _, behindBefore := r.Status()
	committed, err := r.CommitAll(msg)
	if err != nil {
		o.Err = err
		return o
	}
	o.Committed = committed
	if r.RemoteURL() == "" {
		o.NoRemote = true
		return o
	}
	conflicts, err := r.PullRebase()
	if err != nil {
		o.Err = err
		return o
	}
	if len(conflicts) > 0 {
		o.Conflicts = conflicts // 停止后续 push（设计文档 §7）
		return o
	}
	o.Pulled = behindBefore
	_, ahead, _ := r.Status()
	if err := r.Push(); err != nil {
		o.Err = err
		return o
	}
	o.Pushed = ahead
	return o
}

// single-flight：同 dir 并发请求合并为一次执行（设计文档 §4 串行化）。
var flights sync.Map // dir → *flight

type flight struct {
	done chan struct{}
	o    Outcome
}

// SyncOnce 对同 dir 的并发调用只执行一次，后到者等待并拿同一结果。
func SyncOnce(dir, msg string) Outcome {
	f := &flight{done: make(chan struct{})}
	actual, loaded := flights.LoadOrStore(dir, f)
	if loaded {
		af := actual.(*flight)
		<-af.done
		o := af.o
		// 动作由唯一执行者完成：计数（Committed/Pulled/Pushed）归执行者，
		// 后到者只见结果标志（Err/Conflicts/NoRemote/NotRepo），避免并发聚合时重复计数。
		o.Committed, o.Pulled, o.Pushed = false, 0, 0
		return o
	}
	defer flights.Delete(dir)
	f.o = Open(dir).Sync(msg)
	close(f.done)
	return f.o
}
