#!/usr/bin/env bash
# Run cuzk workspace tests that are safe on a laptop without CUDA or Vulkan ICD.
#
# Usage (from anywhere):
#   bash /path/to/extern/cuzk/scripts/run-all-tests.sh
# Or from extern/cuzk:
#   bash scripts/run-all-tests.sh
#
# Environment:
#   CUZK_VK_SKIP_SMOKE=1   default (skip Vulkan integration tests); set to 0 to require loader + ICD.
#   CUZK_RUN_DAEMON=1      also run `cargo test -p cuzk-daemon` (needs CUDA toolchain + default features).
#   CUZK_RUN_BELLPERSON=1 also run bellperson mimc with vulkan-cuzk (from extern/bellperson).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CUZK_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${CUZK_ROOT}"

export CUZK_VK_SKIP_SMOKE="${CUZK_VK_SKIP_SMOKE:-1}"

echo "== cuzk workspace: ${CUZK_ROOT} =="

run() {
  echo ""
  echo ">> $*"
  "$@"
}

run cargo test -p cuzk-pce
run cargo test -p cuzk-core --no-default-features
run cargo test -p cuzk-server
run cargo test -p cuzk-bench
# Single invocation: lib tests (Fr/toy NTT) + integration (device_smoke, field_shader, toy_ntt GPU when ICD present).
run cargo test -p cuzk-vk

if [[ "${CUZK_RUN_DAEMON:-}" == "1" ]]; then
  run cargo test -p cuzk-daemon
else
  echo ""
  echo ">> (skip cuzk-daemon — set CUZK_RUN_DAEMON=1 to include; default build needs CUDA)"
fi

if [[ "${CUZK_RUN_BELLPERSON:-}" == "1" ]]; then
  BP_ROOT="$(cd "${CUZK_ROOT}/../bellperson" && pwd)"
  run bash -c "cd '${BP_ROOT}' && cargo test --no-default-features --features groth16,vulkan-cuzk --test mimc"
else
  echo ""
  echo ">> (skip bellperson mimc — set CUZK_RUN_BELLPERSON=1 to include)"
fi

echo ""
echo "All selected cuzk gates passed."
