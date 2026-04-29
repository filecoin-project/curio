//! Vulkan instance + physical device + logical device with a single compute queue.
//!
//! **Pipeline cache (roadmap §C.1):** [`VulkanDevice`] owns a `VkPipelineCache` passed to
//! `vkCreateComputePipelines`. If **`CUZK_VK_PIPELINE_CACHE`** is set to a filesystem path and that
//! file contains a non-empty blob from `vkGetPipelineCacheData` (same ICD / roughly same driver
//! generation), the device **loads** it at creation; invalid blobs fall back to an empty cache.
//! Call [`VulkanDevice::pipeline_cache_save_to_path`] (or [`VulkanDevice::pipeline_cache_save_from_env`])
//! after compiling pipelines to persist for the next process.

use anyhow::{Context, Result};
use ash::vk;
use std::ffi::CString;
use std::path::{Path, PathBuf};

/// Minimal Vulkan context for headless compute.
pub struct VulkanDevice {
    _entry: ash::Entry,
    pub instance: ash::Instance,
    pub physical_device: vk::PhysicalDevice,
    pub device: ash::Device,
    pub queue: vk::Queue,
    pub queue_family_index: u32,
    /// In-memory pipeline cache for `vkCreateComputePipelines` (optionally warm-started from disk).
    pub pipeline_cache: vk::PipelineCache,
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

        // MoltenVK (macOS) is a portability implementation: the loader requires
        // `VK_KHR_portability_enumeration` + `ENUMERATE_PORTABILITY_KHR`, and the device needs
        // `VK_KHR_portability_subset` (see Vulkan Portability Initiative).
        #[cfg(target_os = "macos")]
        let (instance_flags, instance_ext_names): (
            vk::InstanceCreateFlags,
            &[*const std::ffi::c_char],
        ) = (
            vk::InstanceCreateFlags::ENUMERATE_PORTABILITY_KHR,
            &[vk::KHR_PORTABILITY_ENUMERATION_NAME.as_ptr()],
        );
        #[cfg(not(target_os = "macos"))]
        let (instance_flags, instance_ext_names): (
            vk::InstanceCreateFlags,
            &[*const std::ffi::c_char],
        ) = (vk::InstanceCreateFlags::empty(), &[]);

        let create_info = vk::InstanceCreateInfo {
            s_type: vk::StructureType::INSTANCE_CREATE_INFO,
            p_next: std::ptr::null(),
            flags: instance_flags,
            p_application_info: &app_info,
            enabled_extension_count: instance_ext_names.len() as u32,
            pp_enabled_extension_names: if instance_ext_names.is_empty() {
                std::ptr::null()
            } else {
                instance_ext_names.as_ptr()
            },
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

        #[cfg(target_os = "macos")]
        let device_ext_names: &[*const std::ffi::c_char] =
            &[vk::KHR_PORTABILITY_SUBSET_NAME.as_ptr()];
        #[cfg(not(target_os = "macos"))]
        let device_ext_names: &[*const std::ffi::c_char] = &[];

        let device_create_info = vk::DeviceCreateInfo {
            s_type: vk::StructureType::DEVICE_CREATE_INFO,
            p_next: std::ptr::null(),
            flags: vk::DeviceCreateFlags::empty(),
            queue_create_info_count: 1,
            p_queue_create_infos: &queue_info,
            enabled_extension_count: device_ext_names.len() as u32,
            pp_enabled_extension_names: if device_ext_names.is_empty() {
                std::ptr::null()
            } else {
                device_ext_names.as_ptr()
            },
            p_enabled_features: &features,
            ..Default::default()
        };

        let device = unsafe {
            instance
                .create_device(physical_device, &device_create_info, None)
                .context("vkCreateDevice")?
        };

        let queue = unsafe { device.get_device_queue(queue_family_index, 0) };

        let empty_pipeline_cache_ci = vk::PipelineCacheCreateInfo {
            s_type: vk::StructureType::PIPELINE_CACHE_CREATE_INFO,
            p_next: std::ptr::null(),
            flags: vk::PipelineCacheCreateFlags::empty(),
            initial_data_size: 0,
            p_initial_data: std::ptr::null(),
            ..Default::default()
        };

        let disk_blob: Option<Vec<u8>> = std::env::var("CUZK_VK_PIPELINE_CACHE")
            .ok()
            .filter(|s| !s.is_empty())
            .map(PathBuf::from)
            .and_then(|p| std::fs::read(p).ok())
            .filter(|b| !b.is_empty());

        let pipeline_cache = unsafe {
            if let Some(ref blob) = disk_blob {
                let warm_ci = vk::PipelineCacheCreateInfo {
                    s_type: vk::StructureType::PIPELINE_CACHE_CREATE_INFO,
                    p_next: std::ptr::null(),
                    flags: vk::PipelineCacheCreateFlags::empty(),
                    initial_data_size: blob.len(),
                    p_initial_data: blob.as_ptr().cast(),
                    ..Default::default()
                };
                match device.create_pipeline_cache(&warm_ci, None) {
                    Ok(c) => c,
                    Err(_) => {
                        // Wrong ICD / corrupted blob — fresh cache (same as no env).
                        device
                            .create_pipeline_cache(&empty_pipeline_cache_ci, None)
                            .map_err(|e2| {
                                anyhow::anyhow!(
                                    "vkCreatePipelineCache (warm blob rejected, empty failed {e2:?})"
                                )
                            })?
                    }
                }
            } else {
                device
                    .create_pipeline_cache(&empty_pipeline_cache_ci, None)
                    .context("vkCreatePipelineCache")?
            }
        };

        Ok(Self {
            _entry: entry,
            instance,
            physical_device,
            device,
            queue,
            queue_family_index,
            pipeline_cache,
        })
    }

    /// `vkGetPipelineCacheData` — binary blob suitable for [`Self::pipeline_cache_save_to_path`]
    /// or for seeding another process via **`CUZK_VK_PIPELINE_CACHE`**.
    pub fn pipeline_cache_bytes(&self) -> Result<Vec<u8>> {
        unsafe { self.device.get_pipeline_cache_data(self.pipeline_cache) }
            .map_err(|e| anyhow::anyhow!("vkGetPipelineCacheData: {:?}", e))
    }

    /// Atomically write the current cache blob (replace-on-rename).
    pub fn pipeline_cache_save_to_path(&self, path: &Path) -> Result<()> {
        let bytes = self.pipeline_cache_bytes()?;
        let tmp = path.with_extension("pipeline_cache_tmp");
        std::fs::write(&tmp, &bytes).with_context(|| format!("write {}", tmp.display()))?;
        std::fs::rename(&tmp, path).with_context(|| format!("rename {} -> {}", tmp.display(), path.display()))?;
        Ok(())
    }

    /// If **`CUZK_VK_PIPELINE_CACHE`** is set, save there (same path used when loading in [`Self::new`]).
    pub fn pipeline_cache_save_from_env(&self) -> Result<()> {
        let Some(raw) = std::env::var_os("CUZK_VK_PIPELINE_CACHE") else {
            return Ok(());
        };
        if raw.is_empty() {
            return Ok(());
        }
        self.pipeline_cache_save_to_path(Path::new(&raw))
    }
}

impl Drop for VulkanDevice {
    fn drop(&mut self) {
        unsafe {
            self.device
                .destroy_pipeline_cache(self.pipeline_cache, None);
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
