//! Phase J — H-term quotient scalars (CPU reference aligned with bellperson `execute_fft` /
//! SupraSeal `groth16_ntt_h.cu` NTT half before MSM).
//!
//! Pipeline: **IFFT (standard)** → **coset FFT** → pointwise `a*b` → subtract `c` → **divide by
//! \(Z(g)=g^n-1\)** on the coset → **icoset FFT**; then drop the highest wrapped coefficient (same as
//! bellperson `native::execute_fft` truncation to `n-1` scalars for MSM).

use anyhow::{ensure, Result};
use blstrs::Scalar;
use ff::{Field, PrimeField};

use crate::ntt::{fr_intt_inplace, fr_ntt_inplace, fr_omega};

/// Smallest power-of-two `>= len`, matching bellperson [`EvaluationDomain::from_coeffs`] padding.
#[inline]
pub fn fr_domain_size_at_least(len: usize) -> usize {
    let mut m = 1usize;
    while m < len {
        m *= 2;
    }
    m
}

/// Multiply `coeffs[i]` by `base^i` (bellperson [`EvaluationDomain::distribute_powers`]).
pub fn fr_distribute_powers(coeffs: &mut [Scalar], base: Scalar) {
    let mut pow = Scalar::ONE;
    for x in coeffs.iter_mut() {
        *x *= pow;
        pow *= base;
    }
}

/// Coefficients → evaluations at `ω^k` (forward NTT).
pub fn fr_poly_coeffs_to_evals(coeffs: &mut [Scalar]) {
    let n = coeffs.len();
    assert!(n.is_power_of_two());
    let omega = fr_omega(n);
    fr_ntt_inplace(coeffs, omega);
}

/// Evaluations → coefficients (inverse NTT).
pub fn fr_poly_evals_to_coeffs(evals: &mut [Scalar]) {
    let n = evals.len();
    assert!(n.is_power_of_two());
    let omega = fr_omega(n);
    fr_intt_inplace(evals, omega);
}

/// Groth16 H MSM scalars: same algebra as bellperson [`execute_fft`] (truncated to `n-1` entries).
///
/// `a`, `b`, `c` must have equal length (constraint-domain witness polynomials in Lagrange form).
pub fn fr_quotient_scalars_from_abc(a: &[Scalar], b: &[Scalar], c: &[Scalar]) -> Result<Vec<Scalar>> {
    ensure!(
        a.len() == b.len() && a.len() == c.len(),
        "a, b, c must have equal length (got {}, {}, {})",
        a.len(),
        b.len(),
        c.len()
    );
    let n = fr_domain_size_at_least(a.len());
    let omega = fr_omega(n);
    let g = Scalar::MULTIPLICATIVE_GENERATOR;

    let mut av = vec![Scalar::ZERO; n];
    let mut bv = vec![Scalar::ZERO; n];
    let mut cv = vec![Scalar::ZERO; n];
    av[..a.len()].copy_from_slice(a);
    bv[..b.len()].copy_from_slice(b);
    cv[..c.len()].copy_from_slice(c);

    fr_intt_inplace(&mut av, omega);
    fr_intt_inplace(&mut bv, omega);
    fr_intt_inplace(&mut cv, omega);

    fr_distribute_powers(&mut av, g);
    fr_distribute_powers(&mut bv, g);
    fr_distribute_powers(&mut cv, g);

    fr_ntt_inplace(&mut av, omega);
    fr_ntt_inplace(&mut bv, omega);
    fr_ntt_inplace(&mut cv, omega);

    for i in 0..n {
        av[i] *= bv[i];
        av[i] -= cv[i];
    }

    let zinv = (g.pow_vartime(&[n as u64]) - Scalar::ONE)
        .invert()
        .into_option()
        .ok_or_else(|| anyhow::anyhow!("g^n-1 not invertible on this domain"))?;
    for x in &mut av {
        *x *= zinv;
    }

    fr_intt_inplace(&mut av, omega);
    let geninv = g.invert().unwrap();
    fr_distribute_powers(&mut av, geninv);

    if n > 0 {
        av.truncate(n.saturating_sub(1));
    }
    Ok(av)
}

/// Documented ordering for a fused GPU implementation (current path uses [`crate::h_term_gpu`]).
pub const HTERM_GPU_PIPELINE_ORDER: &str =
    "INTT(std) → GPU coset forward (distribute g + Fr NTT) → coeff_wise_mul → (a-c)*z_inv → INTT(coset) → GPU distribute(g^{-1})";
