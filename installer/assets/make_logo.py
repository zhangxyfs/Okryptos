#!/usr/bin/env python3
"""从 logo.svg 再生成 OpenKnowledge 图标产物（logo.png 256 + logo.ico 多尺寸 + web/favicon.ico）。

设计（2026-08-26 方案 B 定稿）：蓝/墨双页书，左页镂空 O、右页镂空 K。
- logo.svg       主版（笔画 5），用于 48/256 渲染
- logo-small.svg 小尺寸加粗变体（笔画 9），用于 16/32 渲染

管线：Edge headless 透明背景渲染 PNG → 手工打包 ICO（PNG-in-ICO，逐尺寸用各自渲染图，
不用 256 重采样——小尺寸笔画会糊）。运行：python installer/assets/make_logo.py
"""
import struct
import subprocess
import sys
import tempfile
from pathlib import Path

OUT = Path(__file__).resolve().parent
EDGE = Path(r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe")

# (源 svg, 尺寸)
RENDER_PLAN = [
    ("logo-small.svg", 16),
    ("logo-small.svg", 32),
    ("logo.svg", 48),
    ("logo.svg", 256),
]


def render(svg: Path, size: int, dest: Path) -> None:
    html = (
        '<!DOCTYPE html><html><head><meta charset="utf-8"><style>'
        "html,body{margin:0;padding:0;background:transparent}svg{display:block}"
        "</style></head><body>"
        + svg.read_text(encoding="utf-8").replace("<svg ", f'<svg width="{size}" height="{size}" ', 1)
        + "</body></html>"
    )
    with tempfile.NamedTemporaryFile("w", suffix=".html", delete=False, encoding="utf-8") as f:
        f.write(html)
        page = f.name
    subprocess.run(
        [str(EDGE), "--headless", "--disable-gpu", "--default-background-color=00000000",
         f"--screenshot={dest}", f"--window-size={size},{size}", f"file:///{Path(page).as_posix()}"],
        check=True, capture_output=True,
    )


def pack_ico(dest: Path, imgs: list[tuple[Path, int]]) -> None:
    header = struct.pack("<HHH", 0, 1, len(imgs))
    offset = 6 + 16 * len(imgs)
    directory = data = b""
    for path, size in imgs:
        blob = path.read_bytes()
        w = 0 if size == 256 else size
        directory += struct.pack("<BBBBHHII", w, w, 0, 0, 1, 32, len(blob), offset)
        data += blob
        offset += len(blob)
    dest.write_bytes(header + directory + data)


def main() -> None:
    if not EDGE.exists():
        sys.exit(f"Edge 不在: {EDGE}")
    with tempfile.TemporaryDirectory() as td:
        imgs = []
        for svg_name, size in RENDER_PLAN:
            png = Path(td) / f"{size}.png"
            render(OUT / svg_name, size, png)
            imgs.append((png, size))
        (OUT / "logo.png").write_bytes(imgs[-1][0].read_bytes())
        pack_ico(OUT / "logo.ico", imgs)
        pack_ico(OUT.parent.parent / "web" / "favicon.ico", imgs)
    print("written: logo.png / logo.ico / web/favicon.ico")


if __name__ == "__main__":
    main()
