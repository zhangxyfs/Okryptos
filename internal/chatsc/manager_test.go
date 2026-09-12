package chatsc

import (
	"os"
	"path/filepath"
	"testing"

	"okryptos/internal/registry"
)

// TestDefaultModelsDirFrom 默认模型目录决策：
// exe 目录可写 → 跟随 exe（绿色/安装版惯例）；不可写（.deb 装 /usr/lib/openknowledge/
// 为 root 所有）→ 回退 OK_HOME/models；目录已装有模型（.gguf）时即使只读也不换目录
// （防遮蔽已装模型导致重复下载）。
func TestDefaultModelsDirFrom(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OK_HOME", home)

	// exe 目录可写 → <exe>/models
	exeDir := t.TempDir()
	if got := defaultModelsDirFrom(exeDir); got != filepath.Join(exeDir, "models") {
		t.Fatalf("可写 exe 目录: %q", got)
	}

	// exe 路径落在普通文件下（不可创建子目录，等价不可写）→ OK_HOME/models
	f := filepath.Join(t.TempDir(), "ok")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(registry.Home(), "models")
	if got := defaultModelsDirFrom(f); got != want {
		t.Fatalf("不可写应回退 %q, got %q", want, got)
	}

	// 已装模型的目录（模拟只读场景下的已装模型）→ 不切换
	roDir := t.TempDir()
	modelsDir := filepath.Join(roDir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, "qwen3-1.7b-q8.gguf"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := defaultModelsDirFrom(roDir); got != modelsDir {
		t.Fatalf("已装模型目录不应切换: %q", got)
	}
}
