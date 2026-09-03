// okserver：Okryptos 服务端管理面（NAS/Docker 部署）。
// 薄 main：env 配置 → 存储 → root 首启 → HTTP 服务。
package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"okryptos/internal/logx"
	"okryptos/internal/oksrv"
	"okryptos/internal/version"
)

func main() { os.Exit(run()) }

func run() int {
	// 子命令分发（HTTP 服务启动前）：容器内 /out/okserver reset-root
	if len(os.Args) > 1 && os.Args[1] == "reset-root" {
		return resetRoot()
	}

	log := logx.New(os.Stdout)
	listen := envOr("OKSERVER_LISTEN", ":3100")
	dataDir := envOr("OKSERVER_DATA_DIR", "./okserver-data")
	giteaURL := os.Getenv("OKSERVER_GITEA_URL")
	adminToken := os.Getenv("OKSERVER_GITEA_ADMIN_TOKEN")

	st, err := oksrv.OpenStore(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开存储失败: %v\n", err)
		return 1
	}
	defer st.Close()

	// root 首启：明文写 <dataDir>/INITIAL_ROOT_PASSWORD（0600）+ 日志打印一次
	if pw, created, err := st.EnsureRoot(); err != nil {
		fmt.Fprintf(os.Stderr, "root 初始化失败: %v\n", err)
		return 1
	} else if created {
		initFile := filepath.Join(dataDir, "INITIAL_ROOT_PASSWORD")
		if err := os.WriteFile(initFile, []byte(pw+"\n"), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "root 初始密码落盘失败: %v（数据目录尚新时删除数据目录重跑即可重新生成）\n", err)
			return 1
		}
		log.Write([]byte(fmt.Sprintf("root 初始密码已生成并写入 %s（取走后请删除该文件）\n", initFile)))
	}

	var backend oksrv.GitBackend
	if giteaURL == "" {
		backend = oksrv.NewFakeBackend()
		log.Write([]byte("警告：OKSERVER_GITEA_URL 未配置，git 后端为离线 fake 模式（建仓不真实生效）\n"))
	} else {
		backend = oksrv.NewGitea(giteaURL, adminToken)
	}

	log.Write([]byte(fmt.Sprintf("okserver %s 监听 %s（数据目录 %s）\n", version.Version, listen, dataDir)))
	if err := http.ListenAndServe(listen, oksrv.NewMux(st, backend, version.Version)); err != nil {
		fmt.Fprintf(os.Stderr, "服务失败: %v\n", err)
		return 1
	}
	return 0
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// resetRoot 是容器内运维子命令（/out/okserver reset-root）：重置 root 密码。
// stdout 只打印一行新密码（供部署器捕获），提示一律走 stderr。
func resetRoot() int {
	st, err := oksrv.OpenStore(envOr("OKSERVER_DATA_DIR", "./okserver-data"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开数据库失败：", err)
		return 1
	}
	defer st.Close()
	pw, err := st.ResetRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "重置失败：", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "root 密码已重置")
	fmt.Println(pw)
	return 0
}
