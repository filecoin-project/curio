#!/usr/bin/env bash
# Run Synapse SDK tests for the Curio PDP HTTP API contract (SP client + pull).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REF_FILE="${ROOT}/scripts/ci/synapse-sdk.ref"
SYNAPSE_SDK_REF="${SYNAPSE_SDK_REF:-$(tr -d '[:space:]' < "$REF_FILE")}"
SYNAPSE_SDK_DIR="${SYNAPSE_SDK_DIR:-${ROOT}/.synapse-sdk}"

if [[ ! -d "${SYNAPSE_SDK_DIR}/.git" ]]; then
	echo "Cloning synapse-sdk (${SYNAPSE_SDK_REF}) into ${SYNAPSE_SDK_DIR}"
	git clone --depth 1 --branch "${SYNAPSE_SDK_REF}" https://github.com/FilOzone/synapse-sdk.git "${SYNAPSE_SDK_DIR}"
else
	echo "Updating synapse-sdk checkout in ${SYNAPSE_SDK_DIR}"
	git -C "${SYNAPSE_SDK_DIR}" fetch --depth 1 origin "${SYNAPSE_SDK_REF}"
	git -C "${SYNAPSE_SDK_DIR}" checkout -B ci-synapse-test "origin/${SYNAPSE_SDK_REF}"
fi

cd "${SYNAPSE_SDK_DIR}"

if ! command -v pnpm >/dev/null 2>&1; then
	echo "pnpm is required (enable via corepack or actions/setup-node)" >&2
	exit 1
fi

# synapse-sdk does not commit pnpm-lock.yaml; cached CI checkouts can carry a
# stale lockfile that no longer matches workspace manifests (e.g. docs/).
rm -f pnpm-lock.yaml
pnpm install --no-frozen-lockfile --filter @filoz/synapse-core...
pnpm -r --filter @filoz/synapse-core run generate-abi
pnpm -r --filter @filoz/synapse-core run build

cd packages/synapse-core
pnpm exec playwright-test test/sp.test.ts test/pull.test.ts --mode node
