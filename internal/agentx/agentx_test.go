package agentx

import (
	"path/filepath"
	"strings"
	"testing"
)

// currentCLIExe 在非 okd 进程（测试二进制、ok 自身）必须原样透传；
// okd 换算路径由 daemonx.TestCliTargetFor 覆盖。
func TestCurrentCLIExePassthrough(t *testing.T) {
	exe, err := currentCLIExe()
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(exe)
	if strings.TrimSuffix(base, ".exe") == "okd" {
		t.Fatalf("test binary must not resolve to okd: %s", exe)
	}
}
