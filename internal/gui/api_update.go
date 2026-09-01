package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"openknowledge/internal/daemonx"
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
	api("POST /api/update/skip", h.apiUpdateSkip)
	api("POST /api/update/download", h.apiUpdateDownloadStart)
	api("GET /api/update/download", h.apiUpdateDownloadStatus)
	api("POST /api/update/apply", h.apiUpdateApply)
}

// apiUpdateSkip：记录用户跳过的版本（gui.json SkippedVersion）。跳过后该版本不再
// 触发启动弹窗与侧栏红点；检查缓存、last_seen 等其他字段原样保留。
func (h *Handler) apiUpdateSkip(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Version == "" {
		writeErr(w, http.StatusBadRequest, "version 不能为空")
		return
	}
	st, err := readGuiState()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	st.SkippedVersion = req.Version
	if err := writeGuiState(st); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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

// ---------- /api/update/apply（Windows 静默安装） ----------

// 升级序列的副作用抽成包级 var：测试不真拉起安装器、不真退出进程，
// 替换为记录调用的假实现后断言顺序（runInstaller → stopDaemon → exitProcess）。
var (
	// stopDaemon 尽力而为地停止当前 daemon 并删除凭证。
	stopDaemon = daemonx.StopDaemon
	// runInstaller detached 拉起 Inno Setup 安装器（Start+Release，不等待退出）：
	// 本进程随后即自退，等安装器退出没有意义；安装收尾（ssInstall 停旧 okd、
	// 覆盖文件、[Run] 段拉起新 okd）全由安装器完成。
	runInstaller = func(path string) error {
		cmd := exec.Command(path, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART")
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	// exitProcess 退出当前进程（旧 okd 将被安装器覆盖）。
	exitProcess = os.Exit
)

// applyInFlight 防并发 apply：升级序列终点是进程退出，成功后不回收；
// 校验/熔断写失败时复位，避免一次失败永久锁死。
var applyInFlight atomic.Bool

// upgradeMarkPath 升级熔断标记：存在期间 daemon.Ensure/EnsureCurrent 不拉起 daemon。
// apply 时写、本进程绝不删——装完由安装器 [Run] 段拉起新 okd，启动时自愈删除
// （daemon.Run → clearUpgradeMark，兼覆盖安装中断/断电的异常残留）。
func upgradeMarkPath() string {
	return filepath.Join(registry.Home(), "update", ".upgrading")
}

// apiUpdateApply：Windows 静默安装已下载完成的安装器。okd 进程内只做三步：
// 写熔断（200 之前，写失败 500 中止——不无熔断裸奔安装）→ 回 200 + flush →
// goroutine detached 拉起安装器后自退。等安装器退出/删熔断/拉起新 okd 都不在
// 本进程做：GUI 跑在 okd 自身进程里，安装器停 okd 会毫秒级杀掉自己，那些步骤
// 在真实部署下永远跑不到。
func (h *Handler) apiUpdateApply(w http.ResponseWriter, _ *http.Request) {
	if runtime.GOOS != "windows" {
		writeErr(w, http.StatusBadRequest, "unsupported")
		return
	}
	snap := updSnapshot()
	if snap.State != "done" {
		writeErr(w, http.StatusConflict, "安装器尚未下载完成")
		return
	}
	if _, err := os.Stat(snap.Path); err != nil {
		writeErr(w, http.StatusBadRequest, "安装器文件不存在："+snap.Path)
		return
	}
	if !applyInFlight.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "升级已在进行中")
		return
	}
	// 熔断必须先于 200 响应写好：安装期间 hook/托盘的任何 Ensure 拉起都被它挡住。
	mark := upgradeMarkPath()
	if err := os.MkdirAll(filepath.Dir(mark), 0o755); err != nil {
		applyInFlight.Store(false)
		writeErr(w, http.StatusInternalServerError, "写升级熔断标记失败: "+err.Error())
		return
	}
	if err := os.WriteFile(mark, []byte("1"), 0o644); err != nil {
		applyInFlight.Store(false)
		writeErr(w, http.StatusInternalServerError, "写升级熔断标记失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go applyUpdate(snap.Path)
}

// applyUpdate 升级序列：detached 拉起安装器 → 停 daemon → 退出进程。
// 全程错误写日志（daemon.log 即本进程 stderr），不静默丢弃。
func applyUpdate(path string) {
	if err := runInstaller(path); err != nil {
		log.Printf("升级安装器拉起失败 %s: %v", path, err)
		// 安装器没起来而熔断已写：不回滚会永久挡住 daemon 拉起（okd 退出后
		// 再无进程能清标记）。回滚熔断 + 复位 guard，进程继续服务，用户可重试。
		if rerr := os.Remove(upgradeMarkPath()); rerr != nil {
			log.Printf("升级熔断标记回滚失败: %v", rerr)
		}
		applyInFlight.Store(false)
		return
	}
	stopDaemon()
	exitProcess(0)
}
