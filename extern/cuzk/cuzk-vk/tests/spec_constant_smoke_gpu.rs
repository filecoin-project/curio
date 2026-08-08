//! GPU specialization constant plumbing (roadmap §8.3 C.3).

use cuzk_vk::device::VulkanDevice;
use cuzk_vk::spec_constant_smoke_gpu::run_spec_constant_smoke_gpu;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn spec_constant_smoke_writes_k_to_ssbo() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let k = 0xdeadbeefu32;
    let got = run_spec_constant_smoke_gpu(&dev, k).expect("spec constant smoke");
    assert_eq!(got, k);
}
