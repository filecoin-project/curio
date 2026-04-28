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
export CUZK_VK_SKIP_SMOKE="${CUZK_VK_SKIP_SMOKE:-0}"

echo "== cuzk-vk Milestone A smoke (device + fields + EC + Fr NTT + coset + SRS + H-term; ICD required) =="
cargo test -p cuzk-vk --test device_smoke
cargo test -p cuzk-vk --test toy_ntt_gpu
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
cargo test -p cuzk-vk --test msm_dispatch_grid_gpu
cargo test -p cuzk-vk --test split_msm_g1_bitsplit_gpu
cargo test -p cuzk-vk --test srs_layout
cargo test -p cuzk-vk --test srs_file_ic
cargo test -p cuzk-vk --test split_msm_multiexp_cpu
cargo test -p cuzk-vk --test fr_coset_fft_cpu
cargo test -p cuzk-vk --test fr_coset_fft_gpu
cargo test -p cuzk-vk --test srs_g1_gpu_reverse24
cargo test -p cuzk-vk --test fr_ntt_plan_bounds
cargo test -p cuzk-vk --test h_term_cpu
cargo test -p cuzk-vk --test h_term_quotient_bellperson
cargo test -p cuzk-vk --test h_term_quotient_gpu
cargo test -p cuzk-vk --test groth16_verify_tiny
cargo test -p bellperson-vk-smoke
cargo test -p cuzk-vk prove_partition_smoke_matches_roundtrip

echo "apple-m2-vulkan-smoke: OK"
