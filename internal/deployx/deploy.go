package deployx

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

// 秘密保护的两种分工：
//   - runCmdMask（task.go）：命令行含秘密、stdout 无秘密——日志显示掩码命令，stdout 照常进日志。
//   - runStepMasked（本文件）：stdout 本身是秘密（token/root 密码）——日志显示掩码命令，
//     stdout 绝不进日志，只经 post 回调给调用方。
// 两个都保留，按"秘密在命令行还是 stdout"选用。

// PullTimeout 是镜像拉取与健康检查等待的超时（NAS 网络可能很慢）。
const PullTimeout = 30 * time.Minute

// composeCmd 组装 docker compose 调用（固定 env-file 与文件名）。
// dir 必须已过 ValidateDir 白名单校验——原样不加引号，~/$HOME 才能被远端 sh 展开。
func composeCmd(dir, args string) string {
	return "cd " + dir + " && docker compose --env-file .env -f compose.yaml " + args
}

// uploadFile 经 stdin 上传文件（部署产物只此通道，秘密文件 0600）。
// path 由已校验的 Dir 派生，原样使用。
func uploadFile(ctx context.Context, e *Env, step, path string, content []byte, mode string) error {
	cmd := "cat > " + path + " && chmod " + mode + " " + path
	ctxT, cancel := context.WithTimeout(ctx, CmdTimeout)
	defer cancel()
	e.Hub.Publish(step, "info", "$ 上传 "+path)
	code, err := e.Ex.Run(ctxT, cmd, bytes.NewReader(content), func(stream, line string) {
		e.Hub.Publish(step, "info", line)
	})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("上传 %s 退出码 %d", path, code)
	}
	return nil
}

// waitHTTP 生成远端就绪等待命令（curl 优先，wget 兜底）。
func waitHTTP(url string) string {
	return fmt.Sprintf(`ok=0; for i in $(seq 1 60); do `+
		`if command -v curl >/dev/null && curl -sf %s >/dev/null; then ok=1; break; fi; `+
		`if command -v wget >/dev/null && wget -qO- %s >/dev/null 2>&1; then ok=1; break; fi; `+
		`sleep 2; done; [ "$ok" = 1 ]`, shellQuote(url), shellQuote(url))
}

// BuildDeployTask 构建部署任务（full 全新双容器 / external 接入已有 Gitea）。
func BuildDeployTask(s DeploySpec) (Task, error) {
	if err := ValidateDir(s.Dir); err != nil {
		return Task{}, err
	}
	compose, err := RenderCompose(s.Mode)
	if err != nil {
		return Task{}, err
	}
	if s.Mode == "external" {
		return buildExternalTask(s, compose)
	}
	if s.Mode != "full" {
		return Task{}, fmt.Errorf("未知部署模式 %q", s.Mode)
	}
	return buildFullTask(s, compose), nil
}

// buildFullTask 全新双容器部署（对齐 server/nas/README.md §2 五步的无人值守版）。
func buildFullTask(s DeploySpec, compose string) Task {
	giteaHealth := fmt.Sprintf("http://127.0.0.1:%d/api/v1/version", s.GiteaPort)
	okHealth := fmt.Sprintf("http://127.0.0.1:%d/api/v1/meta", s.OKPort)
	return Task{Name: "部署 OpenKnowledge 服务端", Steps: []Step{
		{Name: "创建部署目录", Run: func(ctx context.Context, e *Env) error {
			if _, err := runCmd(ctx, e, "创建部署目录",
				"mkdir -p "+s.Dir+"/okserver-data "+s.Dir+"/gitea-data"); err != nil {
				return err
			}
			// chown 失败不致命（非 root 或已授权目录）：降级为警告。
			// gitea-data 同样给 uid 1000（gitea 容器内 git 用户）——sudo 模式下
			// 目录由 root 创建，不 chown 两个容器都写不进去。
			if _, err := runCmd(ctx, e, "创建部署目录",
				"chown -R 1000:1000 "+s.Dir+"/okserver-data "+s.Dir+"/gitea-data"); err != nil {
				e.Hub.Publish("创建部署目录", "info", "警告：chown 失败（容器可能无法写数据目录）："+err.Error())
			}
			return nil
		}},
		{Name: "上传 compose 配置", Run: func(ctx context.Context, e *Env) error {
			return uploadFile(ctx, e, "上传 compose 配置", s.Dir+"/compose.yaml", []byte(compose), "0644")
		}},
		// compose 调用固定带 --env-file .env，文件必须先行存在；full 模式的
		// Gitea token 要等 Gitea 起来后生成，这里先写占位版，token 生成后重写
		//（okserver 在最后一个 up -d 才启动，读到的是重写后的真 token）。
		{Name: "写入 .env 初版", Run: func(ctx context.Context, e *Env) error {
			s0 := s
			s0.AdminToken = "pending"
			return uploadFile(ctx, e, "写入 .env 初版", s.Dir+"/.env", []byte(RenderEnv(s0)), "0600")
		}},
		{Name: "拉取镜像", Run: func(ctx context.Context, e *Env) error {
			ctxT, cancel := context.WithTimeout(ctx, PullTimeout)
			defer cancel()
			_, err := runCmd(ctxT, e, "拉取镜像", composeCmd(s.Dir, "pull"))
			return err
		}},
		{Name: "启动 Gitea", Run: func(ctx context.Context, e *Env) error {
			if _, err := runCmd(ctx, e, "启动 Gitea", composeCmd(s.Dir, "up -d gitea")); err != nil {
				return err
			}
			ctxT, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			_, err := runCmd(ctxT, e, "启动 Gitea", waitHTTP(giteaHealth))
			return err
		}},
		{Name: "创建 Gitea 管理员", Run: func(ctx context.Context, e *Env) error {
			adminPw := NewGiteaAdminToken()[:16]
			e.Vars["gitea_admin_password"] = adminPw
			return runStepMasked(ctx, e, "创建 Gitea 管理员",
				composeCmd(s.Dir, "exec -T gitea gitea admin user list | grep -qw okadmin || "+
					composeCmd(s.Dir, "exec -T gitea gitea admin user create --admin --username okadmin --password '****' --email okadmin@local --must-change-password=false")),
				composeCmd(s.Dir, "exec -T gitea gitea admin user list | grep -qw okadmin || "+
					composeCmd(s.Dir, "exec -T gitea gitea admin user create --admin --username okadmin --password "+shellQuote(adminPw)+" --email okadmin@local --must-change-password=false")))
		}},
		{Name: "生成 Gitea token", Run: func(ctx context.Context, e *Env) error {
			if err := runStepMasked(ctx, e, "生成 Gitea token",
				composeCmd(s.Dir, "exec -T gitea gitea admin user generate-access-token --username okadmin --token-name okserver --scopes all --raw"),
				composeCmd(s.Dir, "exec -T gitea gitea admin user generate-access-token --username okadmin --token-name okserver --scopes all --raw"),
				withSecret(func(out string) {
					// token 在 stdout 最后一行
					lines := strings.Split(strings.TrimSpace(out), "\n")
					e.Vars["admin_token"] = strings.TrimSpace(lines[len(lines)-1])
				})); err != nil {
				return err
			}
			// 空 token 不得进入 .env，否则假成功部署后 okserver 必然 401
			if e.Vars["admin_token"] == "" {
				return fmt.Errorf("Gitea token 生成为空（generate-access-token 无输出）")
			}
			return nil
		}},
		{Name: "写入 .env", Run: func(ctx context.Context, e *Env) error {
			s.AdminToken = e.Vars["admin_token"]
			return uploadFile(ctx, e, "写入 .env", s.Dir+"/.env", []byte(RenderEnv(s)), "0600")
		}},
		{Name: "启动 okserver", Run: func(ctx context.Context, e *Env) error {
			if _, err := runCmd(ctx, e, "启动 okserver", composeCmd(s.Dir, "up -d")); err != nil {
				return err
			}
			ctxT, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			_, err := runCmd(ctxT, e, "启动 okserver", waitHTTP(okHealth))
			return err
		}},
		{Name: "读取 root 初始密码", Run: func(ctx context.Context, e *Env) error {
			return readRootPassword(ctx, e, s.Dir)
		}},
	}}
}

// GovernanceChecklist 返回接入已有 Gitea 时的治理四件套确认清单（设计 §3.3）。
// 外部 Gitea 的 app.ini 无法远程修改，只能请用户逐项确认。
func GovernanceChecklist() []string {
	return []string{
		"已关闭开放注册（DISABLE_REGISTRATION=true）",
		"已禁止普通用户建仓（MAX_CREATION_LIMIT=0）",
		"已设默认私有仓（DEFAULT_PRIVATE=private）",
		"ROOT_URL 已设为成员实际访问地址",
	}
}

// SmokeExternalGitea 验证外部 Gitea 与 okserver 的兼容性（两条都过才放行）：
// ① admin token 有效且 API 可达；② 支持"token 当用户名"的 Basic 认证
// （okserver 下发 git token 的关键依赖，internal/oksrv/gitea.go 钉死的语义）。
func SmokeExternalGitea(ctx context.Context, e *Env, giteaURL, adminToken string) error {
	base := strings.TrimRight(giteaURL, "/")
	err := runStepMasked(ctx, e, "Gitea 兼容性冒烟",
		"curl -sf -H 'Authorization: token ****' "+base+"/api/v1/version",
		"curl -sf -H "+shellQuote("Authorization: token "+adminToken)+" "+shellQuote(base+"/api/v1/version"))
	if err != nil {
		return fmt.Errorf("Gitea API 不可达或 token 无效：%w", err)
	}
	err = runStepMasked(ctx, e, "Gitea 兼容性冒烟",
		"curl -sf -u '****:x' "+base+"/api/v1/user",
		"curl -sf -u "+shellQuote(adminToken+":x")+" "+shellQuote(base+"/api/v1/user"))
	if err != nil {
		return fmt.Errorf("该 Gitea 不支持 token 当用户名的 Basic 认证（Gitea 版本风险，需 1.22+ 已实证版本）：%w", err)
	}
	return nil
}

// buildExternalTask 接入已有 Gitea：只装 okserver 单容器（设计 §3.3）。
func buildExternalTask(s DeploySpec, compose string) (Task, error) {
	if s.GiteaURL == "" || s.AdminToken == "" {
		return Task{}, fmt.Errorf("external 模式需要 Gitea 地址与管理员 token")
	}
	okHealth := fmt.Sprintf("http://127.0.0.1:%d/api/v1/meta", s.OKPort)
	return Task{Name: "部署 okserver（接入已有 Gitea）", Steps: []Step{
		{Name: "Gitea 兼容性冒烟", Run: func(ctx context.Context, e *Env) error {
			return SmokeExternalGitea(ctx, e, s.GiteaURL, s.AdminToken)
		}},
		{Name: "创建部署目录", Run: func(ctx context.Context, e *Env) error {
			if _, err := runCmd(ctx, e, "创建部署目录",
				"mkdir -p "+s.Dir+"/okserver-data"); err != nil {
				return err
			}
			if _, err := runCmd(ctx, e, "创建部署目录",
				"chown -R 1000:1000 "+s.Dir+"/okserver-data"); err != nil {
				e.Hub.Publish("创建部署目录", "info", "警告：chown 失败（okserver 可能无法写数据目录）："+err.Error())
			}
			return nil
		}},
		{Name: "上传 compose 配置", Run: func(ctx context.Context, e *Env) error {
			return uploadFile(ctx, e, "上传 compose 配置", s.Dir+"/compose.yaml", []byte(compose), "0644")
		}},
		{Name: "写入 .env", Run: func(ctx context.Context, e *Env) error {
			return uploadFile(ctx, e, "写入 .env", s.Dir+"/.env", []byte(RenderEnv(s)), "0600")
		}},
		{Name: "拉取镜像", Run: func(ctx context.Context, e *Env) error {
			ctxT, cancel := context.WithTimeout(ctx, PullTimeout)
			defer cancel()
			_, err := runCmd(ctxT, e, "拉取镜像", composeCmd(s.Dir, "pull"))
			return err
		}},
		{Name: "启动 okserver", Run: func(ctx context.Context, e *Env) error {
			if _, err := runCmd(ctx, e, "启动 okserver", composeCmd(s.Dir, "up -d")); err != nil {
				return err
			}
			ctxT, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			_, err := runCmd(ctxT, e, "启动 okserver", waitHTTP(okHealth))
			return err
		}},
		{Name: "读取 root 初始密码", Run: func(ctx context.Context, e *Env) error {
			return readRootPassword(ctx, e, s.Dir)
		}},
	}}, nil
}

// readRootPassword 读取并删除 INITIAL_ROOT_PASSWORD；密码只进 Vars，不进日志。
func readRootPassword(ctx context.Context, e *Env, dir string) error {
	path := dir + "/okserver-data/INITIAL_ROOT_PASSWORD"
	ctxT, cancel := context.WithTimeout(ctx, CmdTimeout)
	defer cancel()
	var out []string
	code, err := e.Ex.Run(ctxT, "cat "+path, nil, func(stream, line string) {
		if stream == "stdout" {
			out = append(out, line)
		}
	})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("读取 root 初始密码失败（退出码 %d）", code)
	}
	e.Vars["root_password"] = strings.TrimSpace(strings.Join(out, "\n"))
	e.Hub.Publish("读取 root 初始密码", "info", "已读取 root 初始密码（将只在完成页显示一次）")
	_, err = runCmd(ctx, e, "读取 root 初始密码", "rm -f "+path)
	return err
}

// secretOpt 是 runStepMasked 的可选后处理（如捕获 stdout 进 Vars）。
type secretOpt func(stdout string)

// withSecret 包装后处理。
func withSecret(fn func(stdout string)) secretOpt { return fn }

// runStepMasked 执行命令：日志显示 display（掩码版），真实命令 cmd；
// 真实命令的 stdout 不进日志（防秘密泄漏），只经 post 回调给调用方。
func runStepMasked(ctx context.Context, e *Env, step, display, cmd string, post ...secretOpt) error {
	ctxT, cancel := context.WithTimeout(ctx, CmdTimeout)
	defer cancel()
	e.Hub.Publish(step, "info", "$ "+display)
	var out, errLines []string
	code, err := e.Ex.Run(ctxT, cmd, nil, func(stream, line string) {
		if stream == "stdout" {
			out = append(out, line)
		} else {
			errLines = append(errLines, line)
			e.Hub.Publish(step, "info", line) // stderr 进日志（不含 stdout 秘密）
		}
	})
	if err != nil {
		return err
	}
	stdout := strings.Join(out, "\n")
	if code != 0 {
		tail := strings.Join(errLines, "; ")
		if tail == "" {
			tail = "（无 stderr 输出）"
		}
		return fmt.Errorf("退出码 %d：%s", code, tail)
	}
	for _, fn := range post {
		fn(stdout)
	}
	return nil
}
