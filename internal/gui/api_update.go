package gui

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

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
