# okdeploy 一键部署器

图形化的 OpenKnowledge 服务端部署工具：双击运行 → 浏览器向导 → SSH 部署到 NAS。

## 获取与运行

release 页下载独立 artifact（不进客户端安装包）：
- Windows：`okdeploy-windows-amd64.exe`（双击，自动开浏览器）
- Linux：`okdeploy-linux-amd64`（`chmod +x` 后运行）

## 功能

- **部署**：全新双容器（okserver + Gitea，无人值守初始化）或接入 NAS 已有 Gitea
- **管理**：状态查看 / 升级 / 备份 / 恢复 / 卸载

## 须知

- 部署目标需已有 Docker 与 compose 插件，且 SSH 用户在 docker 组（免 sudo 跑 docker）。
- 备份/恢复是**停机一致性**语义（先停容器再打包），停机窗口通常数秒。
- SSH 凭据只存内存，不落盘；v1 接受任意主机密钥（内网 NAS 场景），勿对公网不可信主机使用。
- root 初始密码只在部署完成页显示一次，请立即保存。
- 手工部署/运维细节（systemd、裸二进制、排障）见 `server/nas/README.md`。
