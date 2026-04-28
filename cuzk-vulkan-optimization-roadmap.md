# CuZK Vulkan Performance Parity Roadmap

## 0. Goal

Bring `extern/cuzk/cuzk-vk` from the expected **0.5–0.7×** supraseal-c2 CUDA baseline to **~1.0×** on comparable AMD hardware (e.g. MI250X / RX 7900 XTX vs NVIDIA A100 reference numbers in the c2 optimization proposals).

**Parity definition:** match supraseal throughput on **AMD** stacks where supraseal cannot run; we do **not** target NVIDIA (CUDA remains canonical there).

---

## 1. Baseline measurement protocol

- **Hardware matrix:** document SKU, driver (`radv` vs AMDVLK), `vulkaninfo` subgroup properties, and the supraseal reference row from `c2-optimization-proposal-11.md` for the same logical workload.
- **Workloads:** 32 GiB PoRep C2 with `n_circuits ∈ {1, 10, 30}`; single WindowPoSt partition.
- **Metrics:** end-to-end s/proof; MSM stage s; NTT stage s; peak VRAM; optional `VK_KHR_calibrated_timestamps` deltas once wired in `cuzk-vk`.
- **Recording:** commit `benchmarks/cuzk-vk/results-<gpu>-<date>.csv` plus a one-paragraph summary per optimization batch.

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
| 5 | **Done (grid)** | Host [`msm.rs`](extern/cuzk/cuzk-vk/src/msm.rs) + [`msm_dispatch_grid_smoke`](extern/cuzk/cuzk-vk/shaders/msm_dispatch_grid_smoke.comp) hit-count vs `vkCmdDispatch` (≤4096 threads). **Bucket MSM / G1 EC** → §8.1. |
| 6 | **Milestone A** | [`prove_groth16_partition`](extern/cuzk/cuzk-vk/src/prover.rs) runs GPU Fr NTT8 round-trip + MSM dispatch smoke and returns timings. **Milestone B** (SRS bind, arbitrary *n*, bucket MSM, pairing, valid proofs) → §2 / §8. |

### 3.2 SupraSeal-aligned port (CUDA → Vulkan)

**Goal:** Port local SupraSeal C2 CUDA sources under `extern/supraseal-c2/cuda/` (plus minimal curve/NTT primitives they depend on) to Vulkan compute in `extern/cuzk/cuzk-vk`, test-gated with `CUZK_VK_SKIP_SMOKE`, then apply Milestone B optimizations from §8 where they still apply.

**Phased checklist** (implementation order; see `extern/cuzk/cuzk-vk/shaders/PORTING_NOTES.md` for `naga` constraints):

1. **Preflight** — `fp.rs` (Fp Montgomery limbs), `PORTING_NOTES.md`.
2. **Fp / Fp2 on GPU** — `fp_*12_tail`, `fp2_*` shaders + drivers + tests vs `blstrs`.
3. **G1/G2 EC** — Jacobian + XYZZ shaders, `ec.rs`, tests vs group law.
4. **Fr pointwise** — `coeff_wise_mult`, `sub_mult_with_constant` (SupraSeal `groth16_ntt_h.cu`).
5. **General Fr NTT** — SSBO twiddle bases, multi-dispatch (`vk_oneshot` pass sequence); **coset forward** on GPU ([`fr_coset_gpu`](extern/cuzk/cuzk-vk/src/fr_coset_gpu.rs), `tests/fr_coset_fft_gpu.rs`) through `n = 2^14` (pointwise SSBO cap matches NTT max). **CPU** coset: `tests/fr_coset_fft_cpu.rs`. Single-SPV coset+first-NTT-stage fusion (B.5) still open.
6. **`batch_addition`** — G1/G2 cooperative-style reduction in shared memory. **Milestone A (slice done):** G1 ≤64 affines + 64-bit bitmap (`g1_batch_accum_bitmap1636`), G2 ≤16 affines (`g2_batch_accum_aff16_904`). **Milestone B:** full multi-bucket / `CHUNK_BITS=64` / per-SM CUDA layout.
7. **Split MSM orchestration** — bitmap + GPU batch + CPU tail Pippenger. **Milestone A (slice done):** [`split_msm`](extern/cuzk/cuzk-vk/src/split_msm.rs) G1 bit-plane MSM + **CPU** cross-check vs `G1Projective::multi_exp` (`tests/split_msm_multiexp_cpu.rs`). **Milestone B:** full CUDA bitmap + Scalar tail Pippenger on GPU.
8. **SRS** — mmap layout from `groth16_srs.cuh`, synthetic fixture tests. **Slice done:** [`srs`](extern/cuzk/cuzk-vk/src/srs.rs) VK + IC decode, `srs_read_file`, `tests/srs_file_ic.rs`; [`srs_gpu`](extern/cuzk/cuzk-vk/src/srs_gpu.rs) G1 Montgomery limb `g1_reverse24` vs CPU (`tests/srs_g1_gpu_reverse24.rs`). **Milestone B:** async SRS upload / full proving buffers (§8.3 **C.2**).
9. **H-term pipeline** — INTT/FFT/coset chain vs CPU reference. **Done (Milestone A):** [`h_term`](extern/cuzk/cuzk-vk/src/h_term.rs) `fr_quotient_scalars_from_abc`, [`h_term_gpu`](extern/cuzk/cuzk-vk/src/h_term_gpu.rs) uses [`fr_coset_gpu`](extern/cuzk/cuzk-vk/src/fr_coset_gpu.rs) for coset distribute + forward NTT and GPU tail distribute (`g^{-1}`); host vanishing `z_inv` between GPU stages; tests `h_term_quotient_gpu` (incl. domain 8192). **Milestone B:** optional B.5 single-dispatch coset fusion (§8.2).
10. **`prove_groth16_partition`** — real driver + Groth16 verify test. **Milestone A (slice done):** partition smoke + H tick ([`prover`](extern/cuzk/cuzk-vk/src/prover.rs)); **native** Groth16 prove+verify: [`groth16_verify_tiny`](extern/cuzk/cuzk-vk/tests/groth16_verify_tiny.rs). **Milestone B:** SRS-bound + bucket MSM + Vulkan-valid proof path.
11. **Bellperson `vulkan-cuzk`** — optional path dep on `cuzk-vk`, [`groth16::vulkan_cuzk`](extern/bellperson/src/groth16/vulkan_cuzk.rs) re-exports `VkProverContext` / `prove_groth16_partition`. **Milestone A (slice done):** workspace [`bellperson-vk-smoke`](extern/cuzk/bellperson-vk-smoke/) + `scripts/run-all-tests.sh`. **Milestone B:** route real proving through Vulkan; mimc E2E when `CUZK_RUN_BELLPERSON=1` + `cargo check --features groth16,vulkan-cuzk`.
12. **Milestone B perf** — A.1/A.6/A.7, B.1/B.3/B.6, C.1/C.3, D.1 with benchmark assertions. **Milestone A anchor:** `tests/fr_ntt_plan_bounds.rs` (`FrNttPlan` bounds). **Milestone B:** CSV + threshold assertions per §1.

### 3.2.1 Milestone A closure (SupraSeal port — correctness slice)

| §3.2 step | Status | What “done” means here |
|-----------|--------|-------------------------|
| 1–5 | **Done** | Preflight through general Fr NTT + GPU coset forward + pointwise `n` aligned to `2^14`; CPU coset parity test. |
| 6–7 | **Slice done** | Bitmap batch Jacobian + split MSM GPU/CPU checks; full CUDA-scale MSM → **Milestone B**. |
| 8 | **Slice done** | SRS file/layout + IC decode + GPU G1 limb staging smoke; async proving upload → **Milestone B**. |
| 9 | **Done** | H quotient CPU + GPU vs reference / bellperson; large-domain GPU stress. |
| 10–11 | **Slice done** | `prove_groth16_partition` smoke + Groth16 verify regression + `vulkan-cuzk` type/link gate. |
| 12 | **Anchor only** | Plan bounds test landed; perf benchmark table → **Milestone B**. |

**§3.1 step 6** (`prove_groth16_partition` integration smoke) remains the **Milestone A** umbrella for *orchestration* until **Milestone B** delivers SRS-bound partition + bucket MSM + valid proofs; the **§3.2 phased port checklist** above is now **closed for the stated correctness scope** (steps 1–5, 9; slices 6–8, 10–11; anchor 12).

**Next:** **Milestone B** — §3.1 row 6 text, §2, §8.1–8.5 (MSM, NTT perf, memory, batching, vendor).

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
| A.1 | Per-vendor MSM window auto-tune | ~1.10× | `msm.rs` / future `device_profile.rs`; tune `window_bits` to VALU occupancy. |
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
| C.2 | Async SRS upload vs first NTT | 0.5–3 s hidden | Overlap with compute queue timeline. |
| C.3 | Specialization constants (`log_n`, window, buckets) | ~1.05–1.15× | Fewer pipeline variants; better RDNA3 dispatch. |
| C.4 | Push constants vs UBO for dispatch meta | ~1.05× record | Less descriptor churn per `vkCmdDispatch`. |
| C.5 | Buffer pool metrics | Observability | Allocator hit-rate for tuning batch sizes. |

### 8.4 Multi-circuit batching (category D)

| ID | Lever | Est. vs prior | Notes |
|----|--------|---------------|--------|
| D.1 | Mega-dispatch MSM across *N* circuits | ~1.40–1.80× @ batch≥4 | Main PoRep batch win. |
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
- **SRS:** [`srs`](extern/cuzk/cuzk-vk/src/srs.rs) + [`srs_gpu`](extern/cuzk/cuzk-vk/src/srs_gpu.rs) limb reverse smoke; **`FrNttPlan` bounds:** [`fr_ntt_plan_bounds`](extern/cuzk/cuzk-vk/tests/fr_ntt_plan_bounds.rs).
- **Groth16 partition (Milestone A)**: [`prove_groth16_partition`](extern/cuzk/cuzk-vk/src/prover.rs) wires NTT8 + MSM dispatch smoke (not a cryptographic proof yet).
- **MSM dispatch grid smoke**: `msm_dispatch_grid_smoke.comp` + [`run_msm_dispatch_hitcount_smoke`](extern/cuzk/cuzk-vk/src/msm_gpu.rs) — verifies `MsmBucketReduceDispatch::dense` matches `vkCmdDispatch` × `local_size` (unique linear `tid` into fixed `payload[]`; currently capped at 4096 threads for SSBO / `naga` sizing).

### 8.7 Known performance debt (replace for parity)

| Area | Current state | Target |
|------|----------------|--------|
| **Fr `mul` on GPU** | ~~Double-and-add (255-bit scan)~~ **Done:** CIOS Montgomery (`FIELD_mul_default`) on `blst_fr` limbs; portable `mac_with_carry` via widened 32×32 mul (no `shaderInt64`). | Further: fused butterfly / subgroup (§8.2), Montgomery squaring fast path. |
| **Fr `add` / `sub`** | Wide limb add/sub + one `P` subtract — acceptable | Keep; fuse with butterfly loads/stores later. |
| **NTT on GPU** | **n = 8** (`fr_ntt_gpu`); **general** power-of-two **n ≤ 2^14** (`fr_ntt_general_gpu`); coset forward (`fr_coset_gpu`). | Radix-4/8, subgroup shuffle, single-shot paths (**B.1–B.3, B.5–B.6**); optional twiddle prep micro-opts (**B.4** — plan upload already used). |
| **Pipeline cache** | None (every process pays shader compile via `naga` at build time for **embedded** SPIR-V; runtime still builds `VkPipeline`) | C.1 on-disk cache for **runtime** `VkShaderModule` / `VkPipeline` reuse across jobs. |
| **MSM** | `msm.rs` + dispatch-count GPU smoke; **no** bucket accumulate / G1 EC yet | A.* shaders + bucket reduce as in §8.1. |

### 8.8 Measurement (ties to §1)

When claiming any row in §8.1–8.5, attach: GPU SKU, driver (`radv` / AMDVLK / proprietary), `vulkaninfo` subgroup line, workload (32 GiB C2 *n_circuits*, or WindowPoSt), **s/proof** and **NTT s / MSM s** split, optional `VK_KHR_calibrated_timestamps` once enabled in `device.rs`. Store CSV under `benchmarks/cuzk-vk/` per §1.
