#!/usr/bin/env bash
# MoltenVK smoke on Apple Silicon — runs Vulkan-backed tests (requires `CUZK_VK_SKIP_SMOKE=0`).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

# Homebrew: Vulkan loader (`libvulkan.dylib`) + MoltenVK ICD. Login shells usually have
# `/opt/homebrew/bin` on PATH; CI and minimal shells may not — ash loads the loader by SONAME.
if [[ -d /opt/homebrew ]]; then
  export PATH="/opt/homebrew/bin:${PATH}"
  export DYLD_FALLBACK_LIBRARY_PATH="/opt/homebrew/lib:${DYLD_FALLBACK_LIBRARY_PATH:-}"
  if [[ -z "${VK_ICD_FILENAMES:-}" && -f /opt/homebrew/opt/molten-vk/etc/vulkan/icd.d/MoltenVK_icd.json ]]; then
    export VK_ICD_FILENAMES="/opt/homebrew/opt/molten-vk/etc/vulkan/icd.d/MoltenVK_icd.json"
  fi
fi

# Default to attempting Vulkan; set CUZK_VK_SKIP_SMOKE=1 to skip if no ICD.
# Optional: CUZK_VK_PARTITION_MAX_LOG (default 6, max 14) — larger Fr NTT round-trip in prove_groth16_partition.
# Optional: CUZK_VK_PIPELINE_CACHE=/path/file — load/save VkPipelineCache across processes (see VulkanDevice in device.rs).
# Optional: CUZK_VK_RECORD_BENCH_CSV=1 — append partition timings to ../../benchmarks/cuzk-vk/results-apple-m2-smoke-<stamp>.csv
#   and (unless CUZK_VK_BENCH_HARDWARE_MD is set) a companion .md hardware paragraph log with the same <stamp>.
# Optional: CUZK_VK_BENCH_MAX_*_MS — fail smoke if a stage exceeds the ceiling (see bench_csv module in cuzk-vk).
export CUZK_VK_SKIP_SMOKE="${CUZK_VK_SKIP_SMOKE:-0}"

CURIO="$(cd "$ROOT/../.." && pwd)"
if [[ -n "${CUZK_VK_BENCH_CSV:-}" ]]; then
  :
elif [[ "${CUZK_VK_RECORD_BENCH_CSV:-}" == 1 ]]; then
  _bench_stamp="$(date +%Y%m%d-%H%M%S)"
  export CUZK_VK_BENCH_CSV="$CURIO/benchmarks/cuzk-vk/results-apple-m2-smoke-$_bench_stamp.csv"
  if [[ -z "${CUZK_VK_BENCH_HARDWARE_MD:-}" ]]; then
    export CUZK_VK_BENCH_HARDWARE_MD="$CURIO/benchmarks/cuzk-vk/results-apple-m2-smoke-$_bench_stamp.md"
  fi
fi

echo "== cuzk-vk smoke (Milestone A + B₀ + B₂: SRS MSM, H GPU/CPU, full 255-bit Pippenger MSM; ICD required) =="
cargo test -p cuzk-vk --test device_smoke
cargo test -p cuzk-vk --test toy_ntt_gpu
cargo test -p cuzk-vk --test pipeline_cache_disk
cargo test -p cuzk-vk --test g1_reverse_gpu
cargo test -p cuzk-vk --test g2_reverse_gpu
cargo test -p cuzk-vk --test fp_add_gpu
cargo test -p cuzk-vk --test fp_sub_gpu
cargo test -p cuzk-vk --test fp_mul_gpu
cargo test -p cuzk-vk --test fp2_add_gpu
cargo test -p cuzk-vk --test fp2_sub_gpu
cargo test -p cuzk-vk --test fp2_mul_gpu
cargo test -p cuzk-vk --test fp2_sqr_gpu
cargo test -p cuzk-vk --test g1_jacobian_add_gpu
cargo test -p cuzk-vk --test g1_batch_accum_gpu
cargo test -p cuzk-vk --test g2_batch_accum_gpu
cargo test -p cuzk-vk --test g1_xyzz_add_gpu
cargo test -p cuzk-vk --test g2_jacobian_add_gpu
cargo test -p cuzk-vk --test g2_xyzz_add_gpu
cargo test -p cuzk-vk --test fr_add_gpu
cargo test -p cuzk-vk --test fr_mul_gpu
cargo test -p cuzk-vk --test fr_sub_gpu
cargo test -p cuzk-vk --test fr_coeff_wise_mult_gpu
cargo test -p cuzk-vk --test fr_sub_mult_constant_gpu
cargo test -p cuzk-vk --test fr_ntt8_gpu
cargo test -p cuzk-vk --test fr_ntt_general_gpu
cargo test -p cuzk-vk --test fr_ntt_radix4_gpu
cargo test -p cuzk-vk --test fr_ntt_radix8_gpu
cargo test -p cuzk-vk --test msm_dispatch_grid_gpu
cargo test -p cuzk-vk --test g1_bucket_window4_gpu
cargo test -p cuzk-vk --test g1_pippenger_full_width_gpu
cargo test -p cuzk-vk --test g1_mega_strip_circuit_hit_gpu
cargo test -p cuzk-vk --test srs_dual_queue_echo_gpu
cargo test -p cuzk-vk --test split_msm_g1_bitsplit_gpu
cargo test -p cuzk-vk --test split_msm_scalar_trunc_gpu
cargo test -p cuzk-vk --test srs_layout
cargo test -p cuzk-vk --test srs_file_ic
cargo test -p cuzk-vk --test srs_staging_device_local_gpu
cargo test -p cuzk-vk --test spec_constant_smoke_gpu
cargo test -p cuzk-vk --test split_msm_multiexp_cpu
cargo test -p cuzk-vk --test fr_coset_fft_cpu
cargo test -p cuzk-vk --test fr_coset_fft_gpu
cargo test -p cuzk-vk --test srs_g1_gpu_reverse24
cargo test -p cuzk-vk --test srs_g2_gpu_reverse48
cargo test -p cuzk-vk --test fr_ntt_plan_bounds
cargo test -p cuzk-vk --test h_term_cpu
cargo test -p cuzk-vk --test h_term_quotient_bellperson
cargo test -p cuzk-vk --test h_term_quotient_gpu
cargo test -p cuzk-vk --test groth16_verify_tiny
cargo test -p cuzk-vk --test milestone_b_bellperson_vulkan_smoke
cargo test -p cuzk-vk --test srs_async_read
cargo test -p bellperson-vk-smoke
cargo test -p cuzk-vk prove_partition_smoke_matches_roundtrip

echo "apple-m2-vulkan-smoke: OK"
