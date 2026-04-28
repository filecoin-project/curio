//! Vulkan instance + physical device + logical device with a single compute queue.

use anyhow::{Context, Result};
use ash::vk;
use std::ffi::CString;

/// Minimal Vulkan context for headless compute.
pub struct VulkanDevice {
    _entry: ash::Entry,
    pub instance: ash::Instance,
    pub physical_device: vk::PhysicalDevice,
    pub device: ash::Device,
    pub queue: vk::Queue,
    pub queue_family_index: u32,
}

impl VulkanDevice {
    /// Create with Vulkan 1.0 API (widest MoltenVK / driver support).
    pub fn new() -> Result<Self> {
        Self::with_api_version(vk::API_VERSION_1_0)
    }

    pub fn with_api_version(api_version: u32) -> Result<Self> {
        let entry = unsafe { ash::Entry::load().context("ash::Entry::load (libVulkan)")? };

        let app_name = CString::new("cuzk-vk").unwrap();
        let engine_name = CString::new("cuzk").unwrap();
        let app_info = vk::ApplicationInfo {
            s_type: vk::StructureType::APPLICATION_INFO,
            p_next: std::ptr::null(),
            p_application_name: app_name.as_ptr(),
            application_version: 1,
            p_engine_name: engine_name.as_ptr(),
            engine_version: 1,
            api_version,
            ..Default::default()
        };

        let create_info = vk::InstanceCreateInfo {
            s_type: vk::StructureType::INSTANCE_CREATE_INFO,
            p_next: std::ptr::null(),
            flags: vk::InstanceCreateFlags::empty(),
            p_application_info: &app_info,
            enabled_extension_count: 0,
            pp_enabled_extension_names: std::ptr::null(),
            ..Default::default()
        };

        let instance = unsafe {
            entry
                .create_instance(&create_info, None)
                .context("vkCreateInstance")?
        };

        let pdevices = unsafe {
            instance
                .enumerate_physical_devices()
                .context("vkEnumeratePhysicalDevices")?
        };
        anyhow::ensure!(
            !pdevices.is_empty(),
            "no Vulkan physical devices found (install a driver / ICD)"
        );

        let physical_device = pdevices[0];
        let queue_family_index =
            find_compute_queue_family(&instance, physical_device).context(
                "no queue family with COMPUTE bit — driver or device incompatible",
            )?;

        let queue_priority = 1.0f32;
        let queue_info = vk::DeviceQueueCreateInfo {
            s_type: vk::StructureType::DEVICE_QUEUE_CREATE_INFO,
            p_next: std::ptr::null(),
            flags: vk::DeviceQueueCreateFlags::empty(),
            queue_family_index,
            queue_count: 1,
            p_queue_priorities: &queue_priority,
            ..Default::default()
        };

        let features = vk::PhysicalDeviceFeatures::default();
        let device_create_info = vk::DeviceCreateInfo {
            s_type: vk::StructureType::DEVICE_CREATE_INFO,
            p_next: std::ptr::null(),
            flags: vk::DeviceCreateFlags::empty(),
            queue_create_info_count: 1,
            p_queue_create_infos: &queue_info,
            enabled_extension_count: 0,
            pp_enabled_extension_names: std::ptr::null(),
            p_enabled_features: &features,
            ..Default::default()
        };

        let device = unsafe {
            instance
                .create_device(physical_device, &device_create_info, None)
                .context("vkCreateDevice")?
        };

        let queue = unsafe { device.get_device_queue(queue_family_index, 0) };

        Ok(Self {
            _entry: entry,
            instance,
            physical_device,
            device,
            queue,
            queue_family_index,
        })
    }
}

impl Drop for VulkanDevice {
    fn drop(&mut self) {
        unsafe {
            self.device.destroy_device(None);
            self.instance.destroy_instance(None);
        }
    }
}

fn find_compute_queue_family(
    instance: &ash::Instance,
    physical_device: vk::PhysicalDevice,
) -> Option<u32> {
    let props = unsafe {
        instance.get_physical_device_queue_family_properties(physical_device)
    };
    for (i, p) in props.iter().enumerate() {
        if p.queue_flags.contains(vk::QueueFlags::COMPUTE) {
            return Some(i as u32);
        }
    }
    None
}
