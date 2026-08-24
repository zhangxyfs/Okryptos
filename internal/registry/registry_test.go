package registry

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	got := NormalizePath(`D:\develop\OpenKnowledge\`)
	want := "d:/develop/openknowledge"
	if runtime.GOOS != "windows" {
		// 小写折叠仅 Windows：Linux 大小写敏感 FS 上仅大小写不同的目录不得互相遮蔽
		want = "D:/develop/OpenKnowledge"
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFindByCwdLongestPrefix(t *testing.T) {
	r := &Registry{Projects: []Project{
		{Name: "root", Paths: []string{`D:\develop`}},
		{Name: "ok", Paths: []string{`D:\develop\OpenKnowledge`}},
	}}
	// 大小写不敏感匹配仅 Windows 语义；Linux 下大小写不同的路径不命中
	cwd := `d:\DEVELOP\OpenKnowledge\docs`
	if runtime.GOOS != "windows" {
		cwd = `D:\develop\OpenKnowledge\docs`
	}
	p := r.FindByCwd(cwd)
	if p == nil || p.Name != "ok" {
		t.Fatalf("expected ok, got %+v", p)
	}
	if p := r.FindByCwd(`E:\other`); p != nil {
		t.Fatalf("expected nil, got %+v", p)
	}
}

func TestLoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.toml")
	r := &Registry{}
	if err := r.AddProject("ok", `D:\develop\OpenKnowledge`); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Projects) != 1 || loaded.Projects[0].Name != "ok" {
		t.Fatalf("unexpected %+v", loaded)
	}
	if err := loaded.AddProject("ok", `E:\x`); err == nil {
		t.Fatal("expected duplicate name error")
	}
}

// ValidProjectName 是注册表写入前的形状闸门：路径穿越、盘符/分隔符、
// Windows 保留设备名与尾部点/空格一律拒绝。
func TestValidProjectName(t *testing.T) {
	valid := []string{"demo", "my-proj_2", "变更日志", "console", "com10", "a.b"}
	for _, n := range valid {
		if !ValidProjectName(n) {
			t.Fatalf("ValidProjectName(%q) = false, want true", n)
		}
	}
	invalid := []string{
		"", ".", "..",
		`..\x`, `foo/bar`, `foo\bar`, `C:\x`, "c:", "/abs",
		"con", "CON", "Nul", "com1", "lpt9", "con.txt", // Windows 保留设备名
		"foo.", "foo ", // 尾部点/空格会被 Windows 静默截掉
	}
	for _, n := range invalid {
		if ValidProjectName(n) {
			t.Fatalf("ValidProjectName(%q) = true, want false", n)
		}
	}
}

// 大小写冲突拒绝：Windows 上 projects/Foo 与 projects/foo 是同一目录。
func TestAddProjectCaseConflict(t *testing.T) {
	r := &Registry{}
	if err := r.AddProject("Foo", `D:\src\foo`); err != nil {
		t.Fatal(err)
	}
	if err := r.AddProject("foo", `D:\src\foo2`); err == nil {
		t.Fatal("expected case-conflict error")
	}
	if len(r.Projects) != 1 {
		t.Fatalf("冲突项目不应入册: %+v", r.Projects)
	}
}

// 同路径重复注册拒绝：同目录换名再注册会让 FindByCwd 仍命中先注册者，
// ok add 静默写串到旧项目知识库。
func TestAddProjectDuplicatePath(t *testing.T) {
	r := &Registry{}
	if err := r.AddProject("alpha", "D:/src/foo"); err != nil {
		t.Fatal(err)
	}
	// 分隔符与尾部斜杠差异不影响冲突判定（两平台一致）
	if err := r.AddProject("beta", `D:\src\foo\`); err == nil {
		t.Fatal("expected duplicate path error")
	}
	if len(r.Projects) != 1 {
		t.Fatalf("冲突项目不应入册: %+v", r.Projects)
	}
	// FindByCwd 语义不变：仍命中先注册者
	if p := r.FindByCwd("D:/src/foo/sub"); p == nil || p.Name != "alpha" {
		t.Fatalf("expected alpha, got %+v", p)
	}
}

// 仅大小写不同的路径：Windows 上是同一目录（冲突拒绝），Linux 上是不同目录
// （允许注册，互不遮蔽）。
func TestAddProjectCaseOnlyPathByPlatform(t *testing.T) {
	r := &Registry{}
	if err := r.AddProject("upper", "/src/Foo"); err != nil {
		t.Fatal(err)
	}
	err := r.AddProject("lower", "/src/foo")
	if runtime.GOOS == "windows" {
		if err == nil {
			t.Fatal("Windows: expected case-fold path conflict error")
		}
	} else {
		if err != nil {
			t.Fatalf("Linux: 大小写不同目录不应互相遮蔽: %v", err)
		}
		if p := r.FindByCwd("/src/foo"); p == nil || p.Name != "lower" {
			t.Fatalf("expected lower, got %+v", p)
		}
		if p := r.FindByCwd("/src/Foo"); p == nil || p.Name != "upper" {
			t.Fatalf("expected upper, got %+v", p)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || len(r.Projects) != 0 {
		t.Fatalf("expected empty registry, got %+v err=%v", r, err)
	}
}

func TestRemoveProject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_HOME", dir)
	reg := &Registry{}
	if err := reg.AddProject("alpha", "D:/src/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject("beta", "D:/src/beta"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(DefaultPath()); err != nil {
		t.Fatal(err)
	}

	if !reg.RemoveProject("alpha") {
		t.Fatal("RemoveProject(alpha) = false, want true")
	}
	if reg.RemoveProject("alpha") {
		t.Fatal("RemoveProject(alpha) again = true, want false")
	}

	// Save 往返后注册表只剩 beta
	if err := reg.Save(DefaultPath()); err != nil {
		t.Fatal(err)
	}
	back, err := Load(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Projects) != 1 || back.Projects[0].Name != "beta" {
		t.Fatalf("unexpected projects after remove: %+v", back.Projects)
	}
}

// Update 并发写不丢注册：多个进程（ok init / GUI / 备份恢复）各自 AddProject
// 时，最终注册表必须包含全部项目——无锁的 Load→改→Save 后写者会吃掉先写者，
// 被覆盖项目的 hooks 从此全部失效且无报错。
func TestUpdateConcurrentWritersKeepAll(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OK_HOME", dir)
	const writers = 8
	done := make(chan error, writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			done <- Update(func(reg *Registry) error {
				return reg.AddProject(fmt.Sprintf("p%d", i), fmt.Sprintf("D:/src/p%d", i))
			})
		}(i)
	}
	for i := 0; i < writers; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	reg, err := Load(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Projects) != writers {
		t.Fatalf("lost updates: %d projects, want %d", len(reg.Projects), writers)
	}
}
