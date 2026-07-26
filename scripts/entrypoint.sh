#!/bin/sh
set -e

echo "[pgwarden] Starting pgwarden..."

if [ $# -gt 0 ]; then
  exec /app/pgwarden "$@"
fi

exec /app/pgwarden serve
