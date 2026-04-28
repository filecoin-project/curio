//! Shared Vulkan one-dispatch compute + one storage buffer (host-visible) pattern.

use std::ffi::CString;

use anyhow::{Context, Result};
use ash::vk;

use crate::device::VulkanDevice;

pub(crate) fn find_memory_type(
    instance: &ash::Instance,
    pdev: vk::PhysicalDevice,
    req: vk::MemoryRequirements,
    flags: vk::MemoryPropertyFlags,
) -> Option<u32> {
    let mem_props = unsafe { instance.get_physical_device_memory_properties(pdev) };
    for i in 0..mem_props.memory_type_count {
        let suitable = (req.memory_type_bits & (1 << i)) != 0;
        let props = mem_props.memory_types[i as usize].property_flags;
        if suitable && props.contains(flags) {
            return Some(i);
        }
    }
    None
}

/// Run a compute shader (`entry` = `"main"`) with one storage buffer, one `dispatch` of `(gx,gy,gz)`.
///
/// `buffer_size` is allocated (at least `req.size`); `map_size` is mapped / barrier range (often `buffer_size`).
/// Writes `write` bytes at offset 0 before dispatch; reads `read_len` bytes at offset 0 after completion.
pub(crate) unsafe fn run_compute_1x_storage_buffer(
    dev: &VulkanDevice,
    spirv_words: &[u32],
    buffer_size: u64,
    map_size: u64,
    dispatch: (u32, u32, u32),
    write: &[u8],
    read_len: usize,
    read_out: &mut [u8],
) -> Result<()> {
    anyhow::ensure!(
        write.len() as u64 <= buffer_size && read_len <= map_size as usize,
        "write/read larger than buffer/map"
    );
    read_out[..read_len].fill(0);

    let entry = CString::new("main").unwrap();

    let shader_mod = dev
        .device
        .create_shader_module(
            &vk::ShaderModuleCreateInfo::default()
                .code(spirv_words)
                .flags(vk::ShaderModuleCreateFlags::empty()),
            None,
        )
        .context("create_shader_module")?;

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
        .context("create_descriptor_set_layout")?;

    let pipeline_layout = dev
        .device
        .create_pipeline_layout(
            &vk::PipelineLayoutCreateInfo::default().set_layouts(std::slice::from_ref(&desc_layout)),
            None,
        )
        .context("create_pipeline_layout")?;

    let stage = vk::PipelineShaderStageCreateInfo::default()
        .stage(vk::ShaderStageFlags::COMPUTE)
        .module(shader_mod)
        .name(&entry);

    let cpci = vk::ComputePipelineCreateInfo::default()
        .layout(pipeline_layout)
        .stage(stage);

    let pipeline = match dev
        .device
        .create_compute_pipelines(vk::PipelineCache::null(), std::slice::from_ref(&cpci), None)
    {
        Ok(mut v) => v.pop().expect("one pipeline"),
        Err((_, e)) => anyhow::bail!("create_compute_pipelines: {:?}", e),
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
        .context("create_descriptor_pool")?;

    let desc_sets = dev
        .device
        .allocate_descriptor_sets(
            &vk::DescriptorSetAllocateInfo::default()
                .descriptor_pool(desc_pool)
                .set_layouts(std::slice::from_ref(&desc_layout)),
        )
        .context("allocate_descriptor_sets")?;
    let desc_set = desc_sets[0];

    let buf = dev
        .device
        .create_buffer(
            &vk::BufferCreateInfo::default()
                .size(buffer_size)
                .usage(vk::BufferUsageFlags::STORAGE_BUFFER)
                .sharing_mode(vk::SharingMode::EXCLUSIVE),
            None,
        )
        .context("create_buffer")?;
    let req = dev.device.get_buffer_memory_requirements(buf);
    let mem_index = find_memory_type(
        &dev.instance,
        dev.physical_device,
        req,
        vk::MemoryPropertyFlags::HOST_VISIBLE | vk::MemoryPropertyFlags::HOST_COHERENT,
    )
    .context("no HOST_VISIBLE|HOST_COHERENT memory")?;

    let mem = dev
        .device
        .allocate_memory(
            &vk::MemoryAllocateInfo::default()
                .allocation_size(req.size.max(buffer_size))
                .memory_type_index(mem_index),
            None,
        )
        .context("allocate_memory")?;
    dev.device.bind_buffer_memory(buf, mem, 0)?;

    let ptr = dev
        .device
        .map_memory(mem, 0, map_size, vk::MemoryMapFlags::empty())
        .context("map_memory")?;
    let mapped = std::slice::from_raw_parts_mut(ptr as *mut u8, map_size as usize);
    mapped.fill(0);
    mapped[..write.len()].copy_from_slice(write);

    let buf_info = vk::DescriptorBufferInfo::default()
        .buffer(buf)
        .offset(0)
        .range(buffer_size);

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
    dev.device.cmd_bind_pipeline(cmd_buf, vk::PipelineBindPoint::COMPUTE, pipeline);
    dev.device.cmd_bind_descriptor_sets(
        cmd_buf,
        vk::PipelineBindPoint::COMPUTE,
        pipeline_layout,
        0,
        &[desc_set],
        &[],
    );
    let (gx, gy, gz) = dispatch;
    dev.device.cmd_dispatch(cmd_buf, gx, gy, gz);
    let barrier = vk::MemoryBarrier::default()
        .src_access_mask(vk::AccessFlags::SHADER_WRITE)
        .dst_access_mask(vk::AccessFlags::HOST_READ);
    dev.device.cmd_pipeline_barrier(
        cmd_buf,
        vk::PipelineStageFlags::COMPUTE_SHADER,
        vk::PipelineStageFlags::HOST,
        vk::DependencyFlags::empty(),
        std::slice::from_ref(&barrier),
        &[],
        &[],
    );
    dev.device.end_command_buffer(cmd_buf).context("end_command_buffer")?;

    let fence = dev.device.create_fence(&vk::FenceCreateInfo::default(), None)?;
    let submit = vk::SubmitInfo::default().command_buffers(std::slice::from_ref(&cmd_buf));
    dev.device
        .queue_submit(dev.queue, std::slice::from_ref(&submit), fence)
        .context("queue_submit")?;
    dev.device
        .wait_for_fences(&[fence], true, u64::MAX)
        .context("wait_for_fences")?;

    let out = std::slice::from_raw_parts(ptr as *const u8, map_size as usize);
    read_out[..read_len].copy_from_slice(&out[..read_len]);

    dev.device.unmap_memory(mem);
    dev.device.destroy_fence(fence, None);
    dev.device.destroy_command_pool(cmd_pool, None);
    dev.device.free_memory(mem, None);
    dev.device.destroy_buffer(buf, None);
    dev.device.destroy_descriptor_pool(desc_pool, None);
    dev.device.destroy_pipeline(pipeline, None);
    dev.device.destroy_pipeline_layout(pipeline_layout, None);
    dev.device.destroy_descriptor_set_layout(desc_layout, None);
    dev.device.destroy_shader_module(shader_mod, None);

    Ok(())
}
