//! G2 limb reversal on GPU vs CPU reference.

use cuzk_vk::device::VulkanDevice;
use cuzk_vk::g1::{g2_limbs_reverse_words_inplace, G2AffineLimbs, BLS12_381_FP2_U32_LIMBS};
use cuzk_vk::g2_gpu::run_g2_reverse48_gpu;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn g2_reverse_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut x = [0u32; BLS12_381_FP2_U32_LIMBS];
    let mut y = [0u32; BLS12_381_FP2_U32_LIMBS];
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        x[i] = (i + 1) as u32;
        y[i] = (200 + i) as u32;
    }
    let mut g = G2AffineLimbs { x, y };
    let mut cpu = g;
    g2_limbs_reverse_words_inplace(&mut cpu);
    run_g2_reverse48_gpu(&dev, &mut g).expect("gpu");
    assert_eq!(g, cpu);
}
