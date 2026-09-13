//! Optional second compute queue on the primary family (see [`cuzk_vk::VulkanDevice::queue_compute_1`]).

use cuzk_vk::VulkanDevice;

#[test]
fn device_reports_queue_count() {
    let Ok(dev) = VulkanDevice::new() else {
        return;
    };
    assert!(dev.queue_count_on_family >= 1);
    if dev.queue_count_on_family > 1 {
        assert!(dev.queue_compute_1.is_some());
    } else {
        assert!(dev.queue_compute_1.is_none());
    }
    let note = dev.dual_compute_queue_note();
    assert!(note.contains("Queue family"));
}
