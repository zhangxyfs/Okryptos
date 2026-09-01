package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var okserverPath string // TestMain 里构建（integration_test.go 改造）

// freePort 取空闲端口。
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestOkserverDeploy(t *testing.T) {
	dataDir := t.TempDir()
	port := freePort(t)
	cmd := exec.Command(okserverPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("OKSERVER_LISTEN=127.0.0.1:%d", port),
		"OKSERVER_DATA_DIR="+dataDir,
	) // 不配 GITEA_URL → fake 模式
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// 等就绪
	var meta map[string]any
	for i := 0; i < 50; i++ {
		res, err := http.Get(base + "/api/v1/meta")
		if err == nil {
			json.NewDecoder(res.Body).Decode(&meta)
			res.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if meta == nil || meta["initialized"] != true {
		t.Fatalf("meta: %v", meta)
	}

	// root 密码从文件读
	pwData, err := os.ReadFile(filepath.Join(dataDir, "INITIAL_ROOT_PASSWORD"))
	if err != nil {
		t.Fatalf("root password file: %v", err)
	}
	rootPW := strings.TrimSpace(string(pwData))
	if len(rootPW) != 32 {
		t.Fatalf("root pw length: %d", len(rootPW))
	}

	call := func(method, path, token string, body any) (int, map[string]any) {
		t.Helper()
		var rd *strings.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		} else {
			rd = strings.NewReader("")
		}
		req, _ := http.NewRequest(method, base+path, rd)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}

	// root 登录 → 建用户 → 成员建仓 → 权限负例 → 审计
	code, out := call("POST", "/api/v1/login", "", map[string]string{"username": "root", "password": rootPW})
	if code != 200 {
		t.Fatalf("root login: %d %v", code, out)
	}
	rootTok := out["token"].(string)
	// 初始密码带强制改密标记：gate 拦截一切非改密操作，必须先自助改密
	code, _ = call("POST", "/api/v1/users", rootTok, map[string]string{"username": "alice"})
	if code != 403 {
		t.Fatalf("gated create user want 403 must_change_password: %d", code)
	}
	code, out = call("POST", "/api/v1/change-password", rootTok, map[string]string{"old_password": rootPW, "new_password": "root-new-password-1"})
	if code != 200 {
		t.Fatalf("root change password: %d %v", code, out)
	}
	code, out = call("POST", "/api/v1/users", rootTok, map[string]string{"username": "alice"})
	if code != 200 || out["password"] == nil {
		t.Fatalf("create alice: %d %v", code, out)
	}
	alicePW := out["password"].(string)
	code, out = call("POST", "/api/v1/login", "", map[string]string{"username": "alice", "password": alicePW})
	if code != 200 {
		t.Fatalf("alice login: %d", code)
	}
	aliceTok := out["token"].(string)
	// alice 同样持初始密码，先改密解锁
	code, out = call("POST", "/api/v1/change-password", aliceTok, map[string]string{"old_password": alicePW, "new_password": "alice-new-password-1"})
	if code != 200 {
		t.Fatalf("alice change password: %d %v", code, out)
	}
	code, out = call("POST", "/api/v1/repos/personal", aliceTok, map[string]string{"project": "demo"})
	if code != 200 || out["repo"].(map[string]any)["name"] != "ok-demo" {
		t.Fatalf("personal repo: %d %v", code, out)
	}
	code, _ = call("GET", "/api/v1/users", aliceTok, nil)
	if code != 403 {
		t.Fatalf("member must be forbidden: %d", code)
	}
	code, out = call("GET", "/api/v1/audit", rootTok, nil)
	if code != 200 || len(out["entries"].([]any)) == 0 {
		t.Fatalf("audit: %d", code)
	}
}
