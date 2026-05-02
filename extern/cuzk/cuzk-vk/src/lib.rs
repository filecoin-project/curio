//! Vulkan compute path for cuZK (BLS12-381 Groth16).
//!
//! **Milestone A** — see `cuzk-vulkan-optimization-roadmap.md` **§3.2.1** (Fr through H-term correctness port).
//! **Milestone B (B₀ integration)** — **§3.1 step 6**: [`prove_groth16_partition`] with `CUZK_VK_SKIP_SMOKE=0` runs Fr NTT
//! round-trip, MSM dispatch grid, **SRS `h[]` / `b_g2[0]` decode** ([`srs`]) + G1 bit-plane MSM ([`split_msm`]), and GPU **H** vs CPU
//! ([`h_term_gpu`]). **Milestone B** — roadmap **§3.3** (**B₁** integration vs **B₂** parity); B₁ includes
//! `srs::srs_read_file_spawn` and integration test `milestone_b_bellperson_vulkan_smoke.rs`.
//! **§C.1 slice:** [`VulkanDevice`] `VkPipelineCache` + optional **`CUZK_VK_PIPELINE_CACHE`**. **B₂ C.2:** [`srs_staging_gpu`] device-local **upload** (`vkCmdCopyBuffer`, no readback) + test download path.
//! Also: **bellperson** Groth16 (`tests/groth16_verify_tiny.rs`) + **`vulkan-cuzk`**
//! (`bellperson-vk-smoke`). `bellperson::groth16::vulkan_cuzk` re-exports.

pub mod assignment_layout;
pub mod allocator;
pub mod bench_csv;
pub mod device;
pub mod device_profile;
pub mod ec;
pub mod fp;
pub mod fp2;
pub mod fp_gpu;
pub mod fp2_gpu;
pub mod fr_gpu;
pub mod fr_coset_gpu;
pub mod fr_ntt_general_gpu;
pub mod fr_ntt_gpu;
pub mod fr_ntt_radix_plan;
pub mod fr_pointwise_gpu;
pub mod groth16_proof_abi;
pub mod h_term;
pub mod h_term_gpu;
pub mod g1;
pub mod g1_batch_gpu;
pub mod g1_mega_strip_gpu;
pub mod g1_msm_bucket;
pub mod g1_msm_bucket_gpu;
pub mod g1_pippenger_bucket_gpu;
pub mod g1_ec_gpu;
pub mod g2_batch_gpu;
pub mod g1_gpu;
pub mod g2_ec_gpu;
pub mod g2_gpu;
pub mod msm;
pub mod msm_gpu;
pub mod ntt;
pub mod srs;
pub mod srs_gpu;
pub mod srs_staging_gpu;
pub mod srs_dual_queue_gpu;
pub mod split_msm;
pub mod pipelines;
pub mod prover;
pub mod scalar_limbs;
pub mod spec_constant_smoke_gpu;
pub mod toy_ntt;
pub mod toy_ntt_gpu;
pub mod witness_bridge;

mod vk_oneshot;

pub use device::{PhysicalDeviceInfo, VulkanDevice};
pub use device_profile::{msm_config_for_device, recommended_msm_window_bits};
pub use ntt::{fr_intt_inplace, fr_ntt_inplace, fr_omega, FrNttPlan, FrNttPlanError};
pub use bench_csv::{
    append_partition_benchmark_csv, append_partition_hardware_md, bench_max_ms_from_env,
    duration_ms_exceeds_u64_ceiling, PartitionBenchRow, PARTITION_BENCH_CSV_HEADER,
};
pub use groth16_proof_abi::{
    write_groth16_proof_compressed, G1_COMPRESSED_BYTES, G2_COMPRESSED_BYTES,
    GROTH16_PROOF_COMPRESSED_BYTES,
};
pub use prover::{
    prove_groth16_partition, VkGroth16Job, VkProofKind, VkProofTimings, VkProverContext,
    VkProverSession,
};
