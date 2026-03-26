#!/bin/sh
set -eu

role="${APP_ROLE:-server}"

case "$role" in
  server)
    exec /app/server
    ;;
  external-event-projector)
    exec /app/external-event-projector
    ;;
  webhook-producer)
    exec /app/webhook-producer
    ;;
  webhook-worker)
    exec /app/webhook-worker
    ;;
  indexer)
    exec /app/indexer
    ;;
  mcp-server)
    exec /app/teraslack-mcp-server
    ;;
  *)
    echo "unknown APP_ROLE: $role" >&2
    exit 1
    ;;
esac
