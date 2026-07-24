//! GPU dispatch hit-count vs [`cuzk_vk::msm::MsmBucketReduceDispatch::invocation_count`] (2D grid smoke).

use cuzk_vk::device::VulkanDevice;
use cuzk_vk::msm::{MegaMsmDenseStrip, MsmBucketReduceDispatch};
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

/// §8.4 **D.1** mega-strip: widened `groups_x` follows [`MegaMsmDenseStrip::groups_x_total`] (`msm_mega_dense_groups_x`).
#[test]
fn msm_dispatch_mega_strip_hitcount_matches_host() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let strip = MegaMsmDenseStrip {
        groups_x_per_circuit: 2,
        batch_circuits: 2,
        local_x: MSM_DISPATCH_SMOKE_LOCAL_X,
    };
    let d = MsmBucketReduceDispatch::mega_dense_strip(strip, 4);
    let got = run_msm_dispatch_hitcount_smoke(&dev, d).expect("mega dispatch smoke");
    assert_eq!(u64::from(got), d.invocation_count());
    assert_eq!(d.invocation_count(), 4096);
}
