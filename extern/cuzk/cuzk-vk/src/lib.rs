//! Vulkan compute path for cuZK (BLS12-381 Groth16).
//!
//! **Milestone A (correctness slice)** — see `cuzk-vulkan-optimization-roadmap.md` **§3.2.1**: Fr add/sub/mul; Fr NTT **n = 8**
//! ([`fr_ntt_gpu`]) and **general n ≤ 2^14** ([`fr_ntt_general_gpu`]); coset forward on GPU ([`fr_coset_gpu`], same `n` as
//! pointwise + NTT; [`h_term_gpu`] coset + tail distribute); G1/G2 bitmap batch Jacobian ([`g1_batch_gpu`], [`g2_batch_gpu`]);
//! toy NTT; G1/G2 limb smoke; MSM dispatch-grid smoke ([`msm_gpu`]); split MSM bit-planes ([`split_msm`]); SRS ([`srs`],
//! [`srs_gpu`]); H-term ([`h_term`], [`h_term_gpu`]); [`prove_groth16_partition`] smoke; **bellperson** tiny Groth16
//! (`tests/groth16_verify_tiny.rs`) + **`vulkan-cuzk`** workspace smoke (`bellperson-vk-smoke`). **`Milestone B`:** full bucket
//! MSM, SRS-bound Vulkan proving, pairing, perf rows in roadmap **§2 / §8**. `bellperson::groth16::vulkan_cuzk` re-exports.

pub mod allocator;
pub mod device;
pub mod ec;
pub mod fp;
pub mod fp2;
pub mod fp_gpu;
pub mod fp2_gpu;
pub mod fr_gpu;
pub mod fr_coset_gpu;
pub mod fr_ntt_general_gpu;
pub mod fr_ntt_gpu;
pub mod fr_pointwise_gpu;
pub mod h_term;
pub mod h_term_gpu;
pub mod g1;
pub mod g1_batch_gpu;
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
pub mod split_msm;
pub mod pipelines;
pub mod prover;
pub mod scalar_limbs;
pub mod toy_ntt;
pub mod toy_ntt_gpu;

mod vk_oneshot;

pub use device::VulkanDevice;
pub use ntt::{fr_intt_inplace, fr_ntt_inplace, fr_omega, FrNttPlan, FrNttPlanError};
pub use prover::{
    prove_groth16_partition, VkGroth16Job, VkProofKind, VkProofTimings, VkProverContext,
    VkProverSession,
};
