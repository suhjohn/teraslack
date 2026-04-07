#!/usr/bin/env sh
set -eu

BUMP="${1:-${BUMP:-}}"
if [ -z "$BUMP" ]; then
  echo "usage: scripts/next-cli-version.sh <patch|minor|major>" >&2
  exit 1
fi

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
OUTPUT_DIR="${OUTPUT_DIR:-$ROOT_DIR/dist/cli-release}"
LATEST_FILE="$OUTPUT_DIR/latest.json"

current_version=""
if [ -f "$LATEST_FILE" ]; then
  current_version=$(
    python3 - "$LATEST_FILE" <<'PY'
import json
import sys
from pathlib import Path

data = json.loads(Path(sys.argv[1]).read_text())
version = str(data.get("version", "")).strip()
if version:
    print(version)
PY
  )
fi

if [ -z "$current_version" ]; then
  current_version=$(
    python3 - "$OUTPUT_DIR" <<'PY'
import re
import sys
from pathlib import Path

root = Path(sys.argv[1])
pattern = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
versions = []

if root.exists():
    for child in root.iterdir():
        if not child.is_dir():
            continue
        match = pattern.match(child.name)
        if match:
            versions.append(tuple(int(part) for part in match.groups()))

if versions:
    major, minor, patch = max(versions)
    print(f"v{major}.{minor}.{patch}")
PY
  )
fi

python3 - "$BUMP" "$current_version" <<'PY'
import re
import sys

bump = sys.argv[1]
current = sys.argv[2].strip()
pattern = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")

if current:
    match = pattern.match(current)
    if not match:
        print(f"invalid current version: {current}", file=sys.stderr)
        raise SystemExit(1)
    major, minor, patch = (int(part) for part in match.groups())
else:
    major = minor = patch = 0

if bump == "patch":
    patch += 1
elif bump == "minor":
    minor += 1
    patch = 0
elif bump == "major":
    major += 1
    minor = 0
    patch = 0
else:
    print(f"invalid bump: {bump}", file=sys.stderr)
    raise SystemExit(1)

print(f"v{major}.{minor}.{patch}")
PY
