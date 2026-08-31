#!/usr/bin/env python3
"""一次性：从 logo-deploy.svg 渲染 logo-deploy.png（256，Edge headless 透明底）。
管线与 make_logo.py 相同；主 logo 请用 make_logo.py，本脚本只管 okdeploy 变体。"""
import subprocess
import sys
import tempfile
from pathlib import Path

OUT = Path(__file__).resolve().parent
EDGE = Path(r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe")


def main() -> None:
    if not EDGE.exists():
        sys.exit(f"Edge 不在: {EDGE}")
    svg = OUT / "logo-deploy.svg"
    dest = OUT / "logo-deploy.png"
    html = (
        '<!DOCTYPE html><html><head><meta charset="utf-8"><style>'
        "html,body{margin:0;padding:0;background:transparent}svg{display:block}"
        "</style></head><body>"
        + svg.read_text(encoding="utf-8").replace("<svg ", '<svg width="256" height="256" ', 1)
        + "</body></html>"
    )
    with tempfile.NamedTemporaryFile("w", suffix=".html", delete=False, encoding="utf-8") as f:
        f.write(html)
        page = f.name
    subprocess.run(
        [str(EDGE), "--headless", "--disable-gpu", "--default-background-color=00000000",
         f"--screenshot={dest}", "--window-size=256,256", f"file:///{Path(page).as_posix()}"],
        check=True, capture_output=True,
    )
    print("written:", dest)


if __name__ == "__main__":
    main()
