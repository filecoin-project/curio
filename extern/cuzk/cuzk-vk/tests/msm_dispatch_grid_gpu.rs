//! GPU atomic invocation count vs [`cuzk_vk::msm::MsmBucketReduceDispatch::invocation_count`].

use cuzk_vk::device::VulkanDevice;
use cuzk_vk::msm::MsmBucketReduceDispatch;
use cuzk_vk::msm_gpu::{run_msm_dispatch_hitcount_smoke, MSM_DISPATCH_SMOKE_LOCAL_X};

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn msm_dispatch_grid_hitcount_matches_host() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let d = MsmBucketReduceDispatch::dense(100, 4, MSM_DISPATCH_SMOKE_LOCAL_X);
    let got = run_msm_dispatch_hitcount_smoke(&dev, d).expect("dispatch smoke");
    assert_eq!(u64::from(got), d.invocation_count());
}

#[test]
fn msm_dispatch_grid_hitcount_matches_host_local_x_32() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let d = MsmBucketReduceDispatch::dense(100, 4, 32);
    let got = run_msm_dispatch_hitcount_smoke(&dev, d).expect("dispatch smoke local_x=32");
    assert_eq!(u64::from(got), d.invocation_count());
}
