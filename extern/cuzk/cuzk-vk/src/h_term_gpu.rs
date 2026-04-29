//! Phase J — GPU composition for the Groth16 **H** quotient scalars (SupraSeal `groth16_ntt_h.cu`
//! NTT half, no MSM). Stages match [`crate::h_term::fr_quotient_scalars_from_abc`]. Coset distribute
//! steps use [`crate::fr_coset_gpu`] (no host `fr_distribute_powers` between INTT and forward FFT).

use anyhow::{ensure, Result};
use blstrs::Scalar;
use ff::{Field, PrimeField};

use crate::device::VulkanDevice;
use crate::fr_coset_gpu::{run_fr_coset_fft_forward_gpu, run_fr_distribute_powers_gpu};
use crate::fr_ntt_general_gpu::{run_fr_ntt_general_inverse_gpu, FR_NTT_GENERAL_MAX_N};
use crate::fr_pointwise_gpu::FR_POINTWISE_MAX;
use crate::h_term::fr_domain_size_at_least;

/// Standard INTT → coset forward FFT on GPU ([`run_fr_coset_fft_forward_gpu`] on INTT output).
fn std_intt_then_coset_forward_gpu(dev: &VulkanDevice, padded: &[Scalar]) -> Result<Vec<Scalar>> {
    let t = run_fr_ntt_general_inverse_gpu(dev, padded)?;
    run_fr_coset_fft_forward_gpu(dev, &t)
}

/// Full H scalar pipeline: three coset forward passes, host `(a*b - c) / (g^n-1)`, GPU inverse FFT,
/// then GPU `g^{-1}` power distribution (bellperson [`EvaluationDomain::icoset_fft`]).
pub fn run_fr_quotient_scalars_gpu(
    dev: &VulkanDevice,
    a: &[Scalar],
    b: &[Scalar],
    c: &[Scalar],
) -> Result<Vec<Scalar>> {
    let n = fr_domain_size_at_least(a.len().max(b.len()).max(c.len()));
    ensure!(a.len() == b.len() && a.len() == c.len(), "a/b/c length mismatch");
    ensure!(
        n <= FR_NTT_GENERAL_MAX_N && n <= FR_POINTWISE_MAX,
        "domain n={n} exceeds Fr NTT / pointwise max"
    );

    let mut a_pad = vec![Scalar::ZERO; n];
    let mut b_pad = vec![Scalar::ZERO; n];
    let mut c_pad = vec![Scalar::ZERO; n];
    a_pad[..a.len()].copy_from_slice(a);
    b_pad[..b.len()].copy_from_slice(b);
    c_pad[..c.len()].copy_from_slice(c);

    let a_cos = std_intt_then_coset_forward_gpu(dev, &a_pad)?;
    let b_cos = std_intt_then_coset_forward_gpu(dev, &b_pad)?;
    let c_cos = std_intt_then_coset_forward_gpu(dev, &c_pad)?;

    let g = Scalar::MULTIPLICATIVE_GENERATOR;
    let zinv = (g.pow_vartime(&[n as u64]) - Scalar::ONE)
        .invert()
        .into_option()
        .ok_or_else(|| anyhow::anyhow!("g^n-1 not invertible"))?;

    let mid: Vec<Scalar> = (0..n)
        .map(|i| (a_cos[i] * b_cos[i] - c_cos[i]) * zinv)
        .collect();

    let tail_intt = run_fr_ntt_general_inverse_gpu(dev, &mid)?;
    let geninv = g.invert().unwrap();
    let mut tail = run_fr_distribute_powers_gpu(dev, &tail_intt, geninv)?;

    if n > 0 {
        tail.truncate(n.saturating_sub(1));
    }
    Ok(tail)
}
