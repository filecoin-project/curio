//! Phase I — SRS-style `G1Affine` → Montgomery limbs → [`cuzk_vk::g1_gpu::run_g1_reverse24_gpu`] matches CPU.

use blstrs::{G1Projective, Scalar};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::srs_gpu::srs_g1_affine_gpu_reverse24_matches_cpu;
use ff::Field;
use group::{Curve, Group};
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn srs_g1_affine_reverse24_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0x73u8; 32]);
    let g = G1Projective::generator().to_affine();
    srs_g1_affine_gpu_reverse24_matches_cpu(&dev, &g).expect("generator");

    let s = Scalar::random(&mut rng);
    let p = (G1Projective::generator() * s).to_affine();
    srs_g1_affine_gpu_reverse24_matches_cpu(&dev, &p).expect("random affine");
}
