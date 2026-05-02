//! **Dual-queue upload ↔ compute** sketch: `vkCmdCopyBuffer` on [`VulkanDevice::queue_compute_1`],
//! binary semaphore, then `gpu_echo_u32` on [`VulkanDevice::queue`] (copies SSBO word 64 → word 0).
//!
//! Intended as an ordering / barrier template for future SRS staging overlap.

use std::ffi::CString;
use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use ash::vk;

use crate::device::VulkanDevice;
use crate::vk_oneshot::find_memory_type;

const BUF_BYTES: vk::DeviceSize = 1024;
const WORD64_OFF: vk::DeviceSize = 64 * 4;

unsafe fn alloc_buffer(
    dev: &VulkanDevice,
    size: vk::DeviceSize,
    usage: vk::BufferUsageFlags,
    mem_props: vk::MemoryPropertyFlags,
) -> Result<(vk::Buffer, vk::DeviceMemory)> {
    let buf = dev
        .device
        .create_buffer(
            &vk::BufferCreateInfo::default()
                .size(size)
                .usage(usage)
                .sharing_mode(vk::SharingMode::EXCLUSIVE),
            None,
        )
        .context("dual_queue: create_buffer")?;
    let req = dev.device.get_buffer_memory_requirements(buf);
    let mem_index = find_memory_type(&dev.instance, dev.physical_device, req, mem_props)
        .with_context(|| format!("dual_queue: no memory for {:?}", mem_props))?;
    let mem = dev
        .device
        .allocate_memory(
            &vk::MemoryAllocateInfo::default()
                .allocation_size(req.size.max(size))
                .memory_type_index(mem_index),
            None,
        )
        .context("dual_queue: allocate_memory")?;
    dev.device.bind_buffer_memory(buf, mem, 0)?;
    Ok((buf, mem))
}

/// Upload `payload` into word **64** of a device-local SSBO on `queue_compute_1`, then run
/// `gpu_echo_u32` on `queue` so word **0** holds the same value. Requires a second queue on the
/// compute family ([`VulkanDevice::queue_compute_1`]).
pub fn run_gpu_echo_u32_dual_queue(dev: &VulkanDevice, payload: u32) -> Result<u32> {
    let q1 = dev
        .queue_compute_1
        .ok_or_else(|| anyhow::anyhow!("run_gpu_echo_u32_dual_queue: need queue_compute_1"))?;
    unsafe { run_gpu_echo_u32_dual_queue_inner(dev, q1, payload) }
}

unsafe fn run_gpu_echo_u32_dual_queue_inner(
    dev: &VulkanDevice,
    queue_compute_1: vk::Queue,
    payload: u32,
) -> Result<u32> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/gpu_echo_u32.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv gpu_echo_u32")?;

    let (buf_dev, mem_dev) = alloc_buffer(
        dev,
        BUF_BYTES,
        vk::BufferUsageFlags::STORAGE_BUFFER
            | vk::BufferUsageFlags::TRANSFER_DST
            | vk::BufferUsageFlags::TRANSFER_SRC,
        vk::MemoryPropertyFlags::DEVICE_LOCAL,
    )
    .or_else(|_| {
        alloc_buffer(
            dev,
            BUF_BYTES,
            vk::BufferUsageFlags::STORAGE_BUFFER
                | vk::BufferUsageFlags::TRANSFER_DST
                | vk::BufferUsageFlags::TRANSFER_SRC,
            vk::MemoryPropertyFlags::HOST_VISIBLE | vk::MemoryPropertyFlags::HOST_COHERENT,
        )
    })?;

    let (staging_up, mem_up) = alloc_buffer(
        dev,
        BUF_BYTES,
        vk::BufferUsageFlags::TRANSFER_SRC,
        vk::MemoryPropertyFlags::HOST_VISIBLE | vk::MemoryPropertyFlags::HOST_COHERENT,
    )?;
    let up_ptr = dev
        .device
        .map_memory(mem_up, 0, BUF_BYTES, vk::MemoryMapFlags::empty())
        .context("dual_queue: map staging up")? as *mut u8;
    std::ptr::write_bytes(up_ptr, 0, BUF_BYTES as usize);
    std::ptr::write_unaligned(up_ptr.add(WORD64_OFF as usize) as *mut u32, payload.to_le());
    dev.device.unmap_memory(mem_up);

    let (staging_down, mem_down) = alloc_buffer(
        dev,
        4,
        vk::BufferUsageFlags::TRANSFER_DST,
        vk::MemoryPropertyFlags::HOST_VISIBLE | vk::MemoryPropertyFlags::HOST_COHERENT,
    )?;

    let sem = dev
        .device
        .create_semaphore(&vk::SemaphoreCreateInfo::default(), None)
        .context("dual_queue: create_semaphore")?;

    let entry = CString::new("main").unwrap();
    let shader_mod = dev
        .device
        .create_shader_module(
            &vk::ShaderModuleCreateInfo::default()
                .code(&spirv_words)
                .flags(vk::ShaderModuleCreateFlags::empty()),
            None,
        )
        .context("dual_queue: create_shader_module")?;

    let binding = vk::DescriptorSetLayoutBinding::default()
        .binding(0)
        .descriptor_type(vk::DescriptorType::STORAGE_BUFFER)
        .descriptor_count(1)
        .stage_flags(vk::ShaderStageFlags::COMPUTE);

    let desc_layout = dev
        .device
        .create_descriptor_set_layout(
            &vk::DescriptorSetLayoutCreateInfo::default().bindings(std::slice::from_ref(&binding)),
            None,
        )
        .context("dual_queue: create_descriptor_set_layout")?;

    let pipeline_layout = dev
        .device
        .create_pipeline_layout(
            &vk::PipelineLayoutCreateInfo::default().set_layouts(std::slice::from_ref(&desc_layout)),
            None,
        )
        .context("dual_queue: create_pipeline_layout")?;

    let stage = vk::PipelineShaderStageCreateInfo::default()
        .stage(vk::ShaderStageFlags::COMPUTE)
        .module(shader_mod)
        .name(&entry);

    let cpci = vk::ComputePipelineCreateInfo::default()
        .layout(pipeline_layout)
        .stage(stage);

    let pipeline = match dev
        .device
        .create_compute_pipelines(dev.pipeline_cache, std::slice::from_ref(&cpci), None)
    {
        Ok(mut v) => v.pop().expect("one pipeline"),
        Err((_, e)) => {
            dev.device.destroy_shader_module(shader_mod, None);
            dev.device.destroy_descriptor_set_layout(desc_layout, None);
            dev.device.destroy_semaphore(sem, None);
            dev.device.free_memory(mem_down, None);
            dev.device.destroy_buffer(staging_down, None);
            dev.device.free_memory(mem_up, None);
            dev.device.destroy_buffer(staging_up, None);
            dev.device.free_memory(mem_dev, None);
            dev.device.destroy_buffer(buf_dev, None);
            anyhow::bail!("dual_queue: create_compute_pipelines: {:?}", e);
        }
    };

    let pool_sizes = [vk::DescriptorPoolSize {
        ty: vk::DescriptorType::STORAGE_BUFFER,
        descriptor_count: 1,
    }];
    let desc_pool = dev
        .device
        .create_descriptor_pool(
            &vk::DescriptorPoolCreateInfo::default()
                .pool_sizes(&pool_sizes)
                .max_sets(1),
            None,
        )
        .context("dual_queue: create_descriptor_pool")?;

    let desc_sets = dev
        .device
        .allocate_descriptor_sets(
            &vk::DescriptorSetAllocateInfo::default()
                .descriptor_pool(desc_pool)
                .set_layouts(std::slice::from_ref(&desc_layout)),
        )
        .context("dual_queue: allocate_descriptor_sets")?;
    let desc_set = desc_sets[0];

    let buf_info = vk::DescriptorBufferInfo::default()
        .buffer(buf_dev)
        .offset(0)
        .range(BUF_BYTES);

    dev.device.update_descriptor_sets(
        &[vk::WriteDescriptorSet::default()
            .dst_set(desc_set)
            .dst_binding(0)
            .descriptor_type(vk::DescriptorType::STORAGE_BUFFER)
            .buffer_info(std::slice::from_ref(&buf_info))],
        &[],
    );

    let cmd_pool = dev
        .device
        .create_command_pool(
            &vk::CommandPoolCreateInfo::default()
                .queue_family_index(dev.queue_family_index)
                .flags(vk::CommandPoolCreateFlags::empty()),
            None,
        )
        .context("dual_queue: create_command_pool")?;

    let cmd_bufs = dev.device.allocate_command_buffers(
        &vk::CommandBufferAllocateInfo::default()
            .command_pool(cmd_pool)
            .level(vk::CommandBufferLevel::PRIMARY)
            .command_buffer_count(2),
    )?;
    let cmd_copy = cmd_bufs[0];
    let cmd_compute = cmd_bufs[1];

    // --- queue_compute_1: copy upload into word 64, signal semaphore ---
    dev.device.begin_command_buffer(
        cmd_copy,
        &vk::CommandBufferBeginInfo::default().flags(vk::CommandBufferUsageFlags::ONE_TIME_SUBMIT),
    )?;
    let b_up = vk::BufferMemoryBarrier::default()
        .buffer(staging_up)
        .offset(0)
        .size(BUF_BYTES)
        .src_access_mask(vk::AccessFlags::HOST_WRITE)
        .dst_access_mask(vk::AccessFlags::TRANSFER_READ)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    let b_dev_pre = vk::BufferMemoryBarrier::default()
        .buffer(buf_dev)
        .offset(0)
        .size(BUF_BYTES)
        .src_access_mask(vk::AccessFlags::empty())
        .dst_access_mask(vk::AccessFlags::TRANSFER_WRITE)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    dev.device.cmd_pipeline_barrier(
        cmd_copy,
        vk::PipelineStageFlags::HOST,
        vk::PipelineStageFlags::TRANSFER,
        vk::DependencyFlags::empty(),
        &[],
        &[b_up, b_dev_pre],
        &[],
    );
    dev.device.cmd_copy_buffer(
        cmd_copy,
        staging_up,
        buf_dev,
        &[vk::BufferCopy {
            src_offset: WORD64_OFF,
            dst_offset: WORD64_OFF,
            size: 4,
        }],
    );
    let b_dev_xfer = vk::BufferMemoryBarrier::default()
        .buffer(buf_dev)
        .offset(WORD64_OFF)
        .size(4)
        .src_access_mask(vk::AccessFlags::TRANSFER_WRITE)
        .dst_access_mask(vk::AccessFlags::empty())
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    dev.device.cmd_pipeline_barrier(
        cmd_copy,
        vk::PipelineStageFlags::TRANSFER,
        vk::PipelineStageFlags::TRANSFER,
        vk::DependencyFlags::empty(),
        &[],
        &[b_dev_xfer],
        &[],
    );
    dev.device.end_command_buffer(cmd_copy)?;

    let submit_copy = vk::SubmitInfo::default()
        .command_buffers(std::slice::from_ref(&cmd_copy))
        .signal_semaphores(std::slice::from_ref(&sem));

    dev.device
        .queue_submit(queue_compute_1, std::slice::from_ref(&submit_copy), vk::Fence::null())
        .context("dual_queue: queue_submit copy")?;

    // --- queue: wait semaphore, echo compute, copy word 0 to staging ---
    dev.device.begin_command_buffer(
        cmd_compute,
        &vk::CommandBufferBeginInfo::default().flags(vk::CommandBufferUsageFlags::ONE_TIME_SUBMIT),
    )?;
    let b_vis = vk::BufferMemoryBarrier::default()
        .buffer(buf_dev)
        .offset(WORD64_OFF)
        .size(4)
        .src_access_mask(vk::AccessFlags::TRANSFER_WRITE)
        .dst_access_mask(vk::AccessFlags::SHADER_READ)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    dev.device.cmd_pipeline_barrier(
        cmd_compute,
        vk::PipelineStageFlags::TRANSFER,
        vk::PipelineStageFlags::COMPUTE_SHADER,
        vk::DependencyFlags::empty(),
        &[],
        &[b_vis],
        &[],
    );
    dev.device
        .cmd_bind_pipeline(cmd_compute, vk::PipelineBindPoint::COMPUTE, pipeline);
    dev.device.cmd_bind_descriptor_sets(
        cmd_compute,
        vk::PipelineBindPoint::COMPUTE,
        pipeline_layout,
        0,
        &[desc_set],
        &[],
    );
    dev.device.cmd_dispatch(cmd_compute, 1, 1, 1);
    let b_post = vk::BufferMemoryBarrier::default()
        .buffer(buf_dev)
        .offset(0)
        .size(4)
        .src_access_mask(vk::AccessFlags::SHADER_WRITE)
        .dst_access_mask(vk::AccessFlags::TRANSFER_READ)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    let b_down = vk::BufferMemoryBarrier::default()
        .buffer(staging_down)
        .offset(0)
        .size(4)
        .src_access_mask(vk::AccessFlags::empty())
        .dst_access_mask(vk::AccessFlags::TRANSFER_WRITE)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    dev.device.cmd_pipeline_barrier(
        cmd_compute,
        vk::PipelineStageFlags::COMPUTE_SHADER,
        vk::PipelineStageFlags::TRANSFER,
        vk::DependencyFlags::empty(),
        &[],
        &[b_post, b_down],
        &[],
    );
    dev.device.cmd_copy_buffer(
        cmd_compute,
        buf_dev,
        staging_down,
        &[vk::BufferCopy {
            src_offset: 0,
            dst_offset: 0,
            size: 4,
        }],
    );
    let b_down_host = vk::BufferMemoryBarrier::default()
        .buffer(staging_down)
        .offset(0)
        .size(4)
        .src_access_mask(vk::AccessFlags::TRANSFER_WRITE)
        .dst_access_mask(vk::AccessFlags::HOST_READ)
        .src_queue_family_index(vk::QUEUE_FAMILY_IGNORED)
        .dst_queue_family_index(vk::QUEUE_FAMILY_IGNORED);
    dev.device.cmd_pipeline_barrier(
        cmd_compute,
        vk::PipelineStageFlags::TRANSFER,
        vk::PipelineStageFlags::HOST,
        vk::DependencyFlags::empty(),
        &[],
        &[b_down_host],
        &[],
    );
    dev.device.end_command_buffer(cmd_compute)?;

    let wait_stage = [vk::PipelineStageFlags::COMPUTE_SHADER];
    let submit_compute = vk::SubmitInfo::default()
        .command_buffers(std::slice::from_ref(&cmd_compute))
        .wait_semaphores(std::slice::from_ref(&sem))
        .wait_dst_stage_mask(&wait_stage);

    let fence = dev.device.create_fence(&vk::FenceCreateInfo::default(), None)?;
    dev.device
        .queue_submit(dev.queue, std::slice::from_ref(&submit_compute), fence)
        .context("dual_queue: queue_submit compute")?;
    dev.device.wait_for_fences(&[fence], true, u64::MAX)?;

    let down_ptr = dev.device.map_memory(mem_down, 0, 4, vk::MemoryMapFlags::empty())? as *const u8;
    let mut word0 = [0u8; 4];
    std::ptr::copy_nonoverlapping(down_ptr, word0.as_mut_ptr(), 4);
    dev.device.unmap_memory(mem_down);

    dev.device.destroy_fence(fence, None);
    dev.device.destroy_command_pool(cmd_pool, None);
    dev.device.destroy_descriptor_pool(desc_pool, None);
    dev.device.destroy_pipeline(pipeline, None);
    dev.device.destroy_pipeline_layout(pipeline_layout, None);
    dev.device.destroy_descriptor_set_layout(desc_layout, None);
    dev.device.destroy_shader_module(shader_mod, None);
    dev.device.destroy_semaphore(sem, None);
    dev.device.free_memory(mem_down, None);
    dev.device.destroy_buffer(staging_down, None);
    dev.device.free_memory(mem_up, None);
    dev.device.destroy_buffer(staging_up, None);
    dev.device.free_memory(mem_dev, None);
    dev.device.destroy_buffer(buf_dev, None);

    Ok(u32::from_le_bytes(word0))
}
