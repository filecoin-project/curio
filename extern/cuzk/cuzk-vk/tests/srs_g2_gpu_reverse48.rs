//! Phase I — SRS-style `G2Affine` → Montgomery limbs → [`cuzk_vk::g2_gpu::run_g2_reverse48_gpu`] matches CPU.

use blstrs::{G2Projective, Scalar};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::srs::{srs_decode_bg2_g2, srs_synthetic_partition_smoke_blob};
use cuzk_vk::srs_gpu::srs_g2_affine_gpu_reverse48_matches_cpu;
use ff::Field;
use group::{Curve, Group};
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn srs_g2_affine_reverse48_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let g = G2Projective::generator().to_affine();
    srs_g2_affine_gpu_reverse48_matches_cpu(&dev, &g).expect("generator");

    let mut rng = ChaCha8Rng::from_seed([0x67u8; 32]);
    let s = Scalar::random(&mut rng);
    let p = (G2Projective::generator() * s).to_affine();
    srs_g2_affine_gpu_reverse48_matches_cpu(&dev, &p).expect("random affine");

    let blob = srs_synthetic_partition_smoke_blob();
    let bg2 = srs_decode_bg2_g2(&blob, 0).expect("b_g2[0]");
    srs_g2_affine_gpu_reverse48_matches_cpu(&dev, &bg2).expect("synthetic SRS b_g2[0]");
}
