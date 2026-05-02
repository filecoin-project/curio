//! GPU **single-window bucket sums** for unsigned 4-bit digits.
//!
//! Each bucket holds Σ P_i for digit `i == b`; combine on the host with
//! [`crate::g1_msm_bucket::g1_combine_single_window_bucket_sums`] (full-width MSM stacks
//! windows; this is one correctness slice).
//!
//! The original `g1_bucket_sum_window4_n32_tail.comp` shader used `inout @@FP_T@@` struct
//! parameters, `fp_is_zero`/`fp_eq` short-circuits inside the addition body, and a 16-thread
//! workgroup that all walked the same digit array. Naga + MoltenVK on Apple-M2 miscompiles
//! that pattern (same root cause as the G1/G2 batch accumulation kernels — see the comments
//! in `g1_jacobian_add108_tail.comp` and `g2_batch_gpu.rs`). Instead, this implementation
//! drives the bucket reduction from the host using the per-add Jacobian kernel
//! ([`run_g1_jacobian_add_gpu`]), which is a straight-line GLSL shader and works reliably.
//! With `n ≤ 32` and 16 buckets, that is at most 32 GPU adds per call.

use anyhow::Result;
use blstrs::{G1Affine, G1Projective, Scalar};
use group::Group;

use crate::device::VulkanDevice;
use crate::ec::{
    g1_jacobian_limbs_from_projective, g1_projective_from_jacobian_limbs, G1JacobianLimbs,
    G1_BUCKET_MSM_BUCKETS, G1_BUCKET_MSM_MAX_POINTS,
};
use crate::g1::BLS12_381_FP_U32_LIMBS;
use crate::g1_ec_gpu::run_g1_jacobian_add_gpu;
use crate::g1_msm_bucket::g1_combine_single_window_bucket_sums;

fn jac_identity() -> G1JacobianLimbs {
    G1JacobianLimbs {
        x: [0u32; BLS12_381_FP_U32_LIMBS],
        y: [0u32; BLS12_381_FP_U32_LIMBS],
        z: [0u32; BLS12_381_FP_U32_LIMBS],
    }
}

/// `digits[i]` in `0..16`, `n ≤ 32`. Returns the same single-window MSM as CPU bucket combine.
pub fn run_g1_bucket_sum_window4_gpu(
    dev: &VulkanDevice,
    bases: &[G1Affine],
    digits: &[u32],
    n: usize,
) -> Result<G1Projective> {
    anyhow::ensure!(n > 0 && n <= G1_BUCKET_MSM_MAX_POINTS);
    anyhow::ensure!(bases.len() >= n && digits.len() >= n);
    for &d in &digits[..n] {
        anyhow::ensure!(d < G1_BUCKET_MSM_BUCKETS as u32);
    }

    let mut buckets = vec![G1Projective::identity(); G1_BUCKET_MSM_BUCKETS];
    for b in 0..G1_BUCKET_MSM_BUCKETS as u32 {
        let mut acc = jac_identity();
        for i in 0..n {
            if digits[i] != b {
                continue;
            }
            let p = g1_jacobian_limbs_from_projective(&G1Projective::from(bases[i]));
            acc = run_g1_jacobian_add_gpu(dev, &acc, &p)?;
        }
        buckets[b as usize] = g1_projective_from_jacobian_limbs(&acc);
    }
    Ok(g1_combine_single_window_bucket_sums(4, &buckets)
        .ok_or_else(|| anyhow::anyhow!("g1_combine_single_window_bucket_sums"))?)
}

/// CPU reference for the same window (digits equal scalar values in `0..16`).
pub fn g1_bucket_window_msm_cpu(bases: &[G1Affine], digits: &[u32], n: usize) -> G1Projective {
    let mut acc = G1Projective::identity();
    for i in 0..n {
        acc += G1Projective::from(bases[i]) * Scalar::from(digits[i] as u64);
    }
    acc
}
