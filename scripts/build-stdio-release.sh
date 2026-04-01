#!/usr/bin/env sh
set -eu

VERSION="${1:-${VERSION:-}}"
if [ -z "$VERSION" ]; then
  echo "usage: VERSION=v0.1.0 scripts/build-stdio-release.sh [version]" >&2
  exit 1
fi

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
SERVER_DIR="$ROOT_DIR/server"
OUTPUT_DIR="${OUTPUT_DIR:-$ROOT_DIR/dist/stdio-release}"
DOWNLOAD_BASE_URL="${DOWNLOAD_BASE_URL:-https://downloads.teraslack.ai/teraslack/stdio-mcp}"
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/teraslack-stdio-release.XXXXXX")

cleanup() {
  rm -rf "$TMP_DIR"
}

trap cleanup EXIT INT TERM

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

sha256_file() {
  file_path="$1"

  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file_path" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file_path" | awk '{print $1}'
    return
  fi
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$file_path" | awk '{print $NF}'
    return
  fi

  echo "missing SHA256 tool (need one of: shasum, sha256sum, openssl)" >&2
  exit 1
}

require_command go
require_command tar
require_command python3

targets="
darwin arm64 darwin-arm64
darwin amd64 darwin-amd64
linux amd64 linux-amd64
linux arm64 linux-arm64
"

version_dir="$OUTPUT_DIR/$VERSION"
mkdir -p "$version_dir"
sha_file="$version_dir/SHA256SUMS"
: > "$sha_file"

manifest_entries="$TMP_DIR/manifest_entries.jsonl"
: > "$manifest_entries"

printf '%s\n' "$targets" | while read -r goos goarch platform; do
  [ -n "${goos:-}" ] || continue

  target_dir="$version_dir/$platform"
  build_dir="$TMP_DIR/$platform"
  mkdir -p "$target_dir" "$build_dir"

  binary_path="$build_dir/teraslack-stdio-mcp-bin"
  archive_path="$target_dir/teraslack-stdio-mcp.tar.gz"

  echo "building $platform..."
  (
    cd "$SERVER_DIR"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -o "$binary_path" ./cmd/teraslack-stdio-mcp
  )

  tar -czf "$archive_path" -C "$build_dir" teraslack-stdio-mcp-bin
  sha256=$(sha256_file "$archive_path")
  printf '%s  %s\n' "$sha256" "$platform/teraslack-stdio-mcp.tar.gz" >> "$sha_file"

  python3 - "$platform" "$DOWNLOAD_BASE_URL" "$VERSION" "$sha256" >> "$manifest_entries" <<'PY'
import json
import sys

platform, base_url, version, sha256 = sys.argv[1:5]
print(json.dumps({
    "platform": platform,
    "url": f"{base_url}/{version}/{platform}/teraslack-stdio-mcp.tar.gz",
    "sha256": sha256,
}))
PY
done

python3 - "$VERSION" "$manifest_entries" "$OUTPUT_DIR/latest.json" <<'PY'
import json
import sys
from pathlib import Path

version = sys.argv[1]
entries_path = Path(sys.argv[2])
output_path = Path(sys.argv[3])

artifacts = {}
for line in entries_path.read_text().splitlines():
    if not line.strip():
        continue
    entry = json.loads(line)
    artifacts[entry["platform"]] = {
        "url": entry["url"],
        "sha256": entry["sha256"],
    }

payload = {
    "version": version,
    "artifacts": artifacts,
}
output_path.parent.mkdir(parents=True, exist_ok=True)
output_path.write_text(json.dumps(payload, indent=2) + "\n")
PY

echo "release artifacts written to $OUTPUT_DIR"
echo "upload $version_dir/* and $OUTPUT_DIR/latest.json to your downloads bucket"
