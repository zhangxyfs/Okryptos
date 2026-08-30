package deployx

import (
	"context"
	"strings"
)

// ProbeResult 是远端环境探测结果（设计 §3.2）。*Busy 空串 = 端口空闲。
type ProbeResult struct {
	Arch          string `json:"arch"`
	DockerOK      bool   `json:"docker_ok"`
	DockerVersion string `json:"docker_version"`
	ComposeOK     bool   `json:"compose_ok"`
	PortGiteaBusy string `json:"port_gitea_busy"`
	PortOKBusy    string `json:"port_ok_busy"`
	GiteaFound    bool   `json:"gitea_found"`
	GiteaDetail   string `json:"gitea_detail"`
	Existing      bool   `json:"existing"`
	DeployDir     string `json:"deploy_dir"`
}

// runQuiet 探测专用：直接执行，不进 LogHub，不判非零退出。
func runQuiet(ctx context.Context, ex Executor, cmd string) (string, int) {
	var out []string
	code, err := ex.Run(ctx, cmd, nil, func(stream, line string) {
		if stream == "stdout" {
			out = append(out, line)
		}
	})
	if err != nil {
		return "", -1
	}
	return strings.Join(out, "\n"), code
}

// Probe 探测远端部署环境。所有命令容错：单条失败不致命。
func Probe(ctx context.Context, ex Executor) (*ProbeResult, error) {
	r := &ProbeResult{}

	out, _ := runQuiet(ctx, ex, "uname -m")
	r.Arch = strings.TrimSpace(out)

	out, code := runQuiet(ctx, ex, "docker version --format '{{.Server.Version}}'")
	r.DockerOK = code == 0
	r.DockerVersion = strings.TrimSpace(out)

	_, code = runQuiet(ctx, ex, "docker compose version --short")
	r.ComposeOK = code == 0

	// 端口占用：ss 为主，macOS/极简系统回退 netstat。
	listen, _ := runQuiet(ctx, ex, "ss -ltn 2>/dev/null || netstat -ltn 2>/dev/null")

	// 容器清单：名字|镜像|状态|端口映射。
	containers, _ := runQuiet(ctx, ex, "docker ps -a --format '{{.Names}}|{{.Image}}|{{.Status}}|{{.Ports}}'")

	owner := func(port string) string {
		if !strings.Contains(listen, ":"+port+" ") && !strings.Contains(listen, ":"+port+"\n") && !strings.HasSuffix(listen, ":"+port) {
			return ""
		}
		// 尝试归因到容器
		for _, ln := range strings.Split(containers, "\n") {
			parts := strings.Split(ln, "|")
			if len(parts) == 4 && strings.Contains(parts[3], ":"+port+"->") {
				return "容器 " + parts[0]
			}
		}
		return "未知进程"
	}
	r.PortGiteaBusy = owner("3000")
	r.PortOKBusy = owner("3100")

	for _, ln := range strings.Split(containers, "\n") {
		parts := strings.Split(ln, "|")
		if len(parts) != 4 {
			continue
		}
		name, image := parts[0], parts[1]
		if strings.Contains(strings.ToLower(name+image), "gitea") {
			r.GiteaFound = true
			r.GiteaDetail = name + "（" + image + "，" + parts[2] + "）"
		}
		if name == "okserver" {
			r.Existing = true
		}
	}
	if r.Existing {
		dir, code := runQuiet(ctx, ex, `docker inspect okserver --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}'`)
		if code == 0 {
			r.DeployDir = strings.TrimSpace(dir)
		}
	}
	return r, nil
}
