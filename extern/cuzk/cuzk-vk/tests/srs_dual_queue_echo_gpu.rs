//! Semaphore-ordered copy on `queue_compute_1` then echo compute on `queue`.

use cuzk_vk::device::VulkanDevice;
use cuzk_vk::srs_dual_queue_gpu::run_gpu_echo_u32_dual_queue;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

fn vulkan_device_for_smoke() -> Option<VulkanDevice> {
    if skip_vulkan_smoke() {
        return None;
    }
    match VulkanDevice::new() {
        Ok(d) => Some(d),
        Err(e) => {
            eprintln!(
                "skip: Vulkan init failed with CUZK_VK_SKIP_SMOKE=0 ({e:#})\n\
                 hint (macOS): install loader + MoltenVK (e.g. brew) and use extern/cuzk/apple-m2-vulkan-smoke.sh, or set DYLD_FALLBACK_LIBRARY_PATH to find libvulkan.dylib."
            );
            None
        }
    }
}

#[test]
fn dual_queue_gpu_echo_u32() {
    let Some(dev) = vulkan_device_for_smoke() else {
        return;
    };
    let Some(_) = dev.queue_compute_1 else {
        eprintln!("skip srs_dual_queue_echo_gpu: single compute queue");
        return;
    };
    let payload = 0xAABBCCDDu32;
    let got = run_gpu_echo_u32_dual_queue(&dev, payload).expect("dual-queue echo");
    assert_eq!(got, payload);
}
