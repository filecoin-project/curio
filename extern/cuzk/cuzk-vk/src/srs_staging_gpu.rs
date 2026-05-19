//! **Milestone B₂ §8.3 C.2:** move SRS-sized blobs to **device-local** (or device-preferred)
//! memory via `vkCmdCopyBuffer`.
//!
//! - [`srs_staging_device_local_upload`] — staging → device, **no readback** (blocking until complete).
//! - [`srs_staging_device_local_upload_submit_async`] — submit the same transfer **without waiting**;
//!   pairs with [`SrsDeviceLocalUploadInFlight::wait_finish`] or [`SrsDeviceLocalUploadGuard::finish`]. Uses [`crate::device::VulkanDevice::queue_compute_1`]
//!   when present so the copy can overlap Fr NTT work on [`crate::device::VulkanDevice::queue`].
//! - [`srs_staging_device_local_upload_while`] — upload on a **scoped thread** while CPU work runs (host overlap helper).
//! - [`srs_device_local_buffer_download`] — device → staging for **tests / verification** only.
//! - [`srs_staging_device_local_roundtrip`] — upload + download + equality (integration smoke).

use anyhow::{bail, Context, Result};
use ash::vk;

use crate::device::VulkanDevice;
use crate::vk_oneshot::find_memory_type;

/// Device-resident bytes after [`srs_staging_device_local_upload`] (no host copy until download).
pub struct SrsDeviceLocalBuffer {
    pub buf: vk::Buffer,
    pub mem: vk::DeviceMemory,
    pub size: vk::DeviceSize,
}

impl SrsDeviceLocalBuffer {
    /// Release GPU resources (buffer was created with [`srs_staging_device_local_upload`]).
    pub fn destroy(self, dev: &VulkanDevice) {
        unsafe {
            dev.device.destroy_buffer(self.buf, None);
            dev.device.free_memory(self.mem, None);
        }
    }
}

/// In-flight SRS staging → device-local copy after [`srs_staging_device_local_upload_submit_async`].
///
/// Staging resources stay alive until [`Self::wait_finish`]; the device buffer is safe to use after the fence signals.
pub struct SrsDeviceLocalUploadInFlight {
    pub device_blob: SrsDeviceLocalBuffer,
    fence: vk::Fence,
    staging_buf: vk::Buffer,
    staging_mem: vk::DeviceMemory,
    cmd_pool: vk::CommandPool,
}

impl SrsDeviceLocalUploadInFlight {
    /// Wait for the transfer to finish, free staging + pool + fence, return the device-local buffer.
    pub fn wait_finish(self, dev: &VulkanDevice) -> Result<SrsDeviceLocalBuffer> {
        let SrsDeviceLocalUploadInFlight {
            device_blob,
            fence,
            staging_buf,
            staging_mem,
            cmd_pool,
        } = self;
        unsafe {
            dev.device
                .wait_for_fences(&[fence], true, u64::MAX)
                .context("wait_for_fences (SRS upload)")?;
            dev.device.destroy_fence(fence, None);
            dev.device.destroy_command_pool(cmd_pool, None);
            dev.device.destroy_buffer(staging_buf, None);
            dev.device.free_memory(staging_mem, None);
        }
        Ok(device_blob)
    }
}

/// RAII submit for §8.3 C.2: if [`Self::finish`] is not called (early `?`), [`Drop`] waits and frees staging.
pub struct SrsDeviceLocalUploadGuard<'a> {
    dev: &'a VulkanDevice,
    flight: Option<SrsDeviceLocalUploadInFlight>,
}

impl<'a> SrsDeviceLocalUploadGuard<'a> {
    pub fn submit(dev: &'a VulkanDevice, bytes: &[u8]) -> Result<Self> {
        Ok(Self {
            dev,
            flight: Some(srs_staging_device_local_upload_submit_async(dev, bytes)?),
        })
    }

    pub fn finish(mut self) -> Result<SrsDeviceLocalBuffer> {
        let flight = self
            .flight
            .take()
            .expect("SrsDeviceLocalUploadGuard::finish once");
        let dev = self.dev;
        std::mem::forget(self);
        flight.wait_finish(dev)
    }
}

impl Drop for SrsDeviceLocalUploadGuard<'_> {
    fn drop(&mut self) {
        if let Some(flight) = self.flight.take() {
            let _ = flight.wait_finish(self.dev);
        }
    }
}

fn create_buffer_with_memory(
    dev: &VulkanDevice,
    size: vk::DeviceSize,
    usage: vk::BufferUsageFlags,
    mem_props: vk::MemoryPropertyFlags,
) -> Result<(vk::Buffer, vk::DeviceMemory)> {
    let buf = unsafe {
        dev.device.create_buffer(
            &vk::BufferCreateInfo::default()
                .size(size)
                .usage(usage)
                .sharing_mode(vk::SharingMode::EXCLUSIVE),
            None,
        )
    }
    .context("create_buffer")?;
    let req = unsafe { dev.device.get_buffer_memory_requirements(buf) };
    let mem_index = find_memory_type(&dev.instance, dev.physical_device, req, mem_props)
        .with_context(|| format!("no memory type for flags {:?}", mem_props))?;
    let mem = unsafe {
        dev.device.allocate_memory(
            &vk::MemoryAllocateInfo::default()
                .allocation_size(req.size)
                .memory_type_index(mem_index),
            None,
        )
    }
    .context("allocate_memory")?;
    unsafe { dev.device.bind_buffer_memory(buf, mem, 0) }?;
    Ok((buf, mem))
}

fn pick_device_local_pair(
    dev: &VulkanDevice,
    size: vk::DeviceSize,
) -> Result<(vk::Buffer, vk::DeviceMemory)> {
    let dev_usage = vk::BufferUsageFlags::TRANSFER_DST | vk::BufferUsageFlags::TRANSFER_SRC;
    let cand_flags = [
        vk::MemoryPropertyFlags::DEVICE_LOCAL,
        vk::MemoryPropertyFlags::DEVICE_LOCAL | vk::MemoryPropertyFlags::HOST_VISIBLE,
        vk::MemoryPropertyFlags::HOST_VISIBLE | vk::MemoryPropertyFlags::HOST_COHERENT,
    ];
    let mut last_err: Option<anyhow::Error> = None;
    for &flags in &cand_flags {
        match create_buffer_with_memory(dev, size, dev_usage, flags) {
            Ok(p) => return Ok(p),
            Err(e) => last_err = Some(e),
        }
    }
    Err(last_err
        .map(|e| anyhow::anyhow!("device-local buffer allocation failed: {e:?}"))
        .unwrap_or_else(|| anyhow::anyhow!("device-local buffer allocation failed")))
}

/// Upload `bytes` to a device-preferring buffer via staging `vkCmdCopyBuffer`; wait for completion.
/// Buffer usage includes `TRANSFER_SRC` so [`srs_device_local_buffer_download`] / roundtrip can verify.
/// `bytes` must be non-empty (use [`srs_staging_device_local_roundtrip`] for empty round-trip semantics).
pub fn srs_staging_device_local_upload(
    dev: &VulkanDevice,
    bytes: &[u8],
) -> Result<SrsDeviceLocalBuffer> {
    if bytes.is_empty() {
        bail!("srs_staging_device_local_upload: empty slice");
    }
    let flight = srs_staging_device_local_upload_submit_async(dev, bytes)?;
    flight.wait_finish(dev)
}

/// Submit SRS staging → device-local copy without waiting. Uses the **second** compute queue when
/// [`VulkanDevice::queue_compute_1`] is `Some`, otherwise the primary queue (FIFO with later submits).
pub fn srs_staging_device_local_upload_submit_async(
    dev: &VulkanDevice,
    bytes: &[u8],
) -> Result<SrsDeviceLocalUploadInFlight> {
    if bytes.is_empty() {
        bail!("srs_staging_device_local_upload_submit_async: empty slice");
    }
    unsafe { srs_staging_device_local_upload_submit_async_inner(dev, bytes) }
}

unsafe fn srs_staging_device_local_upload_submit_async_inner(
    dev: &VulkanDevice,
    bytes: &[u8],
) -> Result<SrsDeviceLocalUploadInFlight> {
    let size = bytes.len() as vk::DeviceSize;

    let (staging_up, mem_up) = create_buffer_with_memory(
        dev,
        size,
        vk::BufferUsageFlags::TRANSFER_SRC,
        vk::MemoryPropertyFlags::HOST_VISIBLE | vk::MemoryPropertyFlags::HOST_COHERENT,
    )?;

    let up_ptr = dev
        .device
        .map_memory(mem_up, 0, size, vk::MemoryMapFlags::empty())
        .context("map staging up")?;
    std::ptr::copy_nonoverlapping(bytes.as_ptr(), up_ptr as *mut u8, bytes.len());
    dev.device.unmap_memory(mem_up);

    let (buf_dev, mem_dev) = pick_device_local_pair(dev, size)?;

    let cmd_pool = dev
        .device
        .create_command_pool(
            &vk::CommandPoolCreateInfo::default()
                .queue_family_index(dev.queue_family_index)
                .flags(vk::CommandPoolCreateFlags::empty()),
            None,
        )
        .context("create_command_pool")?;
    let cmd_buf = dev
        .device
        .allocate_command_buffers(
            &vk::CommandBufferAllocateInfo::default()
                .command_pool(cmd_pool)
                .level(vk::CommandBufferLevel::PRIMARY)
                .command_buffer_count(1),
        )
        .context("allocate_command_buffers")?[0];

    dev.device
        .begin_command_buffer(
            cmd_buf,
            &vk::CommandBufferBeginInfo::default()
                .flags(vk::CommandBufferUsageFlags::ONE_TIME_SUBMIT),
        )
        .context("begin_command_buffer")?;

    let b_up = vk::BufferMemoryBarrier::default()
        .buffer(staging_up)
        .offset(0)
        .size(size)
        .src_access_mask(vk::AccessFlags::HOST_WRITE)
        .dst_access_mask(vk::AccessFlags::TRANSFER_READ)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    let b_dev_w = vk::BufferMemoryBarrier::default()
        .buffer(buf_dev)
        .offset(0)
        .size(size)
        .src_access_mask(vk::AccessFlags::empty())
        .dst_access_mask(vk::AccessFlags::TRANSFER_WRITE)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    dev.device.cmd_pipeline_barrier(
        cmd_buf,
        vk::PipelineStageFlags::HOST,
        vk::PipelineStageFlags::TRANSFER,
        vk::DependencyFlags::empty(),
        &[],
        &[b_up, b_dev_w],
        &[],
    );

    dev.device.cmd_copy_buffer(
        cmd_buf,
        staging_up,
        buf_dev,
        &[vk::BufferCopy::default().size(size)],
    );

    let b_dev_ready = vk::BufferMemoryBarrier::default()
        .buffer(buf_dev)
        .offset(0)
        .size(size)
        .src_access_mask(vk::AccessFlags::TRANSFER_WRITE)
        .dst_access_mask(vk::AccessFlags::TRANSFER_READ)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    dev.device.cmd_pipeline_barrier(
        cmd_buf,
        vk::PipelineStageFlags::TRANSFER,
        vk::PipelineStageFlags::TRANSFER,
        vk::DependencyFlags::empty(),
        &[],
        &[b_dev_ready],
        &[],
    );

    dev.device
        .end_command_buffer(cmd_buf)
        .context("end_command_buffer")?;

    let fence = dev
        .device
        .create_fence(&vk::FenceCreateInfo::default(), None)
        .context("create_fence")?;
    let upload_queue = dev.queue_compute_1.unwrap_or(dev.queue);
    let submit = vk::SubmitInfo::default().command_buffers(std::slice::from_ref(&cmd_buf));
    dev.device
        .queue_submit(upload_queue, std::slice::from_ref(&submit), fence)
        .context("queue_submit (SRS upload)")?;

    Ok(SrsDeviceLocalUploadInFlight {
        device_blob: SrsDeviceLocalBuffer {
            buf: buf_dev,
            mem: mem_dev,
            size,
        },
        fence,
        staging_buf: staging_up,
        staging_mem: mem_up,
        cmd_pool,
    })
}

/// Copy device-local buffer to host (for tests; not used on the hot prove path).
pub fn srs_device_local_buffer_download(
    dev: &VulkanDevice,
    blob: &SrsDeviceLocalBuffer,
) -> Result<Vec<u8>> {
    unsafe { srs_device_local_buffer_download_inner(dev, blob) }
}

unsafe fn srs_device_local_buffer_download_inner(
    dev: &VulkanDevice,
    blob: &SrsDeviceLocalBuffer,
) -> Result<Vec<u8>> {
    if blob.size == 0 {
        return Ok(Vec::new());
    }
    let size = blob.size;

    let (staging_down, mem_down) = create_buffer_with_memory(
        dev,
        size,
        vk::BufferUsageFlags::TRANSFER_DST,
        vk::MemoryPropertyFlags::HOST_VISIBLE | vk::MemoryPropertyFlags::HOST_COHERENT,
    )?;

    let cmd_pool = dev
        .device
        .create_command_pool(
            &vk::CommandPoolCreateInfo::default()
                .queue_family_index(dev.queue_family_index)
                .flags(vk::CommandPoolCreateFlags::empty()),
            None,
        )
        .context("create_command_pool (download)")?;
    let cmd_buf = dev
        .device
        .allocate_command_buffers(
            &vk::CommandBufferAllocateInfo::default()
                .command_pool(cmd_pool)
                .level(vk::CommandBufferLevel::PRIMARY)
                .command_buffer_count(1),
        )
        .context("allocate_command_buffers (download)")?[0];

    dev.device
        .begin_command_buffer(
            cmd_buf,
            &vk::CommandBufferBeginInfo::default()
                .flags(vk::CommandBufferUsageFlags::ONE_TIME_SUBMIT),
        )
        .context("begin_command_buffer (download)")?;

    let b_down_w = vk::BufferMemoryBarrier::default()
        .buffer(staging_down)
        .offset(0)
        .size(size)
        .src_access_mask(vk::AccessFlags::empty())
        .dst_access_mask(vk::AccessFlags::TRANSFER_WRITE)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    let b_dev_r = vk::BufferMemoryBarrier::default()
        .buffer(blob.buf)
        .offset(0)
        .size(size)
        .src_access_mask(vk::AccessFlags::TRANSFER_READ)
        .dst_access_mask(vk::AccessFlags::TRANSFER_READ)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    dev.device.cmd_pipeline_barrier(
        cmd_buf,
        vk::PipelineStageFlags::HOST,
        vk::PipelineStageFlags::TRANSFER,
        vk::DependencyFlags::empty(),
        &[],
        &[b_dev_r, b_down_w],
        &[],
    );

    dev.device.cmd_copy_buffer(
        cmd_buf,
        blob.buf,
        staging_down,
        &[vk::BufferCopy::default().size(size)],
    );

    let b_down_read = vk::BufferMemoryBarrier::default()
        .buffer(staging_down)
        .offset(0)
        .size(size)
        .src_access_mask(vk::AccessFlags::TRANSFER_WRITE)
        .dst_access_mask(vk::AccessFlags::HOST_READ)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    dev.device.cmd_pipeline_barrier(
        cmd_buf,
        vk::PipelineStageFlags::TRANSFER,
        vk::PipelineStageFlags::HOST,
        vk::DependencyFlags::empty(),
        &[],
        &[b_down_read],
        &[],
    );

    dev.device
        .end_command_buffer(cmd_buf)
        .context("end_command_buffer (download)")?;

    let fence = dev
        .device
        .create_fence(&vk::FenceCreateInfo::default(), None)
        .context("create_fence (download)")?;
    let submit = vk::SubmitInfo::default().command_buffers(std::slice::from_ref(&cmd_buf));
    dev.device
        .queue_submit(dev.queue, std::slice::from_ref(&submit), fence)
        .context("queue_submit (download)")?;
    dev.device
        .wait_for_fences(&[fence], true, u64::MAX)
        .context("wait_for_fences (download)")?;

    let down_ptr = dev
        .device
        .map_memory(mem_down, 0, size, vk::MemoryMapFlags::empty())
        .context("map staging down")?;
    let mut out = vec![0u8; size as usize];
    std::ptr::copy_nonoverlapping(down_ptr as *const u8, out.as_mut_ptr(), out.len());
    dev.device.unmap_memory(mem_down);

    dev.device.destroy_fence(fence, None);
    dev.device.destroy_command_pool(cmd_pool, None);
    dev.device.destroy_buffer(staging_down, None);
    dev.device.free_memory(mem_down, None);

    Ok(out)
}

/// SRS **H₂D + CPU overlap (B₂ §8.3 C.2):** [`srs_staging_device_local_upload`] runs on a scoped
/// background thread while `work()` executes on the caller thread (e.g. host SRS decode). Joins
/// before return; caller must [`SrsDeviceLocalBuffer::destroy`] the buffer when done.
pub fn srs_staging_device_local_upload_while<T>(
    dev: &VulkanDevice,
    bytes: &[u8],
    work: impl FnOnce() -> T + Send,
) -> Result<(SrsDeviceLocalBuffer, T)>
where
    T: Send,
{
    if bytes.is_empty() {
        bail!("srs_staging_device_local_upload_while: empty slice");
    }
    std::thread::scope(|s| {
        let h = s.spawn(|| srs_staging_device_local_upload(dev, bytes));
        let t = work();
        let buf = h.join().map_err(|_| {
            anyhow::anyhow!("srs_staging_device_local_upload_while: upload thread panicked")
        })??;
        Ok((buf, t))
    })
}

/// Upload then download and return bytes (verifies staging + device path in tests).
pub fn srs_staging_device_local_roundtrip(dev: &VulkanDevice, bytes: &[u8]) -> Result<Vec<u8>> {
    if bytes.is_empty() {
        return Ok(Vec::new());
    }
    let uploaded = srs_staging_device_local_upload(dev, bytes)?;
    let out = srs_device_local_buffer_download(dev, &uploaded)?;
    uploaded.destroy(dev);
    Ok(out)
}
