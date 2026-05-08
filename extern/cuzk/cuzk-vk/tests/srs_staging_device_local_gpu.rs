//! B₂ §8.3 C.2: `vkCmdCopyBuffer` staging → device-preferring → staging round-trip.

use cuzk_vk::device::VulkanDevice;
use cuzk_vk::srs::srs_synthetic_partition_smoke_blob;
use cuzk_vk::srs_staging_gpu::{
    srs_device_local_buffer_download, srs_staging_device_local_roundtrip,
    srs_staging_device_local_upload, srs_staging_device_local_upload_submit_async,
};

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn srs_blob_staging_device_local_roundtrip_matches() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let blob = srs_synthetic_partition_smoke_blob();
    let got = srs_staging_device_local_roundtrip(&dev, &blob).expect("staging roundtrip");
    assert_eq!(got, blob);
}

#[test]
fn srs_blob_device_local_upload_then_download_matches() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let blob = srs_synthetic_partition_smoke_blob();
    let on_dev = srs_staging_device_local_upload(&dev, &blob).expect("device-local upload");
    let got = srs_device_local_buffer_download(&dev, &on_dev).expect("download");
    on_dev.destroy(&dev);
    assert_eq!(got, blob);
}

#[test]
fn srs_blob_submit_async_upload_then_download_matches() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let blob = srs_synthetic_partition_smoke_blob();
    let flight =
        srs_staging_device_local_upload_submit_async(&dev, &blob).expect("submit_async upload");
    let on_dev = flight.wait_finish(&dev).expect("upload fence");
    let got = srs_device_local_buffer_download(&dev, &on_dev).expect("download");
    on_dev.destroy(&dev);
    assert_eq!(got, blob);
}
