//! §8.1 correctness test: full 255-bit scalar Pippenger MSM on the GPU.
//!
//! Tests [`run_g1_pippenger_msm_gpu`] against the CPU Pippenger reference
//! [`g1_pippenger_msm_cpu`] and against `G1Projective::multi_exp` for various n.
//! The `_large` test (n=1024) can be skipped by setting `CUZK_VK_SKIP_LARGE_GPU_TESTS=1`.

use std::sync::Arc;

use blstrs::{G1Affine, G1Projective, Scalar};
use ff::Field;
use group::{Curve, Group};
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

use cuzk_vk::VulkanDevice;
use cuzk_vk::g1_pippenger_bucket_gpu::{g1_pippenger_msm_cpu, run_g1_pippenger_msm_gpu};

fn make_dev() -> Arc<VulkanDevice> {
    Arc::new(VulkanDevice::new().expect("VulkanDevice::new"))
}

fn random_bases_and_scalars(n: usize, seed: u64) -> (Vec<G1Affine>, Vec<Scalar>) {
    let mut rng = ChaCha8Rng::seed_from_u64(seed);
    let bases: Vec<G1Affine> = (0..n)
        .map(|_| (G1Projective::random(&mut rng)).to_affine())
        .collect();
    let scalars: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
    (bases, scalars)
}

fn multi_exp_ref(bases: &[G1Affine], scalars: &[Scalar]) -> G1Projective {
    let pts: Vec<G1Projective> = bases.iter().map(|&a| G1Projective::from(a)).collect();
    G1Projective::multi_exp(&pts, scalars)
}

/// Tiny n=8 sanity check with w=4 (16 buckets, 2 windows).
#[test]
fn g1_pippenger_w4_n8_matches_multi_exp() {
    let dev = make_dev();
    let (bases, scalars) = random_bases_and_scalars(8, 0xabc);
    let window_bits = 4u32;
    let got = run_g1_pippenger_msm_gpu(&dev, &bases, &scalars, 8, window_bits)
        .expect("run_g1_pippenger_msm_gpu");
    let want = multi_exp_ref(&bases, &scalars);
    assert_eq!(
        got.to_affine(),
        want.to_affine(),
        "w=4 n=8 GPU vs multi_exp"
    );
}

/// CPU reference matches `multi_exp` (validates the windowing logic itself).
#[test]
fn g1_pippenger_cpu_matches_multi_exp_n64() {
    let (bases, scalars) = random_bases_and_scalars(64, 0xf00d);
    let window_bits = 8u32;
    let got = g1_pippenger_msm_cpu(&bases, &scalars, 64, window_bits);
    let want = multi_exp_ref(&bases, &scalars);
    assert_eq!(
        got.to_affine(),
        want.to_affine(),
        "CPU Pippenger w=8 n=64 vs multi_exp"
    );
}

/// n=64, window_bits=8 (32 windows of 8 bits → 256 buckets each).
#[test]
fn g1_pippenger_w8_n64_matches_multi_exp() {
    let dev = make_dev();
    let (bases, scalars) = random_bases_and_scalars(64, 0xdeadbeef);
    let window_bits = 8u32;
    let got = run_g1_pippenger_msm_gpu(&dev, &bases, &scalars, 64, window_bits)
        .expect("run_g1_pippenger_msm_gpu w=8");
    let want = multi_exp_ref(&bases, &scalars);
    assert_eq!(
        got.to_affine(),
        want.to_affine(),
        "w=8 n=64 GPU vs multi_exp"
    );
}

/// GPU matches CPU Pippenger reference at n=64 (validates GPU matches the reference algorithm).
#[test]
fn g1_pippenger_gpu_matches_cpu_n64() {
    let dev = make_dev();
    let (bases, scalars) = random_bases_and_scalars(64, 0x1234);
    let window_bits = 12u32; // Apple M2 recommended window size.
    let gpu = run_g1_pippenger_msm_gpu(&dev, &bases, &scalars, 64, window_bits)
        .expect("GPU w=12 n=64");
    let cpu = g1_pippenger_msm_cpu(&bases, &scalars, 64, window_bits);
    assert_eq!(gpu.to_affine(), cpu.to_affine(), "GPU vs CPU Pippenger w=12 n=64");
}

/// Larger n=512 test; skippable via `CUZK_VK_SKIP_LARGE_GPU_TESTS=1`.
#[test]
fn g1_pippenger_w12_n512_matches_multi_exp() {
    if std::env::var("CUZK_VK_SKIP_LARGE_GPU_TESTS").as_deref() == Ok("1") {
        return;
    }
    let dev = make_dev();
    let (bases, scalars) = random_bases_and_scalars(512, 0x5555);
    let window_bits = 12u32;
    let got = run_g1_pippenger_msm_gpu(&dev, &bases, &scalars, 512, window_bits)
        .expect("GPU w=12 n=512");
    let want = multi_exp_ref(&bases, &scalars);
    assert_eq!(
        got.to_affine(),
        want.to_affine(),
        "w=12 n=512 GPU vs multi_exp"
    );
}

/// n=1024 (2^10). Skippable via `CUZK_VK_SKIP_LARGE_GPU_TESTS=1`.
#[test]
fn g1_pippenger_w12_n1024_matches_multi_exp() {
    if std::env::var("CUZK_VK_SKIP_LARGE_GPU_TESTS").as_deref() == Ok("1") {
        return;
    }
    let dev = make_dev();
    let (bases, scalars) = random_bases_and_scalars(1024, 0xbabe);
    let window_bits = 12u32;
    let got = run_g1_pippenger_msm_gpu(&dev, &bases, &scalars, 1024, window_bits)
        .expect("GPU w=12 n=1024");
    let want = multi_exp_ref(&bases, &scalars);
    assert_eq!(
        got.to_affine(),
        want.to_affine(),
        "w=12 n=1024 GPU vs multi_exp"
    );
}
