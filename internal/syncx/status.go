package syncx

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"openknowledge/internal/fsx"
)

// LayerStatus 是一层（personal/team）的同步状态——从第一天就按层建模（设计文档 §8）。
type LayerStatus struct {
	LastSync  time.Time `json:"last_sync"`
	Ahead     int       `json:"ahead"`
	Behind    int       `json:"behind"`
	Conflict  bool      `json:"conflict"`
	LastError string    `json:"last_error"`
}

// StatusFile 对应 state/sync-status.json。
type StatusFile struct {
	Layers map[string]*LayerStatus `json:"layers"`
}

func statusPath(stateDir string) string   { return filepath.Join(stateDir, "sync-status.json") }
func conflictPath(stateDir string) string { return filepath.Join(stateDir, "sync-conflict.json") }

// LoadStatus 读状态文件；不存在或损坏返回空骨架（fail-open）。
func LoadStatus(stateDir string) (*StatusFile, error) {
	sf := &StatusFile{Layers: map[string]*LayerStatus{}}
	data, err := os.ReadFile(statusPath(stateDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sf, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, sf); err != nil {
		return &StatusFile{Layers: map[string]*LayerStatus{}}, nil
	}
	if sf.Layers == nil {
		sf.Layers = map[string]*LayerStatus{}
	}
	return sf, nil
}

// Layer 取层状态，不存在则创建。
func (s *StatusFile) Layer(name string) *LayerStatus {
	if s.Layers == nil {
		s.Layers = map[string]*LayerStatus{}
	}
	if s.Layers[name] == nil {
		s.Layers[name] = &LayerStatus{}
	}
	return s.Layers[name]
}

// Save 原子写状态文件。
func (s *StatusFile) Save(stateDir string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFile(statusPath(stateDir), data, 0o644)
}

type conflictFile struct {
	Files []string  `json:"files"`
	Since time.Time `json:"since"`
}

// WriteConflictFiles 写 state/sync-conflict.json（GUI 冲突页数据源，syncx 独占写）。
func WriteConflictFiles(stateDir string, files []string) error {
	data, err := json.MarshalIndent(conflictFile{Files: files, Since: time.Now()}, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFile(conflictPath(stateDir), data, 0o644)
}

// ReadConflictFiles 读冲突文件列表；不存在返回空。
func ReadConflictFiles(stateDir string) ([]string, error) {
	data, err := os.ReadFile(conflictPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cf conflictFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, nil
	}
	return cf.Files, nil
}

// ClearConflictFiles 删除冲突状态文件（解决完成后调）。
func ClearConflictFiles(stateDir string) error {
	if err := os.Remove(conflictPath(stateDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// RecordOutcome 是 CLI/daemon 共用的状态回写收敛：按 Outcome 更新
// sync-status.json 的 personal 层与 sync-conflict.json。失败仅尽力而为（fail-open）。
func RecordOutcome(dir, stateDir string, o Outcome) {
	// 非仓目录不算同步结果：状态文件原样不动，不能谎报"已同步"。
	if o.NotRepo {
		return
	}
	sf, err := LoadStatus(stateDir)
	if err != nil {
		return
	}
	l := sf.Layer("personal")
	switch {
	case o.Err != nil:
		l.LastError = o.Err.Error()
	case len(o.Conflicts) > 0:
		l.Conflict = true
		l.LastError = ""
		_ = WriteConflictFiles(stateDir, o.Conflicts)
	default:
		l.LastSync = time.Now()
		l.Conflict = false
		l.LastError = ""
		_ = ClearConflictFiles(stateDir)
	}
	r := Open(dir)
	if r.IsRepo() {
		_, l.Ahead, l.Behind = r.Status()
	}
	_ = sf.Save(stateDir)
}
