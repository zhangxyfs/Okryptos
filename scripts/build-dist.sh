#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=$(sed -n 's/^#define AppVersion "\(.*\)"/\1/p' installer/okryptos.iss)
: "${VERSION:?无法从 installer/okryptos.iss 提取 AppVersion}"
# exe 版本资源（VS_VERSION_INFO + 图标）：从 cmd/*/winres.json 重新生成 syso。
# 仓库内 syso 可能只含图标无版本信息，工具缺失或生成失败即中断出包（M-16 守门，不再静默跳过）
WINRES=$(command -v go-winres || true)
if [ -z "$WINRES" ]; then
  GOPATH_BIN="$(go env GOPATH)/bin"
  for c in "$GOPATH_BIN/go-winres.exe" "$GOPATH_BIN/go-winres"; do
    if [ -x "$c" ]; then WINRES="$c"; break; fi
  done
fi
if [ -z "$WINRES" ]; then
  echo "错误：未找到 go-winres，无法生成 exe 版本资源" >&2
  echo "  安装: go install github.com/tc-hib/go-winres@latest" >&2
  exit 1
fi
for pkg in ok okd okmanager okdeploy; do
  (cd "cmd/$pkg" && "$WINRES" make --in winres.json) || { echo "错误：go-winres 生成 cmd/$pkg 版本资源失败" >&2; exit 1; }
done
go build -ldflags "-s -w -H windowsgui -X okryptos/internal/version.Version=$VERSION" -o dist/ok.exe ./cmd/ok
go build -ldflags "-s -w -H windowsgui -X okryptos/internal/version.Version=$VERSION" -o dist/okd.exe ./cmd/okd
go build -ldflags "-s -w -H windowsgui -X okryptos/internal/version.Version=$VERSION" -o dist/OkManager.exe ./cmd/okmanager
# OkMeter（C++ 原生 token 监视器）：MSVC 构建（单测+exe 一体）。
# 注意：本机有 OkMeter 实例在跑会占用 exe 导致 LNK1104——发布前先关掉运行实例
MSYS_NO_PATHCONV=1 cmd //c "okmeter\\build.bat" > /dev/null || {
  echo "错误：okmeter 构建失败（若有运行中的 OkMeter 实例请先关闭）" >&2
  exit 1
}
[ -f okmeter/build/OkMeter.exe ] || { echo "错误：okmeter/build/OkMeter.exe 未产出" >&2; exit 1; }
cp okmeter/build/OkMeter.exe dist/OkMeter.exe
# okdeploy 一键部署器：独立 artifact（dist/deploy/），不进安装包（iss 不打 dist/deploy）
mkdir -p dist/deploy
go build -ldflags "-s -w -H windowsgui -X okryptos/internal/version.Version=$VERSION" -o dist/deploy/okdeploy-windows-amd64.exe ./cmd/okdeploy
rm -rf dist/web dist/changelogs
cp -r web dist/web
cp -r docs/changelogs dist/changelogs
# iss 无条件打包 dist\runtime；本脚本不下载 runtime，缺失时明确报错指路，而非让 ISCC 编译失败
if [ ! -f dist/runtime/llama-server.exe ]; then
  echo "错误：dist/runtime 缺 llama-server.exe——先跑 python scripts/build.py 下载 llama runtime（或设 LLAMA_CPP_BASE_URL 镜像）" >&2
  exit 1
fi
echo "dist/ built: ok.exe + okd.exe + OkManager.exe + web/ + changelogs/"
