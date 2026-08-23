#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=$(sed -n 's/^#define AppVersion "\(.*\)"/\1/p' installer/openknowledge.iss)
: "${VERSION:?无法从 installer/openknowledge.iss 提取 AppVersion}"
go build -ldflags "-s -w -H windowsgui -X openknowledge/internal/version.Version=$VERSION" -o dist/ok.exe ./cmd/ok
go build -ldflags "-s -w -H windowsgui -X openknowledge/internal/version.Version=$VERSION" -o dist/okd.exe ./cmd/okd
go build -ldflags "-s -w -H windowsgui -X openknowledge/internal/version.Version=$VERSION" -o dist/OkManager.exe ./cmd/okmanager
rm -rf dist/web dist/changelogs
cp -r web dist/web
cp -r docs/changelogs dist/changelogs
# iss 无条件打包 dist\runtime；本脚本不下载 runtime，缺失时明确报错指路，而非让 ISCC 编译失败
if [ ! -f dist/runtime/llama-server.exe ]; then
  echo "错误：dist/runtime 缺 llama-server.exe——先跑 python scripts/build.py 下载 llama runtime（或设 LLAMA_CPP_BASE_URL 镜像）" >&2
  exit 1
fi
echo "dist/ built: ok.exe + okd.exe + OkManager.exe + web/ + changelogs/"
