//! Phase J: CPU NTT round-trip helpers used toward the H-term chain.

use blstrs::Scalar;
use cuzk_vk::h_term::{fr_poly_coeffs_to_evals, fr_poly_evals_to_coeffs};
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

#[test]
fn h_term_ntt_roundtrip_coeffs() {
    let n = 64usize;
    let mut rng = ChaCha8Rng::from_seed([0x4Au8; 32]);
    let orig: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
    let mut v = orig.clone();
    fr_poly_coeffs_to_evals(&mut v);
    fr_poly_evals_to_coeffs(&mut v);
    assert_eq!(v, orig);
}
