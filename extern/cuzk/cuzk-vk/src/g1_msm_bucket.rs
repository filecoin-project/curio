//! Host helpers for **window bucket MSM** (Pippenger-style). GPU bucket kernels will write partial
//! sums `Q_b = Σ_{i: digit_i=b} P_i` per window; this module combines them for **one** window,
//! then assembles across all windows.

use blstrs::{G1Projective, Scalar};
use ff::Field;
use group::Group;

use crate::msm::msm_bucket_count;

/// `buckets[b]` must hold `Σ_{i : digit_i = b} P_i` in the group (bucket `0` is unused for unsigned digits).
/// Returns `Σ_{b=1}^{num_buckets-1} b * buckets[b]` in `G1`, matching `Σ_i d_i P_i` when each scalar
/// equals its digit `d_i` in this single window (`d_i < num_buckets`).
pub fn g1_combine_single_window_bucket_sums(
    window_bits: u32,
    buckets: &[G1Projective],
) -> Option<G1Projective> {
    let nb = msm_bucket_count(window_bits);
    if nb > usize::MAX as u64 {
        return None;
    }
    let n = nb as usize;
    if buckets.len() < n {
        return None;
    }
    let mut acc = G1Projective::identity();
    for b in 1..n {
        let coeff = Scalar::from(b as u64);
        acc += buckets[b] * coeff;
    }
    Some(acc)
}

/// Assemble per-window results into the full MSM answer (§8.1 Pippenger multi-window phase).
///
/// `window_results[w]` is `Σ_{b=1}^{bc-1} b · bucket_w[b]`, the combined result for window `w`.
/// The final multiexp value is `Σ_w window_results[w] · 2^(w · window_bits)`.
///
/// Handles `window_bits` up to 30 (safe for any BLS12-381 scalar window).
pub fn g1_combine_pippenger_windows(window_bits: u32, window_results: &[G1Projective]) -> G1Projective {
    let two = Scalar::from(2u64);
    let mut acc = G1Projective::identity();
    for (w, &wr) in window_results.iter().enumerate() {
        let bit_offset = (w as u32) * window_bits;
        if bit_offset == 0 {
            acc += wr;
        } else {
            // 2^bit_offset as a Scalar — bit_offset ≤ 255 for BLS12-381.
            let shift = two.pow_vartime([bit_offset as u64]);
            acc += wr * shift;
        }
    }
    acc
}

#[cfg(test)]
mod tests {
    use super::*;
    use blstrs::G1Affine;
    use group::Curve;

    #[test]
    fn single_window_combine_matches_digit_lincomb() {
        let window_bits = 3u32;
        let bases: Vec<G1Affine> = (0u64..4)
            .map(|i| (G1Projective::generator() * Scalar::from(100 + i)).to_affine())
            .collect();
        let digits: [u32; 4] = [1, 2, 0, 5];
        let mut buckets = vec![G1Projective::identity(); 1 << window_bits];
        for (i, &d) in digits.iter().enumerate() {
            let di = d as usize;
            if di < buckets.len() {
                buckets[di] += G1Projective::from(bases[i]);
            }
        }
        let got = g1_combine_single_window_bucket_sums(window_bits, &buckets).unwrap();
        let mut want = G1Projective::identity();
        for (i, &d) in digits.iter().enumerate() {
            want += G1Projective::from(bases[i]) * Scalar::from(d as u64);
        }
        assert_eq!(got, want);
    }
}
