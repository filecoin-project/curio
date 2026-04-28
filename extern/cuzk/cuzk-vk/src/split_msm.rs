//! Phase H — split MSM orchestration (SupraSeal `groth16_split_msm.cu`).
//!
//! CUDA keeps a **per-circuit** `bit_vector` of `u64` chunks over bases (`CHUNK_BITS = 64`) plus
//! **tail** scalars for a CPU Pippenger pass. Here we land a **correctness slice**: MSM as a sum
//! of **binary digit planes** using the Phase G GPU bitmap batch sum ([`crate::g1_batch_gpu`]) plus
//! host-side `2^b` scaling. This matches integer MSM when scalars are small `u64` values
//! (`< 2^max_bits`, `max_bits ≤ 64`, `n ≤ 64`).

use anyhow::{Context, Result};
use blstrs::{G1Affine, G1Projective, Scalar};
use group::Group;

use crate::device::VulkanDevice;
use crate::ec::g1_projective_from_jacobian_limbs;
use crate::g1_batch_gpu::run_g1_batch_jacobian_accum_bitmap_gpu;

/// Reference MSM for `n` affine bases and **integer** scalars `< 2^max_bits` (same semantics as the
/// bit-plane GPU path). Uses `G1Projective` accumulation (CPU).
pub fn g1_msm_small_u64_reference(bases: &[G1Affine], scalars_u64: &[u64], n: usize, max_bits: u32) -> G1Projective {
    debug_assert!(max_bits <= 64);
    let mut acc = G1Projective::identity();
    let mask = if max_bits == 64 {
        u64::MAX
    } else {
        (1u64 << max_bits) - 1
    };
    for i in 0..n {
        let k = scalars_u64[i] & mask;
        acc += G1Projective::from(bases[i]) * Scalar::from(k);
    }
    acc
}

/// Sanity: [`g1_msm_small_u64_reference`] matches [`G1Projective::multi_exp`] for the same bases and
/// scalars truncated to `max_bits` (independent of the GPU bit-plane path).
pub fn g1_msm_small_u64_agrees_with_multi_exp(
    bases: &[G1Affine],
    scalars_u64: &[u64],
    n: usize,
    max_bits: u32,
) -> bool {
    let refp = g1_msm_small_u64_reference(bases, scalars_u64, n, max_bits);
    let mask = if max_bits == 64 {
        u64::MAX
    } else {
        (1u64 << max_bits) - 1
    };
    let pts: Vec<G1Projective> = (0..n).map(|i| G1Projective::from(bases[i])).collect();
    let sc: Vec<Scalar> = (0..n).map(|i| Scalar::from(scalars_u64[i] & mask)).collect();
    refp == G1Projective::multi_exp(&pts, &sc)
}

/// MSM via GPU bitmap Jacobian batch per bit + host combine (`Σ 2^b · (Σ_{i: k_i,b=1} P_i)`).
///
/// Requires `n ≤ 64`, `max_bits ≤ 64`, and each `scalars_u64[i] < 2^max_bits`.
pub fn g1_msm_bitplanes_u64_gpu_host(
    dev: &VulkanDevice,
    bases: &[G1Affine],
    scalars_u64: &[u64],
    n: usize,
    max_bits: u32,
) -> Result<G1Projective> {
    anyhow::ensure!(n <= 64);
    anyhow::ensure!(max_bits <= 64);
    anyhow::ensure!(bases.len() >= n && scalars_u64.len() >= n);
    let cap = if max_bits == 64 {
        u64::MAX
    } else {
        (1u64 << max_bits) - 1
    };
    for i in 0..n {
        anyhow::ensure!(
            scalars_u64[i] <= cap,
            "scalar[{i}] exceeds 2^{max_bits}-1",
        );
    }

    let mut acc = G1Projective::identity();
    for bit in 0..max_bits {
        let mut mask = 0u64;
        for i in 0..n {
            if (scalars_u64[i] >> bit) & 1 != 0 {
                mask |= 1u64 << i;
            }
        }
        let jac = run_g1_batch_jacobian_accum_bitmap_gpu(dev, n, mask, bases)
            .with_context(|| format!("GPU batch accum bit {bit}"))?;
        let plane = g1_projective_from_jacobian_limbs(&jac);
        let two = Scalar::from(1u64 << bit);
        acc += plane * two;
    }
    Ok(acc)
}
