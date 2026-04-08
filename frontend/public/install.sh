#!/usr/bin/env sh
set -eu

INSTALL_ROOT="${TERASLACK_INSTALL_ROOT:-$HOME/.teraslack}"
BIN_DIR="$INSTALL_ROOT/bin"
MCP_INSTALL_ROOT="$INSTALL_ROOT/mcp"
MCP_BIN_DIR="$MCP_INSTALL_ROOT/bin"
CONFIG_FILE="$INSTALL_ROOT/config.json"
API_BASE_URL="${TERASLACK_INSTALL_API_URL:-${TERASLACK_API_BASE_URL:-https://api.teraslack.ai}}"
CLI_DOWNLOAD_BASE_URL="${TERASLACK_CLI_DOWNLOAD_BASE_URL:-${TERASLACK_DOWNLOAD_BASE_URL:-https://downloads.teraslack.ai/teraslack/cli}}"
CLI_MANIFEST_URL="${TERASLACK_CLI_MANIFEST_URL:-$CLI_DOWNLOAD_BASE_URL/latest.json}"
MCP_DOWNLOAD_BASE_URL="${TERASLACK_MCP_DOWNLOAD_BASE_URL:-https://downloads.teraslack.ai/teraslack/mcp}"
MCP_MANIFEST_URL="${TERASLACK_MCP_MANIFEST_URL:-$MCP_DOWNLOAD_BASE_URL/latest.json}"

TMP_DIR=""
INSTALLED_CLI_BINARY_PATH=""
INSTALLED_MCP_BINARY_PATH=""
CLI_VERSION=""
MCP_VERSION=""

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
    fail "SHA256 mismatch for downloaded binary"
  fi
}

manifest_artifact_shell_vars() {
  prefix="$1"
  platform="$2"
  default_binary_name="$3"
  manifest_json=$(cat)
  if [ -z "$manifest_json" ]; then
    fail "installer received an empty release manifest"
  fi
  python3 - "$prefix" "$manifest_json" "$platform" "$default_binary_name" <<'PY'
import json
import shlex
import sys

prefix = sys.argv[1]
data = json.loads(sys.argv[2])
platform = sys.argv[3]
default_binary_name = sys.argv[4]
artifact = data.get("artifacts", {}).get(platform)
if not artifact:
    print(f"{prefix}_ERROR=missing_artifact")
    raise SystemExit(0)

binary_name = artifact.get("binary_name") or default_binary_name

print(f"{prefix}_VERSION={shlex.quote(str(data.get('version', 'unknown')))}")
print(f"{prefix}_ARTIFACT_URL={shlex.quote(str(artifact.get('url', '')))}")
print(f"{prefix}_ARTIFACT_SHA256={shlex.quote(str(artifact.get('sha256', '')))}")
print(f"{prefix}_ARTIFACT_BINARY_NAME={shlex.quote(str(binary_name))}")
PY
}

install_artifact() {
  label="$1"
  platform="$2"
  artifact_url="$3"
  artifact_sha256="$4"
  artifact_binary_name="$5"
  target_dir="$6"

  label_lower=$(printf '%s' "$label" | tr '[:upper:]' '[:lower:]')
  extract_dir="$TMP_DIR/extract-$label_lower"
  archive_path="$TMP_DIR/$label_lower-$(basename "$artifact_url")"

  printf '%s\n' "Downloading Teraslack $label for $platform..." >&2
  curl -fsSL "$artifact_url" -o "$archive_path"
  verify_sha256 "$archive_path" "$artifact_sha256"

  mkdir -p "$target_dir" "$extract_dir"
  extract_archive "$archive_path" "$extract_dir"

  downloaded_binary="$extract_dir/$artifact_binary_name"
  [ -f "$downloaded_binary" ] || fail "downloaded archive did not contain $artifact_binary_name"

  installed_binary_path="$target_dir/$artifact_binary_name"
  mv "$downloaded_binary" "$installed_binary_path"

  case "$platform" in
    windows-*) ;;
    *) chmod 755 "$installed_binary_path" ;;
  esac

  printf '%s\n' "$installed_binary_path"
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

build_binaries() {
  TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/teraslack-install.XXXXXX")
  platform=$(detect_platform)

  log "Resolving Teraslack CLI binary for $platform..."
  cli_manifest=$(curl -fsSL "$CLI_MANIFEST_URL")
  eval "$(printf '%s' "$cli_manifest" | manifest_artifact_shell_vars CLI "$platform" "teraslack")"

  log "Resolving Teraslack MCP binary for $platform..."
  mcp_manifest=$(curl -fsSL "$MCP_MANIFEST_URL")
  eval "$(printf '%s' "$mcp_manifest" | manifest_artifact_shell_vars MCP "$platform" "teraslack-mcp")"

  [ "${CLI_ERROR:-}" = "" ] || fail "no prebuilt CLI binary is available for platform $platform"
  [ "${MCP_ERROR:-}" = "" ] || fail "no prebuilt MCP binary is available for platform $platform"
  [ -n "${CLI_ARTIFACT_URL:-}" ] || fail "CLI manifest did not include an artifact URL for $platform"
  [ -n "${CLI_ARTIFACT_SHA256:-}" ] || fail "CLI manifest did not include a SHA256 for $platform"
  [ -n "${CLI_ARTIFACT_BINARY_NAME:-}" ] || fail "CLI manifest did not include a binary name for $platform"
  [ -n "${MCP_ARTIFACT_URL:-}" ] || fail "MCP manifest did not include an artifact URL for $platform"
  [ -n "${MCP_ARTIFACT_SHA256:-}" ] || fail "MCP manifest did not include a SHA256 for $platform"
  [ -n "${MCP_ARTIFACT_BINARY_NAME:-}" ] || fail "MCP manifest did not include a binary name for $platform"

  [ -n "${CLI_VERSION:-}" ] || fail "CLI manifest did not include a version"
  [ -n "${MCP_VERSION:-}" ] || fail "MCP manifest did not include a version"
  [ "$CLI_VERSION" = "$MCP_VERSION" ] || fail "CLI version $CLI_VERSION does not match MCP version $MCP_VERSION"

  INSTALLED_CLI_BINARY_PATH=$(install_artifact "CLI" "$platform" "$CLI_ARTIFACT_URL" "$CLI_ARTIFACT_SHA256" "$CLI_ARTIFACT_BINARY_NAME" "$BIN_DIR")
  INSTALLED_MCP_BINARY_PATH=$(install_artifact "MCP" "$platform" "$MCP_ARTIFACT_URL" "$MCP_ARTIFACT_SHA256" "$MCP_ARTIFACT_BINARY_NAME" "$MCP_BIN_DIR")
}

setup_integrations() {
  log "Configuring Codex and Claude integrations..."
  "$INSTALLED_CLI_BINARY_PATH" integrations install --cli-binary-path "$INSTALLED_CLI_BINARY_PATH" --mcp-binary-path "$INSTALLED_MCP_BINARY_PATH"
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
  build_binaries
  setup_integrations
  ensure_path

  log ""
  log "Teraslack CLI and MCP installed."
  log "Config: $CONFIG_FILE"
  log "CLI Binary: $INSTALLED_CLI_BINARY_PATH"
  log "MCP Binary: $INSTALLED_MCP_BINARY_PATH"
  log ""
  log "Open a new shell and run:"
  log "  teraslack signin email --email you@example.com"
  log ""
  log "Then you can verify connectivity with:"
  log "  teraslack health"
  log "  teraslack me"
}

main "$@"
