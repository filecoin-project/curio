//! GPU **Pippenger bucket accumulate** — §8.1 full-width scalar MSM.
//!
//! [`run_g1_pippenger_window_gpu`] dispatches `g1_pippenger_bucket_acc_tail.comp`: one
//! GPU thread per bucket iterates all N affine points and accumulates those whose digit
//! matches. The shader reuses the same proven straight-line EFD pattern as
//! `g1_batch_accum_bitmap1636_tail.comp` (no `inout @@FP_T@@`, no `bool`-returning helpers,
//! mode-select instead of early return).
//!
//! [`run_g1_pippenger_msm_gpu`] drives the full Pippenger multiexp: for each scalar window
//! extract unsigned digits, call the GPU bucket phase, then combine on the host via
//! [`crate::g1_msm_bucket::g1_combine_single_window_bucket_sums`] +
//! [`crate::g1_msm_bucket::g1_combine_pippenger_windows`].

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::{G1Affine, G1Projective, Scalar};
use group::Group;

use crate::device::VulkanDevice;
use crate::ec::{
    g1_affine_montgomery_limbs, g1_projective_from_jacobian_limbs, G1JacobianLimbs,
    G1_PIPP_LOCAL_X, G1_PIPP_MAX_BUCKET_COUNT, G1_PIPP_MAX_N, G1_PIPP_OFF_AFF,
    G1_PIPP_OFF_BUCKET_COUNT, G1_PIPP_OFF_BUCKET_JAC, G1_PIPP_OFF_DIG, G1_PIPP_OFF_N,
    G1_PIPP_SSBO_BYTES,
};
use crate::g1::BLS12_381_FP_U32_LIMBS;
use crate::g1_msm_bucket::{g1_combine_pippenger_windows, g1_combine_single_window_bucket_sums};
use crate::split_msm::fr_scalar_unsigned_windows;
use crate::vk_oneshot;

/// Aligned buffer size: SSBO rounded up to 4 KiB page.
const BUF_SIZE: u64 = ((G1_PIPP_SSBO_BYTES as u64) + 4095) & !4095;

#[inline]
fn put_u32(buf: &mut [u8], word: usize, v: u32) {
    let o = word * 4;
    buf[o..o + 4].copy_from_slice(&v.to_le_bytes());
}

#[inline]
fn put_fp12(buf: &mut [u8], word: usize, limbs: &[u32; BLS12_381_FP_U32_LIMBS]) {
    for i in 0..BLS12_381_FP_U32_LIMBS {
        put_u32(buf, word + i, limbs[i]);
    }
}

#[inline]
fn get_fp12(buf: &[u8], word: usize) -> [u32; BLS12_381_FP_U32_LIMBS] {
    let mut out = [0u32; BLS12_381_FP_U32_LIMBS];
    for i in 0..BLS12_381_FP_U32_LIMBS {
        let o = (word + i) * 4;
        out[i] = u32::from_le_bytes(buf[o..o + 4].try_into().unwrap());
    }
    out
}

/// Run one Pippenger window on the GPU.
///
/// `bases[0..n]` are affine G1 points (BLS12-381), `digits[0..n]` are the unsigned window
/// digits in `0..bucket_count` for this window. Returns a `Vec<G1JacobianLimbs>` of length
/// `bucket_count` where entry `b` holds `Σ_{i: digits[i]==b} bases[i]` in Jacobian form.
///
/// Requires `n ≤ G1_PIPP_MAX_N` and `2 ≤ bucket_count ≤ G1_PIPP_MAX_BUCKET_COUNT`.
pub fn run_g1_pippenger_window_gpu(
    dev: &VulkanDevice,
    bases: &[G1Affine],
    digits: &[u32],
    n: usize,
    bucket_count: usize,
) -> Result<Vec<G1JacobianLimbs>> {
    anyhow::ensure!(
        n > 0 && n <= G1_PIPP_MAX_N,
        "n={n} exceeds G1_PIPP_MAX_N={G1_PIPP_MAX_N}"
    );
    anyhow::ensure!(
        bucket_count >= 2 && bucket_count <= G1_PIPP_MAX_BUCKET_COUNT,
        "bucket_count={bucket_count} out of [2, G1_PIPP_MAX_BUCKET_COUNT]"
    );
    anyhow::ensure!(bases.len() >= n && digits.len() >= n);

    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g1_pippenger_bucket_acc.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice()))
        .context("read_spv g1_pippenger_bucket_acc")?;

    let mut wbytes = vec![0u8; G1_PIPP_SSBO_BYTES];

    put_u32(&mut wbytes, G1_PIPP_OFF_N, n as u32);
    put_u32(&mut wbytes, G1_PIPP_OFF_BUCKET_COUNT, bucket_count as u32);

    // Write digits.
    for (i, &d) in digits[..n].iter().enumerate() {
        put_u32(&mut wbytes, G1_PIPP_OFF_DIG + i, d);
    }

    // Write affine points (x||y, each 12 u32 Montgomery words).
    for (i, base) in bases[..n].iter().enumerate() {
        let (x, y) = g1_affine_montgomery_limbs(base);
        let off = G1_PIPP_OFF_AFF + i * 2 * BLS12_381_FP_U32_LIMBS;
        put_fp12(&mut wbytes, off, &x);
        put_fp12(&mut wbytes, off + BLS12_381_FP_U32_LIMBS, &y);
    }

    // Read back the full SSBO (bucket Jacobians are at G1_PIPP_OFF_BUCKET_JAC).
    let mut rbytes = vec![0u8; G1_PIPP_SSBO_BYTES];
    let groups_x = (bucket_count as u32).div_ceil(G1_PIPP_LOCAL_X);

    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_SIZE,
            BUF_SIZE,
            (groups_x, 1, 1),
            &wbytes,
            G1_PIPP_SSBO_BYTES,
            &mut rbytes,
            None,
        )?;
    }

    let jac_words = 3 * BLS12_381_FP_U32_LIMBS;
    let mut buckets = Vec::with_capacity(bucket_count);
    for b in 0..bucket_count {
        let off = G1_PIPP_OFF_BUCKET_JAC + b * jac_words;
        buckets.push(G1JacobianLimbs {
            x: get_fp12(&rbytes, off),
            y: get_fp12(&rbytes, off + 12),
            z: get_fp12(&rbytes, off + 24),
        });
    }
    Ok(buckets)
}

/// Full 255-bit scalar Pippenger MSM on the GPU.
///
/// Uses unsigned `window_bits`-wide windows over the 255 canonical scalar bits (LSB-first).
/// For each window: extracts digits, dispatches [`run_g1_pippenger_window_gpu`], combines
/// bucket sums on the host with [`g1_combine_single_window_bucket_sums`]. Final result is
/// assembled via [`g1_combine_pippenger_windows`] (Horner: `Σ_w result_w · 2^(w·W)`).
///
/// Requires `n ≤ G1_PIPP_MAX_N` and `1 ≤ window_bits ≤ 16`.
pub fn run_g1_pippenger_msm_gpu(
    dev: &VulkanDevice,
    bases: &[G1Affine],
    scalars: &[Scalar],
    n: usize,
    window_bits: u32,
) -> Result<G1Projective> {
    anyhow::ensure!(n > 0 && n <= G1_PIPP_MAX_N, "n={n} out of range for Pippenger MSM");
    anyhow::ensure!(window_bits >= 1 && window_bits <= 16, "window_bits={window_bits}");
    anyhow::ensure!(bases.len() >= n && scalars.len() >= n);

    let bucket_count = 1usize << window_bits;
    let num_windows = (255u32).div_ceil(window_bits) as usize;

    // Pre-compute per-scalar window digits once.
    let all_digits: Vec<Vec<u32>> = scalars[..n]
        .iter()
        .map(|s| fr_scalar_unsigned_windows(s, window_bits))
        .collect();

    let mut window_results: Vec<G1Projective> = Vec::with_capacity(num_windows);

    for w in 0..num_windows {
        let digits: Vec<u32> = (0..n).map(|i| all_digits[i][w]).collect();
        let bucket_jac = run_g1_pippenger_window_gpu(dev, &bases[..n], &digits, n, bucket_count)
            .with_context(|| format!("pippenger window {w}/{num_windows}"))?;

        // Convert Jacobian buckets → projective, combine: Σ_{b=1}^{bc-1} b·bucket[b].
        let bucket_proj: Vec<G1Projective> =
            bucket_jac.iter().map(g1_projective_from_jacobian_limbs).collect();
        let wr = g1_combine_single_window_bucket_sums(window_bits, &bucket_proj)
            .ok_or_else(|| anyhow::anyhow!("g1_combine_single_window_bucket_sums window {w}"))?;
        window_results.push(wr);
    }

    // Assemble across windows: result = Σ_w window[w] · 2^(w·window_bits).
    Ok(g1_combine_pippenger_windows(window_bits, &window_results))
}

/// CPU reference Pippenger MSM — same windowing as the GPU path, fully on the host.
/// Used to verify correctness of [`run_g1_pippenger_msm_gpu`].
pub fn g1_pippenger_msm_cpu(
    bases: &[G1Affine],
    scalars: &[Scalar],
    n: usize,
    window_bits: u32,
) -> G1Projective {
    assert!(n > 0 && bases.len() >= n && scalars.len() >= n);
    assert!(window_bits >= 1 && window_bits <= 16);

    let bucket_count = 1usize << window_bits;
    let num_windows = (255u32).div_ceil(window_bits) as usize;
    let all_digits: Vec<Vec<u32>> = scalars[..n]
        .iter()
        .map(|s| fr_scalar_unsigned_windows(s, window_bits))
        .collect();

    let mut window_results = Vec::with_capacity(num_windows);
    for w in 0..num_windows {
        let mut buckets = vec![G1Projective::identity(); bucket_count];
        for i in 0..n {
            let b = all_digits[i][w] as usize;
            if b < bucket_count {
                buckets[b] += G1Projective::from(bases[i]);
            }
        }
        let wr = g1_combine_single_window_bucket_sums(window_bits, &buckets)
            .unwrap_or(G1Projective::identity());
        window_results.push(wr);
    }
    g1_combine_pippenger_windows(window_bits, &window_results)
}
