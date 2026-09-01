package gui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"openknowledge/internal/registry"
	"openknowledge/internal/version"
)

// changelogEntry 是一个版本号的更新日志（N.N.N.md 全文）。
type changelogEntry struct {
	Version string `json:"version"`
	Log     string `json:"log"`
}

var changelogFileRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)\.md$`)

// changelogDir 定位更新日志目录：安装态 <webDir 父目录>/changelogs 优先，
// 缺失时回退 dev 仓库内运行的 docs/changelogs；都没有返回 ""。
func (h *Handler) changelogDir() string {
	root := filepath.Dir(h.webDir)
	for _, cand := range []string{
		filepath.Join(root, "changelogs"),
		filepath.Join(root, "docs", "changelogs"),
	} {
		if info, err := os.Stat(cand); err == nil && info.IsDir() {
			return cand
		}
	}
	return ""
}

// readChangelogs 读取全部 N.N.N.md，按版本号数值升序；目录缺失/为空返回空切片。
func (h *Handler) readChangelogs() []changelogEntry {
	entries := []changelogEntry{}
	dir := h.changelogDir()
	if dir == "" {
		return entries
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return entries
	}
	for _, f := range files {
		m := changelogFileRe.FindStringSubmatch(f.Name())
		if m == nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		entries = append(entries, changelogEntry{Version: m[1] + "." + m[2] + "." + m[3], Log: string(data)})
	}
	sort.Slice(entries, func(i, j int) bool {
		a, _ := parseVersion(entries[i].Version)
		b, _ := parseVersion(entries[j].Version)
		return versionLess(a, b)
	})
	return entries
}

// parseVersion 把 "N.N.N" 拆成数值三元组；非规范版本（如 dev）返回 ok=false。
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func versionLess(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// UpdateCheck 是一次更新检查结果的缓存。
type UpdateCheck struct {
	CheckedAt    int64  `json:"checked_at"`
	Latest       string `json:"latest"`
	Body         string `json:"body,omitempty"`
	InstallerURL string `json:"installer_url,omitempty"`
	DebURL       string `json:"deb_url,omitempty"`
	TarURL       string `json:"tar_url,omitempty"`
	// Error 是失败态的机器可读标记（fetch_failed/bad_response），随失败结果一起缓存，
	// TTL 内缓存命中也如实透传给手动「检查更新」。
	Error string `json:"error,omitempty"`
}

// guiState 是 ~/.openknowledge/gui.json 的内容（GUI 侧持久化小状态）。
type guiState struct {
	LastSeenVersion string       `json:"last_seen_version,omitempty"`
	SkippedVersion  string       `json:"skipped_version,omitempty"`
	UpdateCheck     *UpdateCheck `json:"update_check,omitempty"`
}

func guiStatePath() string { return filepath.Join(registry.Home(), "gui.json") }

// readGuiState 读取 gui.json；文件缺失返回零值状态且无错误，损坏则报错。
func readGuiState() (guiState, error) {
	var s guiState
	data, err := os.ReadFile(guiStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return guiState{}, err
	}
	return s, nil
}

// writeGuiState 写 gui.json（0600）。
func writeGuiState(s guiState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(guiStatePath(), data, 0o600)
}

// loadLastSeen 读取已看版本；文件缺失/损坏返回 ""。
func loadLastSeen() string {
	s, err := readGuiState()
	if err != nil {
		return ""
	}
	return s.LastSeenVersion
}

// apiChangelog 返回当前版本、未看版本（pending，升序）与全部历史（all）。
// pending 规则：有 last_seen 记录才计算（首次不弹历史）；current 非规范版本（dev）恒空；
// 条目版本须严格大于 last_seen 且不超过 current（防止新旧包混装时展示未发布内容）。
func (h *Handler) apiChangelog(w http.ResponseWriter, _ *http.Request) {
	current := version.Version
	entries := h.readChangelogs()
	pending := []changelogEntry{}
	cur, curOK := parseVersion(current)
	seen, seenOK := parseVersion(loadLastSeen())
	if curOK && seenOK {
		for _, e := range entries {
			v, _ := parseVersion(e.Version) // 正则已约束，必成功
			if versionLess(seen, v) && !versionLess(cur, v) {
				pending = append(pending, e)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current": current,
		"pending": pending,
		"all":     entries,
	})
}

// apiChangelogSeen 标记当前版本为已看；dev 构建不写文件直接 ok。
func (h *Handler) apiChangelogSeen(w http.ResponseWriter, _ *http.Request) {
	if _, ok := parseVersion(version.Version); !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	st, err := readGuiState()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	st.LastSeenVersion = version.Version
	if err := writeGuiState(st); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
