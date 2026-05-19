//! Phase J: GPU-composed H quotient matches CPU [`fr_quotient_scalars_from_abc`].

use blstrs::Scalar;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::h_term::fr_quotient_scalars_from_abc;
use cuzk_vk::h_term_gpu::run_fr_quotient_scalars_gpu;
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;
use std::sync::Arc;

#[test]
fn h_quotient_gpu_matches_cpu_random() {
    if !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0")) {
        return;
    }
    let dev = Arc::new(VulkanDevice::new().expect("Vulkan"));
    let mut rng = ChaCha8Rng::from_seed([0x47u8; 32]);
    let len = 37usize;
    let a: Vec<Scalar> = (0..len).map(|_| Scalar::random(&mut rng)).collect();
    let b: Vec<Scalar> = (0..len).map(|_| Scalar::random(&mut rng)).collect();
    let c: Vec<Scalar> = (0..len).map(|_| Scalar::random(&mut rng)).collect();

    let want = fr_quotient_scalars_from_abc(&a, &b, &c).expect("cpu");
    let got = run_fr_quotient_scalars_gpu(&dev, &a, &b, &c).expect("gpu");
    assert_eq!(got, want);
}

/// Stresses `n = 8192` domain (requires raised `FR_POINTWISE_MAX` and matching shader caps).
#[test]
fn h_quotient_gpu_matches_cpu_domain_8192() {
    if !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0")) {
        return;
    }
    let dev = Arc::new(VulkanDevice::new().expect("Vulkan"));
    let mut rng = ChaCha8Rng::from_seed([0x91u8; 32]);
    let len = 5000usize;
    let a: Vec<Scalar> = (0..len).map(|_| Scalar::random(&mut rng)).collect();
    let b: Vec<Scalar> = (0..len).map(|_| Scalar::random(&mut rng)).collect();
    let c: Vec<Scalar> = (0..len).map(|_| Scalar::random(&mut rng)).collect();

    let want = fr_quotient_scalars_from_abc(&a, &b, &c).expect("cpu");
    let got = run_fr_quotient_scalars_gpu(&dev, &a, &b, &c).expect("gpu");
    assert_eq!(got, want);
}
