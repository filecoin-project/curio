//! Phase H: small-integer MSM reference matches `blstrs` Pippenger (`G1Projective::multi_exp`).

use blstrs::{G1Affine, G1Projective, Scalar};
use cuzk_vk::split_msm::g1_msm_small_u64_agrees_with_multi_exp;
use ff::Field;
use group::Group;
use rand::RngCore;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

#[test]
fn msm_u64_reference_matches_multi_exp_random() {
    let mut rng = ChaCha8Rng::from_seed([0x6Du8; 32]);
    let n = 23usize;
    let bases: Vec<G1Affine> = (0..n)
        .map(|_| G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)))
        .collect();
    let scalars_u64: Vec<u64> = (0..n).map(|_| rng.next_u64() >> 16).collect();
    assert!(g1_msm_small_u64_agrees_with_multi_exp(
        &bases,
        &scalars_u64,
        n,
        48
    ));
}
