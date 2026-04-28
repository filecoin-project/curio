//! Vulkan compute path for cuZK (BLS12-381 Groth16).
//!
//! Landed: Fr add/sub/mul, Fr NTT **n = 8** forward + inverse + round-trip ([`fr_ntt_gpu`]), toy NTT,
//! G1/G2 limb smoke, MSM dispatch-grid smoke ([`msm_gpu`]), [`prove_groth16_partition`] integration
//! smoke. Full bucket MSM, general-size NTT, SRS-bound Groth16, and pairing remain future work.
//! Checklist: `cuzk-vulkan-optimization-roadmap.md` **§3.1** and **§8** (repo root).

pub mod allocator;
pub mod device;
pub mod fr_gpu;
pub mod fr_ntt_gpu;
pub mod g1;
pub mod g1_gpu;
pub mod g2_gpu;
pub mod msm;
pub mod msm_gpu;
pub mod ntt;
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
