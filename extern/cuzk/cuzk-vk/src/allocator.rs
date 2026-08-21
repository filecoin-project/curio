//! gpu-allocator wrapper for device-local and host-visible Vulkan memory.

use anyhow::Result;
use ash::vk;
use gpu_allocator::vulkan::{Allocator, AllocatorCreateDesc};
use gpu_allocator::AllocationSizes;

/// Sub-allocator for `VkBuffer` / `VkImage` backed by VMA-style pools.
pub struct VkAllocator {
    pub inner: Allocator,
}

impl VkAllocator {
    /// Construct from an initialized [`crate::VulkanDevice`].
    pub fn new(
        instance: &ash::Instance,
        device: &ash::Device,
        physical_device: vk::PhysicalDevice,
    ) -> Result<Self> {
        let desc = AllocatorCreateDesc {
            instance: instance.clone(),
            device: device.clone(),
            physical_device,
            debug_settings: Default::default(),
            buffer_device_address: false,
            allocation_sizes: AllocationSizes::default(),
        };
        let inner = Allocator::new(&desc)?;
        Ok(Self { inner })
    }
}
