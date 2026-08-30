package deployx

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ContainerStatus 是一个容器的运行状态。
type ContainerStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ServerStatus 是管理模式的状态总览。
type ServerStatus struct {
	Containers []ContainerStatus `json:"containers"`
	Image      string            `json:"image"`
	DiskUsage  string            `json:"disk_usage"`
}

// QueryStatus 同步查询已部署服务端状态（快查询，API 层直接调，不走 Task）。
func QueryStatus(ctx context.Context, ex Executor, dir string) (*ServerStatus, error) {
	if err := ValidateDir(dir); err != nil {
		return nil, err
	}
	st := &ServerStatus{}
	out, code := runQuiet(ctx, ex, composeCmd(dir, `ps --format '{{.Name}}|{{.Status}}'`))
	if code != 0 {
		return nil, fmt.Errorf("查询容器状态失败（部署目录 %s 可能不存在）", dir)
	}
	for _, ln := range strings.Split(out, "\n") {
		parts := strings.SplitN(ln, "|", 2)
		if len(parts) == 2 && parts[0] != "" {
			st.Containers = append(st.Containers, ContainerStatus{Name: parts[0], Status: parts[1]})
		}
	}
	img, _ := runQuiet(ctx, ex, "grep '^OKSERVER_IMAGE=' "+dir+"/.env | cut -d= -f2-")
	st.Image = strings.TrimSpace(img)
	// cut -f1 在远端已截取首列，这里再按 tab 取一次兜底（输出含路径列时仍只取容量）。
	du, _ := runQuiet(ctx, ex, "du -sh "+dir+" | cut -f1")
	st.DiskUsage = strings.TrimSpace(strings.SplitN(du, "\t", 2)[0])
	return st, nil
}

// newReadOKPortStep 读取 .env 的 OKSERVER_PORT 存 Vars["ok_port"]（健康检查 URL 需要；备份/恢复任务复用）。
func newReadOKPortStep(dir string) Step {
	return Step{Name: "读取服务端口", Run: func(ctx context.Context, e *Env) error {
		out, err := runCmd(ctx, e, "读取服务端口",
			"grep '^OKSERVER_PORT=' "+dir+"/.env | cut -d= -f2")
		if err != nil {
			return fmt.Errorf("读不到 OKSERVER_PORT：%v", err)
		}
		port := strings.TrimSpace(out)
		if port == "" {
			return fmt.Errorf("读不到 OKSERVER_PORT（.env 缺该键）")
		}
		e.Vars["ok_port"] = port
		return nil
	}}
}

// healthStep 等待 okserver 就绪（端口取自 Vars["ok_port"]；备份/恢复任务复用）。
func healthStep(dir string) Step {
	return Step{Name: "健康检查", Run: func(ctx context.Context, e *Env) error {
		ctxT, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		_, err := runCmd(ctxT, e, "健康检查",
			waitHTTP("http://127.0.0.1:"+e.Vars["ok_port"]+"/api/v1/meta"))
		return err
	}}
}

// tagRe 是镜像 tag 的允许字符集——newTag 只进 sed 替换值，白名单防注入。
var tagRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// BuildUpgradeTask 升级：改 .env 镜像 tag → pull → up -d → 健康检查（数据卷不动）。
func BuildUpgradeTask(dir, newTag string) Task {
	return Task{Name: "升级到 " + newTag, Steps: []Step{
		{Name: "校验部署目录", Run: func(_ context.Context, _ *Env) error {
			return ValidateDir(dir)
		}},
		{Name: "校验版本 tag", Run: func(_ context.Context, _ *Env) error {
			if !tagRe.MatchString(newTag) {
				return fmt.Errorf("镜像 tag 含非法字符：%q（允许字母数字、._-）", newTag)
			}
			return nil
		}},
		newReadOKPortStep(dir),
		{Name: "更新镜像 tag", Run: func(ctx context.Context, e *Env) error {
			_, err := runCmd(ctx, e, "更新镜像 tag",
				"sed -i 's|^OKSERVER_IMAGE=.*|OKSERVER_IMAGE=openknowledge/okserver:"+newTag+"|' "+dir+"/.env")
			return err
		}},
		{Name: "拉取新镜像", Run: func(ctx context.Context, e *Env) error {
			ctxT, cancel := context.WithTimeout(ctx, PullTimeout)
			defer cancel()
			_, err := runCmd(ctxT, e, "拉取新镜像", composeCmd(dir, "pull"))
			return err
		}},
		{Name: "重启服务", Run: func(ctx context.Context, e *Env) error {
			_, err := runCmd(ctx, e, "重启服务", composeCmd(dir, "up -d"))
			return err
		}},
		healthStep(dir),
	}}
}

// BuildUninstallTask 卸载：down --rmi all；deleteData=true 才删部署目录（默认保留）。
func BuildUninstallTask(dir string, deleteData bool) Task {
	steps := []Step{
		{Name: "校验部署目录", Run: func(_ context.Context, _ *Env) error {
			return ValidateDir(dir)
		}},
		{Name: "停止并删除容器镜像", Run: func(ctx context.Context, e *Env) error {
			ctxT, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			_, err := runCmd(ctxT, e, "停止并删除容器镜像", composeCmd(dir, "down --rmi all"))
			return err
		}},
	}
	if deleteData {
		steps = append(steps, Step{Name: "删除数据目录", Run: func(ctx context.Context, e *Env) error {
			_, err := runCmd(ctx, e, "删除数据目录", "rm -rf "+dir)
			return err
		}})
	} else {
		steps = append(steps, Step{Name: "保留数据目录", Run: func(ctx context.Context, e *Env) error {
			e.Hub.Publish("保留数据目录", "info", "数据目录已保留："+dir+"（重装可复用）")
			return nil
		}})
	}
	return Task{Name: "卸载 OpenKnowledge 服务端", Steps: steps}
}
