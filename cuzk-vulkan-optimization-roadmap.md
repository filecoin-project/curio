# CuZK Vulkan Performance Parity Roadmap

## 0. Goal

Bring `extern/cuzk/cuzk-vk` from the expected **0.5–0.7×** supraseal-c2 CUDA baseline to **~1.0×** on comparable AMD hardware (e.g. MI250X / RX 7900 XTX vs NVIDIA A100 reference numbers in the c2 optimization proposals).

**Parity definition:** match supraseal throughput on **AMD** stacks where supraseal cannot run; we do **not** target NVIDIA (CUDA remains canonical there).

---

## 1. Baseline measurement protocol

- **Hardware matrix:** document SKU, driver (`radv` vs AMDVLK), `vulkaninfo` subgroup properties, and the supraseal reference row from `c2-optimization-proposal-11.md` for the same logical workload.
- **Workloads:** 32 GiB PoRep C2 with `n_circuits ∈ {1, 10, 30}`; single WindowPoSt partition.
- **Metrics:** end-to-end s/proof; MSM stage s; NTT stage s; peak VRAM; optional `VK_KHR_calibrated_timestamps` deltas once wired in `cuzk-vk`.
- **Recording:** commit `benchmarks/cuzk-vk/results-<gpu>-<date>.csv` plus a one-paragraph summary per optimization batch. **Hooks:** set **`CUZK_VK_BENCH_CSV`** for per-stage ms rows; optional **`CUZK_VK_BENCH_HARDWARE_MD`** for a markdown hardware + timing section per run (see `benchmarks/cuzk-vk/README.md`).

---

## 2. Optimization categories (priority order)

Each item lists: **[estimated speedup vs prior state]**, **effort (S/M/L)**, **files**, **benchmark assertion to land first**, **risk + mitigation**.

### A. MSM (largest lever)

| ID | Item |
|----|------|
| A.1 | Per-vendor window-size auto-tuning — **[1.10×, S, `src/msm.rs` + `src/device_profile.rs`, assert `msm_g1_2pow22` under T_A1 on RX 7900 XTX, risk: low]** |
| A.2 | Signed bucket / wNAF-style halving — **[1.30×, M, `glsl/msm_bucket_acc.comp`, bucket count halves + ≥1.25× at 2²², risk: low]** |
| A.3 | GLV endomorphism on G1 — **[1.35–1.80×, L, `glsl/bls12_381_glv.glsl` + `src/msm.rs`, scalar split round-trip tests before bench, risk: med]** |
| A.4 | Mixed coordinates (xyzz buckets, Jacobian output) — **[1.10×, M, `glsl/bls12_381_g1_xyzz.glsl`, micro-bench −8% add latency, risk: low]** |
| A.5 | Split MSM (density-aware) port of `groth16_split_msm.cu` — **[1.15× PoRep density, M, `glsl/batch_addition.comp` + dispatcher, −10% MSM on MI250X batch=10, risk: low]** |
| A.6 | Async transfer queue + multi compute queues — **[1.10× large n, M, `src/device.rs`/`msm.rs`, ≥8% overlap vs serial, risk: med — validation layers in CI]** |
| A.7 | Subgroup cooperative bucket path (wave32 vs wave64 variants) — **[1.15×, M, `glsl/msm_bucket_acc.comp` + spec constants in `pipelines.rs`, risk: low]** |

### B. NTT

| ID | Item |
|----|------|
| B.1 | Radix-4 butterflies — **[1.30× @2²⁰, M, `glsl/ntt_radix4.comp` + `ntt.rs`, fwd+inv wall time ≤ T_B1, risk: low]** |
| B.2 | Radix-8 smallest stages — **[1.10×, S, `glsl/ntt_radix8.comp`, +8% micro-bench, risk: low]** |
| B.3 | Subgroup-shuffle inner stages — **[1.20×, M, all `ntt_radix*.comp`, 50% shared-mem traffic drop (RGP), risk: low]** |
| B.4 | SSBO twiddle tables (no on-the-fly trig) — **[1.10×, S, `src/ntt.rs`, twiddle prep time → 0 in trace, risk: low]** |
| B.5 | Coset first-stage fusion — **[1.05×, S, fuse `ntt_radix_step` + `ntt_coset_twiddle`, one fewer dispatch, risk: low]** |
| B.6 | Single-shot shared-mem NTT n≤2¹⁴ — **[1.50× small n, M, `glsl/ntt_single_shot.comp`, −40% latency @2¹⁴, risk: low]** |

### C. Memory & pipeline

| ID | Item |
|----|------|
| C.1 | Persistent `VkPipelineCache` on disk — **[first proof saves 30–90s compile, S, `pipelines.rs`, second process zero compile spike, risk: low]** |
| C.2 | Async SRS upload overlapping first NTT — **[hide 0.5–3s upload, M, `srs.rs` + `device.rs`, MSM start within 50ms of NTT start, risk: low]** |
| C.3 | Specialization constants for `log_n`, window, buckets — **[1.05–1.15×, M, all `.comp` + `pipelines.rs`, RDNA3 dispatch −5%, risk: low]** |
| C.4 | Push constants instead of UBO for dispatch params — **[1.05× record time, S, all `.comp`, −5% `vkCmdDispatch` preamble, risk: low]** |
| C.5 | Buffer pool hit-rate metrics — **[observability, S, `allocator.rs`, bench exposes hit %, risk: none]** |

### D. Multi-circuit batching

| ID | Item |
|----|------|
| D.1 | Mega-dispatch MSM across N circuits — **[1.40–1.80× batch≥4, L, `msm.rs` + `msm_bucket_acc.comp`, n=10 PoRep within 1.2× supraseal-10, risk: med — extend memory-budget tests]** |
| D.2 | Cross-circuit bucket sharing for SRS windows — **[1.10×, M, `msm.rs`, SRS read BW ÷N, risk: low]** |

### E. Vendor-specific

| ID | Item |
|----|------|
| E.1 | RDNA3 dual-issue VALU layout in field shaders — **[1.05–1.15×, M, GLSL ISA via RGA, dual-issue ratio ≥2× baseline, risk: low]** |
| E.2 | CDNA wave64 shader variant — **[1.05× MI250X, S, `build.rs` dual SPIR-V + `pipelines.rs` picker, +5% vs wave32, risk: low]** |
| E.3 | Apple M2 / MoltenVK — **[TBD, L, `device.rs` unified-memory path + extended `apple-m2-vulkan-smoke.sh`, risk: med]** |

---

## 3. Sequencing guidance

TDD per item: land the benchmark assertion first, watch it fail, implement, watch it pass. Re-profile every 2–3 items and reorder if the bottleneck shifts. **Consolidated speed-up tables and “what’s landed vs debt”** live in **§8** (use that section for write-ups and release notes).

### 3.1 Vulkan bring-up sequence (`extern/cuzk/cuzk-vk`)

| Step | Status | Notes |
|------|--------|-------|
| 1 | **Done** | `VulkanDevice`, `vk_oneshot`, CI gate `CUZK_VK_SKIP_SMOKE` in `scripts/run-all-tests.sh` |
| 2 | **Done** | Fr add / sub / mul (Montgomery CIOS mul), toy `u32` NTT |
| 3 | **Done** | Fr NTT **n = 8** ([`fr_ntt_gpu`](extern/cuzk/cuzk-vk/src/fr_ntt_gpu.rs)) and **general power-of-two n ≤ 2^14** ([`fr_ntt_general_gpu`](extern/cuzk/cuzk-vk/src/fr_ntt_general_gpu.rs)) vs CPU [`ntt.rs`](extern/cuzk/cuzk-vk/src/ntt.rs). **Coset forward on GPU** ([`fr_coset_gpu`](extern/cuzk/cuzk-vk/src/fr_coset_gpu.rs), `tests/fr_coset_fft_gpu.rs`) through the same *n* bound. **Perf-only** coset/NTT work: §8.2 **B.4–B.6** (twiddle fusion, radix-4/8, single-shot). |
| 4 | **Done** | G1 / G2 affine limb smoke (`g1_reverse24`, `g2_reverse48`) |
| 5 | **Done (grid)** | Host [`msm.rs`](extern/cuzk/cuzk-vk/src/msm.rs) + [`msm_dispatch_grid_smoke`](extern/cuzk/cuzk-vk/shaders/msm_dispatch_grid_smoke.comp) hit-count vs `vkCmdDispatch` (≤4096 threads); workgroup **X** = specialization **SpecId 0** ([`msm_gpu`](extern/cuzk/cuzk-vk/src/msm_gpu.rs)). **Bucket MSM / G1 EC** → §8.1. |
| 6 | **Done (B₀ + B₁ slice)** | [`prove_groth16_partition`](extern/cuzk/cuzk-vk/src/prover.rs): Fr NTT round-trip + MSM dispatch grid; with `CUZK_VK_SKIP_SMOKE=0`, **SRS-bound** G1 MSM ([`srs_decode_h_g1`](extern/cuzk/cuzk-vk/src/srs.rs) + [`split_msm::g1_msm_bitplanes_scalars_trunc_gpu_host`](extern/cuzk/cuzk-vk/src/split_msm.rs)) and **H** GPU vs CPU ([`h_term_gpu`](extern/cuzk/cuzk-vk/src/h_term_gpu.rs)). **B₂ parity backlog:** [`MILESTONE_B.md`](extern/cuzk/MILESTONE_B.md) (Vulkan-native proof, full-width MSM, witness path, C.2 overlap on hot path, …). |

### 3.2 SupraSeal-aligned port (CUDA → Vulkan)

**Goal:** Port local SupraSeal C2 CUDA sources under `extern/supraseal-c2/cuda/` (plus minimal curve/NTT primitives they depend on) to Vulkan compute in `extern/cuzk/cuzk-vk`, test-gated with `CUZK_VK_SKIP_SMOKE`, then apply Milestone B optimizations from §8 where they still apply.

**Phased checklist** (implementation order; see `extern/cuzk/cuzk-vk/shaders/PORTING_NOTES.md` for `naga` constraints):

1. **Preflight** — `fp.rs` (Fp Montgomery limbs), `PORTING_NOTES.md`.
2. **Fp / Fp2 on GPU** — `fp_*12_tail`, `fp2_*` shaders + drivers + tests vs `blstrs`.
3. **G1/G2 EC** — Jacobian + XYZZ shaders, `ec.rs`, tests vs group law.
4. **Fr pointwise** — `coeff_wise_mult`, `sub_mult_with_constant` (SupraSeal `groth16_ntt_h.cu`).
5. **General Fr NTT** — SSBO twiddle bases, multi-dispatch (`vk_oneshot` pass sequence); **coset forward** on GPU ([`fr_coset_gpu`](extern/cuzk/cuzk-vk/src/fr_coset_gpu.rs), `tests/fr_coset_fft_gpu.rs`) through `n = 2^14` (pointwise SSBO cap matches NTT max). **CPU** coset: `tests/fr_coset_fft_cpu.rs`. Single-SPV coset+first-NTT-stage fusion (B.5) still open.
6. **`batch_addition`** — G1/G2 cooperative-style reduction in shared memory. **Milestone A (slice done):** G1 ≤64 affines + 64-bit bitmap (`g1_batch_accum_bitmap1636`), G2 ≤16 affines (`g2_batch_accum_aff16_904`). **Milestone B:** full multi-bucket / `CHUNK_BITS=64` / per-SM CUDA layout.
7. **Split MSM orchestration** — bitmap + GPU batch + CPU tail Pippenger. **Milestone A (slice done):** [`split_msm`](extern/cuzk/cuzk-vk/src/split_msm.rs) G1 bit-plane MSM + **CPU** cross-check vs `G1Projective::multi_exp` (`tests/split_msm_multiexp_cpu.rs`). **Milestone B₂ (partial):** [`g1_msm_bitplanes_scalars_trunc_gpu_host`](extern/cuzk/cuzk-vk/src/split_msm.rs) + `tests/split_msm_scalar_trunc_gpu.rs`. **Still open:** full-width Scalar + CUDA-scale bitmap + GPU Pippenger tail.
8. **SRS** — mmap layout from `groth16_srs.cuh`, synthetic fixture tests. **Slice done:** [`srs`](extern/cuzk/cuzk-vk/src/srs.rs) VK + IC decode + `b_g2[]`, `srs_read_file`, **[`srs_read_file_spawn`](extern/cuzk/cuzk-vk/src/srs.rs)** (B₁ disk-read overlap), `tests/srs_file_ic.rs` / `tests/srs_async_read.rs`; [`srs_gpu`](extern/cuzk/cuzk-vk/src/srs_gpu.rs) G1 `g1_reverse24` + **G2** `g2_reverse48` Montgomery limb staging vs CPU (`tests/srs_g1_gpu_reverse24.rs`, `tests/srs_g2_gpu_reverse48.rs`). **Milestone B₂:** mmap + **GPU** upload overlap vs first NTT (§8.3 **C.2** remainder).
9. **H-term pipeline** — INTT/FFT/coset chain vs CPU reference. **Done (Milestone A):** [`h_term`](extern/cuzk/cuzk-vk/src/h_term.rs) `fr_quotient_scalars_from_abc`, [`h_term_gpu`](extern/cuzk/cuzk-vk/src/h_term_gpu.rs) uses [`fr_coset_gpu`](extern/cuzk/cuzk-vk/src/fr_coset_gpu.rs) for coset distribute + forward NTT and GPU tail distribute (`g^{-1}`); host vanishing `z_inv` between GPU stages; tests `h_term_quotient_gpu` (incl. domain 8192). **Milestone B:** optional B.5 single-dispatch coset fusion (§8.2).
10. **`prove_groth16_partition`** — real driver + Groth16 verify test. **B₀:** partition ties Fr NTT + MSM grid + SRS `h[]` MSM + GPU H ([`prover`](extern/cuzk/cuzk-vk/src/prover.rs)) when `CUZK_VK_SKIP_SMOKE=0`; **native** Groth16 prove+verify: [`groth16_verify_tiny`](extern/cuzk/cuzk-vk/tests/groth16_verify_tiny.rs). **B₁:** [`milestone_b_bellperson_vulkan_smoke`](extern/cuzk/cuzk-vk/tests/milestone_b_bellperson_vulkan_smoke.rs). **B₂:** full-width Scalar bucket MSM + Vulkan-emitted proof from bellperson witness + pairing on that proof — see [`MILESTONE_B.md`](extern/cuzk/MILESTONE_B.md).
11. **Bellperson `vulkan-cuzk`** — optional path dep on `cuzk-vk`, [`groth16::vulkan_cuzk`](extern/bellperson/src/groth16/vulkan_cuzk.rs) re-exports `VkProverContext` / `prove_groth16_partition`. **Milestone A (slice done):** workspace [`bellperson-vk-smoke`](extern/cuzk/bellperson-vk-smoke/) + `scripts/run-all-tests.sh`. **Milestone B₁ (complete):** [`tests/milestone_b_bellperson_vulkan_smoke.rs`](extern/cuzk/cuzk-vk/tests/milestone_b_bellperson_vulkan_smoke.rs) — bellperson **prove+verify** (pairing) then Vulkan partition smoke when `CUZK_VK_SKIP_SMOKE=0`. **Milestone B₂:** witness + MSM **through** Vulkan; optional `CUZK_RUN_BELLPERSON` CI expansion.
12. **Milestone B perf** — A.1/A.6/A.7, B.1/B.3/B.6, C.1/C.3, D.1 with benchmark assertions. **Milestone A anchor:** `tests/fr_ntt_plan_bounds.rs` (`FrNttPlan` bounds). **Milestone B:** CSV + threshold assertions per §1.

### 3.2.1 Milestone A closure (SupraSeal port — correctness slice)

| §3.2 step | Status | What “done” means here |
|-----------|--------|-------------------------|
| 1–5 | **Done** | Preflight through general Fr NTT + GPU coset forward + pointwise `n` aligned to `2^14`; CPU coset parity test. |
| 6–7 | **Slice done** | Bitmap batch Jacobian + split MSM GPU/CPU checks; full CUDA-scale MSM → **Milestone B**. |
| 8 | **B₁ slice done** | SRS file/layout + IC/`b_g2` decode + GPU G1+G2 limb staging + `srs_read_file_spawn`; **B₂:** GPU upload overlap / mmap hot path. |
| 9 | **Done** | H quotient CPU + GPU vs reference / bellperson; large-domain GPU stress. |
| 10–11 | **B₁ slice done** | `prove_groth16_partition` smoke + [`milestone_b_bellperson_vulkan_smoke`](extern/cuzk/cuzk-vk/tests/milestone_b_bellperson_vulkan_smoke.rs) + `vulkan-cuzk` link gate; **B₂:** Vulkan-native proof + witness path. |
| 12 | **Anchor only** | Plan bounds test landed; perf benchmark table → **Milestone B**. |
| **B₀** | **Done** | §3.1 step 6: [`prove_groth16_partition`](extern/cuzk/cuzk-vk/src/prover.rs) SRS `h[]` + GPU MSM + GPU H vs CPU (Vulkan smoke). |

**§3.1 step 6 (B₀)** — [`prove_groth16_partition`](extern/cuzk/cuzk-vk/src/prover.rs) now exercises **SRS `h[]` decode + GPU MSM + GPU H** alongside Fr NTT and dispatch smoke when `CUZK_VK_SKIP_SMOKE=0`. The **§3.2 phased port checklist** remains **closed for the Milestone A correctness scope**. **Milestone B** is split into **B₁ / B₂** below (B₁ is incremental integration; B₂ is parity-scale proving).

### 3.3 Milestone B status (honest scope split)

“Milestone B” in §2/§8 is split so **integration (B₁)** can close independently of **parity-scale proving (B₂)**. Authoritative checklist: [`extern/cuzk/MILESTONE_B.md`](extern/cuzk/MILESTONE_B.md).

| Tier | Status | Goal | Representative landed slices |
|------|--------|------|--------------------------------|
| **B₁ — integration & I/O** | **Complete** | Optional stacks link; Groth16 **pairing verify** (bellperson) coexists with Vulkan partition smoke; SRS/proving **host** ergonomics | `VkPipelineCache` + `CUZK_VK_PIPELINE_CACHE` ([`device.rs`](extern/cuzk/cuzk-vk/src/device.rs)); [`srs_read_file_spawn`](extern/cuzk/cuzk-vk/src/srs.rs); [`tests/milestone_b_bellperson_vulkan_smoke.rs`](extern/cuzk/cuzk-vk/tests/milestone_b_bellperson_vulkan_smoke.rs); SRS + G2 [`srs_gpu`](extern/cuzk/cuzk-vk/src/srs_gpu.rs); §1 CSV / ceilings / hardware MD ([`bench_csv`](extern/cuzk/cuzk-vk/src/bench_csv.rs), [`prover`](extern/cuzk/cuzk-vk/src/prover.rs)); partition G1 MSM uses **Scalar trunc** bit-planes ([`split_msm::g1_msm_bitplanes_scalars_trunc_gpu_host`](extern/cuzk/cuzk-vk/src/split_msm.rs)); **B₂ first witness hook:** [`VkGroth16Job::witness_ntt_coeffs`](extern/cuzk/cuzk-vk/src/prover.rs) → Fr NTT GPU ([`witness_ntt_partition_gpu`](extern/cuzk/cuzk-vk/tests/witness_ntt_partition_gpu.rs)) |
| **B₂ — proving parity** | **Open** (multi-release) | Full **255-bit Scalar** bucket MSM on GPU, **async** SRS → VRAM overlap on hot path, bellperson witness **through** Vulkan, **pairing** on a Vulkan-emitted proof, §8 perf rows | See [`MILESTONE_B.md` § B₂](extern/cuzk/MILESTONE_B.md#b₂--proving-parity-not-complete--performance--semantics-tier) and §3.4 / §8.1–8.4 |

### 3.4 Remaining work (execution order — parity / B₂)

After **Milestone A** closure (**§3.2.1**), this is the default sequencing for **performance parity** and **B₂** (cross-ref **§2**, **§8**):

| # | Track | What is left |
|---|--------|----------------|
| 1 | **§1 Measurement** | Hardware matrix + committed `benchmarks/cuzk-vk/results-*.csv`; **optional** per-stage ceilings **`CUZK_VK_BENCH_MAX_*_MS`** ([`bench_csv`](extern/cuzk/cuzk-vk/src/bench_csv.rs)). **Landed:** per-stage [`VkProofTimings`](extern/cuzk/cuzk-vk/src/prover.rs) + **`CUZK_VK_BENCH_CSV`** + **`CUZK_VK_BENCH_HARDWARE_MD`** / **`CUZK_VK_BENCH_TAG`**; template [`HARDWARE_MATRIX.md`](benchmarks/cuzk-vk/HARDWARE_MATRIX.md); committed header drift check [`results-ci-host-baseline.csv`](benchmarks/cuzk-vk/results-ci-host-baseline.csv); Apple script **`CUZK_VK_RECORD_BENCH_CSV`**. **Open:** optional CI row append from real runners; subgroup line automation. |
| 2 | **§8.1 MSM (A.*)** | Full-width Scalar buckets, bucket-accumulate shaders, window tuning, async queues, subgroup path (see **§8.7** debt row). |
| 3 | **§8.2 NTT (B.*)** | GPU radix-4/8, subgroup stages, coset fusion (**B.5**), single-shot small-*n* (**B.6**). |
| 4 | **§8.3 C.2** | SRS **GPU** upload overlapping first NTT on the hot prover path (beyond [`srs_staging_gpu`](extern/cuzk/cuzk-vk/src/srs_staging_gpu.rs) round-trip). |
| 5 | **§8.3 C.3** | Specialization on Fr NTT tails / bucket MSM where **naga** GLSL cannot express `constant_id` (prebuilt SPV or alternate front-end). |
| 6 | **§8.4 D.*** | Mega-dispatch shader + **D.2** SRS window sharing (host helper [`msm_mega_dense_groups_x`](extern/cuzk/cuzk-vk/src/msm.rs) only today). |
| 7 | **B₂ parity backlog** | **B₁ closed** — see [`MILESTONE_B.md`](extern/cuzk/MILESTONE_B.md). Still open: Vulkan-emitted proof + pairing, witness through Vulkan, full MSM / NTT perf rows (**§3.1** step 6 remainder). |

**B₂ work queue (not “one PR”):** §8.1 **partial:** [`g1_msm_bitplanes_scalars_trunc_gpu_host`](extern/cuzk/cuzk-vk/src/split_msm.rs) + [`scalar_low_u64_canonical`](extern/cuzk/cuzk-vk/src/scalar_limbs.rs) (`tests/split_msm_scalar_trunc_gpu.rs`) — full **255-bit** window buckets + bucket accumulate shaders still open; [`device_profile::msm_config_for_device`](extern/cuzk/cuzk-vk/src/device_profile.rs) logs vendor **window_bits** hint only. §8.2 **partial:** [`FrNttPlan::fused_layer_count`](extern/cuzk/cuzk-vk/src/ntt.rs) for radix-4/8 **schedule** preview (GPU radix butterflies still open). §8.3 **C.2 partial:** [`srs_staging_device_local_upload`](extern/cuzk/cuzk-vk/src/srs_staging_gpu.rs) (no readback) + roundtrip test — **C.2 remainder:** overlap transfer submit vs first NTT on a second queue or pipelined command buffers (no verify download on hot path). **C.3 partial:** [`pipelines::spec_constant_u32_le`](extern/cuzk/cuzk-vk/src/pipelines.rs) + [`vk_oneshot::run_compute_1x_storage_buffer`](extern/cuzk/cuzk-vk/src/vk_oneshot.rs) optional [`ComputeShaderStageSpec`](extern/cuzk/cuzk-vk/src/vk_oneshot.rs) + multi-pass `ComputePassDesc::stage_spec`; smoke [`run_spec_constant_smoke_gpu`](extern/cuzk/cuzk-vk/src/spec_constant_smoke_gpu.rs); MSM grid smoke [`run_msm_dispatch_hitcount_smoke`](extern/cuzk/cuzk-vk/src/msm_gpu.rs) (`local_size_x_id`, `msm_dispatch_grid_smoke_prebuilt.spv`). **Remainder:** Fr NTT tails / bucket MSM (naga path). §8.4 **D.1 partial:** [`msm_mega_dense_groups_x`](extern/cuzk/cuzk-vk/src/msm.rs) — **D.* remainder:** mega-dispatch shader + D.2 SRS window sharing + batching. §1 **partial:** [`VkProofTimings`](extern/cuzk/cuzk-vk/src/prover.rs) per-stage wall + **`CUZK_VK_BENCH_CSV`** → [`bench_csv`](extern/cuzk/cuzk-vk/src/bench_csv.rs); hardware matrix + threshold asserts still open.

---

## 4. Acceptance criteria for “parity”

- 32 GiB PoRep C2 **single** proof on MI250X within **1.10×** A100 supraseal reference.
- **Batch n=10** within **1.05×**.
- **WindowPoSt partition** within **1.20×**.
- All `cuzk-vk` correctness tests remain green at the parity commit.

---

## 8. Speed-up catalogue (write-up reference)

This section consolidates **expected speedups** from §2, **infrastructure choices** that affect wall time today, and **known performance debt** in `extern/cuzk/cuzk-vk`. Use it as the single checklist for parity work and for release notes.

### 8.1 MSM (category A) — planned levers

| ID | Lever | Est. vs prior | Notes |
|----|--------|---------------|--------|
| A.1 | Per-vendor MSM window auto-tune | ~1.10× | **B₂ slice:** [`device_profile`](extern/cuzk/cuzk-vk/src/device_profile.rs) host default + **`CUZK_VK_MSM_WINDOW_BITS`**; VALU profiling + bucket shader wiring still open. |
| A.2 | Signed buckets / wNAF-style halving | ~1.30× | Halves bucket count at same window; needs scalar recoding + shader path. |
| A.3 | G1 GLV endomorphism | ~1.35–1.80× | Scalar split + second table; highest risk; strong pre-merge tests. |
| A.4 | Mixed coords (xyzz buckets, Jacobian out) | ~1.10× | Fewer field ops per bucket accumulate. |
| A.5 | Split / density-aware MSM (groth16_split_msm style) | ~1.15× PoRep | Batch-friendly on MI-class GPUs. |
| A.6 | Async transfer + multi compute queues | ~1.10× large *n* | Overlap SRS / H2D with first NTT/MSM waves. |
| A.7 | Subgroup cooperative bucket reduce | ~1.15× | wave32 vs wave64 variants; spec constants in `pipelines.rs`. |

### 8.2 NTT (category B) — planned levers

| ID | Lever | Est. vs prior | Notes |
|----|--------|---------------|--------|
| B.1 | Radix-4 butterflies | ~1.30× @2²⁰ | Fewer passes than radix-2 at large *n*. |
| B.2 | Radix-8 on smallest stages | ~1.10× | Shrink tail dispatches. |
| B.3 | Subgroup shuffle inner stages | ~1.20× | Cuts shared-memory traffic (profile with RGP). |
| B.4 | SSBO twiddle tables (no on-the-fly pow) | ~1.10× | Moves root work off hot dispatch; aligns with `FrNttPlan` upload. |
| B.5 | Coset first-stage fusion | ~1.05× | One fewer barrier/dispatch at coset multiply. |
| B.6 | Single-shot shared-mem NTT (small *n*) | ~1.50× @2¹⁴ | Latency win for small circuits. |

### 8.3 Memory & pipeline (category C)

| ID | Lever | Impact | Notes |
|----|--------|--------|--------|
| C.1 | On-disk `VkPipelineCache` | **30–90 s** first compile avoided on repeat | Biggest “feel” win for CI and operators; `pipelines.rs`. |
| C.2 | Async SRS upload vs first NTT | 0.5–3 s hidden | **B₁:** [`srs_read_file_spawn`](extern/cuzk/cuzk-vk/src/srs.rs) overlaps **disk** read with host work. **B₂:** [`srs_staging_device_local_upload`](extern/cuzk/cuzk-vk/src/srs_staging_gpu.rs) / [`srs_device_local_buffer_download`](extern/cuzk/cuzk-vk/src/srs_staging_gpu.rs) (`vkCmdCopyBuffer` to device-preferring memory, **no readback** on upload path); [`srs_staging_device_local_roundtrip`](extern/cuzk/cuzk-vk/src/srs_staging_gpu.rs) for tests. **remainder:** overlap transfer submit vs first NTT (second queue or interleaved submits), no verification download on hot path. |
| C.3 | Specialization constants (`log_n`, window, buckets) | ~1.05–1.15× | **B₂ partial:** `vk_oneshot` + [`run_spec_constant_smoke_gpu`](extern/cuzk/cuzk-vk/src/spec_constant_smoke_gpu.rs) + [`run_msm_dispatch_hitcount_smoke`](extern/cuzk/cuzk-vk/src/msm_gpu.rs) (`local_size_x_id = 0` / prebuilt SPV) + [`spec_constant_u32_le`](extern/cuzk/cuzk-vk/src/pipelines.rs); **remainder:** Fr NTT / full MSM bucket shaders (naga GLSL still no `constant_id`). |
| C.4 | Push constants vs UBO for dispatch meta | ~1.05× record | Less descriptor churn per `vkCmdDispatch`. |
| C.5 | Buffer pool metrics | Observability | Allocator hit-rate for tuning batch sizes. |

### 8.4 Multi-circuit batching (category D)

| ID | Lever | Est. vs prior | Notes |
|----|--------|---------------|--------|
| D.1 | Mega-dispatch MSM across *N* circuits | ~1.40–1.80× @ batch≥4 | **B₂ partial:** [`msm_mega_dense_groups_x`](extern/cuzk/cuzk-vk/src/msm.rs) host grid helper; **remainder:** shader + SRS window sharing (D.2). |
| D.2 | Cross-circuit SRS window sharing | ~1.10× | Read bandwidth ÷ *N* on SRS-heavy phases. |

### 8.5 Vendor-specific (category E)

| ID | Lever | Est. / goal | Notes |
|----|--------|-------------|--------|
| E.1 | RDNA3 dual-issue VALU layout | ~1.05–1.15× | Shader ISA tuning (RGA). |
| E.2 | CDNA wave64 variant | ~1.05× MI250X | Alternate SPIR-V + picker. |
| E.3 | Apple M2 / MoltenVK path | TBD | Unified memory + `apple-m2-vulkan-smoke.sh` coverage. |

### 8.6 Already landed (correctness / CI / dev velocity)

These are not all “kernel faster,” but they reduce **time-to-green** and **repeat iteration cost**:

- **`CUZK_VK_SKIP_SMOKE`**: Vulkan integration tests return early without an ICD (default in `scripts/run-all-tests.sh`), so host CI stays green while GPU tests remain opt-in (`=0` or `apple-m2-vulkan-smoke.sh`).
- **SPIR-V via `naga` (`glsl-in` / `spv-out`)**: No `shaderc` / system toolchain dependency in `cuzk-vk/build.rs`; reproducible offline compile.
- **`vk_oneshot`**: One shared path for single-dispatch + one SSBO smoke tests (toy NTT, Fr add/mul/sub, G1/G2 reverse), reducing duplicated unsafe Vulkan.
- **`FrNttPlan` / `scalar_limbs` / `msm` dispatch helpers**: Host-side scheduling math and limb packing ready for GPU upload without changing Groth16 semantics.
- **Fr `mul` CIOS path**: GPU kernel matches `ec-gpu-gen` `FIELD_mul_default`; host uses `blst_fr` ↔ `u32[8]` Montgomery packing (`scalar_montgomery_u32_limbs` / `scalar_from_montgomery_u32_limbs`) so the API stays canonical `Scalar` while the shader stays aligned with OpenCL `FIELD_mul`.
- **Apple smoke script** ([`apple-m2-vulkan-smoke.sh`](extern/cuzk/apple-m2-vulkan-smoke.sh)): device + field + EC + Fr NTT (8 + general) + coset CPU/GPU + SRS + H-term + `bellperson-vk-smoke` when an ICD is present (`CUZK_VK_SKIP_SMOKE=0`).
- **Fr NTT n = 8 (GPU)**: `fr_ntt8_forward` / `fr_ntt8_inverse` SPIR-V (build-time twiddles + `n^{-1}`), [`fr_ntt_gpu`](extern/cuzk/cuzk-vk/src/fr_ntt_gpu.rs) forward, inverse, round-trip; tests vs CPU [`ntt.rs`](extern/cuzk/cuzk-vk/src/ntt.rs).
- **Fr NTT general n ≤ 2^14 (GPU):** [`fr_ntt_general_gpu`](extern/cuzk/cuzk-vk/src/fr_ntt_general_gpu.rs) forward/inverse vs CPU; **coset forward** [`fr_coset_gpu`](extern/cuzk/cuzk-vk/src/fr_coset_gpu.rs) vs bellperson (`tests/fr_coset_fft_gpu.rs`); Fr pointwise max aligned to same *n*.
- **H-term + Groth16 smoke:** [`h_term`](extern/cuzk/cuzk-vk/src/h_term.rs) / [`h_term_gpu`](extern/cuzk/cuzk-vk/src/h_term_gpu.rs); [`groth16_verify_tiny`](extern/cuzk/cuzk-vk/tests/groth16_verify_tiny.rs); workspace [`bellperson-vk-smoke`](extern/cuzk/bellperson-vk-smoke/) for `vulkan-cuzk` link check.
- **SRS:** [`srs`](extern/cuzk/cuzk-vk/src/srs.rs) + [`srs_gpu`](extern/cuzk/cuzk-vk/src/srs_gpu.rs) G1+G2 Montgomery limb reverse smoke (`tests/srs_g{1,2}_gpu_reverse*.rs`); **`FrNttPlan` bounds:** [`fr_ntt_plan_bounds`](extern/cuzk/cuzk-vk/tests/fr_ntt_plan_bounds.rs).
- **Groth16 partition (B₀ + B₁ smoke)**: [`prove_groth16_partition`](extern/cuzk/cuzk-vk/src/prover.rs) wires Fr NTT + MSM dispatch + **SRS `h[]` decode + G1 Scalar-trunc bit-plane MSM** ([`g1_msm_bitplanes_scalars_trunc_gpu_host`](extern/cuzk/cuzk-vk/src/split_msm.rs)) + **`b_g2[0]` decode + G2 limb staging (`srs_gpu`)** + **H GPU vs CPU** when `CUZK_VK_SKIP_SMOKE=0`; **H commit** checks `G1Projective::multi_exp` against a naive Σ `s_i · P_i` on decoded `h[]` bases. **B₁** integration checklist: [`MILESTONE_B.md`](extern/cuzk/MILESTONE_B.md). **B₂** (Vulkan-native proof, full-width MSM, witness path) remains open.
- **Pipeline cache (§C.1 slice)**: [`VulkanDevice`](extern/cuzk/cuzk-vk/src/device.rs) uses `VkPipelineCache` for `vkCreateComputePipelines`; optional **`CUZK_VK_PIPELINE_CACHE`** path loads a prior `vkGetPipelineCacheData` blob, with [`pipeline_cache_save_to_path`](extern/cuzk/cuzk-vk/src/device.rs) / env save for the next process. **`tests/pipeline_cache_disk.rs`** round-trips via toy NTT.
- **SRS async read (§C.2 B₁ slice):** [`srs_read_file_spawn`](extern/cuzk/cuzk-vk/src/srs.rs) + **`tests/srs_async_read.rs`** — overlap disk read with the caller thread; GPU upload overlap remains B₂.
- **Milestone B₁ bellperson + Vulkan:** [`tests/milestone_b_bellperson_vulkan_smoke.rs`](extern/cuzk/cuzk-vk/tests/milestone_b_bellperson_vulkan_smoke.rs) — Groth16 **verify** (pairing engine) then `prove_groth16_partition` when `CUZK_VK_SKIP_SMOKE=0`.
- **§1 partition CSV (B₂ slice):** [`VkProofTimings`](extern/cuzk/cuzk-vk/src/prover.rs) + [`bench_csv`](extern/cuzk/cuzk-vk/src/bench_csv.rs) + `benchmarks/cuzk-vk/README.md`; optional **`CUZK_VK_BENCH_HARDWARE_MD`** / **`CUZK_VK_BENCH_TAG`**; committed [`results-ci-host-baseline.csv`](benchmarks/cuzk-vk/results-ci-host-baseline.csv) header drift test.
- **Milestone B₂ (first slices):** [`VkGroth16Job::witness_ntt_coeffs`](extern/cuzk/cuzk-vk/src/prover.rs) → Fr NTT GPU round-trip in partition ([`tests/witness_ntt_partition_gpu.rs`](extern/cuzk/cuzk-vk/tests/witness_ntt_partition_gpu.rs)); [`split_msm::g1_msm_bitplanes_scalars_trunc_gpu_host`](extern/cuzk/cuzk-vk/src/split_msm.rs) + [`scalar_limbs::scalar_low_u64_canonical`](extern/cuzk/cuzk-vk/src/scalar_limbs.rs) + `tests/split_msm_scalar_trunc_gpu.rs`; [`FrNttPlan::fused_layer_count`](extern/cuzk/cuzk-vk/src/ntt.rs) for radix-4/8 **schedule** depth (GPU radix shaders still open); [`srs_staging_device_local_upload`](extern/cuzk/cuzk-vk/src/srs_staging_gpu.rs) / roundtrip + `tests/srs_staging_device_local_gpu.rs` (§C.2 upload-without-readback API); [`device_profile`](extern/cuzk/cuzk-vk/src/device_profile.rs) MSM window vendor hint + **`CUZK_VK_MSM_WINDOW_BITS`** (§8.1 A.1 host slice); [`pipelines::spec_constant_u32_le`](extern/cuzk/cuzk-vk/src/pipelines.rs) + [`run_spec_constant_smoke_gpu`](extern/cuzk/cuzk-vk/src/spec_constant_smoke_gpu.rs) + [`run_msm_dispatch_hitcount_smoke`](extern/cuzk/cuzk-vk/src/msm_gpu.rs) workgroup **SpecId 0** (§C.3); [`msm_mega_dense_groups_x`](extern/cuzk/cuzk-vk/src/msm.rs) (§D.1 host grid); [`benchmarks/cuzk-vk/README.md`](benchmarks/cuzk-vk/README.md) CSV drop point (§1); [`HARDWARE_MATRIX.md`](benchmarks/cuzk-vk/HARDWARE_MATRIX.md) §1 template.
- **MSM dispatch grid smoke**: `msm_dispatch_grid_smoke.comp` + `msm_dispatch_grid_smoke_prebuilt.spv` (glslang; `local_size_x_id = 0`) + [`run_msm_dispatch_hitcount_smoke`](extern/cuzk/cuzk-vk/src/msm_gpu.rs) — verifies `MsmBucketReduceDispatch::dense` vs `vkCmdDispatch` with **specialized** workgroup X (unique linear `tid`; capped at 4096 threads for SSBO sizing).

### 8.7 Known performance debt (replace for parity)

| Area | Current state | Target |
|------|----------------|--------|
| **Fr `mul` on GPU** | ~~Double-and-add (255-bit scan)~~ **Done:** CIOS Montgomery (`FIELD_mul_default`) on `blst_fr` limbs; portable `mac_with_carry` via widened 32×32 mul (no `shaderInt64`). | Further: fused butterfly / subgroup (§8.2), Montgomery squaring fast path. |
| **Fr `add` / `sub`** | Wide limb add/sub + one `P` subtract — acceptable | Keep; fuse with butterfly loads/stores later. |
| **NTT on GPU** | **n = 8** (`fr_ntt_gpu`); **general** power-of-two **n ≤ 2^14** (`fr_ntt_general_gpu`); coset forward (`fr_coset_gpu`). Host [`FrNttPlan::fused_layer_count`](extern/cuzk/cuzk-vk/src/ntt.rs) previews radix-4/8 **layer counts** only. | Radix-4/8 **GPU** butterflies, subgroup shuffle, single-shot paths (**B.1–B.3, B.5–B.6**); optional twiddle prep micro-opts (**B.4** — plan upload already used). |
| **Pipeline cache** | **In-memory** `VkPipelineCache` + optional **`CUZK_VK_PIPELINE_CACHE`** load/save (`device.rs`); SPIR-V still from `naga` at build time | C.1 remainder: merge blobs across ICD versions / CI artifact, auto-save hooks, measure compile-time delta. |
| **MSM** | Bit-plane + **Scalar→trunc→GPU** ([`split_msm`](extern/cuzk/cuzk-vk/src/split_msm.rs), `tests/split_msm_scalar_trunc_gpu.rs`); **no** window bucket accumulate / full-width Scalar yet | A.* shaders + bucket reduce + 255-bit tail as in §8.1. |

### 8.8 Measurement (ties to §1)

When claiming any row in §8.1–8.5, attach: GPU SKU, driver (`radv` / AMDVLK / proprietary), `vulkaninfo` subgroup line, workload (32 GiB C2 *n_circuits*, or WindowPoSt), **s/proof** and **NTT s / MSM s** split, optional `VK_KHR_calibrated_timestamps` once enabled in `device.rs`. Store CSV under `benchmarks/cuzk-vk/` per §1; optional **`CUZK_VK_BENCH_HARDWARE_MD`** appends the same run’s [`PhysicalDeviceInfo::measurement_paragraph`](extern/cuzk/cuzk-vk/src/device.rs) + stage ms for prose release notes.
