//! CPU reference radix-2 NTT over Fr (`blstrs::Scalar`) for power-of-two lengths.
//!
//! Vulkan: forward + inverse **n = 8** are in [`crate::fr_ntt_gpu`]; power-of-two **n ≤ 2^14** in
//! [`crate::fr_ntt_general_gpu`]. [`FrNttPlan::fused_layer_count`] previews radix-4/8 **dispatch depth**
//! (B₂ §8.2 scheduling only — GPU radix butterflies still TBD). Larger *n* or coset-fused paths may still use this module on the host.

use blstrs::Scalar;
use ff::{Field, PrimeField};

/// `log_n` was zero or too large for a length-`2^log_n` Fr NTT.
#[derive(Debug, Clone, thiserror::Error)]
pub enum FrNttPlanError {
    #[error("log_n must be >= 1")]
    LogNZero,
    #[error("log_n {log_n} exceeds Fr two-adicity ({max})")]
    LogNTooLarge { log_n: u32, max: u32 },
}

/// Precomputed primitive root and per-stage `wlen` values for GPU scheduling / uniform uploads.
#[derive(Clone, Debug)]
pub struct FrNttPlan {
    pub log_n: u32,
    pub n: usize,
    pub omega: Scalar,
    pub omega_inv: Scalar,
    /// `wlen = ω^{n/m}` for each stage with half-length `m = 2, 4, …, n` (same order as [`fr_ntt_inplace`]).
    pub stage_wlens: Vec<Scalar>,
}

impl FrNttPlan {
    /// Build a plan for `n = 2^log_n` with `1 <= log_n <= Scalar::S`.
    pub fn try_new(log_n: u32) -> Result<Self, FrNttPlanError> {
        if log_n == 0 {
            return Err(FrNttPlanError::LogNZero);
        }
        if log_n > Scalar::S {
            return Err(FrNttPlanError::LogNTooLarge {
                log_n,
                max: Scalar::S,
            });
        }
        let n = 1usize << log_n;
        let omega = fr_omega(n);
        let omega_inv = omega.invert().unwrap();
        let stage_wlens = fr_stage_wlens(n, omega);
        Ok(Self {
            log_n,
            n,
            omega,
            omega_inv,
            stage_wlens,
        })
    }

    /// Fused butterfly **layer count** if each layer consumed `radix_log2` bits of index space
    /// (§8.2 **B.1** scheduling preview; GPU radix-4/8 shaders still open).
    ///
    /// Radix-2: `radix_log2 = 1` ⇒ `log_n` layers (matches [`Self::stage_wlens`] length). Radix-4:
    /// `radix_log2 = 2` ⇒ `ceil(log_n / 2)` layers, etc.
    #[inline]
    pub fn fused_layer_count(log_n: u32, radix_log2: u32) -> u32 {
        assert!(radix_log2 >= 1);
        log_n.div_ceil(radix_log2)
    }
}

/// Twiddle `ω^{n/m}` for each Cooley–Tukey stage (`m = 2, 4, …, n`), length `log2(n)`.
pub fn fr_stage_wlens(n: usize, root: Scalar) -> Vec<Scalar> {
    assert!(n.is_power_of_two());
    let mut out = Vec::with_capacity(n.trailing_zeros() as usize);
    let mut m = 1usize;
    while m < n {
        m *= 2;
        out.push(root.pow_vartime([(n / m) as u64]));
    }
    out
}

/// Primitive `n`-th root of unity in Fr, for `n = 2^k` and `k <= Scalar::S`.
#[inline]
pub fn fr_omega(n: usize) -> Scalar {
    assert!(n.is_power_of_two());
    let k = n.trailing_zeros();
    assert!(k <= Scalar::S, "n exceeds two-adicity of Fr");
    Scalar::ROOT_OF_UNITY.pow_vartime([1u64 << (Scalar::S - k)])
}

fn bit_reverse<T: Copy>(a: &mut [T]) {
    let n = a.len();
    let mut j = 0usize;
    for i in 1..n {
        let mut bit = n >> 1;
        while j & bit != 0 {
            j ^= bit;
            bit >>= 1;
        }
        j ^= bit;
        if i < j {
            a.swap(i, j);
        }
    }
}

/// Decimation-in-time forward NTT. `root` must be a primitive `n`-th root of unity (typically
/// [`fr_omega`]).
pub fn fr_ntt_inplace(a: &mut [Scalar], root: Scalar) {
    let n = a.len();
    assert!(n.is_power_of_two());
    bit_reverse(a);
    let mut m = 1usize;
    while m < n {
        m *= 2;
        let wlen = root.pow_vartime([(n / m) as u64]);
        for i in (0..n).step_by(m) {
            let mut w = Scalar::ONE;
            for j in 0..m / 2 {
                let u = a[i + j];
                let v = a[i + j + m / 2] * w;
                a[i + j] = u + v;
                a[i + j + m / 2] = u - v;
                w *= wlen;
            }
        }
    }
}

/// Inverse NTT: same butterflies with `root^{-1}`, then multiply each coefficient by `1/n`.
pub fn fr_intt_inplace(a: &mut [Scalar], root: Scalar) {
    let n = a.len();
    assert!(n.is_power_of_two());
    let root_inv = root.invert().unwrap();
    fr_ntt_inplace(a, root_inv);
    let n_inv = Scalar::from(n as u64).invert().unwrap();
    for x in a.iter_mut() {
        *x *= n_inv;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use rand::SeedableRng;
    use rand_chacha::ChaCha8Rng;

    #[test]
    fn fr_ntt_roundtrip_64() {
        let n = 64usize;
        let omega = fr_omega(n);
        let mut rng = ChaCha8Rng::from_seed([9u8; 32]);
        let mut v: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
        let orig = v.clone();
        fr_ntt_inplace(&mut v, omega);
        fr_intt_inplace(&mut v, omega);
        assert_eq!(v, orig);
    }

    #[test]
    fn fr_ntt_plan_matches_stage_wlens() {
        let plan = FrNttPlan::try_new(6).expect("plan");
        assert_eq!(plan.n, 64);
        assert_eq!(plan.stage_wlens.len(), 6);
        assert_eq!(plan.omega, fr_omega(64));
        let w = &plan.stage_wlens;
        assert_eq!(w[0], plan.omega.pow_vartime([32u64]));
        assert_eq!(w[1], plan.omega.pow_vartime([16u64]));
        assert_eq!(w[5], plan.omega);
    }

    #[test]
    fn fr_ntt_plan_rejects_oversized_log_n() {
        assert!(matches!(
            FrNttPlan::try_new(Scalar::S + 1),
            Err(FrNttPlanError::LogNTooLarge { .. })
        ));
        assert!(matches!(FrNttPlan::try_new(0), Err(FrNttPlanError::LogNZero)));
    }

    #[test]
    fn fr_ntt_fused_layer_count_radix_preview() {
        assert_eq!(FrNttPlan::fused_layer_count(14, 1), 14);
        assert_eq!(FrNttPlan::fused_layer_count(14, 2), 7);
        assert_eq!(FrNttPlan::fused_layer_count(15, 2), 8);
        assert_eq!(FrNttPlan::fused_layer_count(14, 3), 5);
    }
}
