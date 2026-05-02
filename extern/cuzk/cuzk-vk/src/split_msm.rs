//! Phase H — split MSM orchestration (SupraSeal `groth16_split_msm.cu`).
//!
//! CUDA keeps a **per-circuit** `bit_vector` of `u64` chunks over bases (`CHUNK_BITS = 64`) plus
//! **tail** scalars for a CPU Pippenger pass. Here we land a **correctness slice**: MSM as a sum
//! of **binary digit planes** using the Phase G GPU bitmap batch sum ([`crate::g1_batch_gpu`]) plus
//! host-side `2^b` scaling. This matches integer MSM when scalars are small `u64` values
//! (`< 2^max_bits`, `max_bits ≤ 64`, `n ≤ 64`).
//!
//! **Milestone B₂ §8.1 (first rung):** [`g1_msm_bitplanes_scalars_trunc_gpu_host`] accepts full
//! [`Scalar`] operands by truncating canonical LE bits to `max_bits` (see
//! [`crate::scalar_limbs::scalar_low_u64_canonical`]). **Still open:** 255-bit window buckets +
//! GPU Pippenger tail (§8.1 A.4–A.7).

use anyhow::{Context, Result};
use blstrs::{G1Affine, G1Projective, Scalar};
use ff::PrimeField;
use group::Group;

use crate::device::VulkanDevice;
use crate::ec::g1_projective_from_jacobian_limbs;
use crate::g1_batch_gpu::run_g1_batch_jacobian_accum_bitmap_gpu;
use crate::scalar_limbs::scalar_low_u64_canonical;

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

/// Reference MSM with coefficients from truncated canonical scalar bits (same semantics as
/// [`g1_msm_bitplanes_scalars_trunc_gpu_host`]).
pub fn g1_msm_scalars_trunc_reference(
    bases: &[G1Affine],
    scalars: &[Scalar],
    n: usize,
    max_bits: u32,
) -> G1Projective {
    debug_assert!(max_bits <= 64 && max_bits > 0);
    let mut acc = G1Projective::identity();
    for i in 0..n {
        let k = scalar_low_u64_canonical(&scalars[i], max_bits);
        acc += G1Projective::from(bases[i]) * Scalar::from(k);
    }
    acc
}

/// [`g1_msm_bitplanes_u64_gpu_host`] with scalars supplied as [`Scalar`]; each coefficient is
/// `scalar_low_u64_canonical(s, max_bits)` (see [`crate::scalar_limbs::scalar_low_u64_canonical`]).
///
/// Requires `n ≤ 64`, `1 ≤ max_bits ≤ 64`, and (implicitly) that the truncated integers fit the
/// bit-plane path’s `< 2^max_bits` bound (always true after masking).
pub fn g1_msm_bitplanes_scalars_trunc_gpu_host(
    dev: &VulkanDevice,
    bases: &[G1Affine],
    scalars: &[Scalar],
    n: usize,
    max_bits: u32,
) -> Result<G1Projective> {
    anyhow::ensure!(max_bits >= 1 && max_bits <= 64);
    anyhow::ensure!(n <= 64);
    anyhow::ensure!(bases.len() >= n && scalars.len() >= n);
    let mut u = vec![0u64; n];
    for i in 0..n {
        u[i] = scalar_low_u64_canonical(&scalars[i], max_bits);
    }
    g1_msm_bitplanes_u64_gpu_host(dev, bases, &u, n, max_bits)
}

/// Full 255-bit scalar Pippenger MSM on the GPU (§8.1 A.4–A.7).
///
/// Delegates to [`crate::g1_pippenger_bucket_gpu::run_g1_pippenger_msm_gpu`]; see that module
/// for window-size constraints. `window_bits` is taken from the device profile by the caller.
pub fn g1_msm_pippenger_gpu(
    dev: &crate::device::VulkanDevice,
    bases: &[blstrs::G1Affine],
    scalars: &[blstrs::Scalar],
    n: usize,
    window_bits: u32,
) -> anyhow::Result<G1Projective> {
    crate::g1_pippenger_bucket_gpu::run_g1_pippenger_msm_gpu(dev, bases, scalars, n, window_bits)
}

/// Unsigned fixed-width windows over the **low 255** scalar bits (LSB-first digits). Each entry is in
/// `0 .. (1 << window_bits)`. Length = `ceil(255 / window_bits)` — **B₂ §8.1** bucket-index reference for
/// future Pippenger / full-width bucket MSM shaders.
pub fn fr_scalar_unsigned_windows(scalar: &Scalar, window_bits: u32) -> Vec<u32> {
    assert!(window_bits >= 1 && window_bits <= 31);
    let repr = scalar.to_repr();
    let bytes = repr.as_ref();
    let wb = window_bits as usize;
    let mut out = Vec::new();
    let mut bit_idx = 0usize;
    while bit_idx < 255 {
        let mut v = 0u32;
        for i in 0..wb {
            if bit_idx + i < 255 {
                let b = (bit_idx + i) / 8;
                let k = (bit_idx + i) % 8;
                if b < bytes.len() && (bytes[b] >> k) & 1 != 0 {
                    v |= 1u32 << i;
                }
            }
        }
        out.push(v);
        bit_idx += wb;
    }
    out
}

#[cfg(test)]
mod fr_scalar_window_tests {
    use super::*;

    #[test]
    fn fr_scalar_windows_small_value() {
        let s = Scalar::from(8u64);
        let w = fr_scalar_unsigned_windows(&s, 4);
        assert_eq!(w.first().copied().unwrap() & 0xf, 8);
    }

    #[test]
    fn fr_scalar_windows_reconstruct_low_bits() {
        let s = Scalar::from(0x12345u64);
        let w = fr_scalar_unsigned_windows(&s, 8);
        let mut acc = 0u64;
        let mut shift = 0u32;
        for &digit in w.iter().take(8) {
            acc |= (digit as u64) << shift;
            shift += 8;
        }
        assert_eq!(acc, 0x12345u64);
    }
}
