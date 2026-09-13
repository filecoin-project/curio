//! Phase I: SRS header walk with zero point counts and VK-prefix decode.

use blstrs::{G1Affine, G1Projective, G2Affine, G2Projective, Scalar};
use cuzk_vk::srs::{
    srs_decode_vk_prefix, srs_walk_counts_and_point_blobs, SrsPointCounts, SrsVkPrefix,
    SRS_VK_PREFIX_BYTES,
};
use ff::Field;
use group::Group;
use rand::SeedableRng;
use rand_chacha::ChaCha20Rng;

#[test]
fn srs_walk_zero_counts_min_size() {
    let buf = vec![0u8; SRS_VK_PREFIX_BYTES + 24];
    let (c, end) = srs_walk_counts_and_point_blobs(&buf).expect("walk");
    assert_eq!(
        c,
        SrsPointCounts {
            n_ic: 0,
            n_h: 0,
            n_l: 0,
            n_a: 0,
            n_b_g1: 0,
            n_b_g2: 0,
        }
    );
    assert_eq!(end, SRS_VK_PREFIX_BYTES + 24);
}

#[test]
fn srs_vk_prefix_uncompressed_roundtrip() {
    let mut rng = ChaCha20Rng::seed_from_u64(0x5352535f564b5f31);
    let want = SrsVkPrefix {
        alpha_g1: G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)),
        beta_g1: G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)),
        beta_g2: G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)),
        gamma_g2: G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)),
        delta_g1: G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)),
        delta_g2: G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)),
    };
    let mut buf = Vec::new();
    buf.extend_from_slice(&want.alpha_g1.to_uncompressed());
    buf.extend_from_slice(&want.beta_g1.to_uncompressed());
    buf.extend_from_slice(&want.beta_g2.to_uncompressed());
    buf.extend_from_slice(&want.gamma_g2.to_uncompressed());
    buf.extend_from_slice(&want.delta_g1.to_uncompressed());
    buf.extend_from_slice(&want.delta_g2.to_uncompressed());
    for _ in 0..6 {
        buf.extend_from_slice(&0u32.to_be_bytes());
    }
    let got = srs_decode_vk_prefix(&buf).expect("decode vk");
    assert_eq!(got, want);
    let (c, end) = srs_walk_counts_and_point_blobs(&buf).expect("walk");
    assert_eq!(c.n_ic, 0);
    assert_eq!(end, buf.len());
}
