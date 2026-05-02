//! **Milestone B₂ §8.3 C.2:** move SRS-sized blobs to **device-local** (or device-preferred)
//! memory via `vkCmdCopyBuffer`.
//!
//! - [`srs_staging_device_local_upload`] — staging → device, **no readback** (hot-path building block).
//! - [`srs_staging_device_local_upload_while`] — upload on a **scoped thread** while CPU work runs (**B₂** host overlap).
//!   Full transfer-queue / NTT pipelining remains future work.
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
pub fn srs_staging_device_local_upload(dev: &VulkanDevice, bytes: &[u8]) -> Result<SrsDeviceLocalBuffer> {
    if bytes.is_empty() {
        bail!("srs_staging_device_local_upload: empty slice");
    }
    unsafe { srs_staging_device_local_upload_inner(dev, bytes) }
}

unsafe fn srs_staging_device_local_upload_inner(
    dev: &VulkanDevice,
    bytes: &[u8],
) -> Result<SrsDeviceLocalBuffer> {
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
            &vk::CommandBufferBeginInfo::default().flags(vk::CommandBufferUsageFlags::ONE_TIME_SUBMIT),
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

    dev.device.end_command_buffer(cmd_buf).context("end_command_buffer")?;

    let fence = dev
        .device
        .create_fence(&vk::FenceCreateInfo::default(), None)
        .context("create_fence")?;
    let submit = vk::SubmitInfo::default().command_buffers(std::slice::from_ref(&cmd_buf));
    dev.device
        .queue_submit(dev.queue, std::slice::from_ref(&submit), fence)
        .context("queue_submit")?;
    dev.device
        .wait_for_fences(&[fence], true, u64::MAX)
        .context("wait_for_fences")?;

    dev.device.destroy_fence(fence, None);
    dev.device.destroy_command_pool(cmd_pool, None);
    dev.device.destroy_buffer(staging_up, None);
    dev.device.free_memory(mem_up, None);

    Ok(SrsDeviceLocalBuffer {
        buf: buf_dev,
        mem: mem_dev,
        size,
    })
}

/// Copy device-local buffer to host (for tests; not used on the hot prove path).
pub fn srs_device_local_buffer_download(dev: &VulkanDevice, blob: &SrsDeviceLocalBuffer) -> Result<Vec<u8>> {
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
            &vk::CommandBufferBeginInfo::default().flags(vk::CommandBufferUsageFlags::ONE_TIME_SUBMIT),
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
