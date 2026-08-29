#!/bin/sh
# 两数据卷备份：okserver SQLite（.backup 在线）+ gitea-data（rsync）。
# 用法：./backup.sh /path/to/backup-dest
set -e
DEST="${1:?用法: backup.sh <备份目标目录>}"
SRC="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$DEST"
# okserver（SQLite 在线备份）
sqlite3 "$SRC/okserver-data/okserver.db" ".backup '$DEST/okserver.db'"
# gitea 仓库存储
rsync -a --delete "$SRC/gitea-data/" "$DEST/gitea-data/"
echo "备份完成：$DEST（另：每台成员设备本身就是完整 git 副本）"
