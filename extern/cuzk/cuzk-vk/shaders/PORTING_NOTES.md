# Vulkan GLSL porting notes (`naga` glsl-in → SPIR-V)

These constraints come from compiling with **`naga`** in [`build.rs`](../build.rs) (no `shaderc`). SupraSeal CUDA ports must respect them.

## Language / parser pitfalls

1. **`atomicAdd` on `storage` buffers** — `naga` glsl-in reports `Unknown function 'atomicAdd'`. Use per-thread unique indices + `buffer` writes, or fixed-size arrays + bounds checks (see `msm_dispatch_grid_smoke.comp`).
2. **Array-of-struct temporaries** — e.g. `Fr t[8]` where `Fr` is a struct caused an internal SPIR-V backend panic (“Expression is not cached”). Use a **flat** `uint tmp[N]` scratch and manual load/store (see `fr_ntt8_forward_tail.comp` `bitrev8_perm`).
3. **`umulExtended`** — not available in this path. Implement 32×32→64 via four 16×16 partial products (see `toy_ntt8.comp`).
4. **Identifier `flat`** — `flat` is a GLSL interpolation qualifier; using it as a variable name fails parse (“InvalidToken(Interpolation(Flat)…”). Use `tid`, `idx`, `linear_id`, etc.
5. **Runtime-sized SSBO arrays** — `uint payload[]` after another member failed parse. Use **fixed** maximum length (e.g. `uint payload[4096]`) and enforce `thread_count <= cap` on the host.
6. **Unsized arrays in `buffer` blocks** — prefer `uint data[MAX_N]` with host-checked `N`.

## Test gating (Vulkan ICD)

All GPU integration tests use:

```rust
fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}
```

- **CI / `scripts/run-all-tests.sh`**: default `CUZK_VK_SKIP_SMOKE=1` → tests early-return.
- **Local / Apple**: `apple-m2-vulkan-smoke.sh` sets default `0` so tests run when an ICD exists.

## Limb layout (BLS12-381)

| Field | Canonical | Montgomery (GPU / CIOS) |
|-------|-------------|---------------------------|
| **Fr** | `u32[8]` LE (`scalar_to_le_u32_limbs`) | `u32[8]` from `blst_fr::l` pairs (`scalar_limbs.rs`) |
| **Fp** | `u32[12]` LE (`fp_to_le_u32_limbs`) | `u32[12]` from `blst_fp::l` pairs (`fp.rs`) |
| **Fp2** | `c0 \|\| c1` each `Fp` canonical or Montgomery per shader contract | 24×`u32` |

Do **not** rely on `uint64_t` in GLSL for MoltenVK portability; keep 32-bit limb arithmetic.

## Fp helper reuse (`fp_helpers.glsl`)

`shaders/fp_helpers.glsl` holds shared Montgomery / mod-`p` routines (`fp_mul_mont_cios`, `fp_add_mod`, …) with placeholders `@@FP_T@@`, `@@FP_P@@`, `@@FP_INV@@`, `@@FP_ONE@@`. **`build.rs`** concatenates `glsl_32_bit_field_params::<Fp>()` + substituted helpers + an `fp2_*_tail.comp` that declares the `d[72]` SSBO and `main()`. Single-Fp tails (`fp_*12_tail.comp`) remain self-contained to avoid changing their compile path.

G1/G2 EC shaders (`g1_jacobian_add108_tail.comp`, `g1_xyzz_add_mixed72_tail.comp`, `g2_*`) concatenate the same `fp_helpers.glsl`; G2 also prepends `fp2_mont_ops.glsl` (Fp2 mul / add / sub / sqr over Montgomery `Fp` components, \(u^2 = -1\)). **XYZZ** follows sppark `ec/xyzz_t.hpp` (`X`, `Y`, `ZZZ`, `ZZ` field order) and the EFD `shortw/xyzz` formulas linked in that header.

`fr_helpers.glsl` mirrors the Fr CIOS / add / sub helpers from `fr_*8_tail.comp` for vectorized kernels: `fr_coeff_wise_mult_tail.comp` (`local_size_x = 64`, `d[0]` = `n`, max `16384` coeffs) and `fr_sub_mult_constant_tail.comp` (same + Montgomery `c` slot after the three max-length regions). It also exposes `fr_one()` / `fr_pow_u32()` (needs `@@FR_ONE@@` substitution in **`build.rs`**, same as the tails) for kernels that derive twiddles from per-stage `wlen` in the SSBO.

**General Fr NTT (`n ≤ 2^14`):** `fr_ntt_general_*_tail.comp` shaders use a fixed `uint d[…]` layout (64-word header, `data[]`, `scratch[]`, then `wlen` per stage × 8 words). Bitrev is a **scatter to scratch** plus a **linear copy back** (two dispatches) so threads never read/write the same coefficient slot in one pass. Radix-2 DIT stages use `layout(push_constant)` for the stage index; `vk_oneshot::run_compute_passes_1x_storage_buffer` records one command buffer with **storage buffer barriers** between stages. **Coset** pre/post multiply is not fused here yet (roadmap §3.2 step 5).

## SupraSeal `batch_addition` (CUDA → Vulkan sketch)

Source: [`extern/supraseal-c2/cuda/groth16_split_msm.cu`](../../../supraseal-c2/cuda/groth16_split_msm.cu) (kernel body lives in upstream `msm/batch_addition.cuh`, not vendored here).

- Constants: **`CHUNK_BITS = 64`**, **`NUM_BATCHES = 8`**, cooperative-style **`launch_coop`** for `batch_addition<bucket_t>`.
- Each thread walks bases with a **64-bit bitmap** chunk; accumulates into **XYZZ** bucket state (`bucket_t::add_or_double` in sppark).
- **Vulkan analog:** one workgroup + **`shared` memory** tree reduction + `barrier()` instead of CUDA cooperative grid-wide sync. Subgroup `subgroupAdd` does **not** apply to elliptic-curve adds; reduction is manual.

**Landed slice (Phase G):** `g1_batch_accum_bitmap1636_tail.comp` — workgroup 8, up to **64** affine G1 points, **64-bit** bitmap (`u32` lo/hi), Jacobian tree in `shared` (same add/double as `g1_jacobian_add108_tail.comp`). **`g2_batch_accum_aff16_904_tail.comp`** — workgroup 8, up to **16** affine G2 points, **16-bit** bitmap, Fp2 Jacobian merge (same math as `g2_jacobian_add216_tail.comp`). Drivers: [`g1_batch_gpu.rs`](../src/g1_batch_gpu.rs), [`g2_batch_gpu.rs`](../src/g2_batch_gpu.rs). Still **not** multi-bucket / per-SM `batch_addition.cuh` layout.

Document deviations when the Vulkan shader intentionally simplifies sppark’s warp layout.

## Phase H–J host modules (Rust)

- [`split_msm.rs`](../src/split_msm.rs) — G1 MSM as per-bit GPU bitmap batch sums + host `2^b` combine (small `u64` scalars, `n ≤ 64`).
- [`srs.rs`](../src/srs.rs) — SRS file byte layout constants + walk past VK + six big-endian counts and affine blobs (`groth16_srs.cuh`).
- [`h_term.rs`](../src/h_term.rs) / [`h_term_gpu.rs`](../src/h_term_gpu.rs) — Groth16 H quotient scalars (CPU = bellperson `execute_fft`; GPU = general Fr NTT + host coset / vanishing glue).

## References

- `extern/ec-gpu-gen/src/cl/{field.cl,ec.cl,fft.cl,multiexp.cl}` — limb / EC reference math.
- Repo root `c2-optimization-proposal-*.md` — SupraSeal C2 orchestration and line refs into `groth16_cuda.cu`.
- [`cuzk-vulkan-optimization-roadmap.md`](../../../../cuzk-vulkan-optimization-roadmap.md) §3.2 — port phase checklist.
