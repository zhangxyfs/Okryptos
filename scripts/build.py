#!/usr/bin/env python3
"""OpenKnowledge 一键构建：编译 dist/（含 exe 图标嵌入）+ 打包安装程序。

用法:
  python scripts/build.py                  # 完整流程（dist + 安装程序）
  python scripts/build.py --test           # 测试安装包：版本号追加 _test（OpenKnowledgeSetup-<ver>_test.exe），不改 iss 文件
  python scripts/build.py --skip-installer # 只构建 dist/
  python scripts/build.py --skip-winres    # 跳过 exe 图标/版本信息嵌入

环境变量 ISCC 可覆盖 Inno Setup 编译器路径。
"""
import argparse
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ISCC = os.environ.get("ISCC", r"C:\Users\Administrator\AppData\Local\Programs\Inno Setup 7\ISCC.exe")
LDFLAGS = "-s -w -H windowsgui"
LLAMA_TAG = "b10405"
LLAMA_BASE_DEFAULT = "https://github.com/ggml-org/llama.cpp/releases/download"


def app_version(test=False):
    """从 installer/openknowledge.iss 提取 #define AppVersion，提取不到则报错退出。
    test=True 时追加 _test 后缀（测试包与正式包可同机区分，幂等不叠加）。"""
    text = (ROOT / "installer" / "openknowledge.iss").read_text(encoding="utf-8")
    m = re.search(r'^#define AppVersion "([^"]+)"', text, re.MULTILINE)
    if not m:
        sys.exit("未能从 installer/openknowledge.iss 提取 AppVersion")
    v = m.group(1)
    if test and not v.endswith("_test"):
        v += "_test"
    return v


def run(cmd, cwd=ROOT):
    print("+", " ".join(str(c) for c in cmd))
    subprocess.run([str(c) for c in cmd], check=True, cwd=cwd)


def prepare_runtime():
    """下载 llama.cpp 预编译 llama-server（win cpu x64）到 dist/runtime/。

    内置 embedding sidecar 的运行时；版本钉死 LLAMA_TAG。
    LLAMA_CPP_BASE_URL 可覆盖下载源（国内代理/镜像）。
    """
    import zipfile
    dest = ROOT / "dist" / "runtime"
    tag_file = dest / ".llama-tag"   # 下载来源版本标记：LLAMA_TAG 升级后旧 runtime 不再静默复用
    current = tag_file.read_text(encoding="utf-8").strip() if tag_file.exists() else ""
    if (dest / "llama-server.exe").exists() and current == LLAMA_TAG:
        print(f"runtime 已存在（llama.cpp {LLAMA_TAG}），跳过下载（删除 dist/runtime 可强制刷新）")
        return
    if (dest / "llama-server.exe").exists():
        print(f"runtime 版本不符（{current or '无标记'} ≠ {LLAMA_TAG}），重新下载")
        shutil.rmtree(dest)
    base = os.environ.get("LLAMA_CPP_BASE_URL", LLAMA_BASE_DEFAULT)
    url = f"{base}/{LLAMA_TAG}/llama-{LLAMA_TAG}-bin-win-cpu-x64.zip"
    zip_path = ROOT / "dist" / "llama-win.zip"
    run(["curl", "-fSL", "-o", str(zip_path), url])
    dest.mkdir(exist_ok=True)
    with zipfile.ZipFile(zip_path) as z:
        z.extractall(dest)
    zip_path.unlink()
    if not (dest / "llama-server.exe").exists():
        sys.exit("runtime 解包后缺 llama-server.exe（llama.cpp 资产布局变化？）")
    tag_file.write_text(LLAMA_TAG + "\n", encoding="utf-8")
    print(f"runtime 就绪: {dest}（llama.cpp {LLAMA_TAG}）")


def main():
    ap = argparse.ArgumentParser(description="OpenKnowledge 一键构建")
    ap.add_argument("--skip-installer", action="store_true", help="只构建 dist/，不打包安装程序")
    ap.add_argument("--skip-winres", action="store_true", help="跳过 exe 图标/版本信息嵌入")
    ap.add_argument("--test", action="store_true", help="测试安装包：版本号追加 _test，不改 iss 文件")
    args = ap.parse_args()
    version = app_version(test=args.test)

    # 1. exe 图标与版本信息（go-winres）：缺失或失败即中断——静默跳过会让发布的
    #    exe 无 VS_VERSION_INFO 版本资源（M-16）；确需跳过须显式 --skip-winres
    if not args.skip_winres:
        winres = shutil.which("go-winres") or str(Path(os.environ.get("GOPATH", Path.home() / "go")) / "bin" / "go-winres.exe")
        if not (shutil.which("go-winres") or Path(winres).exists()):
            sys.exit("未找到 go-winres，无法生成 exe 图标/版本资源\n"
                     "  安装: go install github.com/tc-hib/go-winres@latest\n"
                     "  确需跳过: --skip-winres")
        for pkg in ("ok", "okd", "okmanager"):
            run([winres, "make", "--in", "winres.json"], cwd=ROOT / "cmd" / pkg)

    # 2. 编译 dist/ 三 exe + 拷贝 web/（注入版本号，与 build-dist.sh 一致）
    ldflags = f"{LDFLAGS} -X openknowledge/internal/version.Version={version}"
    (ROOT / "dist").mkdir(exist_ok=True)
    run(["go", "build", "-ldflags", ldflags, "-o", "dist/ok.exe", "./cmd/ok"])
    run(["go", "build", "-ldflags", ldflags, "-o", "dist/okd.exe", "./cmd/okd"])
    run(["go", "build", "-ldflags", ldflags, "-o", "dist/OkManager.exe", "./cmd/okmanager"])
    web_dist = ROOT / "dist" / "web"
    if web_dist.exists():
        shutil.rmtree(web_dist)
    shutil.copytree(ROOT / "web", web_dist)
    # changelogs 随包分发（GUI 更新日志弹窗的数据源；iss 打的是 dist\changelogs）——
    # 与 build-dist.sh 保持一致，漏拷会导致安装包内更新日志陈旧
    cl_dist = ROOT / "dist" / "changelogs"
    if cl_dist.exists():
        shutil.rmtree(cl_dist)
    shutil.copytree(ROOT / "docs" / "changelogs", cl_dist)
    prepare_runtime()
    print("dist/ built: ok.exe + okd.exe + OkManager.exe + web/ + changelogs/")

    # 3. Inno Setup 打包
    if not args.skip_installer:
        if not Path(ISCC).exists():
            sys.exit(f"未找到 ISCC: {ISCC}（可用环境变量 ISCC 覆盖，或 --skip-installer）")
        (ROOT / "installer" / "output").mkdir(parents=True, exist_ok=True)
        if args.test:
            # 测试包不改 openknowledge.iss：生成同目录临时脚本（相对 Source 路径按
            # 脚本所在目录解析，必须同目录），替换 AppVersion 后编译，编完即删。
            src = (ROOT / "installer" / "openknowledge.iss").read_text(encoding="utf-8")
            tmp_iss = ROOT / "installer" / ".openknowledge-test.iss"
            tmp_iss.write_text(re.sub(r'^#define AppVersion "[^"]+"',
                                      f'#define AppVersion "{version}"',
                                      src, count=1, flags=re.MULTILINE), encoding="utf-8")
            try:
                run([ISCC, "/Q", str(tmp_iss)])
            finally:
                tmp_iss.unlink(missing_ok=True)
        else:
            run([ISCC, "/Q", "installer/openknowledge.iss"])
        for f in sorted((ROOT / "installer" / "output").glob("*.exe")):
            print(f"安装程序: {f}  ({f.stat().st_size / 1024 / 1024:.1f} MB)")


if __name__ == "__main__":
    main()
