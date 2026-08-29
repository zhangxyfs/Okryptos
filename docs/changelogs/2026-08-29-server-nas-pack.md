# server/nas NAS 部署包

日期：2026-08-29

`server/nas/` 落地设计文档 §9.5 部署包七件：`docker-compose.yml`（gitea + okserver 双容器，Gitea 治理配置四项——关注册/禁普通用户建仓/默认私有/ROOT_URL——以 `GITEA__*` env 注入，端口与镜像走 `.env` 占位）；`.env.example`；`Dockerfile`（多架构 linux/amd64+arm64，构建上下文=仓库根目录，CGO_ENABLED=0 纯 Go SQLite，运行态非 root 用户 okserver）；`README.md`（部署文档单一事实源，§9.5 逐项落地：Docker/裸二进制双路径、五步部署流程、治理四配置、升级、备份、网络 TLS、资源底线，另含评审移交两条——admin token 必须带 user/admin scope 否则建仓/发 token 403、v1 权限语义 git 侧 org Owners 队全权仅内网受信适用、token 下发依赖 Gitea "token 当 Basic 用户名" 行为故 Gitea 升级后先冒烟）；`systemd/okserver.service` + `systemd/gitea.service`（裸机备选路径，后者照 Gitea 官方示例）；`backup/backup.sh`（SQLite `.backup` 在线 + rsync 两数据卷）。验证：`sh -n backup/backup.sh` 通过；compose YAML 解析通过（本机无 docker，`docker compose config` 与 `docker build` 未跑）。
