# Specialization constants vs `naga` GLSL-in (§8.3 C.3)

| Artifact | `constant_id` / spec | Build path | Used by |
|----------|----------------------|------------|---------|
| `spec_constant_smoke_prebuilt.spv` | `uint` id **0** | **Prebuilt** (`glslangValidator`); source `spec_constant_smoke.comp` | [`spec_constant_smoke_gpu`](../src/spec_constant_smoke_gpu.rs) |
| `msm_dispatch_grid_smoke_prebuilt.spv` | `local_size_x` id **0** | **Prebuilt**; source `msm_dispatch_grid_smoke.comp` | [`msm_gpu::run_msm_dispatch_hitcount_smoke`](../src/msm_gpu.rs) |
| `g1_pippenger_bucket_acc.spv` | **None** — fixed WG **256** | **`naga`** `glsl-in` (`g1_pippenger_bucket_acc_tail.comp`) | Fallback: copied to `g1_pippenger_bucket_acc_spec.spv` when **`glslangValidator`** absent |
| `g1_pippenger_bucket_acc_spec.spv` | `local_size_x` id **0** — Rust passes LE `u32` **[`G1_PIPP_LOCAL_X`](../src/ec.rs)** ([`spec_constant_u32_le`](../src/pipelines.rs) + [`ComputeShaderStageSpec`](../src/vk_oneshot.rs)) when **`cfg(g1_pippenger_bucket_acc_spec)`** | **`glslangValidator`** (`g1_pippenger_bucket_acc_spec_tail.comp`); else copy of **`naga`** module above | [`g1_pippenger_bucket_gpu`](../src/g1_pippenger_bucket_gpu.rs) |
| `fr_ntt_general_{bitrev_scatter,copy_scratch_to_data,scale_ninv}.spv` | **None** (reserved push words unused) | **`naga`** | [`fr_ntt_general_gpu`](../src/fr_ntt_general_gpu.rs) |
| `fr_ntt_general_radix{2,4,8}_*.spv` | Radix stride + **`twiddle_word_off`** (words from `OFF_TWIDDLE`; §8.2 **B.4**). Radix-2 also passes `stage`. | **`naga`**; radix-2 spec via **`glslangValidator`** when available | [`fr_ntt_general_gpu`](../src/fr_ntt_general_gpu.rs) |
| `fr_ntt_general_radix2_stage_spec.spv` | `local_size_x` id **0** — Rust passes **`VkSpecializationInfo`** (LE `u32` **256** = `WG`, [`spec_constant_u32_le`](../src/pipelines.rs) + [`ComputeShaderStageSpec`](../src/vk_oneshot.rs)) on trailing radix-2 passes in radix-4/8 schedules | **`glslangValidator`** when available (`layout(local_size_x_id = 0)`); else fallback copies naga `fr_ntt_general_radix2_stage.spv` (fixed layout; build sets `cargo:rustc-cfg=fr_ntt_radix2_spec` only when glslang succeeds) | [`fr_ntt_general_gpu`](../src/fr_ntt_general_gpu.rs) |
| `fr_ntt8_*.spv` | **None** | **`naga`** | [`fr_ntt_gpu`](../src/fr_ntt_gpu.rs) |
| `subgroup_shuffle_smoke.spv` | **None** | **`naga` `wgsl-in`** | [`subgroup_shuffle_smoke_gpu`](../src/subgroup_shuffle_smoke_gpu.rs) |

**Gap (B₂):** Bitrev / copy / `scale_ninv` tails still lack spec constants; further **`layout(constant_id = …)`** for `log_n` / window tuning where **`naga` glsl-in** still cannot emit stable spec constants — use **prebuilt SPIR-V** (as above) or a second compiler stage.

See [`pipelines::spec_constant_u32_le`](../src/pipelines.rs) and [`vk_oneshot::ComputePassDesc::stage_spec`](../src/vk_oneshot.rs).
