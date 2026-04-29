//! Coefficient-wise Fr multiply on GPU vs CPU.

use blstrs::Scalar;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fr_pointwise_gpu::run_fr_coeff_wise_mult_gpu;
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

fn check_lens(n: usize) {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xE3u8; 32]);
    let a: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
    let b: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
    let mut out = vec![Scalar::ZERO; n];
    run_fr_coeff_wise_mult_gpu(&dev, &mut out, &a, &b).expect("gpu coeff mult");
    for i in 0..n {
        assert_eq!(out[i], a[i] * b[i]);
    }
}

#[test]
fn fr_coeff_wise_mult_lens() {
    for n in [1usize, 7, 64, 1024, 8192, 16384] {
        check_lens(n);
    }
}

#[test]
fn fr_coeff_wise_mult_zero_len_noop() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut out: Vec<Scalar> = vec![];
    run_fr_coeff_wise_mult_gpu(&dev, &mut out, &[], &[]).expect("noop");
    assert!(out.is_empty());
}
