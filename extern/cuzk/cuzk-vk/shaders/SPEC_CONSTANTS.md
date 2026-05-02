# Specialization constants vs `naga` GLSL-in (§8.3 C.3)

| Artifact | `constant_id` / spec | Build path | Used by |
|----------|----------------------|------------|---------|
| `spec_constant_smoke_prebuilt.spv` | `uint` id **0** | **Prebuilt** (`glslangValidator`); source `spec_constant_smoke.comp` | [`spec_constant_smoke_gpu`](../src/spec_constant_smoke_gpu.rs) |
| `msm_dispatch_grid_smoke_prebuilt.spv` | `local_size_x` id **0** | **Prebuilt**; source `msm_dispatch_grid_smoke.comp` | [`msm_gpu::run_msm_dispatch_hitcount_smoke`](../src/msm_gpu.rs) |
| `fr_ntt_general_*.spv` (bitrev / copy / scale) | **None** (push constants for stage index only) | **`naga`** from `build.rs` | [`fr_ntt_general_gpu`](../src/fr_ntt_general_gpu.rs) |
| `fr_ntt_general_radix2_stage_spec.spv` | `local_size_x` id **0** (default **256** on host) | **`glslangValidator`** when available; else copy of naga radix-2 stage | [`fr_ntt_general_gpu`](../src/fr_ntt_general_gpu.rs) (`cfg(fr_ntt_radix2_spec)`) |
| `fr_ntt8_*.spv` | **None** | **`naga`** | [`fr_ntt_gpu`](../src/fr_ntt_gpu.rs) |
| Field / EC tails (`fp_*`, `g1_*`, …) | **None** | **`naga`** | respective `*_gpu.rs` |

**Gap (B₂):** Bitrev / copy / `scale_ninv` tails still lack spec constants; full MSM bucket shaders need **`layout(constant_id = …)`** for `log_n` / window tuning where **`naga` glsl-in** still cannot emit stable spec constants — use **prebuilt SPIR-V** (as above) or a second compiler stage.

See [`pipelines::spec_constant_u32_le`](../src/pipelines.rs) and [`vk_oneshot::ComputePassDesc::stage_spec`](../src/vk_oneshot.rs).
