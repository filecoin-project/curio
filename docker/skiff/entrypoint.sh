#!/usr/bin/env bash
set -euo pipefail

# Skiff entrypoint.
#
# 1. Wait until the Yugabyte (Postgres-wire) endpoint accepts connections.
# 2. Hand off to the skiff binary, which auto-seeds its `base` config layer,
#    runs DB migrations, scans $SKIFF_DATA (/data) for writable storage, and
#    serves the PDP API + admin GUI.

DB_HOST="${CURIO_DB_HOST:-${CURIO_HARMONYDB_HOSTS:-yugabyte}}"
DB_PORT="${CURIO_DB_PORT:-${CURIO_HARMONYDB_PORT:-5433}}"
DB_WAIT_RETRIES="${DB_WAIT_RETRIES:-120}"

# Use the first host if a comma-separated list is provided.
DB_HOST="${DB_HOST%%,*}"

echo "Waiting for Yugabyte at ${DB_HOST}:${DB_PORT} ..."
tries=0
until (exec 3<>"/dev/tcp/${DB_HOST}/${DB_PORT}") 2>/dev/null; do
  tries=$((tries + 1))
  if [[ "$tries" -ge "$DB_WAIT_RETRIES" ]]; then
    echo "Yugabyte not reachable at ${DB_HOST}:${DB_PORT} after ${DB_WAIT_RETRIES} attempts" >&2
    echo "Hint: Yugabyte must be started with --advertise_address=yugabyte (and hostname: yugabyte) for cross-container access." >&2
    exit 1
  fi
  if (( tries % 15 == 0 )); then
    echo "Still waiting for Yugabyte (${tries}/${DB_WAIT_RETRIES})..."
  fi
  sleep 2
done
exec 3>&- 2>/dev/null || true
echo "Yugabyte is reachable."

echo "CURIO_REPO_PATH=${CURIO_REPO_PATH:-} SKIFF_DATA=${SKIFF_DATA:-/data}"
export GOLOG_LOG_LEVEL="${GOLOG_LOG_LEVEL:-info}"
export GOLOG_LOG_FMT="${GOLOG_LOG_FMT:-stderr}"
echo "Starting skiff ..."
exec skiff "$@"
