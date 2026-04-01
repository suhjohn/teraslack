#!/usr/bin/env sh
set -eu

INSTALL_ROOT="${TERASLACK_INSTALL_ROOT:-$HOME/.teraslack}"
BIN_DIR="$INSTALL_ROOT/bin"
CONFIG_FILE="$INSTALL_ROOT/config.json"
API_BASE_URL="${TERASLACK_INSTALL_API_URL:-${TERASLACK_API_BASE_URL:-https://api.teraslack.ai}}"
DOWNLOAD_BASE_URL="${TERASLACK_DOWNLOAD_BASE_URL:-https://downloads.teraslack.ai/teraslack/cli}"
MANIFEST_URL="${TERASLACK_CLI_MANIFEST_URL:-$DOWNLOAD_BASE_URL/latest.json}"

TMP_DIR=""
INSTALLED_BINARY_PATH=""
INSTALLED_BINARY_NAME=""

cleanup() {
  if [ -n "${TMP_DIR:-}" ] && [ -d "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}

trap cleanup EXIT INT TERM

log() {
  printf '%s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "missing required command: $1"
  fi
}

write_config() {
  mkdir -p "$INSTALL_ROOT"
  umask 077
  python3 - "$CONFIG_FILE" "$API_BASE_URL" <<'PY'
import json
import os
import sys
from pathlib import Path

path = Path(sys.argv[1])
base_url = sys.argv[2]
payload = {}

if path.exists():
    try:
        payload = json.loads(path.read_text())
    except Exception:
        payload = {}

payload["base_url"] = base_url
path.write_text(json.dumps(payload, indent=2) + "\n")
os.chmod(path, 0o600)
PY
}

detect_platform() {
  os=$(uname -s 2>/dev/null || true)
  arch=$(uname -m 2>/dev/null || true)

  case "$os" in
    Darwin)
      case "$arch" in
        arm64|aarch64) printf '%s' "darwin-arm64" ;;
        x86_64|amd64) printf '%s' "darwin-amd64" ;;
        *) fail "unsupported macOS architecture: $arch" ;;
      esac
      ;;
    Linux)
      case "$arch" in
        x86_64|amd64) printf '%s' "linux-amd64" ;;
        arm64|aarch64) printf '%s' "linux-arm64" ;;
        *) fail "unsupported Linux architecture: $arch" ;;
      esac
      ;;
    MINGW*|MSYS*|CYGWIN*)
      fail "Windows uses the native PowerShell installer: powershell -ExecutionPolicy Bypass -c \"irm https://teraslack.ai/install.ps1 | iex\""
      ;;
    *)
      fail "unsupported operating system: $os"
      ;;
  esac
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

  fail "missing SHA256 tool (need one of: shasum, sha256sum, openssl)"
}

verify_sha256() {
  file_path="$1"
  expected="$2"
  actual=$(sha256_file "$file_path")
  if [ "$actual" != "$expected" ]; then
    fail "SHA256 mismatch for downloaded CLI binary"
  fi
}

manifest_artifact_shell_vars() {
  platform="$1"
  manifest_json=$(cat)
  if [ -z "$manifest_json" ]; then
    fail "installer received an empty release manifest"
  fi
  python3 - "$manifest_json" "$platform" <<'PY'
import json
import shlex
import sys

data = json.loads(sys.argv[1])
platform = sys.argv[2]
artifact = data.get("artifacts", {}).get(platform)
if not artifact:
    print("ERROR=missing_artifact")
    raise SystemExit(0)

binary_name = artifact.get("binary_name")
if not binary_name:
    binary_name = "teraslack.exe" if platform.startswith("windows-") else "teraslack"

print(f"VERSION={shlex.quote(str(data.get('version', 'unknown')))}")
print(f"ARTIFACT_URL={shlex.quote(str(artifact.get('url', '')))}")
print(f"ARTIFACT_SHA256={shlex.quote(str(artifact.get('sha256', '')))}")
print(f"ARTIFACT_BINARY_NAME={shlex.quote(str(binary_name))}")
PY
}

extract_archive() {
  archive_path="$1"
  extract_dir="$2"
  python3 - "$archive_path" "$extract_dir" <<'PY'
import sys
import tarfile
import zipfile
from pathlib import Path

archive = Path(sys.argv[1])
dest = Path(sys.argv[2])
dest.mkdir(parents=True, exist_ok=True)

name = archive.name.lower()
if name.endswith(".zip"):
    with zipfile.ZipFile(archive) as zf:
        zf.extractall(dest)
elif name.endswith(".tar.gz") or name.endswith(".tgz"):
    with tarfile.open(archive, "r:gz") as tf:
        tf.extractall(dest)
else:
    raise SystemExit(f"unsupported archive format: {archive.name}")
PY
}

build_binary() {
  TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/teraslack-install.XXXXXX")
  platform=$(detect_platform)
  extract_dir="$TMP_DIR/extract"

  log "Resolving Teraslack CLI binary for $platform..."
  manifest=$(curl -fsSL "$MANIFEST_URL")
  eval "$(printf '%s' "$manifest" | manifest_artifact_shell_vars "$platform")"

  [ "${ERROR:-}" = "" ] || fail "no prebuilt CLI binary is available for platform $platform"
  [ -n "${ARTIFACT_URL:-}" ] || fail "manifest did not include an artifact URL for $platform"
  [ -n "${ARTIFACT_SHA256:-}" ] || fail "manifest did not include a SHA256 for $platform"
  [ -n "${ARTIFACT_BINARY_NAME:-}" ] || fail "manifest did not include a binary name for $platform"

  archive_path="$TMP_DIR/$(basename "$ARTIFACT_URL")"

  log "Downloading Teraslack CLI $VERSION for $platform..."
  curl -fsSL "$ARTIFACT_URL" -o "$archive_path"
  verify_sha256 "$archive_path" "$ARTIFACT_SHA256"

  mkdir -p "$BIN_DIR" "$extract_dir"
  extract_archive "$archive_path" "$extract_dir"

  downloaded_binary="$extract_dir/$ARTIFACT_BINARY_NAME"
  [ -f "$downloaded_binary" ] || fail "downloaded archive did not contain $ARTIFACT_BINARY_NAME"

  INSTALLED_BINARY_PATH="$BIN_DIR/$ARTIFACT_BINARY_NAME"
  INSTALLED_BINARY_NAME="$ARTIFACT_BINARY_NAME"
  mv "$downloaded_binary" "$INSTALLED_BINARY_PATH"

  case "$platform" in
    windows-*) ;;
    *) chmod 755 "$INSTALLED_BINARY_PATH" ;;
  esac
}

ensure_path() {
  case ":$PATH:" in
    *":$BIN_DIR:"*) return 0 ;;
  esac

  profile_file=""
  if [ -n "${ZDOTDIR:-}" ] && [ -f "${ZDOTDIR}/.zprofile" ]; then
    profile_file="${ZDOTDIR}/.zprofile"
  elif [ -f "$HOME/.zprofile" ]; then
    profile_file="$HOME/.zprofile"
  elif [ -f "$HOME/.bash_profile" ]; then
    profile_file="$HOME/.bash_profile"
  else
    profile_file="$HOME/.profile"
  fi

  mkdir -p "$(dirname "$profile_file")"
  touch "$profile_file"

  if ! grep -Fq "$BIN_DIR" "$profile_file"; then
    {
      printf '\n'
      printf '# Added by Teraslack installer\n'
      printf 'export PATH="%s:$PATH"\n' "$BIN_DIR"
    } >> "$profile_file"
    log "Added $BIN_DIR to PATH in $profile_file"
  fi
}

main() {
  require_command curl
  require_command python3

  write_config
  build_binary
  ensure_path

  log ""
  log "Teraslack CLI installed."
  log "Config: $CONFIG_FILE"
  log "Binary: $INSTALLED_BINARY_PATH"
  log ""
  log "Open a new shell and run one of:"
  log "  teraslack signin email --email you@example.com --name \"Your Name\""
  log "  teraslack signin google"
  log "  teraslack signin github"
}

main "$@"
