//! Vulkan Groth16 prover ABI. `cuzk-core` will grow into this once PCE buffers and full bucket MSM land.
//!
//! [`prove_groth16_partition`] runs **Milestone B integration smoke** when `CUZK_VK_SKIP_SMOKE=0`:
//! Fr NTT general round-trip (size from `circuit_log_n`, upper bound from `CUZK_VK_PARTITION_MAX_LOG`, default 6),
//! MSM dispatch grid, **SRS-bound** G1 MSM
//! (decode `h[]` from [`crate::srs::srs_synthetic_partition_smoke_blob`], bit-plane path in
//! [`crate::split_msm`]), **SRS `b_g2[0]`** decode + **G2 limb staging** ([`crate::srs::srs_decode_bg2_g2`],
//! [`crate::srs_gpu::srs_g2_affine_gpu_reverse48_matches_cpu`]),
//! **H-quotient** GPU vs CPU ([`crate::h_term_gpu::run_fr_quotient_scalars_gpu`]), and
//! **H commit** `multi_exp` vs naive Σ `s_i·P_i` on decoded `h[]` bases.
//! Default CI (`CUZK_VK_SKIP_SMOKE=1`) keeps the lighter **n = 8** NTT + dispatch + tiny CPU H tick.
//! Full Groth16 (pairing, bellperson-driven assignments, production SRS mmap) remains future work;
//! see `cuzk-vulkan-optimization-roadmap.md` **§3.1** step 6 and **Milestone B**.

use std::collections::HashMap;
use std::sync::{Arc, OnceLock};
use std::time::Instant;

use anyhow::{bail, Result};
use blstrs::{G1Affine, G1Projective, G2Affine, G2Projective, Scalar};
use group::Group;

use crate::device::VulkanDevice;
use crate::fr_ntt_general_gpu::run_fr_ntt_general_roundtrip_gpu;
use crate::fr_ntt_gpu::run_fr_ntt8_roundtrip_gpu;
use crate::h_term::fr_quotient_scalars_from_abc;
use crate::h_term_gpu::run_fr_quotient_scalars_gpu;
use crate::msm::MsmBucketReduceDispatch;
use crate::msm_gpu::{run_msm_dispatch_hitcount_smoke, MSM_DISPATCH_SMOKE_LOCAL_X};
use crate::ntt::{FrNttPlan, FrNttPlanError};
use crate::split_msm::{g1_msm_bitplanes_u64_gpu_host, g1_msm_small_u64_reference};
use crate::srs::{srs_decode_bg2_g2, srs_decode_h_g1, srs_synthetic_partition_smoke_blob};
use crate::srs_gpu::srs_g2_affine_gpu_reverse48_matches_cpu;

static SRS_PARTITION_SMOKE: OnceLock<Vec<u8>> = OnceLock::new();

/// Max `circuit_log_n` for the Fr NTT general round-trip in [`prove_groth16_partition`] when Vulkan smoke is on.
/// Default **6** (`n = 64`). Set **`CUZK_VK_PARTITION_MAX_LOG`** to override (parsed as `u32`, clamped to `1..=14`).
fn partition_ntt_max_log() -> u32 {
    const DEF: u32 = 6;
    const CAP: u32 = 14;
    std::env::var("CUZK_VK_PARTITION_MAX_LOG")
        .ok()
        .and_then(|s| s.parse::<u32>().ok())
        .unwrap_or(DEF)
        .clamp(1, CAP)
}

/// Logical proof kind the daemon already distinguishes (subset mirrored here for ABI stability).
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum VkProofKind {
    #[default]
    PoRepC2,
    WinningPost,
    WindowPost,
    SnapDeals,
}

/// Host-side view of one partition worth of proving work for the Vulkan backend.
#[derive(Clone, Debug, Default)]
pub struct VkGroth16Job {
    pub kind: VkProofKind,
    pub circuit_log_n: u32,
    pub partition_index: Option<u32>,
}

/// Timings until calibrated timestamps land (`VK_KHR_calibrated_timestamps`).
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct VkProofTimings {
    pub total: std::time::Duration,
}

/// Long-lived Vulkan proving context (device + future pipeline caches).
#[derive(Clone)]
pub struct VkProverContext {
    pub device: Arc<VulkanDevice>,
}

impl VkProverContext {
    pub fn new(device: Arc<VulkanDevice>) -> Self {
        Self { device }
    }

    /// Precompute Fr NTT roots / stage twiddles for `n = 2^log_n` (host-side). GPU uses this for
    /// scheduling; **n = 8** forward + inverse NTT run on-device ([`crate::fr_ntt_gpu`]).
    pub fn fr_ntt_plan(&self, log_n: u32) -> Result<FrNttPlan, FrNttPlanError> {
        let _ = &self.device;
        FrNttPlan::try_new(log_n)
    }

    /// Short-lived session with cached host-side NTT plans keyed by `log_n`.
    pub fn session(&self) -> VkProverSession<'_> {
        VkProverSession {
            ctx: self,
            fr_ntt: HashMap::new(),
        }
    }
}

/// Per-job or per-thread scratch: reuse `FrNttPlan` allocations without a global lock.
pub struct VkProverSession<'a> {
    ctx: &'a VkProverContext,
    fr_ntt: HashMap<u32, FrNttPlan>,
}

impl VkProverSession<'_> {
    /// Returns a cached plan or builds it once for this session.
    pub fn warm_fr_ntt(&mut self, log_n: u32) -> Result<&FrNttPlan, FrNttPlanError> {
        use std::collections::hash_map::Entry;
        match self.fr_ntt.entry(log_n) {
            Entry::Occupied(e) => Ok(e.into_mut()),
            Entry::Vacant(v) => {
                let _ = &self.ctx.device;
                let plan = FrNttPlan::try_new(log_n)?;
                Ok(v.insert(plan))
            }
        }
    }
}

/// Partition driver smoke: Fr NTT round-trip + MSM dispatch + (when Vulkan smoke is on) SRS-bound
/// G1 MSM + H quotient GPU vs CPU.
pub fn prove_groth16_partition(ctx: &VkProverContext, job: &VkGroth16Job) -> Result<VkProofTimings> {
    let t0 = Instant::now();
    let vulkan_smoke = matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"));
    if vulkan_smoke {
        let lg = job.circuit_log_n.clamp(1, partition_ntt_max_log());
        let n = 1usize << lg;
        let coeffs: Vec<Scalar> = (0..n).map(|i| Scalar::from((i + 1) as u64)).collect();
        let round = run_fr_ntt_general_roundtrip_gpu(&ctx.device, &coeffs)?;
        if round != coeffs {
            bail!(
                "GPU Fr NTT general round-trip mismatch (log_n={}, n={})",
                lg,
                n
            );
        }
    } else {
        let input: [Scalar; 8] = std::array::from_fn(|i| Scalar::from((i + 1) as u64));
        let round = run_fr_ntt8_roundtrip_gpu(&ctx.device, &input)?;
        if round != input {
            bail!("GPU Fr NTT n=8 round-trip mismatch");
        }
    }
    let d = MsmBucketReduceDispatch::dense(100, 4, MSM_DISPATCH_SMOKE_LOCAL_X);
    run_msm_dispatch_hitcount_smoke(&ctx.device, d)?;
    if vulkan_smoke {
        let srs = SRS_PARTITION_SMOKE.get_or_init(srs_synthetic_partition_smoke_blob);
        let mut h_bases = [G1Affine::default(); 8];
        for i in 0..8 {
            h_bases[i] = srs_decode_h_g1(srs, i)?;
        }
        let bg2_0 = srs_decode_bg2_g2(srs, 0)?;
        let want_bg2 = G2Affine::from(G2Projective::generator() * Scalar::from(201u64));
        if bg2_0 != want_bg2 {
            bail!("SRS synthetic b_g2[0] decode mismatch (partition smoke)");
        }
        srs_g2_affine_gpu_reverse48_matches_cpu(&ctx.device, &bg2_0)?;
        let scalars_u64: [u64; 8] = [1, 2, 3, 4, 5, 6, 7, 8];
        let refp = g1_msm_small_u64_reference(&h_bases, &scalars_u64, 8, 8);
        let gpu_msm = g1_msm_bitplanes_u64_gpu_host(&ctx.device, &h_bases, &scalars_u64, 8, 8)?;
        if refp != gpu_msm {
            bail!("SRS-bound G1 bit-plane MSM mismatch (CPU ref vs GPU)");
        }
        let ha: Vec<Scalar> = (0..8).map(|i| Scalar::from(i as u64 + 1)).collect();
        let hb: Vec<Scalar> = (0..8).map(|i| Scalar::from(10u64 + i as u64)).collect();
        let hc: Vec<Scalar> = (0..8).map(|i| Scalar::from(100u64 + i as u64)).collect();
        let h_cpu = fr_quotient_scalars_from_abc(&ha, &hb, &hc)?;
        let h_gpu = run_fr_quotient_scalars_gpu(&ctx.device, &ha, &hb, &hc)?;
        if h_cpu != h_gpu {
            bail!("H quotient GPU vs CPU mismatch in partition smoke");
        }
        let h_pts: Vec<G1Projective> = h_bases[..h_cpu.len()]
            .iter()
            .map(|p| G1Projective::from(*p))
            .collect();
        let h_commit = G1Projective::multi_exp(&h_pts, &h_cpu);
        let mut h_naive = G1Projective::identity();
        for i in 0..h_cpu.len() {
            h_naive += G1Projective::from(h_bases[i]) * h_cpu[i];
        }
        if h_commit != h_naive {
            bail!("H-commit multi_exp vs naive scalar loop mismatch (SRS bases × H scalars)");
        }
    } else {
        let ha = vec![Scalar::from(3u64), Scalar::from(5u64)];
        let hb = vec![Scalar::from(7u64), Scalar::from(11u64)];
        let hc = vec![Scalar::from(13u64), Scalar::from(17u64)];
        let _h_scalars = fr_quotient_scalars_from_abc(&ha, &hb, &hc)?;
    }
    Ok(VkProofTimings {
        total: t0.elapsed(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn prover_session_reuses_fr_ntt_plan() {
        let dev = match VulkanDevice::new() {
            Ok(d) => Arc::new(d),
            Err(_) => return,
        };
        let ctx = VkProverContext::new(dev);
        let mut s = ctx.session();
        let p1 = s.warm_fr_ntt(6).expect("plan") as *const FrNttPlan;
        let p2 = s.warm_fr_ntt(6).expect("plan") as *const FrNttPlan;
        assert_eq!(p1, p2);
    }

    #[test]
    fn prove_partition_smoke_matches_roundtrip() {
        if !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0")) {
            return;
        }
        let dev = Arc::new(VulkanDevice::new().expect("Vulkan init"));
        let ctx = VkProverContext::new(dev);
        let job = VkGroth16Job {
            kind: VkProofKind::PoRepC2,
            circuit_log_n: 3,
            partition_index: None,
        };
        let t = prove_groth16_partition(&ctx, &job).expect("partition smoke");
        assert!(t.total.as_nanos() > 0);
    }
}
