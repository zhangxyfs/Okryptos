package deployx

import (
	"context"
	"strings"
)

// ProbeResult 是远端环境探测结果（设计 §3.2）。*Busy 空串 = 端口空闲。
type ProbeResult struct {
	Arch          string `json:"arch"`
	DockerCLI     bool   `json:"docker_cli"`    // docker 命令存在（command -v）
	DockerOK      bool   `json:"docker_ok"`     // 当前用户能连 daemon（可直接部署）
	DockerVersion string `json:"docker_version"`
	DockerDetail  string `json:"docker_detail"` // 不可用时的真实 stderr 摘要（权限/未运行/未安装）
	ComposeOK     bool   `json:"compose_ok"`
	NeedSudo      bool   `json:"need_sudo"`   // docker 可用但须走 sudo（探测时已验证通过）
	SudoFailed    bool   `json:"sudo_failed"` // 用登录密码试 sudo 失败（需用户另给 sudo 密码）
	PortGiteaBusy string `json:"port_gitea_busy"`
	PortOKBusy    string `json:"port_ok_busy"`
	GiteaFound    bool   `json:"gitea_found"`
	GiteaDetail   string `json:"gitea_detail"`
	Existing      bool   `json:"existing"`
	DeployDir     string `json:"deploy_dir"`
}

// runQuiet 探测专用：直接执行，不进 LogHub，不判非零退出。
func runQuiet(ctx context.Context, ex Executor, cmd string) (string, int) {
	out, _, code := runQuiet2(ctx, ex, cmd)
	return out, code
}

// runQuiet2 同 runQuiet，另带回 stderr 尾行（诊断展示用）。
func runQuiet2(ctx context.Context, ex Executor, cmd string) (stdout, stderrTail string, code int) {
	var out, errLines []string
	c, err := ex.Run(ctx, cmd, nil, func(stream, line string) {
		if stream == "stdout" {
			out = append(out, line)
		} else {
			errLines = append(errLines, line)
			if len(errLines) > 3 {
				errLines = errLines[1:]
			}
		}
	})
	if err != nil {
		return "", "", -1
	}
	return strings.Join(out, "\n"), strings.Join(errLines, "\n"), c
}

// Probe 探测远端部署环境。所有命令容错：单条失败不致命。
// sudoPw 非空且 daemon 直连被拒时，自动尝试 sudo 路径（NAS 常见）；一旦 sudo
// 验证通过，余下探测命令全部改走 sudo 包装（容器清单/inspect 才有权限）。
func Probe(ctx context.Context, ex Executor, sudoPw string) (*ProbeResult, error) {
	r := &ProbeResult{}

	out, _ := runQuiet(ctx, ex, "uname -m")
	r.Arch = strings.TrimSpace(out)

	// Docker 分两档：CLI 存在 ≠ 当前用户能连 daemon（NAS 常见：SSH 用户不在
	// docker 组，daemon socket permission denied）。真实 stderr 摘要带回前端。
	_, cliCode := runQuiet(ctx, ex, "command -v docker")
	r.DockerCLI = cliCode == 0
	if r.DockerCLI {
		ver, tail, code := runQuiet2(ctx, ex, "docker version --format '{{.Server.Version}}'")
		r.DockerVersion = strings.TrimSpace(ver)
		r.DockerOK = code == 0
		r.DockerDetail = strings.TrimSpace(tail)
		if !r.DockerOK && sudoPw != "" {
			// 直连被拒 → 用登录密码试 sudo；通过则余下探测改走 sudo 包装
			if sx, err := WrapSudo(ex, sudoPw); err == nil {
				ver2, _, code2 := runQuiet2(ctx, sx, "docker version --format '{{.Server.Version}}'")
				if code2 == 0 {
					r.NeedSudo = true
					r.DockerOK = true
					r.DockerVersion = strings.TrimSpace(ver2)
					r.DockerDetail = ""
					ex = sx
				} else {
					r.SudoFailed = true
				}
			}
		}
	} else {
		r.DockerDetail = "docker 命令不存在（PATH 中找不到）"
	}

	// chown uid 1000（容器数据目录属主）需要 root：docker 直连可用但非 root 时
	// 同样需要 sudo（NAS 常见：docker 组用户但无 root）。有登录密码则试 sudo 提权。
	if r.DockerOK && !r.NeedSudo && sudoPw != "" {
		uid, _ := runQuiet(ctx, ex, "id -u")
		if strings.TrimSpace(uid) != "0" {
			if sx, err := WrapSudo(ex, sudoPw); err == nil {
				su, _, scode := runQuiet2(ctx, sx, "id -u")
				if scode == 0 && strings.TrimSpace(su) == "0" {
					r.NeedSudo = true
					ex = sx
				}
			}
		}
	}

	_, code := runQuiet(ctx, ex, "docker compose version --short")
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

	existingName := ""
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
		if okserverContainer(name, image) {
			r.Existing = true
			existingName = name
		}
	}
	if r.Existing {
		dir, code := runQuiet(ctx, ex, `docker inspect '`+existingName+`' --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}'`)
		if code == 0 {
			r.DeployDir = strings.TrimSpace(dir)
		}
	}
	return r, nil
}

// okserverContainer 识别 okserver 容器。compose 未指定 name:/-p 时项目名取部署目录
// basename，真机容器名是 <目录名>-okserver-1 而非裸 okserver（2026-08-30 实测
// Existing 检测因此落空）；镜像名含 okserver（z7dream/okryptos-okserver、旧名
// z7dream/openknowledge-okserver、ghcr …/okryptos/okserver）作兜底，新旧部署都认。
func okserverContainer(name, image string) bool {
	if name == "okserver" || strings.HasPrefix(name, "okserver-") || strings.Contains(name, "-okserver-") {
		return true
	}
	return strings.Contains(strings.ToLower(image), "okserver")
}
