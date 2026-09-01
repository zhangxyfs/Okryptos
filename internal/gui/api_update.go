package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"openknowledge/internal/registry"
	"openknowledge/internal/version"
)

// githubAPI 是包级变量以便测试替换为 httptest server。
var githubAPI = "https://api.github.com/repos/zhangxyfs/OpenKnowledge/releases/latest"

var updateHTTPClient = &http.Client{Timeout: 15 * time.Second}

// updateCheckTTL 是 /api/update/check 结果在 gui.json 里的缓存时长。
const updateCheckTTL = 6 * time.Hour

// updateCheckResp 是 GET /api/update/check 的响应。
type updateCheckResp struct {
	UpdateAvailable bool   `json:"update_available"`
	Latest          string `json:"latest,omitempty"`
	Current         string `json:"current"`
	Body            string `json:"body,omitempty"`
	InstallerURL    string `json:"installer_url,omitempty"`
	DebURL          string `json:"deb_url,omitempty"`
	TarURL          string `json:"tar_url,omitempty"`
	SkippedVersion  string `json:"skipped_version,omitempty"`
}

func (h *Handler) registerUpdateAPI(api func(string, http.HandlerFunc)) {
	api("GET /api/update/check", h.apiUpdateCheck)
	api("POST /api/update/download", h.apiUpdateDownloadStart)
	api("GET /api/update/download", h.apiUpdateDownloadStatus)
}

// githubRelease 是 GitHub releases/latest 响应中我们关心的字段。
type githubRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// apiUpdateCheck 检查 GitHub 最新发布版本。结果缓存 6h（gui.json UpdateCheck）；
// GitHub 请求失败/超时/解析失败一律 fail-open 返回 200 + update_available:false，
// 仍写缓存 CheckedAt（Latest 留空）防雪崩。
func (h *Handler) apiUpdateCheck(w http.ResponseWriter, _ *http.Request) {
	st, err := readGuiState()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	uc := st.UpdateCheck
	if uc == nil || time.Since(time.Unix(uc.CheckedAt, 0)) >= updateCheckTTL {
		uc = fetchLatestRelease()
		st.UpdateCheck = uc
		if err := writeGuiState(st); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	resp := updateCheckResp{
		Current:        version.Version,
		Latest:         uc.Latest,
		Body:           uc.Body,
		InstallerURL:   uc.InstallerURL,
		DebURL:         uc.DebURL,
		TarURL:         uc.TarURL,
		SkippedVersion: st.SkippedVersion,
	}
	cur, curOK := parseVersion(version.Version)
	latest, latestOK := parseVersion(uc.Latest)
	resp.UpdateAvailable = curOK && latestOK && versionLess(cur, latest)
	writeJSON(w, http.StatusOK, resp)
}

// fetchLatestRelease 请求 GitHub 最新 release 并转成缓存结构；任何失败返回
// 仅带 CheckedAt 的空结果（fail-open，Latest 留空）。
func fetchLatestRelease() *UpdateCheck {
	uc := &UpdateCheck{CheckedAt: time.Now().Unix()}
	req, err := http.NewRequest(http.MethodGet, githubAPI, nil)
	if err != nil {
		return uc
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return uc
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return uc
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return uc
	}
	uc.Latest = strings.TrimPrefix(rel.TagName, "v")
	uc.Body = rel.Body
	// 宽松后缀匹配资产 URL（防命名微调）：Setup*.exe / *.deb / *linux_amd64.tar.gz
	for _, a := range rel.Assets {
		switch {
		case strings.HasSuffix(a.Name, ".exe") && strings.Contains(a.Name, "Setup"):
			uc.InstallerURL = a.URL
		case strings.HasSuffix(a.Name, ".deb"):
			uc.DebURL = a.URL
		case strings.HasSuffix(a.Name, "linux_amd64.tar.gz"):
			uc.TarURL = a.URL
		}
	}
	return uc
}

// updateURLPrefix 是安装器下载 URL 的合法前缀（SSRF 纪律：check 端点给的
// GitHub release 资产地址之外的一律 400）。包级 var 以便测试替换为 httptest server。
var updateURLPrefix = "https://github.com/zhangxyfs/OpenKnowledge/releases/download/"

// updateDownloadClient 不设整体 Timeout（安装器动辄数百 MB，慢速网络下整体超时会
// 误杀正常下载），只给响应头 30s 兜底——同 embed/download.go defaultClient 的思路。
// 包级 var 以便测试替换。
var updateDownloadClient = func() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: t}
}

// 安装器下载是单任务：updCur 为 nil 表示 idle。
var (
	updMu  sync.Mutex
	updCur *updJob
)

// updJob 是安装器下载任务的状态（GUI 轮询展示）。照 embedding.go 的 dlJob 模式，
// cancel 保留以便后续暴露取消端点。
type updJob struct {
	State  string `json:"state"` // idle|running|done|error
	Done   int64  `json:"done"`
	Total  int64  `json:"total"`
	Path   string `json:"path"` // 最终文件路径（.part 后缀在完成后去掉）
	Err    string `json:"err"`
	cancel context.CancelFunc
	mu     sync.Mutex
}

// updSnapshot 返回当前下载任务快照（无任务返回 idle）。
// 逐字段拷贝避免复制 sync.Mutex（go vet copylocks）。
func updSnapshot() updJob {
	updMu.Lock()
	j := updCur
	updMu.Unlock()
	if j == nil {
		return updJob{State: "idle"}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return updJob{State: j.State, Done: j.Done, Total: j.Total, Path: j.Path, Err: j.Err}
}

// apiUpdateDownloadStart：后台下载安装器到 ~/.openknowledge/update/（单任务；
// 已有 running 任务的重复 POST 直接返回当前快照，幂等）。
func (h *Handler) apiUpdateDownloadStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL     string `json:"url"`
		Version string `json:"version"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !strings.HasPrefix(req.URL, updateURLPrefix) {
		writeErr(w, http.StatusBadRequest, "非法下载地址：仅允许 GitHub release 资产")
		return
	}
	if req.Version == "" || strings.ContainsAny(req.Version, `/\`) {
		writeErr(w, http.StatusBadRequest, "非法版本号")
		return
	}
	dest := filepath.Join(registry.Home(), "update", "OpenKnowledge-Setup-"+req.Version+".exe")
	updMu.Lock()
	if updCur != nil {
		updCur.mu.Lock()
		st := updCur.State
		updCur.mu.Unlock()
		if st == "running" {
			updMu.Unlock()
			writeJSON(w, http.StatusOK, updSnapshot())
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &updJob{State: "running", Path: dest, cancel: cancel}
	updCur = job
	updMu.Unlock()
	go func() {
		err := downloadFile(ctx, updateDownloadClient(), req.URL, dest, func(done, total int64) {
			job.mu.Lock()
			job.Done = done
			job.Total = total
			job.mu.Unlock()
		})
		job.mu.Lock()
		defer job.mu.Unlock()
		switch {
		case err == nil:
			job.State = "done"
			job.Done = job.Total
		case ctx.Err() != nil:
			job.State = "idle" // 取消：复位（.part 保留可续传）
			job.Err = ""
		default:
			job.State = "error"
			job.Err = err.Error()
		}
	}()
	writeJSON(w, http.StatusOK, updSnapshot())
}

// apiUpdateDownloadStatus：返回当前下载任务快照。
func (h *Handler) apiUpdateDownloadStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, updSnapshot())
}

// downloadFile 把 url 下载到 dest：先写 dest+".part"（残留可断点续传），完成后原子改名。
// 复用 embed.Download 的思路，但面向任意安装器文件（无 sha256 校验，大小以服务端
// Content-Length 为准）。ctx 取消保留 .part 供下次续传。
func downloadFile(ctx context.Context, hc *http.Client, url, dest string, progress func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	part := dest + ".part"
	var offset int64
	if st, err := os.Stat(part); err == nil {
		offset = st.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		// .part 比服务端还长（源变了）：删掉重头来
		_ = os.Remove(part)
		return fmt.Errorf("续传偏移越界（416），已清除 %s，请重试", filepath.Base(part))
	case offset > 0 && resp.StatusCode == http.StatusOK:
		// 服务端不认 Range：截断重下
		offset = 0
	case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent:
		return fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
	}
	total := int64(-1) // 服务端未给 Content-Length 时未知
	if resp.ContentLength >= 0 {
		total = resp.ContentLength + offset
	}
	flag := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flag, 0o644)
	if err != nil {
		return err
	}
	written := offset
	buf := make([]byte, 256*1024)
	copyErr := func() error {
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := f.Write(buf[:n]); werr != nil {
					return werr
				}
				written += int64(n)
				if progress != nil {
					progress(written, total)
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					return nil
				}
				return rerr
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}()
	_ = f.Close()
	if copyErr != nil {
		return copyErr // .part 保留
	}
	if total >= 0 && written != total {
		return fmt.Errorf("下载大小不符：%d，期望 %d（.part 已保留可续传）", written, total)
	}
	return os.Rename(part, dest)
}
