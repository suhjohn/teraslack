#!/usr/bin/env sh
set -eu

VERSION="${1:-${VERSION:-}}"
if [ -z "$VERSION" ]; then
  echo "usage: VERSION=v0.1.0 scripts/upload-cli-release.sh [version]" >&2
  exit 1
fi

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
OUTPUT_DIR="${OUTPUT_DIR:-$ROOT_DIR/dist/cli-release}"
VERSION_DIR="$OUTPUT_DIR/$VERSION"
LATEST_FILE="$OUTPUT_DIR/latest.json"
S3_DOWNLOADS_BUCKET="${S3_DOWNLOADS_BUCKET:-}"
S3_DOWNLOADS_PREFIX="${S3_DOWNLOADS_PREFIX:-teraslack/cli}"
S3_DOWNLOADS_ACCOUNT_ID="${S3_DOWNLOADS_ACCOUNT_ID:-}"
S3_DOWNLOADS_ENDPOINT="${S3_DOWNLOADS_ENDPOINT:-}"
S3_DOWNLOADS_ACCESS_KEY_ID="${S3_DOWNLOADS_ACCESS_KEY_ID:-${AWS_ACCESS_KEY_ID:-}}"
S3_DOWNLOADS_SECRET_ACCESS_KEY="${S3_DOWNLOADS_SECRET_ACCESS_KEY:-${AWS_SECRET_ACCESS_KEY:-}}"
S3_DOWNLOADS_REGION="${S3_DOWNLOADS_REGION:-auto}"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_env() {
  name="$1"
  value="$2"
  if [ -z "$value" ]; then
    echo "missing required env: $name" >&2
    exit 1
  fi
}

if [ -z "$S3_DOWNLOADS_ENDPOINT" ] && [ -n "$S3_DOWNLOADS_ACCOUNT_ID" ]; then
  S3_DOWNLOADS_ENDPOINT="https://${S3_DOWNLOADS_ACCOUNT_ID}.r2.cloudflarestorage.com"
fi

require_command aws
require_env S3_DOWNLOADS_BUCKET "$S3_DOWNLOADS_BUCKET"
require_env S3_DOWNLOADS_ENDPOINT "$S3_DOWNLOADS_ENDPOINT"
require_env S3_DOWNLOADS_ACCESS_KEY_ID "$S3_DOWNLOADS_ACCESS_KEY_ID"
require_env S3_DOWNLOADS_SECRET_ACCESS_KEY "$S3_DOWNLOADS_SECRET_ACCESS_KEY"

[ -d "$VERSION_DIR" ] || {
  echo "missing release directory: $VERSION_DIR" >&2
  echo "run: make build-cli-release VERSION=$VERSION" >&2
  exit 1
}

[ -f "$LATEST_FILE" ] || {
  echo "missing latest manifest: $LATEST_FILE" >&2
  echo "run: make build-cli-release VERSION=$VERSION" >&2
  exit 1
}

aws_s3_cp() {
  src="$1"
  dst="$2"
  content_type="$3"
  cache_control="$4"

  AWS_ACCESS_KEY_ID="$S3_DOWNLOADS_ACCESS_KEY_ID" \
  AWS_SECRET_ACCESS_KEY="$S3_DOWNLOADS_SECRET_ACCESS_KEY" \
  AWS_DEFAULT_REGION="$S3_DOWNLOADS_REGION" \
  AWS_EC2_METADATA_DISABLED=true \
    aws s3 cp "$src" "$dst" \
      --endpoint-url "$S3_DOWNLOADS_ENDPOINT" \
      --content-type "$content_type" \
      --cache-control "$cache_control"
}

echo "uploading latest manifest to s3://$S3_DOWNLOADS_BUCKET/$S3_DOWNLOADS_PREFIX/latest.json"
aws_s3_cp \
  "$LATEST_FILE" \
  "s3://$S3_DOWNLOADS_BUCKET/$S3_DOWNLOADS_PREFIX/latest.json" \
  "application/json" \
  "no-cache"

echo "uploading checksum file to s3://$S3_DOWNLOADS_BUCKET/$S3_DOWNLOADS_PREFIX/$VERSION/SHA256SUMS"
aws_s3_cp \
  "$VERSION_DIR/SHA256SUMS" \
  "s3://$S3_DOWNLOADS_BUCKET/$S3_DOWNLOADS_PREFIX/$VERSION/SHA256SUMS" \
  "text/plain; charset=utf-8" \
  "no-cache"

for platform_dir in "$VERSION_DIR"/*; do
  [ -d "$platform_dir" ] || continue
  platform=$(basename "$platform_dir")

  archive=""
  content_type=""
  if [ -f "$platform_dir/teraslack.tar.gz" ]; then
    archive="$platform_dir/teraslack.tar.gz"
    content_type="application/gzip"
  elif [ -f "$platform_dir/teraslack.zip" ]; then
    archive="$platform_dir/teraslack.zip"
    content_type="application/zip"
  else
    continue
  fi

  archive_name=$(basename "$archive")
  echo "uploading $platform archive to s3://$S3_DOWNLOADS_BUCKET/$S3_DOWNLOADS_PREFIX/$VERSION/$platform/$archive_name"
  aws_s3_cp \
    "$archive" \
    "s3://$S3_DOWNLOADS_BUCKET/$S3_DOWNLOADS_PREFIX/$VERSION/$platform/$archive_name" \
    "$content_type" \
    "public, max-age=31536000, immutable"
done

echo "upload complete"
