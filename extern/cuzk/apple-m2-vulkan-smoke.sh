#!/usr/bin/env bash
# MoltenVK smoke on Apple Silicon — runs Vulkan-backed tests (requires `CUZK_VK_SKIP_SMOKE=0`).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

# Default to attempting Vulkan; set CUZK_VK_SKIP_SMOKE=1 to skip if no ICD.
export CUZK_VK_SKIP_SMOKE="${CUZK_VK_SKIP_SMOKE:-0}"

echo "== cuzk-vk (device_smoke + toy NTT + G1 reverse GPU when ICD present) =="
cargo test -p cuzk-vk --test device_smoke
cargo test -p cuzk-vk --test toy_ntt_gpu
cargo test -p cuzk-vk --test g1_reverse_gpu
cargo test -p cuzk-vk --test g2_reverse_gpu
cargo test -p cuzk-vk --test fr_add_gpu
cargo test -p cuzk-vk --test fr_mul_gpu
cargo test -p cuzk-vk --test fr_sub_gpu
cargo test -p cuzk-vk --test fr_ntt8_gpu
cargo test -p cuzk-vk --test msm_dispatch_grid_gpu
cargo test -p cuzk-vk prove_partition_smoke_matches_roundtrip

echo "apple-m2-vulkan-smoke: OK"
