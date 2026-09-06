//! Phase F follow-up: manual coset FFT (distribute generator + forward NTT) matches bellperson
//! [`EvaluationDomain::coset_fft`] on the CPU.

use bellperson::domain::EvaluationDomain;
use blstrs::Scalar;
use cuzk_vk::h_term::fr_distribute_powers;
use cuzk_vk::ntt::{fr_ntt_inplace, fr_omega};
use ec_gpu_gen::threadpool::Worker;
use ff::{Field, PrimeField};
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

#[test]
fn coset_fft_manual_matches_bellperson_domain() {
    let mut rng = ChaCha8Rng::from_seed([0x63u8; 32]);
    let n = 32usize;
    let coeffs: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
    let worker = Worker::new();
    let mut domain = EvaluationDomain::from_coeffs(coeffs.clone()).expect("domain");
    domain
        .coset_fft(&worker, &mut None)
        .expect("coset_fft");
    let want: Vec<Scalar> = domain.into_coeffs();

    let mut manual = coeffs;
    fr_distribute_powers(&mut manual, Scalar::MULTIPLICATIVE_GENERATOR);
    let omega = fr_omega(n);
    fr_ntt_inplace(&mut manual, omega);
    assert_eq!(manual, want);
}
