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

detect_device_name() {
  if command -v scutil >/dev/null 2>&1; then
    name=$(scutil --get ComputerName 2>/dev/null || true)
    if [ -n "${name:-}" ]; then
      printf '%s' "$name"
      return
    fi
  fi

  name=$(hostname 2>/dev/null || true)
  if [ -n "${name:-}" ]; then
    printf '%s' "$name"
    return
  fi

  printf '%s' "local-machine"
}

open_browser() {
  url="$1"
  if command -v open >/dev/null 2>&1; then
    open "$url" >/dev/null 2>&1 && return 0
  fi
  if command -v xdg-open >/dev/null 2>&1; then
    xdg-open "$url" >/dev/null 2>&1 && return 0
  fi
  return 1
}

json_shell_vars() {
  python3 - "$@" <<'PY'
import json
import shlex
import sys

keys = sys.argv[1:]
data = json.load(sys.stdin)

for key in keys:
    value = data.get(key, "")
    if value is None:
        value = ""
    print(f'{key.upper()}={shlex.quote(str(value))}')
PY
}

write_config() {
  mkdir -p "$INSTALL_ROOT"
  umask 077
  python3 - "$CONFIG_FILE" "$BASE_URL" "$WORKSPACE_ID" "$USER_ID" "$API_KEY" <<'PY'
import json
import os
import sys
from pathlib import Path

path = Path(sys.argv[1])
payload = {
    "base_url": sys.argv[2],
    "workspace_id": sys.argv[3],
    "user_id": sys.argv[4],
    "api_key": sys.argv[5],
}
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
  eval "$(printf '%s' "$manifest" | python3 - "$platform" <<'PY'
import json
import shlex
import sys

platform = sys.argv[1]
data = json.load(sys.stdin)
artifact = data.get("artifacts", {}).get(platform)
if not artifact:
    print("ERROR=missing_artifact")
    sys.exit(0)

binary_name = artifact.get("binary_name")
if not binary_name:
    binary_name = "teraslack.exe" if platform.startswith("windows-") else "teraslack"

print(f"VERSION={shlex.quote(str(data.get('version', 'unknown')))}")
print(f"ARTIFACT_URL={shlex.quote(str(artifact.get('url', '')))}")
print(f"ARTIFACT_SHA256={shlex.quote(str(artifact.get('sha256', '')))}")
print(f"ARTIFACT_BINARY_NAME={shlex.quote(str(binary_name))}")
PY
)"

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

poll_install_session() {
  install_id="$1"
  poll_token="$2"
  while :; do
    poll_payload=$(python3 - "$poll_token" <<'PY'
import json
import sys
print(json.dumps({"poll_token": sys.argv[1]}))
PY
)
    poll_response=$(curl -fsSL -X POST "$API_BASE_URL/cli/install/$install_id/poll" \
      -H "Content-Type: application/json" \
      -d "$poll_payload")

    eval "$(printf '%s' "$poll_response" | json_shell_vars status base_url workspace_id user_id api_key)"

    case "$STATUS" in
      pending)
        sleep 2
        ;;
      approved)
        export BASE_URL WORKSPACE_ID USER_ID API_KEY
        return 0
        ;;
      expired)
        fail "install session expired before approval completed"
        ;;
      cancelled)
        fail "install session was cancelled"
        ;;
      consumed)
        fail "install credential was already claimed"
        ;;
      *)
        fail "unexpected install session status: $STATUS"
        ;;
    esac
  done
}

main() {
  require_command curl
  require_command python3

  device_name=$(detect_device_name)

  log "Creating Teraslack install session..."
  create_payload=$(python3 - "$device_name" <<'PY'
import json
import sys
print(json.dumps({
    "client_kind": "local_cli",
    "device_name": sys.argv[1],
}))
PY
)

  create_response=$(curl -fsSL -X POST "$API_BASE_URL/cli/install/sessions" \
    -H "Content-Type: application/json" \
    -d "$create_payload")

  eval "$(printf '%s' "$create_response" | json_shell_vars install_id approval_url poll_token expires_at)"

  log "Opening browser for Teraslack login and approval..."
  if ! open_browser "$APPROVAL_URL"; then
    log "Open this URL in your browser to continue:"
    log "  $APPROVAL_URL"
  fi

  log "Waiting for approval..."
  poll_install_session "$INSTALL_ID" "$POLL_TOKEN"

  write_config
  build_binary
  ensure_path

  log ""
  log "Teraslack CLI installed."
  log "Config: $CONFIG_FILE"
  log "Binary: $INSTALLED_BINARY_PATH"
  log ""
  log "Open a new shell and run:"
  log "  ${INSTALLED_BINARY_NAME} auth get-me"
}

main "$@"
