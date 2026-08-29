# OpenKnowledge 服务端 NAS 部署（okserver + Gitea）

本文档是 OpenKnowledge 服务端部署/运维的**单一事实源**，落地设计文档 §9.5（`docs/superpowers/specs/2026-08-25-personal-sync-p1-design.md`）全文；GUI 部署指引卡与本文同源维护。

服务端 = 两个进程：

- **okserver**：管理面（建用户/组织/仓、发 git token、审计），监听 `3100`，本地 OkManager GUI 连的是它；
- **Gitea**：git 托管后端，监听 `3000`（git over HTTPS）。对终端用户完全透明——成员不持有 Gitea 密码、不登录 Gitea 网页。

---

## 1. 前置形态矩阵（双路径）

| 路径 | 适用 | 说明 |
|---|---|---|
| **首选 Docker** | 群晖 Container Manager / 威联通 Container Station / 绿联极空间 / 任意 Linux 主机 | 本目录 `docker-compose.yml` 一键双容器 |
| **备选裸二进制** | 无 Docker 的老 NAS 或极简环境 | okserver 与 Gitea 都是单二进制 + 数据目录（`systemd/` 下 unit 示例，或直接进程守护） |

两条路径**功能无差异**，Docker 只是省心。下文第 2 节走 Docker，第 3 节走裸二进制。

## 2. 路径一：Docker 部署（五步）

前置：NAS 上已有 Docker 与 compose 插件；`git clone`（或下载）本仓库后 `cd server/nas`。

**第 1 步：建持久化目录并准备 .env**

```bash
mkdir -p gitea-data okserver-data
# okserver 容器内以非 root 用户 okserver（uid 1000）运行，bind mount 需先授权：
sudo chown -R 1000:1000 okserver-data
cp .env.example .env   # 编辑 .env：GITEA_ROOT_URL 改成 NAS 实际地址
```

**第 2 步：起 Gitea，走安装向导**

```bash
docker compose up -d gitea
```

访问 `http://<nas>:3000` 走完安装向导：**数据库选 SQLite**、**建管理员账号**、确认**关闭开放注册**（compose 已注入 `DISABLE_REGISTRATION=true`，向导页确认即可）。

**第 3 步：生成 Gitea admin token 并填入 .env**

Gitea 网页：管理员账号 → 设置 → Applications → 生成 token。

> **token scope 必修**：token 必须带 **user / admin 相关 scope**（或直接用全权 token）。scope 不足时，okserver 经它建仓 / 给用户发 token 会得到 **403**，表现为 OkManager 里建仓、加成员全部失败。

把 token 填入 `.env` 的 `GITEA_ADMIN_TOKEN=`。

**第 4 步：起 okserver，取 root 初始密码**

```bash
docker compose up -d
docker exec $(docker compose ps -q okserver) cat /data/INITIAL_ROOT_PASSWORD
# 或看容器日志：docker compose logs okserver | grep 初始密码
```

密码只在**首次启动**生成一次（32 位随机，bcrypt 存库）。**取走后删除该文件**：

```bash
docker exec $(docker compose ps -q okserver) rm /data/INITIAL_ROOT_PASSWORD
```

**第 5 步：成员设备接入**

每台成员设备：OkManager → 服务器页三步向导 → 连 `http://<nas>:3100` → root 登录建用户/组织 → 成员各自登录、建仓绑定、开始同步。

## 3. 路径二：裸二进制 + systemd（备选）

与 Docker 路径对等的五步；功能无差异。

**第 1 步：拿二进制**

- okserver：release 页下载 `okserver_<版本>_linux_amd64` / `okserver_<版本>_linux_arm64`，或自行构建：`CGO_ENABLED=0 go build -o okserver ./cmd/okserver`（纯 Go，无 CGO 依赖）。放到 `/usr/local/bin/okserver`；
- Gitea：按官方文档装单二进制（https://docs.gitea.com/installation/install-from-binary），放到 `/usr/local/bin/gitea`。

**第 2 步：建用户与数据目录**

```bash
sudo useradd -r -m git                                  # Gitea 运行用户
sudo useradd -r -M okserver                             # okserver 运行用户
sudo mkdir -p /var/lib/okserver /var/lib/gitea /etc/okserver /etc/gitea
sudo chown okserver: /var/lib/okserver
sudo chown git:git /var/lib/gitea
```

**第 3 步：起 Gitea，走安装向导 + 治理配置**

先只跑 Gitea（`sudo -u git /usr/local/bin/gitea web` 临时起，或直接配好 unit 后 `systemctl start gitea`），访问 `http://<nas>:3000` 走安装向导（SQLite + 建管理员账号）。然后在 `/etc/gitea/app.ini` 写入**第 4 节的治理配置四项**（Docker 路径由 compose 注入，裸机路径必须手写），重启 Gitea。

**第 4 步：生成 admin token（scope 要求同第 2 节第 3 步的加粗警告），写 okserver 环境文件**

```bash
sudo tee /etc/okserver/env <<'EOF'
OKSERVER_GITEA_ADMIN_TOKEN=<上一步生成的 token>
EOF
sudo chmod 600 /etc/okserver/env
```

`okserver.service` 的 `EnvironmentFile=/etc/okserver/env` 是**必需**的（无 `-` 前缀），缺文件服务起不来。监听地址 / 数据目录 / Gitea 地址已在 unit 里写死（`:3100` / `/var/lib/okserver` / `http://127.0.0.1:3000`），要改就编辑 unit。

**第 5 步：装 unit 并启动**

```bash
sudo cp systemd/okserver.service systemd/gitea.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now gitea okserver
sudo -u okserver cat /var/lib/okserver/INITIAL_ROOT_PASSWORD   # 取 root 初始密码，取走后删除
```

成员设备接入同 Docker 路径第 5 步。

## 4. Gitea 侧治理配置（四项，缺一不可）

| # | 配置 | 作用 |
|---|---|---|
| ① | `[service] DISABLE_REGISTRATION = true` | 关闭开放注册——okserver 是**唯一**建用户入口 |
| ② | `[repository] MAX_CREATION_LIMIT = 0` | 禁止普通用户自行建仓——仓一律经 okserver provisioning，命名空间治理才不被绕开 |
| ③ | `[repository] DEFAULT_PRIVATE = private` | 新仓默认私有 |
| ④ | `[server] ROOT_URL = http://<nas>:3000/` | 指向 NAS 实际地址——否则 Gitea 生成的 clone URL 是容器内地址，成员拿到的 remote 是错的 |

Docker 路径：四项已由 `docker-compose.yml` 的 `GITEA__*` 环境变量注入，④的值来自 `.env` 的 `GITEA_ROOT_URL`。裸机路径：手写进 `/etc/gitea/app.ini`。

## 5. 权限语义须知（v1，部署人必读）

经 okserver 加入组织的成员，在 **git 侧进的是 Gitea org 的 Owners 队（全权）**——org 内仓库的读写在 git 层不做细分；okserver 自有角色（root / admin / member）只在 **okserver 应用层 API** 生效。这是 v1 刻意从简的语义（org 内角色下放留待 v1.1），**适用于内网受信团队**；若有"成员只能读部分仓"的诉求，v1 做不到，部署前知悉。

另一已知限制：**v1 无 git token 重发**——建仓/建用户时 token 下发失败或 token 丢失，只能重建仓或待 v1.1（reset 联动重发）。

## 6. 升级

```bash
docker compose pull && docker compose up -d
```

- okserver 启动时自动跑 SQLite schema migrate（只增不毁），降级不受支持；
- Gitea 按其官方升级路径（镜像 tag 升级即可）；
- **升级前先备份数据卷**（见第 7 节）；
- **Gitea 版本升级后先跑冒烟**：okserver 给用户发 token 的链路依赖 Gitea "Basic 认证支持 token 当用户名" 的行为（Gitea 1.22/main 已实证，`internal/oksrv/gitea.go` 钉死该语义）。Gitea 大版本升级后，立刻做一次冒烟：root 登录 okserver → 建测试用户 → 测试建仓 → 用下发的 token `git clone`。失败则回退 Gitea 版本并查该行为是否变更。

裸机路径：换二进制 + `systemctl restart`，其余相同。

## 7. 备份（知识历史的最后一道防线）

两个数据卷都要备：

- `okserver-data/`：SQLite 单文件，停机拷贝或用 SQLite `.backup` 在线备份；
- `gitea-data/`：全部仓库存储——NAS 快照 / 定时 rsync / `gitea dump`。

本目录 `backup/backup.sh` 是现成示例（NAS 上需有 `sqlite3` 与 `rsync`，群晖/威联通可从套件中心或 Entware 装）：

```bash
cd server/nas
./backup/backup.sh /volume1/backups/openknowledge   # 建议 cron 每日跑
```

> 另记住：**每台成员设备本身就是一份完整 git 副本**——双层防丢的题中之义，服务器重建后任一设备 push 即可恢复历史。

## 8. 网络与 TLS

默认假设部署在**内网**，HTTP 即可。需要公网暴露时**必须**自行加反代 TLS（Caddy / Nginx Proxy Manager 均可），且两个端口都要罩住：

- `3100`：okserver 管理面 API；
- `3000`：git over HTTPS（clone/push 走它）；
- `2222`：SSH，按需开放（v1 git 走 HTTPS + token，不开无影响）。

反代后记得把 Gitea 的 `ROOT_URL`（`.env` 的 `GITEA_ROOT_URL`）改成 https 地址，否则 clone URL 仍是错的。

## 9. 资源底线

okserver 常驻约 **20MB** 内存；Gitea 约 **200–500MB**。**1C1G 入门 NAS 可跑**。

## 10. 镜像与架构（维护者向）

okserver 镜像多架构（`linux/amd64` + `linux/arm64`——ARM NAS 是常态），构建上下文是**仓库根目录**：

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  --build-arg VERSION=<版本> \
  -f server/nas/Dockerfile -t openknowledge/okserver:<版本> --push .
```

随 OK release 流水线双发 **GHCR + Docker Hub**，版本号与 ok/okd 对齐（sync-version 纪律，bump 时同步）。镜像内以非 root 用户 `okserver` 运行，无 CGO（SQLite 用 modernc.org/sqlite 纯 Go 实现）。

发布边界：`cmd/okserver` 产物与 `server/` 目录**不进入任何客户端安装包**；okserver 的分发渠道只有 Docker 镜像与 release 页独立二进制（`okserver_<版本>_linux_amd64/arm64`）。

## 目录说明

```
server/nas/
├── docker-compose.yml      # 双容器编排（治理配置①②③④已注入）
├── .env.example            # GITEA_ADMIN_TOKEN / GITEA_ROOT_URL / 端口 / 镜像占位
├── Dockerfile              # okserver 多架构镜像（构建上下文 = 仓库根目录）
├── README.md               # 本文档（部署文档单一事实源）
├── systemd/                # 裸二进制备选路径 unit 示例
│   ├── okserver.service
│   └── gitea.service       # Gitea 官方示例（工作目录 + gitea web）
└── backup/
    └── backup.sh           # 两数据卷备份：SQLite .backup 在线 + rsync
```
