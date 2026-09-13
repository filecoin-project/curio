//! §8.2 **B.3** — verify WGSL→naga→SPIR-V `subgroupShuffle` executes on the ICD (optional smoke).

use cuzk_vk::device::VulkanDevice;
use cuzk_vk::subgroup_shuffle_smoke_gpu::run_subgroup_shuffle_smoke_gpu;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn subgroup_shuffle_wgsl_smoke_broadcasts_42() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = match VulkanDevice::new() {
        Ok(d) => d,
        Err(e) => {
            eprintln!("skip: Vulkan unavailable ({e:#})");
            return;
        }
    };
    let got = match run_subgroup_shuffle_smoke_gpu(&dev) {
        Ok(v) => v,
        Err(e) => {
            eprintln!("skip: subgroup shuffle smoke (ICD may lack subgroup shuffle): {e:#}");
            return;
        }
    };
    for (i, &w) in got.iter().enumerate() {
        assert_eq!(w, 42, "buf[{i}]");
    }
}
