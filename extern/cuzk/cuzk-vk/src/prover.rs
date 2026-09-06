//! Vulkan Groth16 prover ABI. `cuzk-core` will grow into this once PCE buffers and production-scale MSM orchestration land.
//!
//! [`prove_groth16_partition`] runs **Milestone B integration smoke** when `CUZK_VK_SKIP_SMOKE=0`:
//! Fr NTT general round-trip (size from `circuit_log_n`, upper bound from `CUZK_VK_PARTITION_MAX_LOG`, default 6;
//! optional caller-supplied coefficients in [`VkGroth16Job::witness_ntt_coeffs`] — **B₂** witness→GPU slice),
//! MSM dispatch grid (**§8.4 D.1** mega-strip `groups_x` tiling via [`crate::msm::MsmBucketReduceDispatch::mega_dense_strip`]), **SRS-bound** G1 MSM
//! (decode `h[]` from [`crate::srs::srs_synthetic_partition_smoke_blob`], then
//! [`crate::split_msm::g1_msm_pippenger_gpu_batch_shared_bases`] — §8.4 **D.2** prototype: two scalar MSMs over the same decoded `h[]`
//! table vs `multi_exp`; Montgomery limbs for `h[]` built once on the host).
//! **SRS `b_g2[0]`** decode + **G2 limb staging** ([`crate::srs::srs_decode_bg2_g2`],
//! [`crate::srs_gpu::srs_g2_affine_gpu_reverse48_matches_cpu`]),
//! **H-quotient** GPU vs CPU ([`crate::h_term_gpu::run_fr_quotient_scalars_gpu`]), and
//! **H commit** `multi_exp` vs naive Σ `s_i·P_i` on decoded `h[]` bases.
//! Default CI (`CUZK_VK_SKIP_SMOKE=1`) keeps the lighter **n = 8** NTT + dispatch + tiny CPU H tick.
//! Optional **`CUZK_VK_BENCH_CSV`** (path): append one CSV sample per successful run ([`crate::bench_csv`]).
//! Optional **`CUZK_VK_BENCH_HARDWARE_MD`** (path): append a §1 markdown section (GPU paragraph + timings).
//! Optional **`CUZK_VK_BENCH_TAG`**: label included in that section (e.g. git SHA).
//! Optional **`CUZK_VK_BENCH_MAX_*_MS`** ceilings: see [`crate::bench_csv`] module docs.
//! **§8.3 C.2:** Vulkan smoke uses [`crate::srs_staging_gpu::SrsDeviceLocalUploadGuard`]: SRS staging→device-local is submitted before Fr NTT (no fence wait); [`SrsDeviceLocalUploadGuard::finish`](crate::srs_staging_gpu::SrsDeviceLocalUploadGuard::finish) runs after MSM.
//! [`crate::device::VulkanDevice::queue_compute_1`] routes the copy when present so transfer can overlap Fr NTT submits on the primary queue.
//! The device-local buffer is destroyed immediately after `finish`; SRS decode still reads host bytes until GPU consumers consume the blob.
//! Optional **`CUZK_VK_MSM_WINDOW_BITS`**: passed through [`crate::device_profile::msm_config_for_device`] and clamped for Pippenger via [`crate::device_profile::msm_config_for_pippenger_gpu`] (§8.1 A.1; benchmark markdown logs effective **`window_bits`**).
//! Vulkan smoke Fr NTT round-trip uses dedicated **`n = 8`** kernels when `n = 8`, **radix-8** when `n > 8`, else radix-4 (`n ∈ {2, 4}`).
//! Full Groth16 (pairing, bellperson-driven assignments, production SRS mmap) remains future work;
//! see `cuzk-vulkan-optimization-roadmap.md` **§3.1** step 6 and workspace `MILESTONE_B.md`.

use std::borrow::Cow;
use std::collections::HashMap;
use std::sync::{Arc, OnceLock};
use std::time::Instant;

use anyhow::{bail, Result};
use blstrs::{G1Affine, G1Projective, G2Affine, G2Projective, Scalar};
use ff::Field;
use group::Group;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

use crate::bench_csv::{
    append_partition_benchmark_csv, append_partition_hardware_md, bench_max_ms_from_env,
    duration_ms_exceeds_u64_ceiling, PartitionBenchRow,
};
use crate::device::{PhysicalDeviceInfo, VulkanDevice};
use crate::device_profile::msm_config_for_pippenger_gpu;
use crate::fr_ntt_general_gpu::{
    run_fr_ntt_general_roundtrip_gpu, run_fr_ntt_radix8_roundtrip_gpu,
};
use crate::fr_ntt_gpu::run_fr_ntt8_roundtrip_gpu;
use crate::h_term::fr_quotient_scalars_from_abc;
use crate::h_term_gpu::run_fr_quotient_scalars_gpu;
use crate::msm::{MegaMsmDenseStrip, MsmBucketReduceDispatch};
use crate::msm_gpu::{run_msm_dispatch_hitcount_smoke, MSM_DISPATCH_SMOKE_LOCAL_X};
use crate::ntt::{FrNttPlan, FrNttPlanError};
use crate::split_msm::g1_msm_pippenger_gpu_batch_shared_bases;
use crate::srs::{srs_decode_bg2_g2, srs_decode_h_g1, srs_synthetic_partition_smoke_blob};
use crate::srs_gpu::srs_g2_affine_gpu_reverse48_matches_cpu;
use crate::srs_staging_gpu::SrsDeviceLocalUploadGuard;

static SRS_PARTITION_SMOKE: OnceLock<Vec<u8>> = OnceLock::new();

/// Max `circuit_log_n` for the Fr NTT round-trip in [`prove_groth16_partition`] when Vulkan smoke is on.
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

/// Fr NTT round-trip for partition smoke: dedicated **`n = 8`** kernels when applicable (§8.2 **B.6** slice); **radix-8** when `n > 8`; radix-4 when `n ∈ {2, 4}`.
fn run_fr_ntt_partition_roundtrip_gpu(
    dev: &VulkanDevice,
    coeffs: &[Scalar],
) -> Result<Vec<Scalar>> {
    match coeffs.len() {
        8 => {
            let a: [Scalar; 8] = coeffs
                .try_into()
                .map_err(|_| anyhow::anyhow!("partition NTT: expected exactly 8 coefficients"))?;
            run_fr_ntt8_roundtrip_gpu(dev, &a).map(Vec::from)
        }
        n if n > 8 => run_fr_ntt_radix8_roundtrip_gpu(dev, coeffs),
        _ => run_fr_ntt_general_roundtrip_gpu(dev, coeffs),
    }
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
    /// **B₂ — witness → Vulkan (Fr NTT leg):** when `CUZK_VK_SKIP_SMOKE=0`, these coefficients are
    /// passed to the partition Fr NTT round-trip ([`prove_groth16_partition`]; `n = 8` uses [`crate::fr_ntt_gpu::run_fr_ntt8_roundtrip_gpu`], `n > 8` uses radix-8, else radix-4) instead of the default `(i+1)` pattern.
    /// Length must be exactly `2^L` where `L = min(circuit_log_n, CUZK_VK_PARTITION_MAX_LOG)` (clamped `1..=14`).
    /// When `None`, synthetic coefficients are used (backward compatible).
    pub witness_ntt_coeffs: Option<Vec<Scalar>>,
}

/// Wall-clock stages until [`VK_KHR_calibrated_timestamps`](https://registry.khronos.org/vulkan/specs/latest/man/html/VK_KHR_calibrated_timestamps.html) lands.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct VkProofTimings {
    pub total: std::time::Duration,
    /// Fr NTT on GPU (`n = 8` light path, or general `n` when `CUZK_VK_SKIP_SMOKE=0`).
    pub fr_ntt_gpu: std::time::Duration,
    pub msm_dispatch_grid: std::time::Duration,
    /// SRS `b_g2[0]` decode + [`crate::srs_gpu::srs_g2_affine_gpu_reverse48_matches_cpu`] (Vulkan smoke only).
    pub srs_g2_reverse48: Option<std::time::Duration>,
    /// SRS `h[]` decode + GPU Pippenger batch over shared `h[]` (`g1_msm_pippenger_gpu_batch_shared_bases`, Vulkan smoke only).
    pub g1_msm_bitplanes: Option<std::time::Duration>,
    /// `fr_quotient_scalars_from_abc` + GPU quotient + H-commit `multi_exp` check (Vulkan smoke only).
    pub h_term_smoke: Option<std::time::Duration>,
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

fn format_partition_bench_hardware_md(
    unix_ms: u128,
    tag: &str,
    pdev: &PhysicalDeviceInfo,
    job: &VkGroth16Job,
    partition_max_log: u32,
    vulkan_smoke: bool,
    t: &VkProofTimings,
) -> String {
    let tag_line = if tag.is_empty() {
        String::new()
    } else {
        format!(" (tag: {tag})")
    };
    let mut out = format!(
        "## partition bench unix_ms={}{}\n\n{}\n\n",
        unix_ms,
        tag_line,
        pdev.measurement_paragraph()
    );
    out.push_str(&format!(
        "Workload: Groth16 partition smoke; kind={:?}; circuit_log_n={}; partition_index={:?}; partition_ntt_cap_log={}; vulkan_smoke={}.\n\n",
        job.kind,
        job.circuit_log_n,
        job.partition_index,
        partition_max_log,
        vulkan_smoke
    ));
    let msm = msm_config_for_pippenger_gpu(pdev);
    out.push_str(&format!(
        "MSM window_bits={} for Pippenger GPU (`msm_config_for_pippenger_gpu`: env `CUZK_VK_MSM_WINDOW_BITS` / vendor default, capped at {}).\n\n",
        msm.window_bits,
        crate::ec::G1_PIPP_MAX_WINDOW_BITS,
    ));
    out.push_str("Wall-clock (ms): ");
    out.push_str(&format!(
        "total {:.3}; fr_ntt {:.3}; msm_grid {:.3}",
        t.total.as_secs_f64() * 1000.0,
        t.fr_ntt_gpu.as_secs_f64() * 1000.0,
        t.msm_dispatch_grid.as_secs_f64() * 1000.0,
    ));
    if let Some(d) = t.srs_g2_reverse48 {
        out.push_str(&format!("; srs_g2 {:.3}", d.as_secs_f64() * 1000.0));
    }
    if let Some(d) = t.g1_msm_bitplanes {
        out.push_str(&format!("; g1_msm {:.3}", d.as_secs_f64() * 1000.0));
    }
    if let Some(d) = t.h_term_smoke {
        out.push_str(&format!("; h_term {:.3}", d.as_secs_f64() * 1000.0));
    }
    out.push('.');
    out
}

fn enforce_partition_bench_ceilings(t: &VkProofTimings) -> Result<()> {
    let chk = |var: &'static str, label: &str, d: std::time::Duration| -> Result<()> {
        if let Some(max) = bench_max_ms_from_env(var) {
            if duration_ms_exceeds_u64_ceiling(d, max) {
                bail!(
                    "{label}: {}ms exceeds {}={}ms (partition bench ceiling; unset or raise for slow GPUs)",
                    d.as_millis(),
                    var,
                    max,
                );
            }
        }
        Ok(())
    };
    chk("CUZK_VK_BENCH_MAX_FR_NTT_MS", "fr_ntt_gpu", t.fr_ntt_gpu)?;
    chk(
        "CUZK_VK_BENCH_MAX_MSM_GRID_MS",
        "msm_dispatch_grid",
        t.msm_dispatch_grid,
    )?;
    if let Some(d) = t.srs_g2_reverse48 {
        chk("CUZK_VK_BENCH_MAX_SRS_G2_MS", "srs_g2_reverse48", d)?;
    }
    if let Some(d) = t.g1_msm_bitplanes {
        chk("CUZK_VK_BENCH_MAX_G1_MSM_MS", "g1_msm_bitplanes", d)?;
    }
    if let Some(d) = t.h_term_smoke {
        chk("CUZK_VK_BENCH_MAX_H_TERM_MS", "h_term_smoke", d)?;
    }
    chk("CUZK_VK_BENCH_MAX_TOTAL_MS", "total", t.total)?;
    Ok(())
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
pub fn prove_groth16_partition(
    ctx: &VkProverContext,
    job: &VkGroth16Job,
) -> Result<VkProofTimings> {
    let t0 = Instant::now();
    let vulkan_smoke = matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"));
    let pmax = partition_ntt_max_log();

    let mut srs_upload_guard = if vulkan_smoke {
        let srs = SRS_PARTITION_SMOKE.get_or_init(srs_synthetic_partition_smoke_blob);
        Some(SrsDeviceLocalUploadGuard::submit(
            &ctx.device,
            srs.as_slice(),
        )?)
    } else {
        None
    };

    let t_fr = Instant::now();
    if vulkan_smoke {
        let lg = job.circuit_log_n.clamp(1, pmax);
        let n = 1usize << lg;
        let coeffs: Cow<'_, [Scalar]> = match job.witness_ntt_coeffs.as_ref() {
            Some(w) if w.len() == n => Cow::Borrowed(w.as_slice()),
            Some(w) => {
                bail!(
                    "witness_ntt_coeffs: len {} != n={} (effective log_n={}; circuit_log_n={}, partition_max_log={})",
                    w.len(),
                    n,
                    lg,
                    job.circuit_log_n,
                    pmax
                );
            }
            None => Cow::Owned((0..n).map(|i| Scalar::from((i + 1) as u64)).collect()),
        };
        let round = run_fr_ntt_partition_roundtrip_gpu(&ctx.device, coeffs.as_ref())?;
        if round.as_slice() != coeffs.as_ref() {
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
    let fr_ntt_gpu = t_fr.elapsed();

    let t_msm = Instant::now();
    // §8.4 D.1: mega-strip X grid (`groups_x = batch · ceil(n/local_x)` sketch) + bucket-column `groups_y`,
    // capped at msm_dispatch_grid_smoke's 4096 threads (2 circuits × 2 workgroups × 64 × 16 buckets).
    let mega_strip = MegaMsmDenseStrip {
        groups_x_per_circuit: 2,
        batch_circuits: 2,
        local_x: MSM_DISPATCH_SMOKE_LOCAL_X,
    };
    let d = MsmBucketReduceDispatch::mega_dense_strip(mega_strip, 4);
    run_msm_dispatch_hitcount_smoke(&ctx.device, d)?;
    let msm_dispatch_grid = t_msm.elapsed();

    let mut srs_g2_reverse48 = None;
    let mut g1_msm_bitplanes = None;
    let mut h_term_smoke = None;

    if vulkan_smoke {
        let t_srs = Instant::now();
        let srs = SRS_PARTITION_SMOKE.get_or_init(srs_synthetic_partition_smoke_blob);
        let guard = srs_upload_guard
            .take()
            .expect("SRS GPU upload guard when vulkan_smoke");
        let buf = guard.finish()?;
        buf.destroy(&ctx.device);

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
        srs_g2_reverse48 = Some(t_srs.elapsed());

        let t_msm_b = Instant::now();
        // B₂ §8.1: full 255-bit scalar Pippenger MSM on GPU. Use the device-profile window size,
        // capped to the shader's maximum (w ≤ 16). Compare against `multi_exp` for n = 8 with
        // deterministic random scalars (exercises non-sparse bit patterns).
        let mut rng = ChaCha8Rng::seed_from_u64(0xc0ff_eecb_01_ab05);
        let scalars: Vec<Scalar> = (0..8).map(|_| Scalar::random(&mut rng)).collect();
        let scalars_b: Vec<Scalar> = (0..8).map(|_| Scalar::random(&mut rng)).collect();
        let msm_cfg = msm_config_for_pippenger_gpu(&ctx.device.physical_device_info());
        let window_bits = msm_cfg.window_bits;
        let gpu_pair = g1_msm_pippenger_gpu_batch_shared_bases(
            &ctx.device,
            &h_bases,
            &[scalars.as_slice(), scalars_b.as_slice()],
            window_bits,
        )?;
        let pts: Vec<G1Projective> = h_bases.iter().map(|p| G1Projective::from(*p)).collect();
        let refp = G1Projective::multi_exp(&pts, &scalars);
        let refp_b = G1Projective::multi_exp(&pts, &scalars_b);
        if gpu_pair[0] != refp {
            bail!(
                "SRS-bound G1 Pippenger MSM mismatch (GPU vs multi_exp, circuit 0, window_bits={window_bits})"
            );
        }
        if gpu_pair[1] != refp_b {
            bail!(
                "SRS-bound G1 Pippenger MSM mismatch (GPU vs multi_exp, circuit 1, window_bits={window_bits})"
            );
        }
        g1_msm_bitplanes = Some(t_msm_b.elapsed());

        let t_h = Instant::now();
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
        h_term_smoke = Some(t_h.elapsed());
    } else {
        let ha = vec![Scalar::from(3u64), Scalar::from(5u64)];
        let hb = vec![Scalar::from(7u64), Scalar::from(11u64)];
        let hc = vec![Scalar::from(13u64), Scalar::from(17u64)];
        let _h_scalars = fr_quotient_scalars_from_abc(&ha, &hb, &hc)?;
    }

    let total = t0.elapsed();
    let timings = VkProofTimings {
        total,
        fr_ntt_gpu,
        msm_dispatch_grid,
        srs_g2_reverse48,
        g1_msm_bitplanes,
        h_term_smoke,
    };

    enforce_partition_bench_ceilings(&timings)?;

    let bench_csv = std::env::var("CUZK_VK_BENCH_CSV")
        .ok()
        .filter(|s| !s.is_empty());
    let bench_hw_md = std::env::var("CUZK_VK_BENCH_HARDWARE_MD")
        .ok()
        .filter(|s| !s.is_empty());
    if bench_csv.is_some() || bench_hw_md.is_some() {
        let unix_ms = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_millis())
            .unwrap_or(0);
        let pdev = ctx.device.physical_device_info();
        if let Some(ref csv_path) = bench_csv {
            let row = PartitionBenchRow {
                unix_ms,
                circuit_log_n: job.circuit_log_n,
                partition_max_log: pmax,
                vulkan_smoke,
                fr_ntt_ms: timings.fr_ntt_gpu.as_secs_f64() * 1000.0,
                msm_grid_ms: timings.msm_dispatch_grid.as_secs_f64() * 1000.0,
                srs_g2_ms: timings.srs_g2_reverse48.map(|d| d.as_secs_f64() * 1000.0),
                g1_msm_ms: timings.g1_msm_bitplanes.map(|d| d.as_secs_f64() * 1000.0),
                h_term_ms: timings.h_term_smoke.map(|d| d.as_secs_f64() * 1000.0),
                total_ms: timings.total.as_secs_f64() * 1000.0,
                gpu_name: pdev.device_name.clone(),
                driver_version: pdev.driver_version.clone(),
                api_version: pdev.api_version.clone(),
                vendor_id: pdev.vendor_id,
                device_id: pdev.device_id,
            };
            let path = std::path::Path::new(csv_path);
            if let Err(e) = append_partition_benchmark_csv(path, &row) {
                eprintln!("cuzk-vk: CUZK_VK_BENCH_CSV append failed ({path:?}): {e}");
            }
        }
        if let Some(ref md_path) = bench_hw_md {
            let tag = std::env::var("CUZK_VK_BENCH_TAG").unwrap_or_default();
            let section = format_partition_bench_hardware_md(
                unix_ms,
                tag.trim(),
                &pdev,
                job,
                pmax,
                vulkan_smoke,
                &timings,
            );
            let path = std::path::Path::new(md_path);
            if let Err(e) = append_partition_hardware_md(path, &section) {
                eprintln!("cuzk-vk: CUZK_VK_BENCH_HARDWARE_MD append failed ({path:?}): {e}");
            }
        }
    }

    Ok(timings)
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
            witness_ntt_coeffs: None,
        };
        let t = prove_groth16_partition(&ctx, &job).expect("partition smoke");
        assert!(t.total.as_nanos() > 0);
        assert!(t.fr_ntt_gpu.as_nanos() > 0);
        assert!(t.msm_dispatch_grid.as_nanos() > 0);
        assert!(t.srs_g2_reverse48.is_some());
        assert!(t.g1_msm_bitplanes.is_some());
        assert!(t.h_term_smoke.is_some());
    }
}
