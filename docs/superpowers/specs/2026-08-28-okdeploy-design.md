# okdeploy 一键部署器设计（P1-D）

> 日期：2026-08-28　状态：设计已批准，待实施
> 上游：`2026-08-25-personal-sync-p1-design.md`（多端同步 P1）、`server/nas/README.md`（手工部署路径）

## 1. 背景与目标

okserver 服务端目前只有手工部署路径（`server/nas/`：cp .env、docker compose up -d、取 root 密码）。
目标：一个**独立可执行程序**（Windows/Linux），通过 SSH 连到 NAS，图形界面引导完成
部署/升级/备份/恢复/卸载，执行过程实时输出到界面。

### 已定决策（用户拍板）

| 决策点 | 结论 |
|---|---|
| 形态 | 独立可执行程序，与客户端完全分离（服务器侧代码不进客户端） |
| GUI 技术栈 | 内嵌 Web UI，双击自动开浏览器（同 `ok gui` 模式；不用 Wails/Fyne） |
| 镜像来源 | 发布镜像仓库拉取（GHCR + Docker Hub，随 release 双发）；部署器只传 compose+配置 |
| 功能范围 | 全生命周期：部署 / 升级 / 备份 / 恢复 / 卸载 |
| 已有 Gitea | 支持两分支：全新双容器部署；或接入 NAS 已有 Gitea（复用，避免端口冲突） |

## 2. 架构

```
浏览器（UI） ←REST + SSE→ okdeploy 进程（本地 127.0.0.1:随机端口） ←SSH→ NAS
```

- 单 Go 二进制，前端静态资源 embed 进二进制；启动后监听 127.0.0.1 随机端口并拉起浏览器（复用 `ok gui` 的开浏览器逻辑）。
- **零新增第三方依赖**：SSH 用 `golang.org/x/crypto/ssh`（已是直接依赖）；文件上传走 SSH stdin（`cat > path`），备份下载走 stdout tar 流；实时日志用 SSE（net/http + 浏览器原生 EventSource）。不引 sftp/websocket 库。

### 2.1 代码归属与模块

遵循"服务器代码全在 server/ 下"的边界纪律：

| 位置 | 内容 |
|---|---|
| `cmd/okdeploy/` | main：起本地 HTTP 服务、embed 前端、开浏览器 |
| `internal/deployx/` | 核心逻辑（纯 Go，可单测） |
| `internal/deployx/webui/` | 前端静态资源（go:embed 要求被嵌文件与 Go 源同包，server/ 下无 Go 包可嵌，故归此处） |
| `server/deploy/` | README、发布脚本说明 |

`internal/deployx` 模块划分（各自独立可测）：

- `SSHClient`：拨号（密码或私钥）、执行命令（流式回传 stdout/stderr 行）、上传（stdin 写文件）、下载（stdout 读 tar 流）
- `Probe`：环境探测——docker/compose 版本、3000/3100 端口占用、已有 Gitea 容器、已有 okserver 部署
- `Task`：任务编排——deploy / upgrade / backup / restore / uninstall，每步一条日志，失败即停
- `LogHub`：SSE 广播，前端实时滚动
- 编排逻辑全部面向 `Executor` 接口写，单测用 fake executor（见 §5）

### 2.2 凭据与安全

- SSH 密码/私钥**只存内存，不落地**；每次启动重新输入。
- 日志脱敏：密码、Gitea token、root 初始密码以外的秘密值永不回显（命令行中的敏感参数打码后再入日志）。
- root 初始密码：部署完成后从 NAS 读 `/var/lib/okserver/INITIAL_ROOT_PASSWORD`（对应部署目录下的卷路径），显示后即删该文件（沿用 `server/nas/README.md` 既有约定）。

## 3. 流程与 UI 页面流

四个页面：线性向导（连接 → 探测 → 部署）+ 管理模式。

### 3.1 连接页

SSH 地址 / 端口（默认 22）/ 用户名 / 密码或私钥文件路径。"测试连接"按钮，失败给原因（网络不可达/认证失败/超时）。

### 3.2 探测结果页（连上后自动执行 Probe）

- Docker 及 compose 插件版本；**没有 Docker → 红色提示 + 安装指引，终止流程**（v1 不代装 Docker）。
- 3000/3100 端口占用：被占则自动建议 3001/3101（可手改），写入 `.env`。
- 检测到已有 Gitea（容器或端口特征）→ 分支选择：**全新部署双容器** 或 **接入已有 Gitea**。
- 检测到已有 okserver 部署（compose 项目/容器）→ 跳过向导直接进**管理模式**。

### 3.3 部署页

- 全新部署：填远端目录（默认 `~/openknowledge`，可浏览远端目录选择）、Gitea/okserver 端口、镜像 tag（默认与部署器自身版本对齐）。治理四件套（关注册/建仓限额 0/默认 private/ROOT_URL）自动写进 compose 环境变量（与 `server/nas/docker-compose.yml` 一致）。
- 接入已有 Gitea：只装 okserver 单容器；填 Gitea URL + 管理员 token。部署前做**兼容性冒烟**：建测试仓 → token 当用户名 Basic 认证拉取 → 删仓；失败则明确提示 Gitea 版本风险（依赖行为见 `internal/oksrv/gitea.go` 钉死的语义）。治理四件套无法远程改外部 Gitea 的 app.ini → 出**手动配置清单**让用户逐项确认后才可继续。
- 执行：实时日志滚动（SSE）；成功后**醒目显示 root 初始密码**（读后删文件），附"下一步：客户端 OkManager 服务器页接入"指引。

### 3.4 管理模式（已部署设备再连时进入）

- **状态**：两容器（或单容器）运行状态、镜像版本、数据目录磁盘占用。
- **升级**：选新 tag → pull → up -d；数据卷不动。
- **查看日志**：拉取两容器 `docker compose logs --tail=N --no-color` 显示在日志面板（排障刚需，同步快查询）。
- **重置 root 密码**：root 初始密码只显示一次，丢失需要恢复入口。依赖 okserver 新增 `reset-root` 子命令（重生成 32 位随机密码、bcrypt 入库、重写 INITIAL_ROOT_PASSWORD）；部署器执行 `docker compose exec -T okserver okserver reset-root` 后读密码、完成页语义显示一次。确认词 `RESET`。
- **备份**：远端执行 `server/nas/backup/backup.sh` 逻辑打包数据卷 → stdout tar 流式下载到本地目录，进度条。
- **恢复**：选本地备份包 → 上传 → 停容器 → 解压 → 起 → 健康检查。覆盖性操作，二次确认。
- **卸载**：停删容器+镜像；数据目录**默认保留**，显式勾选才删除；二次确认。

破坏性操作（恢复覆盖、卸载删数据）需输入确认词；任何步骤失败即停，日志可下载。

## 4. 错误处理

- 每条远程命令独立超时：常规命令 60s；备份/恢复流传输 30min。非零退出即中断任务，日志标红并给人话诊断（如"端口 3000 被占用：进程 nginx"）。
- 任务幂等可重入：部署按步骤幂等（已存在的目录/文件跳过），失败修复后从断点重跑。
- 日志面板带"下载日志"按钮，整段会话落本地临时文件，便于反馈问题。

## 5. 测试

- **单测**：`deployx` 全部编排逻辑面向 `Executor` 接口；fake executor 断言命令序列与分支，探测三分支（全新/接入已有 Gitea/已有部署进管理）都覆盖。
- **集成测试**（可选，不进默认套件）：`go test -tags=integration`，Docker 可用时起 openssh-server 容器跑真 SSH 冒烟。
- **真机验收**（发版前手动，列入发版清单）：Windows + Linux 各跑一遍全流程到真 NAS/VM，含接入已有 Gitea 分支。

## 6. 构建分发与边界

- `scripts/build.py` 增加 okdeploy 目标：纯 Go 交叉编译出 `okdeploy-windows-amd64.exe` / `okdeploy-linux-amd64`（前端 embed）。
- release 流水线挂两个**独立** artifact；**iss/nfpm 客户端安装包不含 okdeploy**（与 okserver 同纪律），`installer/` 无需改动。
- 版本号与 ok/okd/okserver 对齐（sync-version 纪律，bump 时同步）。
- 部署页镜像 tag 可选值来自 GHCR 发布清单，默认与部署器自身版本一致。

## 7. 文档落点

- 使用文档：`server/deploy/README.md`。
- `server/nas/README.md` 顶部加一句"图形化一键部署见 server/deploy"（手工路径保留）。
- P1 设计文档（`2026-08-25-personal-sync-p1-design.md`）补一小节：部署器存在、边界（独立 artifact、不进客户端包）。

## 8. 明确不做（v1 边界）

- 不代装 Docker / 不处理 NAS 系统级依赖。
- 不做多服务器管理（一次连一台；连接配置不保存）。
- 不做 okserver 应用层用户管理（那是 OkManager 服务器页的职责）。
- 外部 Gitea 的治理四件套不远程改，只检测+出清单。
- **不做数据库管理**：两端都是 SQLite 单文件，无实例/账号/连接池可管；数据层运维已由备份/恢复覆盖。
- **不做通用配置编辑**：配置面极小（端口/ROOT_URL/镜像 tag 已在部署与升级流程覆盖），为两三个极少改的字段做编辑+重启+回滚不值得。
