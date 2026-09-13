//! Phase F — **coset forward FFT** on the GPU: multiply coefficients by `MULTIPLICATIVE_GENERATOR^i`
//! ([`crate::h_term::fr_distribute_powers`] semantics), then forward Fr NTT with pack fusion.
//!
//! §8.2 **B.5:** distribute powers are fused into Fr NTT SSBO packing — **no** separate pointwise dispatch.
//! **`n ≥ 8`:** [`crate::fr_ntt_general_gpu::run_fr_ntt_radix8_forward_gpu_distribute`] (fewer butterfly dispatches than radix-4).
//! **`n < 8`:** [`crate::fr_ntt_general_gpu::run_fr_ntt_general_forward_gpu_distribute`] (radix-4 schedule).
//! Matches bellperson [`bellperson::domain::EvaluationDomain::coset_fft`] output for the same
//! coefficient vector (see `tests/fr_coset_fft_gpu.rs`).

use anyhow::{ensure, Result};
use blstrs::Scalar;
use ff::{Field, PrimeField};

use crate::device::VulkanDevice;
use crate::fr_ntt_general_gpu::{
    run_fr_ntt_general_forward_gpu_distribute, run_fr_ntt_radix8_forward_gpu_distribute,
};
use crate::fr_pointwise_gpu::{run_fr_coeff_wise_mult_gpu, FR_POINTWISE_MAX};

fn fr_powers_of_base(n: usize, base: Scalar) -> Vec<Scalar> {
    let mut pow = Scalar::ONE;
    let mut v = vec![Scalar::ZERO; n];
    for p in v.iter_mut() {
        *p = pow;
        pow *= base;
    }
    v
}

/// `out[i] = coeffs[i] * base^i` on the GPU (Montgomery), same algebra as [`crate::h_term::fr_distribute_powers`].
pub fn run_fr_distribute_powers_gpu(
    dev: &VulkanDevice,
    coeffs: &[Scalar],
    base: Scalar,
) -> Result<Vec<Scalar>> {
    let n = coeffs.len();
    if n == 0 {
        return Ok(vec![]);
    }
    ensure!(
        n <= FR_POINTWISE_MAX,
        "n={n} exceeds FR_POINTWISE_MAX ({FR_POINTWISE_MAX})"
    );
    let powers = fr_powers_of_base(n, base);
    let mut out = vec![Scalar::ZERO; n];
    run_fr_coeff_wise_mult_gpu(dev, &mut out, coeffs, &powers)?;
    Ok(out)
}

/// `coeffs[i] *= MULTIPLICATIVE_GENERATOR^i`, then forward Fr NTT (same as one branch of coset FFT).
pub fn run_fr_coset_fft_forward_gpu(dev: &VulkanDevice, coeffs: &[Scalar]) -> Result<Vec<Scalar>> {
    let n = coeffs.len();
    ensure!(n > 0, "empty coeffs");
    ensure!(n.is_power_of_two(), "n must be a power of two");
    ensure!(
        n <= FR_POINTWISE_MAX,
        "n={n} exceeds FR_POINTWISE_MAX ({FR_POINTWISE_MAX})"
    );
    if n >= 8 {
        run_fr_ntt_radix8_forward_gpu_distribute(dev, coeffs, Scalar::MULTIPLICATIVE_GENERATOR)
    } else {
        run_fr_ntt_general_forward_gpu_distribute(dev, coeffs, Scalar::MULTIPLICATIVE_GENERATOR)
    }
}
