"""Copy licenses for runtime Go modules; no private or runtime files are read."""
import json
import os
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parent.parent
go = os.environ.get("GO", "go")
decoder = json.JSONDecoder()
modules = {}
for platform in ("linux", "windows"):
    env = dict(os.environ, GOOS=platform, GOARCH="amd64", CGO_ENABLED="0")
    raw = subprocess.check_output([go, "list", "-deps", "-json", "./cmd/sshdesk"], cwd=root, env=env, text=True)
    while raw.strip():
        obj, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        module = obj.get("Module", {})
        if module and not module.get("Main"):
            modules[module["Path"]] = module
lines = ["# Third-party notices", "", "Runtime Go module license copies. See go.mod/go.sum for pinned versions.", "", "| Module | Version | License files |", "|---|---|---|"]
for name, module in sorted(modules.items()):
    slug = name.replace("/", "_")
    target = root / "licenses" / slug
    target.mkdir(parents=True, exist_ok=True)
    files = []
    for source in sorted(Path(module["Dir"]).iterdir()):
        if source.is_file() and source.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "AUTHORS")):
            (target / source.name).write_bytes(source.read_bytes())
            files.append(f"[{source.name}](licenses/{slug}/{source.name})")
    if not files:
        raise SystemExit(f"No license found for {name}")
    lines.append(f"| {name} | {module['Version']} | {', '.join(files)} |")
goroot = Path(subprocess.check_output([go, "env", "GOROOT"], text=True).strip())
(root / "licenses" / "Go-LICENSE").write_bytes((goroot / "LICENSE").read_bytes())
lines += ["", "The Go runtime and standard library use the [Go BSD license](licenses/Go-LICENSE).", "", "xterm.js and addon-fit MIT licenses, upstream package URLs and SHA256 hashes are in [web/static/vendor](web/static/vendor).", "", "SQLite core is public domain. Consult the modernc SQLite license for the Go port and associated notices.", ""]
(root / "THIRD_PARTY_NOTICES.md").write_text("\n".join(lines), encoding="utf-8")
print(f"Collected licenses for {len(modules)} runtime modules")
