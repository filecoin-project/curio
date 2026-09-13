//! G1 limb reversal on GPU vs CPU reference.

use cuzk_vk::device::VulkanDevice;
use cuzk_vk::g1::{g1_limbs_reverse_words_inplace, G1AffineLimbs};
use cuzk_vk::g1_gpu::run_g1_reverse24_gpu;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn g1_reverse_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut g = G1AffineLimbs {
        x: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12],
        y: [100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111],
    };
    let mut cpu = g;
    g1_limbs_reverse_words_inplace(&mut cpu);
    run_g1_reverse24_gpu(&dev, &mut g).expect("gpu");
    assert_eq!(g, cpu);
}
