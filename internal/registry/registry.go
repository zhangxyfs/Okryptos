package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"

	"openknowledge/internal/fsx"
)

type Project struct {
	Name  string   `toml:"name"`
	Paths []string `toml:"paths"`
}

type Registry struct {
	Projects []Project `toml:"project"`
}

// Home 返回知识库根目录：OK_HOME 环境变量优先，否则真实用户目录下的 ~/.openknowledge。
// 真实目录解析对 HOME/USERPROFILE 重定向免疫——CodePilot 等宿主 spawn 子进程时会把
// 它们重定向到 shadow 临时目录做 provider 隔离，跟随重定向会看到空数据根而静默失效。
func Home() string {
	if h := os.Getenv("OK_HOME"); h != "" {
		return h
	}
	if home, err := realProfileDir(); err == nil && home != "" {
		return filepath.Join(home, ".openknowledge")
	}
	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, ".openknowledge")
	}
	return fallbackHome()
}

var (
	fallbackHomeOnce sync.Once
	fallbackHomeDir  string
)

// fallbackHome 是 realProfileDir 与 os.UserHomeDir 双重失败时的兜底根：
// 裸相对路径 ".openknowledge" 会让数据根随 cwd 漂移，且两次调用可能解析到
// 不同目录——进程内只解析一次并转绝对路径，保证一致性。
func fallbackHome() string {
	fallbackHomeOnce.Do(func() {
		if abs, err := filepath.Abs(".openknowledge"); err == nil {
			fallbackHomeDir = abs
		} else {
			fallbackHomeDir = ".openknowledge"
		}
	})
	return fallbackHomeDir
}

func DefaultPath() string { return filepath.Join(Home(), "registry.toml") }

// NormalizePath 统一路径用于比较：分隔符转为 "/"，去掉尾部 "/"。
// 小写折叠仅 Windows 生效——Linux 文件系统大小写敏感，/src/Foo 与 /src/foo
// 是不同目录，折叠会让两个项目互相遮蔽（FindByCwd 命中先注册者串库）。
func NormalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimRight(p, "/")
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func Load(path string) (*Registry, error) {
	r := &Registry{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	return r, nil
}

func (r *Registry) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(r); err != nil {
		return err
	}
	return fsx.WriteFile(path, []byte(buf.String()), 0o644)
}

// FindByCwd 按规范化路径最长前缀匹配项目；未命中返回 nil。
func (r *Registry) FindByCwd(cwd string) *Project {
	ncwd := NormalizePath(cwd)
	var best *Project
	bestLen := -1
	for i := range r.Projects {
		for _, p := range r.Projects[i].Paths {
			np := NormalizePath(p)
			if ncwd == np || strings.HasPrefix(ncwd, np+"/") {
				if len(np) > bestLen {
					bestLen = len(np)
					best = &r.Projects[i]
				}
			}
		}
	}
	return best
}

func (r *Registry) AddProject(name, path string) error {
	npath := NormalizePath(path)
	for _, p := range r.Projects {
		if p.Name == name {
			return fmt.Errorf("项目 %q 已存在", name)
		}
		// 大小写冲突同样拒绝：Windows 上 projects/Foo 与 projects/foo 是同一
		// 目录，两个项目会共享同一 store 串数据
		if strings.EqualFold(p.Name, name) {
			return fmt.Errorf("项目 %q 与已注册的 %q 仅大小写不同（Windows 上会共享同一知识库目录）", name, p.Name)
		}
		// 同路径冲突拒绝：同目录换名重复注册后 FindByCwd 仍命中先注册者，
		// ok add 会静默写进旧项目知识库（串库且无告警）
		for _, ep := range p.Paths {
			if NormalizePath(ep) == npath {
				return fmt.Errorf("路径 %q 已注册给项目 %q（同目录重复注册会导致写串知识库）", path, p.Name)
			}
		}
	}
	r.Projects = append(r.Projects, Project{Name: name, Paths: []string{path}})
	return nil
}

// ValidProjectName 校验项目名形状：必须是不含路径分隔符与盘符的基本名，
// 且不是 Windows 保留设备名/尾部带点或空格的名字。项目名会被拼进
// projects/<name>/ 目录路径，穿越段（../、绝对路径、C: 盘符）与 NTFS 上
// 无法创建的名字一律拒绝；ok init 与备份导入在写注册表前调用（与 GUI 同款校验）。
func ValidProjectName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if name != filepath.Base(name) {
		return false
	}
	if strings.ContainsAny(name, `\/:`) {
		return false
	}
	// Windows 会把尾部点/空格静默截掉，实际目录名与注册名漂移
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	// 保留设备名（大小写不敏感，且 "con.txt" 这类带扩展形态同样被保留）
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToLower(base) {
	case "con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return false
	}
	return true
}

// RemoveProject 按名移除项目，返回是否找到；持久化需另调 Save。
func (r *Registry) RemoveProject(name string) bool {
	for i, p := range r.Projects {
		if p.Name == name {
			r.Projects = append(r.Projects[:i], r.Projects[i+1:]...)
			return true
		}
	}
	return false
}

// HooksDisabled 报告 hooks 全局开关是否关闭（标志文件存在）。
func HooksDisabled() bool {
	_, err := os.Stat(filepath.Join(Home(), "hooks-disabled"))
	return err == nil
}

// Update 在跨进程锁内完成注册表的读-改-写。并发的 ok init、GUI 删除项目、备份
// 恢复各自 Load→改→Save 会互相覆盖（后写者吃掉先写者的项目注册——该项目的
// hooks 从此全部失效且无任何报错）。锁用严格模式（fsx.WithFileLockStrict）：
// 等满超时返回错误而非 fail-open 裸跑——无锁读-改-写正是丢注册的复现路径，
// 与 hook 的 state 会话锁（fail-open 可接受）语义不同。
func Update(fn func(*Registry) error) error {
	return fsx.WithFileLockStrict(DefaultPath(), func() error {
		reg, err := Load(DefaultPath())
		if err != nil {
			return err
		}
		if err := fn(reg); err != nil {
			return err
		}
		return reg.Save(DefaultPath())
	})
}
