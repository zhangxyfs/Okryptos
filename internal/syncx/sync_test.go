package syncx

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// mkPair 建 bare + 两台已关联设备仓，返回 (bare, dirA, dirB)。
func mkPair(t *testing.T) (string, string, string) {
	t.Helper()
	bare := t.TempDir()
	if _, err := execGit(bare, localTimeout, "init", "--bare", "-b", "main"); err != nil {
		t.Fatalf("bare: %v", err)
	}
	dirA := t.TempDir()
	ra := Open(dirA)
	if err := ra.Init(); err != nil {
		t.Fatalf("A init: %v", err)
	}
	writeFile(t, dirA, "k.md", "v1\n")
	if _, err := ra.CommitAll("init"); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	if err := ra.SetRemote(bare); err != nil {
		t.Fatalf("A remote: %v", err)
	}
	if err := ra.Push(); err != nil {
		t.Fatalf("A push: %v", err)
	}
	dirB := t.TempDir()
	if err := Open(dirB).CloneToDir(bare); err != nil {
		t.Fatalf("B clone: %v", err)
	}
	return bare, dirA, dirB
}

func TestSyncNoRemote(t *testing.T) {
	dir := t.TempDir()
	r := Open(dir)
	if err := r.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	writeFile(t, dir, "k.md", "v1\n")
	o := r.Sync("sync: test")
	if o.Err != nil || !o.NoRemote || !o.Committed {
		t.Fatalf("outcome: %+v", o)
	}
}

func TestSyncNotRepo(t *testing.T) {
	o := Open(t.TempDir()).Sync("sync: test")
	if !o.NotRepo {
		t.Fatalf("want NotRepo: %+v", o)
	}
}

func TestSyncPushPullCounts(t *testing.T) {
	_, dirA, dirB := mkPair(t)
	// A 改并 sync → 推 1
	writeFile(t, dirA, "k.md", "v2\n")
	if _, err := Open(dirA).CommitAll("sync: a"); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	// 推送前：本地领先 1 个提交
	if _, ahead, _ := Open(dirA).Status(); ahead != 1 {
		t.Fatalf("A ahead before push = %d, want 1", ahead)
	}
	o := Open(dirA).Sync("sync: a")
	if o.Err != nil || o.Pushed != 1 || o.Pulled != 0 {
		t.Fatalf("A outcome: %+v err=%v", o, o.Err)
	}
	// B sync → 拉 1
	o = Open(dirB).Sync("sync: b")
	if o.Err != nil || o.Pulled != 1 || o.Pushed != 0 {
		t.Fatalf("B outcome: %+v err=%v", o, o.Err)
	}
	data, _ := os.ReadFile(filepath.Join(dirB, "k.md"))
	if string(data) != "v2\n" {
		t.Fatalf("B content: %q", data)
	}
	// 再 sync：已是最新
	o = Open(dirB).Sync("sync: b")
	if o.Err != nil || o.Pulled != 0 || o.Pushed != 0 || o.Committed {
		t.Fatalf("B idle outcome: %+v", o)
	}
}

func TestSyncConflictStopsPush(t *testing.T) {
	_, dirA, dirB := mkPair(t)
	// A 改同一行为 v2a 并推
	writeFile(t, dirA, "k.md", "v2a\n")
	if o := Open(dirA).Sync("sync: a"); o.Err != nil {
		t.Fatalf("A sync: %v", o.Err)
	}
	// B 改同一行为 v2b 并 sync → 冲突
	writeFile(t, dirB, "k.md", "v2b\n")
	o := Open(dirB).Sync("sync: b")
	if o.Err != nil {
		t.Fatalf("conflict should not be Err: %v", o.Err)
	}
	if len(o.Conflicts) != 1 || o.Conflicts[0] != "k.md" {
		t.Fatalf("conflicts: %v", o.Conflicts)
	}
	if o.Pushed != 0 {
		t.Fatalf("must not push on conflict: %+v", o)
	}
	// 内容不丢：工作区含 B 的改动（冲突标记内）
	data, _ := os.ReadFile(filepath.Join(dirB, "k.md"))
	if !strings.Contains(string(data), "v2b") {
		t.Fatalf("B content lost: %q", data)
	}
	// rebase 停在半途
	if _, err := execGit(dirB, localTimeout, "rev-parse", "--verify", "--quiet", "REBASE_HEAD"); err != nil {
		t.Fatal("rebase should be in progress")
	}
	// 清理
	_, _ = execGit(dirB, localTimeout, "rebase", "--abort")
}

func TestSyncDuringConflictRefuses(t *testing.T) {
	_, dirA, dirB := mkPair(t)
	// A 改同一行并推
	writeFile(t, dirA, "k.md", "v2a\n")
	if o := Open(dirA).Sync("sync: a"); o.Err != nil {
		t.Fatalf("A sync: %v", o.Err)
	}
	// B 改同一行并 sync → 冲突，rebase 停半途
	writeFile(t, dirB, "k.md", "v2b\n")
	o := Open(dirB).Sync("sync: b")
	if len(o.Conflicts) == 0 {
		t.Fatalf("want conflicts: %+v", o)
	}
	count := func() string {
		out, err := execGit(dirB, localTimeout, "rev-list", "--count", "HEAD")
		if err != nil {
			t.Fatalf("rev-list: %v", err)
		}
		return out
	}
	before := count()
	// 冲突未解决时再 Sync：直接返回未决冲突，不提交、不吞冲突标记
	o = Open(dirB).Sync("sync: again")
	if len(o.Conflicts) == 0 {
		t.Fatalf("want conflicts on re-sync: %+v", o)
	}
	if o.Committed {
		t.Fatalf("must not commit during conflict: %+v", o)
	}
	if after := count(); after != before {
		t.Fatalf("HEAD moved during conflict: %s → %s", before, after)
	}
	// 清理
	_, _ = execGit(dirB, localTimeout, "rebase", "--abort")
}

func TestSyncDuringConflictResolvedStaged(t *testing.T) {
	_, dirA, dirB := mkPair(t)
	// A 改同一行并推
	writeFile(t, dirA, "k.md", "v2a\n")
	if o := Open(dirA).Sync("sync: a"); o.Err != nil {
		t.Fatalf("A sync: %v", o.Err)
	}
	// B 改同一行并 sync → 冲突，rebase 停半途
	writeFile(t, dirB, "k.md", "v2b\n")
	if o := Open(dirB).Sync("sync: b"); len(o.Conflicts) == 0 {
		t.Fatalf("want conflicts: %+v", o)
	}
	// 用户手工改好（去掉冲突标记）并暂存，但不 rebase --continue
	writeFile(t, dirB, "k.md", "v2 resolved\n")
	if _, err := execGit(dirB, localTimeout, "add", "-A"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// 此时 U 列表为空但 rebase 未结束：必须报错引导 continue/abort，不能谎报成功
	o := Open(dirB).Sync("sync: again")
	if o.Err == nil {
		t.Fatalf("want Err on staged-but-uncontinued rebase: %+v", o)
	}
	if !strings.Contains(o.Err.Error(), "rebase --continue") {
		t.Fatalf("Err should guide to rebase --continue: %v", o.Err)
	}
	if o.Committed {
		t.Fatalf("must not commit during rebase: %+v", o)
	}
	// 清理
	_, _ = execGit(dirB, localTimeout, "rebase", "--abort")
}

func TestSyncDuringMergeConflictRefuses(t *testing.T) {
	_, dirA, dirB := mkPair(t)
	// A 改同一行并推
	writeFile(t, dirA, "k.md", "v2a\n")
	if o := Open(dirA).Sync("sync: a"); o.Err != nil {
		t.Fatalf("A sync: %v", o.Err)
	}
	// B 改同一行并本地提交（不走 Sync——Sync 的 pull --rebase 不会制造 MERGE_HEAD）
	writeFile(t, dirB, "k.md", "v2b\n")
	if _, err := Open(dirB).CommitAll("sync: b"); err != nil {
		t.Fatalf("B commit: %v", err)
	}
	// 复现 ok sync init 情形 3 的官方指引路径：用户手工 merge 远端，冲突停半途。
	// pull 默认 merge 策略，两边改了同一行 → 冲突停下，留下 MERGE_HEAD。
	_, _ = execGit(dirB, networkTimeout, "pull", "--no-rebase")
	if _, err := execGit(dirB, localTimeout, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err != nil {
		t.Fatal("merge should be in progress (MERGE_HEAD)")
	}
	count := func() string {
		out, err := execGit(dirB, localTimeout, "rev-list", "--count", "HEAD")
		if err != nil {
			t.Fatalf("rev-list: %v", err)
		}
		return out
	}
	before := count()
	// merge 冲突未解决时 Sync：直接返回未决冲突，不提交、不吞冲突标记
	o := Open(dirB).Sync("sync: b")
	if len(o.Conflicts) == 0 {
		t.Fatalf("want conflicts on sync during merge: %+v", o)
	}
	if o.Committed {
		t.Fatalf("must not commit during merge: %+v", o)
	}
	if after := count(); after != before {
		t.Fatalf("HEAD moved during merge: %s → %s", before, after)
	}
	// 清理
	_, _ = execGit(dirB, localTimeout, "merge", "--abort")
}

func TestSyncOnceSingleFlight(t *testing.T) {
	_, dirA, _ := mkPair(t)
	writeFile(t, dirA, "k.md", "v3\n")
	const n = 8
	var wg sync.WaitGroup
	outs := make([]Outcome, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outs[i] = SyncOnce(dirA, "sync: concurrent")
		}(i)
	}
	wg.Wait()
	pushed := 0
	for _, o := range outs {
		if o.Err != nil {
			t.Fatalf("err: %v", o.Err)
		}
		pushed += o.Pushed
	}
	// 并发合并：最多一次执行真的推出 1 个提交；其余等同结果或零
	if pushed > 1 {
		t.Fatalf("single-flight violated: pushed total %d", pushed)
	}
}
