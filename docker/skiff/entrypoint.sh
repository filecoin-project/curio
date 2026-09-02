#!/usr/bin/env bash
set -euo pipefail

# Curio-PDP (skiff) entrypoint.
#
# 1. Wait until the Yugabyte (Postgres-wire) endpoint accepts connections.
# 2. If FULLNODE_API_INFO is unset, wait for the bundled Forest token and build it.
# 3. Hand off to the skiff binary, which auto-seeds its `base` config layer,
#    runs DB migrations, and serves the PDP API + admin GUI.
#    Storage paths under $SKIFF_DATA (/data) are selected via the admin GUI.

DB_HOST="${CURIO_DB_HOST:-${CURIO_HARMONYDB_HOSTS:-yugabyte}}"
DB_PORT="${CURIO_DB_PORT:-${CURIO_HARMONYDB_PORT:-5433}}"
DB_WAIT_RETRIES="${DB_WAIT_RETRIES:-120}"
FOREST_WAIT_RETRIES="${FOREST_WAIT_RETRIES:-180}"

# Use the first host if a comma-separated list is provided.
DB_HOST="${DB_HOST%%,*}"

wait_tcp() {
  local host="$1"
  local port="$2"
  local retries="$3"
  local label="$4"
  local tries=0
  until (exec 3<>"/dev/tcp/${host}/${port}") 2>/dev/null; do
    tries=$((tries + 1))
    if [[ "$tries" -ge "$retries" ]]; then
      echo "${label} not reachable at ${host}:${port} after ${retries} attempts" >&2
      return 1
    fi
    if (( tries % 15 == 0 )); then
      echo "Still waiting for ${label} (${tries}/${retries})..."
    fi
    sleep 2
  done
  exec 3>&- 2>/dev/null || true
  echo "${label} is reachable."
}

echo "Waiting for Yugabyte at ${DB_HOST}:${DB_PORT} ..."
if ! wait_tcp "${DB_HOST}" "${DB_PORT}" "${DB_WAIT_RETRIES}" "Yugabyte"; then
  echo "Hint: Yugabyte must be started with --advertise_address=yugabyte (and hostname: yugabyte) for cross-container access." >&2
  exit 1
fi

if [[ -z "${FULLNODE_API_INFO:-}" ]]; then
  FOREST_HOST="${FOREST_HOST:-forest}"
  FOREST_RPC_PORT="${FOREST_RPC_PORT:-2345}"
  FOREST_TOKEN_FILE="${FOREST_TOKEN_FILE:-/forest-token/token}"

  echo "FULLNODE_API_INFO unset; waiting for bundled Forest at ${FOREST_HOST}:${FOREST_RPC_PORT} ..."
  if ! wait_tcp "${FOREST_HOST}" "${FOREST_RPC_PORT}" "${FOREST_WAIT_RETRIES}" "Forest"; then
    echo "Hint: set FULLNODE_API_INFO to an external Lotus/Forest endpoint, or ensure the forest service is healthy." >&2
    exit 1
  fi

  tries=0
  until [[ -s "${FOREST_TOKEN_FILE}" ]]; do
    tries=$((tries + 1))
    if [[ "$tries" -ge "$FOREST_WAIT_RETRIES" ]]; then
      echo "Forest token not found at ${FOREST_TOKEN_FILE} after ${FOREST_WAIT_RETRIES} attempts" >&2
      exit 1
    fi
    if (( tries % 15 == 0 )); then
      echo "Still waiting for Forest token (${tries}/${FOREST_WAIT_RETRIES})..."
    fi
    sleep 2
  done

  TOKEN="$(tr -d '[:space:]' < "${FOREST_TOKEN_FILE}")"
  if [[ -z "${TOKEN}" ]]; then
    echo "Forest token file ${FOREST_TOKEN_FILE} is empty" >&2
    exit 1
  fi
  # Compose uses the service DNS name "forest". /ip4/<name> is not a valid
  # multiaddr and fails with: unknown url scheme ''.
  forest_ma="/dns4/${FOREST_HOST}/tcp/${FOREST_RPC_PORT}/http"
  if [[ "${FOREST_HOST}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    forest_ma="/ip4/${FOREST_HOST}/tcp/${FOREST_RPC_PORT}/http"
  elif [[ "${FOREST_HOST}" == *:* ]]; then
    forest_ma="/ip6/${FOREST_HOST}/tcp/${FOREST_RPC_PORT}/http"
  fi
  export FULLNODE_API_INFO="${TOKEN}:${forest_ma}"
  echo "Using bundled Forest for FULLNODE_API_INFO (${forest_ma})."
else
  echo "Using operator-provided FULLNODE_API_INFO."
fi

echo "CURIO_REPO_PATH=${CURIO_REPO_PATH:-} SKIFF_DATA=${SKIFF_DATA:-/data}"
export GOLOG_LOG_LEVEL="${GOLOG_LOG_LEVEL:-info}"
export GOLOG_LOG_FMT="${GOLOG_LOG_FMT:-stderr}"
echo "Starting skiff ..."
exec skiff "$@"
