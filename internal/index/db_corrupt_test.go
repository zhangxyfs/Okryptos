package index

import (
	"os"
	"path/filepath"
	"testing"
)

// 损坏的 vectors.json（旧版 O_TRUNC 直写残留的半截 JSON）不得让 Open 永久
// 失败：改名为 vectors.json.bad 隔离、按无向量继续——向量可由条目重建。
func TestCorruptVectorsJSONIsolated(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "kb.db")
	if err := os.WriteFile(filepath.Join(root, "vectors.json"),
		[]byte(`{"vectors":{"a.md":{"vector":[0.1,`), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("损坏的 vectors.json 不得让 Open 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := os.Stat(filepath.Join(root, "vectors.json.bad")); err != nil {
		t.Fatalf("损坏文件应改名为 vectors.json.bad: %v", err)
	}
	if hv, err := db.HasVectors(); err != nil || hv {
		t.Fatalf("不应导入任何向量: has=%v err=%v", hv, err)
	}
	// 再次 Open：.bad 已隔离，不再重复告警/失败
	db2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("再次 Open 应正常: %v", err)
	}
	_ = db2.Close()
}
