//! Gate for Step 1.1: Vulkan ICD present + compute queue available.
//!
//! Without a Vulkan loader/ICD, tests are skipped by default. Set `CUZK_VK_SKIP_SMOKE=0` to run.

use cuzk_vk::VulkanDevice;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn vulkan_device_can_enumerate_and_create_compute_queue() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let info = dev.physical_device_info();
    assert!(!info.device_name.is_empty());
    assert!(!info.driver_version.is_empty());
    assert!(!info.api_version.is_empty());
}
