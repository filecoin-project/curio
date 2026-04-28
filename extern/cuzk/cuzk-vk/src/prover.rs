//! Vulkan Groth16 prover ABI. `cuzk-core` will grow into this once SRS + PCE buffers are bound.
//!
//! [`prove_groth16_partition`] runs **bounded GPU integration smoke**: when `CUZK_VK_SKIP_SMOKE=0`,
//! Fr NTT **general** round-trip for `n = 2^min(max(circuit_log_n,1), 6)` plus MSM dispatch grid;
//! when smoke is skipped (default CI), fixed **n = 8** NTT round-trip. Also runs a tiny **H-quotient**
//! CPU tick ([`crate::h_term::fr_quotient_scalars_from_abc`]);
//! it does **not** execute full Groth16 (SRS upload, bucket MSM, pairing). Bit-plane MSM lives in
//! [`crate::split_msm`]; SRS layout in [`crate::srs`]; H NTT composition on GPU in [`crate::h_term_gpu`].
//! See `cuzk-vulkan-optimization-roadmap.md` §3.1.

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;

use anyhow::{bail, Result};
use blstrs::Scalar;

use crate::device::VulkanDevice;
use crate::fr_ntt_general_gpu::run_fr_ntt_general_roundtrip_gpu;
use crate::fr_ntt_gpu::run_fr_ntt8_roundtrip_gpu;
use crate::h_term::fr_quotient_scalars_from_abc;
use crate::msm::MsmBucketReduceDispatch;
use crate::msm_gpu::{run_msm_dispatch_hitcount_smoke, MSM_DISPATCH_SMOKE_LOCAL_X};
use crate::ntt::{FrNttPlan, FrNttPlanError};

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

/// Integration smoke: GPU Fr NTT **n = 8** round-trip + MSM dispatch hit-count vs host sizing.
///
/// `job` is accepted for ABI stability; fields are not yet used to select kernels. When
/// `circuit_log_n` and SRS land, this entry point will grow into the real partition driver.
pub fn prove_groth16_partition(ctx: &VkProverContext, job: &VkGroth16Job) -> Result<VkProofTimings> {
    let t0 = Instant::now();
    let vulkan_smoke = matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"));
    if vulkan_smoke {
        let lg = job.circuit_log_n.clamp(1, 6);
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
    // Phase J / K bridge: H-quotient scalar path (CPU) exercised on every partition call.
    let ha = vec![Scalar::from(3u64), Scalar::from(5u64)];
    let hb = vec![Scalar::from(7u64), Scalar::from(11u64)];
    let hc = vec![Scalar::from(13u64), Scalar::from(17u64)];
    let _h_scalars = fr_quotient_scalars_from_abc(&ha, &hb, &hc)?;
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
