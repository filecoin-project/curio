//! Phase I: `srs_read_file` + `srs_decode_ic_g1` round-trip on a synthetic SRS blob.

use blstrs::{G1Affine, G1Projective, G2Affine, G2Projective, Scalar};
use cuzk_vk::srs::{
    srs_decode_ic_g1, srs_decode_vk_prefix, srs_read_file, srs_walk_counts_and_point_blobs,
    SrsVkPrefix,
};
use ff::Field;
use group::Group;
use rand::SeedableRng;
use rand_chacha::ChaCha20Rng;
use std::path::PathBuf;

fn build_minimal_srs_bytes(ic0: &G1Affine) -> Vec<u8> {
    // Reuse the same RNG stream as `srs_layout` VK test so G2 uncompressed bytes are known-good.
    let mut rng = ChaCha20Rng::seed_from_u64(0x5352535f564b5f31);
    let vk = SrsVkPrefix {
        alpha_g1: G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)),
        beta_g1: G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)),
        beta_g2: G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)),
        gamma_g2: G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)),
        delta_g1: G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)),
        delta_g2: G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)),
    };
    let mut buf = Vec::new();
    buf.extend_from_slice(&vk.alpha_g1.to_uncompressed());
    buf.extend_from_slice(&vk.beta_g1.to_uncompressed());
    buf.extend_from_slice(&vk.beta_g2.to_uncompressed());
    buf.extend_from_slice(&vk.gamma_g2.to_uncompressed());
    buf.extend_from_slice(&vk.delta_g1.to_uncompressed());
    buf.extend_from_slice(&vk.delta_g2.to_uncompressed());
    buf.extend_from_slice(&1u32.to_be_bytes());
    buf.extend_from_slice(&ic0.to_uncompressed());
    for _ in 0..5 {
        buf.extend_from_slice(&0u32.to_be_bytes());
    }
    buf
}

#[test]
fn srs_read_and_first_ic_roundtrip() {
    let ic0 = G1Affine::from(G1Projective::generator() * Scalar::from(42u64));
    let bytes = build_minimal_srs_bytes(&ic0);
    srs_decode_vk_prefix(&bytes).expect("vk");
    let got = srs_decode_ic_g1(&bytes, 0).expect("ic0");
    assert_eq!(got, ic0);

    let dir = std::env::temp_dir();
    let path: PathBuf = dir.join(format!("cuzk_srs_ic_{}.bin", std::process::id()));
    std::fs::write(&path, &bytes).expect("write srs temp");
    let read = srs_read_file(&path).expect("read");
    std::fs::remove_file(&path).ok();
    assert_eq!(read, bytes);
    assert_eq!(srs_decode_ic_g1(&read, 0).expect("ic0 disk"), ic0);
    let (c, end) = srs_walk_counts_and_point_blobs(&read).expect("walk");
    assert_eq!(c.n_ic, 1);
    assert_eq!(end, read.len());
}
