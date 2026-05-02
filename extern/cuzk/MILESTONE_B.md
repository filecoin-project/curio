# Milestone B — status (`extern/cuzk`)

Roadmap context: repo root [`cuzk-vulkan-optimization-roadmap.md`](../../cuzk-vulkan-optimization-roadmap.md) **§3.3** / **§3.4**.

## B₁ — integration & I/O (**complete**)

These acceptance items are satisfied in-tree; CI defaults keep Vulkan optional (`CUZK_VK_SKIP_SMOKE=1`).

| Criterion | Where |
|-----------|--------|
| Optional `bellperson` ↔ `cuzk-vk` link (`vulkan-cuzk` feature) | [`bellperson/src/groth16/vulkan_cuzk.rs`](../bellperson/src/groth16/vulkan_cuzk.rs) |
| Compile/link gate workspace | [`bellperson-vk-smoke/`](bellperson-vk-smoke/), `scripts/run-all-tests.sh` |
| Groth16 **prove + verify** (pairing), then Vulkan partition smoke on same machine | [`cuzk-vk/tests/milestone_b_bellperson_vulkan_smoke.rs`](cuzk-vk/tests/milestone_b_bellperson_vulkan_smoke.rs) (`CUZK_VK_SKIP_SMOKE=0`) |
| `VkPipelineCache` + optional disk warm | [`cuzk-vk/src/device.rs`](cuzk-vk/src/device.rs), `CUZK_VK_PIPELINE_CACHE` |
| SRS file read overlap (host) | [`cuzk-vk/src/srs.rs`](cuzk-vk/src/srs.rs) `srs_read_file_spawn`, tests |
| SRS decode + G1/G2 GPU limb staging smoke | [`cuzk-vk/src/srs_gpu.rs`](cuzk-vk/src/srs_gpu.rs), `prove_groth16_partition` |
| §1 partition CSV / ceilings / hardware MD hooks | [`cuzk-vk/src/bench_csv.rs`](cuzk-vk/src/bench_csv.rs), [`prover.rs`](cuzk-vk/src/prover.rs) |
| **B₂ hook (Fr NTT leg):** optional `witness_ntt_coeffs` on [`VkGroth16Job`](cuzk-vk/src/prover.rs) | [`tests/witness_ntt_partition_gpu.rs`](cuzk-vk/tests/witness_ntt_partition_gpu.rs), [`milestone_b_bellperson_vulkan_smoke.rs`](cuzk-vk/tests/milestone_b_bellperson_vulkan_smoke.rs) |

**B₁ does not** route bellperson R1CS witness values through Vulkan kernels end-to-end; it proves coexistence and smoke coverage for the integration path. **`witness_ntt_coeffs`** is an explicit **B₂** entry point for Fr data on the GPU NTT round-trip only.

## B₂ — proving parity (**not complete** — performance / semantics tier)

This tier matches supraseal-scale **proving through** Vulkan (full-width scalars, bucket accumulate, production SRS hot path, optional pairing on a **Vulkan-emitted** Groth16 proof). The following work is **still missing** (ordered roughly as in roadmap **§3.4** / **§8**):

1. **§8.1 MSM (A.\*)** — Full **255-bit** `Scalar` window MSM: bucket-accumulate compute shaders, GPU Pippenger / multi-exp tail, signed buckets / GLV / mixed coords where listed; host [`device_profile`](cuzk-vk/src/device_profile.rs) is **hints only** today. Partition smoke uses **truncated** Scalars (`g1_msm_bitplanes_scalars_trunc_gpu_host`), not full-width buckets.
2. **§8.2 NTT (B.\*)** — GPU radix-4/8 butterflies, subgroup shuffle stages, coset+first-stage fusion (**B.5**), single-shot small-*n* (**B.6**); [`FrNttPlan::fused_layer_count`](cuzk-vk/src/ntt.rs) is schedule preview only.
3. **§8.3 C.2 remainder** — Overlap SRS **GPU** upload submit with first Fr NTT (second queue or pipelined command buffers); no verification download on hot path. [`srs_staging_device_local_upload`](cuzk-vk/src/srs_staging_gpu.rs) exists without folding into `prove_groth16_partition` timing path by default.
4. **§8.3 C.3** — `constant_id` / specialization on Fr NTT tail and full MSM shaders where **naga** GLSL is insufficient (prebuilt SPIR-V or alternate front-end).
5. **§8.4 D.\*** — Mega-dispatch MSM shader + cross-circuit SRS window sharing (host [`msm_mega_dense_groups_x`](cuzk-vk/src/msm.rs) only).
6. **Witness → Vulkan** — **Partial:** [`VkGroth16Job::witness_ntt_coeffs`](cuzk-vk/src/prover.rs) feeds caller `Scalar` vectors into the partition **Fr NTT general GPU round-trip** when `CUZK_VK_SKIP_SMOKE=0` ([`tests/witness_ntt_partition_gpu.rs`](cuzk-vk/tests/witness_ntt_partition_gpu.rs)). **Still open:** bellperson `ProvingAssignment` / domain wiring, A/B/H assignment buffers into the full prove path (not only this NTT leg).
7. **Vulkan-native Groth16 proof + pairing** — A proof object produced entirely by the Vulkan prover path, then verified with the same pairing engine as bellperson (today: bellperson proves; Vulkan runs orthogonal smoke).
8. **§1 CI baselines** — Optional committed timing rows / subgroup automation from real GPU runners (template: [`benchmarks/cuzk-vk/HARDWARE_MATRIX.md`](../../benchmarks/cuzk-vk/HARDWARE_MATRIX.md)).

Closing **B₂** is intentionally **multi-release**; track slices in `cuzk-vulkan-optimization-roadmap.md` **§8.6–8.7** and **§3.4**.
