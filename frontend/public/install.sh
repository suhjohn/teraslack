#!/usr/bin/env sh
set -eu

INSTALL_ROOT="${TERASLACK_INSTALL_ROOT:-$HOME/.teraslack}"
BIN_DIR="$INSTALL_ROOT/bin"
CONFIG_FILE="$INSTALL_ROOT/config.json"
LAUNCHER_PATH="$BIN_DIR/teraslack-stdio-mcp"
BINARY_PATH="$BIN_DIR/teraslack-stdio-mcp-bin"
API_BASE_URL="${TERASLACK_INSTALL_API_URL:-${TERASLACK_API_BASE_URL:-https://api.teraslack.ai}}"
DOWNLOAD_BASE_URL="${TERASLACK_DOWNLOAD_BASE_URL:-https://downloads.teraslack.ai/teraslack/stdio-mcp}"
MANIFEST_URL="${TERASLACK_STDIN_MANIFEST_URL:-$DOWNLOAD_BASE_URL/latest.json}"

TMP_DIR=""

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

write_launcher() {
  mkdir -p "$BIN_DIR"
  cat >"$LAUNCHER_PATH" <<'SH'
#!/usr/bin/env sh
set -eu

INSTALL_ROOT="${TERASLACK_INSTALL_ROOT:-$HOME/.teraslack}"
CONFIG_FILE="${TERASLACK_CONFIG_FILE:-$INSTALL_ROOT/config.json}"
BINARY_PATH="$INSTALL_ROOT/bin/teraslack-stdio-mcp-bin"

[ -f "$CONFIG_FILE" ] || {
  printf 'error: missing Teraslack config at %s\n' "$CONFIG_FILE" >&2
  exit 1
}

[ -x "$BINARY_PATH" ] || {
  printf 'error: missing Teraslack MCP binary at %s\n' "$BINARY_PATH" >&2
  exit 1
}

eval "$(
  python3 - "$CONFIG_FILE" <<'PY'
import json
import shlex
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    data = json.load(fh)

for env_name, key in (
    ("TERASLACK_BASE_URL", "base_url"),
    ("TERASLACK_API_KEY", "api_key"),
    ("TERASLACK_DEFAULT_CONVERSATION_ID", "default_conversation_id"),
):
    value = data.get(key, "")
    if value:
        print(f'export {env_name}={shlex.quote(str(value))}')
PY
)"

exec "$BINARY_PATH"
SH
  chmod 755 "$LAUNCHER_PATH"
}

build_binary() {
  TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/teraslack-install.XXXXXX")
  platform=$(detect_platform)
  archive_path="$TMP_DIR/teraslack-stdio-mcp.tar.gz"
  extract_dir="$TMP_DIR/extract"

  log "Resolving Teraslack MCP binary for $platform..."
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

print(f"VERSION={shlex.quote(str(data.get('version', 'unknown')))}")
print(f"ARTIFACT_URL={shlex.quote(str(artifact.get('url', '')))}")
print(f"ARTIFACT_SHA256={shlex.quote(str(artifact.get('sha256', '')))}")
PY
)"

  [ "${ERROR:-}" = "" ] || fail "no prebuilt MCP binary is available for platform $platform"
  [ -n "${ARTIFACT_URL:-}" ] || fail "manifest did not include an artifact URL for $platform"
  [ -n "${ARTIFACT_SHA256:-}" ] || fail "manifest did not include a SHA256 for $platform"

  log "Downloading Teraslack MCP $VERSION for $platform..."
  curl -fsSL "$ARTIFACT_URL" -o "$archive_path"
  verify_sha256 "$archive_path" "$ARTIFACT_SHA256"

  mkdir -p "$BIN_DIR"
  mkdir -p "$extract_dir"
  tar -xzf "$archive_path" -C "$extract_dir"

  downloaded_binary="$extract_dir/teraslack-stdio-mcp-bin"
  [ -f "$downloaded_binary" ] || fail "downloaded archive did not contain teraslack-stdio-mcp-bin"
  mv "$downloaded_binary" "$BINARY_PATH"
  chmod 755 "$BINARY_PATH"
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
    fail "SHA256 mismatch for downloaded MCP binary"
  fi
}

install_codex_instructions() {
  instructions_file="$HOME/.codex/AGENTS.md"
  marker="<!-- teraslack -->"

  mkdir -p "$(dirname "$instructions_file")"
  touch "$instructions_file"

  tmp_file=$(mktemp "${TMPDIR:-/tmp}/teraslack-codex.XXXXXX")
  awk -v marker="$marker" '
    BEGIN { skip = 0 }
    $0 == marker { skip = 1; next }
    skip && $0 == "<!-- /teraslack -->" { skip = 0; next }
    !skip { print }
  ' "$instructions_file" > "$tmp_file"
  mv "$tmp_file" "$instructions_file"

  if [ -s "$instructions_file" ]; then
    printf '\n' >> "$instructions_file"
  fi

  cat >>"$instructions_file" <<'MD'
<!-- teraslack -->
## Teraslack

- A local Teraslack MCP server named `teraslack` is installed on this machine.
- Call `whoami` first before sending messages so you can confirm the active workspace and user.
- Use `create_dm` or `set_default_conversation` before `send_message`, `list_messages`, or `wait_for_message`.
- Use `api_request` only when the dedicated Teraslack tools do not cover the endpoint you need.
<!-- /teraslack -->
MD
}

install_claude_instructions() {
  instructions_file="$HOME/.claude/CLAUDE.md"
  marker="<!-- teraslack -->"

  mkdir -p "$(dirname "$instructions_file")"
  touch "$instructions_file"

  tmp_file=$(mktemp "${TMPDIR:-/tmp}/teraslack-claude.XXXXXX")
  awk -v marker="$marker" '
    BEGIN { skip = 0 }
    $0 == marker { skip = 1; next }
    skip && $0 == "<!-- /teraslack -->" { skip = 0; next }
    !skip { print }
  ' "$instructions_file" > "$tmp_file"
  mv "$tmp_file" "$instructions_file"

  if [ -s "$instructions_file" ]; then
    printf '\n' >> "$instructions_file"
  fi

  cat >>"$instructions_file" <<'MD'
<!-- teraslack -->
## Teraslack

- A local Teraslack MCP server named `teraslack` is installed on this machine.
- Call `whoami` first before sending messages so you can confirm the active workspace and user.
- Use `create_dm` or `set_default_conversation` before `send_message`, `list_messages`, or `wait_for_message`.
- Use `api_request` only when the dedicated Teraslack tools do not cover the endpoint you need.
<!-- /teraslack -->
MD
}

register_codex() {
  if ! command -v codex >/dev/null 2>&1; then
    log "Codex not found, skipping Codex MCP registration."
    return
  fi

  log "Registering Teraslack MCP with Codex..."
  codex mcp remove teraslack >/dev/null 2>&1 || true
  codex mcp add teraslack -- "$LAUNCHER_PATH" >/dev/null
  install_codex_instructions
}

register_claude() {
  if ! command -v claude >/dev/null 2>&1; then
    log "Claude Code not found, skipping Claude MCP registration."
    return
  fi

  log "Registering Teraslack MCP with Claude Code..."
  claude mcp remove -s user teraslack >/dev/null 2>&1 || true
  claude mcp add -s user --transport stdio teraslack -- "$LAUNCHER_PATH" >/dev/null
  install_claude_instructions
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
  require_command tar

  device_name=$(detect_device_name)

  log "Creating Teraslack install session..."
  create_payload=$(python3 - "$device_name" <<'PY'
import json
import sys
print(json.dumps({
    "client_kind": "local_mcp",
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
  write_launcher
  register_codex
  register_claude

  log ""
  log "Teraslack MCP installed."
  log "Config: $CONFIG_FILE"
  log "Launcher: $LAUNCHER_PATH"
}

main "$@"
