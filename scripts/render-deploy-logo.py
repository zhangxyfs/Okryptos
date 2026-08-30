import subprocess
import tempfile
from pathlib import Path

svg = Path("installer/assets/logo-deploy.svg").read_text(encoding="utf-8")
svg = svg.replace("<svg ", '<svg width="256" height="256" ', 1)
html = (
    '<!DOCTYPE html><html><head><meta charset="utf-8"><style>'
    "html,body{margin:0;padding:0;background:transparent}svg{display:block}"
    "</style></head><body>" + svg + "</body></html>"
)
f = tempfile.NamedTemporaryFile("w", suffix=".html", delete=False, encoding="utf-8")
f.write(html)
f.close()
edge = r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
url = "file:///" + f.name.replace("\\", "/")
subprocess.run(
    [edge, "--headless", "--disable-gpu", "--default-background-color=00000000",
     "--screenshot=installer/assets/logo-deploy.png", "--window-size=256,256", url],
    check=True,
)
print("rendered")
