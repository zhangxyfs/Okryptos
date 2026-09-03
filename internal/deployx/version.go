package deployx

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// dockerHubTagsURL 是 okserver 镜像的 Docker Hub tag 列表 API（默认拉取源；
// GHCR 匿名列 tag 要走 token 交换，不做）。离线/被墙时查询失败静默降级为空串。
const dockerHubTagsURL = "https://hub.docker.com/v2/repositories/z7dream/okryptos-okserver/tags?page_size=100"

// metaHTTPClient 管理页版本探测专用：短超时，不跟随重定向到外网。
var metaHTTPClient = &http.Client{Timeout: 3 * time.Second}

// queryRunningVersion 从 okdeploy 本机直连 okserver 的 /api/v1/meta 取运行版本
// （okdeploy 与 NAS 同 LAN，无需绕 SSH）。任何失败都返回空串——状态页降级显示镜像 tag。
func queryRunningVersion(ctx context.Context, metaURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return ""
	}
	res, err := metaHTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return ""
	}
	return out.Version
}

// semverRe 只认 vX.Y.Z 正式版本 tag（latest/分支名等一律排除）。
var semverRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// compareSemver 比较两个 vX.Y.Z：a<b 返回 -1，相等 0，a>b 1。
// 非标准格式的串恒视为最小（排在所有正式版本之前）。
func compareSemver(a, b string) int {
	ma := semverRe.FindStringSubmatch(a)
	mb := semverRe.FindStringSubmatch(b)
	switch {
	case ma == nil && mb == nil:
		return 0
	case ma == nil:
		return -1
	case mb == nil:
		return 1
	}
	for i := 1; i <= 3; i++ {
		na, _ := strconv.Atoi(ma[i])
		nb, _ := strconv.Atoi(mb[i])
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
	}
	return 0
}

// latestImageTag 查 Docker Hub 上 okserver 镜像的全部 tag，返回最高 vX.Y.Z。
// 失败（网络/限流/无正式 tag）返回空串，前端据此隐藏"有新版本"提示。
func latestImageTag(ctx context.Context, tagsURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return ""
	}
	res, err := metaHTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return ""
	}
	best := ""
	for _, r := range out.Results {
		if compareSemver(r.Name, best) > 0 {
			best = r.Name
		}
	}
	return best
}
