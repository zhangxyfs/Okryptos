package deployx

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testServer() *Server { return NewServer("testtok") }

func TestAPIRequiresToken(t *testing.T) {
	s := testServer()
	h := s.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>")}})
	req := httptest.NewRequest("GET", "/api/probe", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401，得 %d", rec.Code)
	}
}

func TestProbeRequiresConnection(t *testing.T) {
	s := testServer()
	h := s.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>")}})
	req := httptest.NewRequest("GET", "/api/probe", nil)
	req.Header.Set("X-Ok-Token", "testtok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("未连接应 412，得 %d", rec.Code)
	}
}

func TestDeployExternalRequiresChecklistAck(t *testing.T) {
	s := testServer()
	s.mu.Lock()
	s.ex = &fakeExec{t: t} // 假装已连接
	s.mu.Unlock()
	h := s.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>")}})
	body := `{"mode":"external","dir":"/d","ok_port":3100,"tag":"v1","gitea_url":"http://g:3000","admin_token":"tok","checklist_ack":false}`
	req := httptest.NewRequest("POST", "/api/deploy", strings.NewReader(body))
	req.Header.Set("X-Ok-Token", "testtok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未确认清单应 400，得 %d", rec.Code)
	}
	var resp map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["need_checklist"]; !ok {
		t.Fatalf("响应应含 need_checklist：%s", rec.Body.String())
	}
}

func TestUninstallRequiresConfirmWord(t *testing.T) {
	s := testServer()
	s.mu.Lock()
	s.ex = &fakeExec{t: t}
	s.mu.Unlock()
	h := s.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>")}})
	req := httptest.NewRequest("POST", "/api/uninstall", strings.NewReader(`{"dir":"/d","confirm":"yes"}`))
	req.Header.Set("X-Ok-Token", "testtok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("确认词错误应 400，得 %d", rec.Code)
	}
}

func TestSingleFlightConflict(t *testing.T) {
	s := testServer()
	s.mu.Lock()
	s.ex = &fakeExec{t: t}
	s.running = true
	s.mu.Unlock()
	h := s.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>")}})
	req := httptest.NewRequest("POST", "/api/upgrade", strings.NewReader(`{"dir":"/d","tag":"v2"}`))
	req.Header.Set("X-Ok-Token", "testtok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("任务冲突应 409，得 %d", rec.Code)
	}
}

func TestDeployResult404BeforeDeploy(t *testing.T) {
	s := testServer()
	h := s.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>")}})
	req := httptest.NewRequest("GET", "/api/deploy/result", nil)
	req.Header.Set("X-Ok-Token", "testtok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("应 404，得 %d", rec.Code)
	}
}

func TestWriteSSEFormat(t *testing.T) {
	var buf bytes.Buffer
	writeSSE(&buf, LogEvent{Step: "s", Level: "info", Text: "hello"})
	got := buf.String()
	if !strings.HasPrefix(got, "data: {") || !strings.HasSuffix(got, "\n\n") || !strings.Contains(got, "hello") {
		t.Fatalf("SSE 帧格式错：%q", got)
	}
}

func TestLsRejectsBadPath(t *testing.T) {
	s := testServer()
	s.mu.Lock()
	s.ex = &fakeExec{t: t}
	s.mu.Unlock()
	h := s.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>")}})
	req := httptest.NewRequest("POST", "/api/ls", strings.NewReader(`{"path":"/a;id"}`))
	req.Header.Set("X-Ok-Token", "testtok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法路径应 400，得 %d", rec.Code)
	}
}

func TestResetRootRequiresConfirmWord(t *testing.T) {
	s := testServer()
	s.mu.Lock()
	s.ex = &fakeExec{t: t}
	s.mu.Unlock()
	h := s.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>")}})
	req := httptest.NewRequest("POST", "/api/reset-root", strings.NewReader(`{"dir":"/d","confirm":"yes"}`))
	req.Header.Set("X-Ok-Token", "testtok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("确认词错误应 400，得 %d", rec.Code)
	}
}

// 恢复缺/错确认词 → 400（照 TestResetRootRequiresConfirmWord 模式，multipart 表单）。
func TestRestoreRequiresConfirmWord(t *testing.T) {
	s := testServer()
	s.mu.Lock()
	s.ex = &fakeExec{t: t}
	s.mu.Unlock()
	h := s.Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>")}})
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("dir", "/d")
	_ = mw.WriteField("confirm", "yes")
	fw, err := mw.CreateFormFile("file", "b.tar")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("x"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/restore", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Ok-Token", "testtok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("确认词错误应 400，得 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "RESTORE") {
		t.Fatalf("错误文案应提示 RESTORE，得 %s", rec.Body.String())
	}
}
