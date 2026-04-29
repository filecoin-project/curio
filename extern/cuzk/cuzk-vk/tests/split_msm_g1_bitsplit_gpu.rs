//! Phase H: bit-plane GPU MSM vs CPU reference for small `u64` scalars.

use blstrs::{G1Affine, G1Projective, Scalar};
use cuzk_vk::device::VulkanDevice;
use ff::Field;
use cuzk_vk::split_msm::{g1_msm_bitplanes_u64_gpu_host, g1_msm_small_u64_reference};
use group::Group;
use rand::RngCore;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn split_msm_g1_bitplanes_matches_reference() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0x48u8; 32]);
    let max_bits = 18u32;
    let cap = (1u64 << max_bits) - 1;
    for n in [1usize, 8, 33, 64] {
        let bases: Vec<G1Affine> = (0..n)
            .map(|_| G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)))
            .collect();
        let scalars_u64: Vec<u64> = (0..n).map(|_| rng.next_u64() & cap).collect();
        let ref_sum = g1_msm_small_u64_reference(&bases, &scalars_u64, n, max_bits);
        let gpu_sum = g1_msm_bitplanes_u64_gpu_host(&dev, &bases, &scalars_u64, n, max_bits).expect("bitplane msm");
        assert_eq!(gpu_sum, ref_sum, "n={}", n);
    }
}
