"""Scan staged blobs before publication; prints filenames/rule names, never matches."""
import re
import getpass
import subprocess
import sys
from pathlib import PurePosixPath

def git(*args):
    return subprocess.check_output(["git", *args])

paths = git("diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z").decode().split("\0")
patterns = {
    "private key material": rb"-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----",
    "GitHub credential": rb"(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})",
    "AWS access key": rb"(?:AKIA|ASIA)[A-Z0-9]{16}",
    "local account name": re.escape(getpass.getuser().encode()),
}
blocked = []
for name in filter(None, paths):
    path = PurePosixPath(name)
    if any(p in {".tools", ".test-data", ".ssh", "secrets", "exports", "backups", "keys", "local", "dist"} for p in path.parts) or re.search(r"(?:\.pem|\.key|\.ppk|\.p12|\.pfx|\.db(?:-.*)?|\.sqlite.*|\.json|\.exe)$", name, re.IGNORECASE) or path.name.startswith(".env"):
        blocked.append((name, "sensitive/runtime file path"))
    content = git("show", ":" + name)
    for rule, pattern in patterns.items():
        # The scanner's own patterns are not credentials or a user-specific data file.
        if name == "scripts/check_secrets.py":
            continue
        if re.search(pattern, content, re.IGNORECASE):
            blocked.append((name, rule))
for name, rule in blocked:
    print(f"BLOCKED: {name}: {rule}")
if blocked:
    sys.exit(1)
print(f"Checked {len(list(filter(None, paths)))} staged files: no matching sensitive material.")
