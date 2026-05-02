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

/// One `uint` map entry for [`vk::SpecializationInfo`] (size = 4, little-endian payload in `data`).
#[inline]
pub(crate) fn spec_map_u32(constant_id: u32, byte_offset: u32) -> vk::SpecializationMapEntry {
    vk::SpecializationMapEntry {
        constant_id,
        offset: byte_offset,
        size: 4,
    }
}

/// Map entries + raw specialization blob for one compute stage (`vk::SpecializationInfo`).
#[derive(Clone, Copy)]
pub(crate) struct ComputeShaderStageSpec<'a> {
    pub map_entries: &'a [vk::SpecializationMapEntry],
    pub data: &'a [u8],
}

/// Run a compute shader (`entry` = `"main"`) with one storage buffer, one `dispatch` of `(gx,gy,gz)`.
///
/// `buffer_size` is allocated (at least `req.size`); `map_size` is mapped / barrier range (often `buffer_size`).
/// Writes `write` bytes at offset 0 before dispatch; reads `read_len` bytes at offset 0 after completion.
///
/// `stage_spec`: optional [`ComputeShaderStageSpec`] for `vk::PipelineShaderStageCreateInfo::specialization_info`
/// (e.g. `layout(constant_id = …)` in GLSL).
pub(crate) unsafe fn run_compute_1x_storage_buffer(
    dev: &VulkanDevice,
    spirv_words: &[u32],
    buffer_size: u64,
    map_size: u64,
    dispatch: (u32, u32, u32),
    write: &[u8],
    read_len: usize,
    read_out: &mut [u8],
    stage_spec: Option<&ComputeShaderStageSpec<'_>>,
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

    let spec_info_storage = stage_spec.map(|s| {
        vk::SpecializationInfo::default()
            .map_entries(s.map_entries)
            .data(s.data)
    });
    let mut stage = vk::PipelineShaderStageCreateInfo::default()
        .stage(vk::ShaderStageFlags::COMPUTE)
        .module(shader_mod)
        .name(&entry);
    if let Some(ref si) = spec_info_storage {
        stage = stage.specialization_info(si);
    }

    let cpci = vk::ComputePipelineCreateInfo::default()
        .layout(pipeline_layout)
        .stage(stage);

    let pipeline = match dev
        .device
        .create_compute_pipelines(dev.pipeline_cache, std::slice::from_ref(&cpci), None)
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
    dev.device
        .flush_mapped_memory_ranges(&[vk::MappedMemoryRange::default()
            .memory(mem)
            .offset(0)
            .size(write.len() as u64)])
        .context("flush_mapped_memory_ranges")?;

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
    // Host memcpy into the mapped SSBO is not automatically visible to the compute stage on
    // every implementation (MoltenVK / Apple in particular). Without this barrier, the shader
    // can read stale or zeroed storage and return wrong-but-deterministic curve data.
    let buf_barrier_host_to_shader = vk::BufferMemoryBarrier::default()
        .buffer(buf)
        .offset(0)
        .size(vk::WHOLE_SIZE)
        .src_access_mask(vk::AccessFlags::HOST_WRITE)
        .dst_access_mask(vk::AccessFlags::SHADER_READ | vk::AccessFlags::SHADER_WRITE);
    dev.device.cmd_pipeline_barrier(
        cmd_buf,
        vk::PipelineStageFlags::HOST,
        vk::PipelineStageFlags::COMPUTE_SHADER,
        vk::DependencyFlags::empty(),
        &[],
        std::slice::from_ref(&buf_barrier_host_to_shader),
        &[],
    );
    let (gx, gy, gz) = dispatch;
    dev.device.cmd_dispatch(cmd_buf, gx, gy, gz);
    // SSBO → host readback: use an explicit buffer barrier (some Metal/MoltenVK stacks are
    // unreliable with only a global memory barrier for storage writes).
    let buf_barrier_host = vk::BufferMemoryBarrier::default()
        .buffer(buf)
        .offset(0)
        .size(vk::WHOLE_SIZE)
        .src_access_mask(vk::AccessFlags::SHADER_WRITE)
        .dst_access_mask(vk::AccessFlags::HOST_READ);
    dev.device.cmd_pipeline_barrier(
        cmd_buf,
        vk::PipelineStageFlags::COMPUTE_SHADER,
        vk::PipelineStageFlags::ALL_COMMANDS,
        vk::DependencyFlags::empty(),
        &[],
        std::slice::from_ref(&buf_barrier_host),
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

    dev.device
        .invalidate_mapped_memory_ranges(&[vk::MappedMemoryRange::default()
            .memory(mem)
            .offset(0)
            .size(vk::WHOLE_SIZE)])
        .context("invalidate_mapped_memory_ranges")?;

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

const PUSH_CONSTANT_BYTES: usize = 16;

/// One compute dispatch in a multi-pass sequence sharing one host-visible storage buffer.
pub(crate) struct ComputePassDesc<'a> {
    pub spirv_words: &'a [u32],
    pub dispatch: (u32, u32, u32),
    /// Push constants (16 bytes, compute stage offset 0). Unused words should be zero.
    pub push_constants: [u8; PUSH_CONSTANT_BYTES],
    /// Optional per-pass specialization constants for the compute stage.
    pub stage_spec: Option<ComputeShaderStageSpec<'a>>,
}

/// Run several compute passes on the same storage buffer: one write at offset 0, then
/// `cmd_dispatch` + storage buffer barriers between passes, then read `read_len` bytes at 0.
///
/// All pipelines share one descriptor set (binding 0) and a 16-byte push constant range; each
/// pass may use a different SPIR-V module.
pub(crate) unsafe fn run_compute_passes_1x_storage_buffer(
    dev: &VulkanDevice,
    buffer_size: u64,
    map_size: u64,
    initial_write: &[u8],
    passes: &[ComputePassDesc<'_>],
    read_len: usize,
    read_out: &mut [u8],
) -> Result<()> {
    anyhow::ensure!(
        !passes.is_empty(),
        "run_compute_passes_1x_storage_buffer: empty passes"
    );
    anyhow::ensure!(
        initial_write.len() as u64 <= buffer_size && read_len <= map_size as usize,
        "write/read larger than buffer/map"
    );
    read_out[..read_len].fill(0);

    let entry = CString::new("main").unwrap();

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
        .context("create_descriptor_set_layout (passes)")?;

    let push_range = vk::PushConstantRange::default()
        .stage_flags(vk::ShaderStageFlags::COMPUTE)
        .offset(0)
        .size(PUSH_CONSTANT_BYTES as u32);

    let pipeline_layout = dev
        .device
        .create_pipeline_layout(
            &vk::PipelineLayoutCreateInfo::default()
                .set_layouts(std::slice::from_ref(&desc_layout))
                .push_constant_ranges(std::slice::from_ref(&push_range)),
            None,
        )
        .context("create_pipeline_layout (passes)")?;

    let mut shader_modules = Vec::with_capacity(passes.len());
    let mut pipelines = Vec::with_capacity(passes.len());

    for pass in passes {
        let shader_mod = dev
            .device
            .create_shader_module(
                &vk::ShaderModuleCreateInfo::default()
                    .code(pass.spirv_words)
                    .flags(vk::ShaderModuleCreateFlags::empty()),
                None,
            )
            .context("create_shader_module (passes)")?;

        let spec_info_storage = pass.stage_spec.map(|s| {
            vk::SpecializationInfo::default()
                .map_entries(s.map_entries)
                .data(s.data)
        });
        let mut stage = vk::PipelineShaderStageCreateInfo::default()
            .stage(vk::ShaderStageFlags::COMPUTE)
            .module(shader_mod)
            .name(&entry);
        if let Some(ref si) = spec_info_storage {
            stage = stage.specialization_info(si);
        }

        let cpci = vk::ComputePipelineCreateInfo::default()
            .layout(pipeline_layout)
            .stage(stage);

        let pipeline = match dev.device.create_compute_pipelines(
            dev.pipeline_cache,
            std::slice::from_ref(&cpci),
            None,
        ) {
            Ok(mut v) => v.pop().expect("one pipeline"),
            Err((_, e)) => {
                dev.device.destroy_shader_module(shader_mod, None);
                for sm in shader_modules.drain(..) {
                    dev.device.destroy_shader_module(sm, None);
                }
                for p in pipelines.drain(..) {
                    dev.device.destroy_pipeline(p, None);
                }
                dev.device.destroy_pipeline_layout(pipeline_layout, None);
                dev.device.destroy_descriptor_set_layout(desc_layout, None);
                anyhow::bail!("create_compute_pipelines (passes): {:?}", e);
            }
        };
        shader_modules.push(shader_mod);
        pipelines.push(pipeline);
    }

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
        .context("create_descriptor_pool (passes)")?;

    let desc_sets = dev
        .device
        .allocate_descriptor_sets(
            &vk::DescriptorSetAllocateInfo::default()
                .descriptor_pool(desc_pool)
                .set_layouts(std::slice::from_ref(&desc_layout)),
        )
        .context("allocate_descriptor_sets (passes)")?;
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
        .context("create_buffer (passes)")?;
    let req = dev.device.get_buffer_memory_requirements(buf);
    let mem_index = find_memory_type(
        &dev.instance,
        dev.physical_device,
        req,
        vk::MemoryPropertyFlags::HOST_VISIBLE | vk::MemoryPropertyFlags::HOST_COHERENT,
    )
    .context("no HOST_VISIBLE|HOST_COHERENT memory (passes)")?;

    let mem = dev
        .device
        .allocate_memory(
            &vk::MemoryAllocateInfo::default()
                .allocation_size(req.size.max(buffer_size))
                .memory_type_index(mem_index),
            None,
        )
        .context("allocate_memory (passes)")?;
    dev.device.bind_buffer_memory(buf, mem, 0)?;

    let ptr = dev
        .device
        .map_memory(mem, 0, map_size, vk::MemoryMapFlags::empty())
        .context("map_memory (passes)")?;
    let mapped = std::slice::from_raw_parts_mut(ptr as *mut u8, map_size as usize);
    mapped.fill(0);
    mapped[..initial_write.len()].copy_from_slice(initial_write);
    dev.device
        .flush_mapped_memory_ranges(&[vk::MappedMemoryRange::default()
            .memory(mem)
            .offset(0)
            .size(initial_write.len() as u64)])
        .context("flush_mapped_memory_ranges (passes)")?;

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
        .context("create_command_pool (passes)")?;
    let cmd_buf = dev
        .device
        .allocate_command_buffers(
            &vk::CommandBufferAllocateInfo::default()
                .command_pool(cmd_pool)
                .level(vk::CommandBufferLevel::PRIMARY)
                .command_buffer_count(1),
        )
        .context("allocate_command_buffers (passes)")?[0];

    dev.device
        .begin_command_buffer(
            cmd_buf,
            &vk::CommandBufferBeginInfo::default().flags(vk::CommandBufferUsageFlags::ONE_TIME_SUBMIT),
        )
        .context("begin_command_buffer (passes)")?;

    let buf_barrier_compute = vk::BufferMemoryBarrier::default()
        .buffer(buf)
        .offset(0)
        .size(vk::WHOLE_SIZE)
        .src_access_mask(vk::AccessFlags::SHADER_WRITE)
        .dst_access_mask(vk::AccessFlags::SHADER_READ | vk::AccessFlags::SHADER_WRITE);

    for (pi, pass) in passes.iter().enumerate() {
        dev.device.cmd_bind_pipeline(
            cmd_buf,
            vk::PipelineBindPoint::COMPUTE,
            pipelines[pi],
        );
        dev.device.cmd_bind_descriptor_sets(
            cmd_buf,
            vk::PipelineBindPoint::COMPUTE,
            pipeline_layout,
            0,
            &[desc_set],
            &[],
        );
        if pi == 0 {
            let buf_barrier_host_to_shader = vk::BufferMemoryBarrier::default()
                .buffer(buf)
                .offset(0)
                .size(vk::WHOLE_SIZE)
                .src_access_mask(vk::AccessFlags::HOST_WRITE)
                .dst_access_mask(vk::AccessFlags::SHADER_READ | vk::AccessFlags::SHADER_WRITE);
            dev.device.cmd_pipeline_barrier(
                cmd_buf,
                vk::PipelineStageFlags::HOST,
                vk::PipelineStageFlags::COMPUTE_SHADER,
                vk::DependencyFlags::empty(),
                &[],
                std::slice::from_ref(&buf_barrier_host_to_shader),
                &[],
            );
        }
        dev.device.cmd_push_constants(
            cmd_buf,
            pipeline_layout,
            vk::ShaderStageFlags::COMPUTE,
            0,
            &pass.push_constants,
        );
        let (gx, gy, gz) = pass.dispatch;
        dev.device.cmd_dispatch(cmd_buf, gx, gy, gz);

        if pi + 1 < passes.len() {
            dev.device.cmd_pipeline_barrier(
                cmd_buf,
                vk::PipelineStageFlags::COMPUTE_SHADER,
                vk::PipelineStageFlags::COMPUTE_SHADER,
                vk::DependencyFlags::empty(),
                &[],
                std::slice::from_ref(&buf_barrier_compute),
                &[],
            );
        }
    }

    let buf_barrier_host = vk::BufferMemoryBarrier::default()
        .buffer(buf)
        .offset(0)
        .size(vk::WHOLE_SIZE)
        .src_access_mask(vk::AccessFlags::SHADER_WRITE)
        .dst_access_mask(vk::AccessFlags::HOST_READ);
    dev.device.cmd_pipeline_barrier(
        cmd_buf,
        vk::PipelineStageFlags::COMPUTE_SHADER,
        vk::PipelineStageFlags::ALL_COMMANDS,
        vk::DependencyFlags::empty(),
        &[],
        std::slice::from_ref(&buf_barrier_host),
        &[],
    );
    dev.device
        .end_command_buffer(cmd_buf)
        .context("end_command_buffer (passes)")?;

    let fence = dev.device.create_fence(&vk::FenceCreateInfo::default(), None)?;
    let submit = vk::SubmitInfo::default().command_buffers(std::slice::from_ref(&cmd_buf));
    dev.device
        .queue_submit(dev.queue, std::slice::from_ref(&submit), fence)
        .context("queue_submit (passes)")?;
    dev.device
        .wait_for_fences(&[fence], true, u64::MAX)
        .context("wait_for_fences (passes)")?;

    dev.device
        .invalidate_mapped_memory_ranges(&[vk::MappedMemoryRange::default()
            .memory(mem)
            .offset(0)
            .size(vk::WHOLE_SIZE)])
        .context("invalidate_mapped_memory_ranges (passes)")?;

    let out = std::slice::from_raw_parts(ptr as *const u8, map_size as usize);
    read_out[..read_len].copy_from_slice(&out[..read_len]);

    dev.device.unmap_memory(mem);
    dev.device.destroy_fence(fence, None);
    dev.device.destroy_command_pool(cmd_pool, None);
    dev.device.free_memory(mem, None);
    dev.device.destroy_buffer(buf, None);
    dev.device.destroy_descriptor_pool(desc_pool, None);
    for p in pipelines {
        dev.device.destroy_pipeline(p, None);
    }
    for sm in shader_modules {
        dev.device.destroy_shader_module(sm, None);
    }
    dev.device.destroy_pipeline_layout(pipeline_layout, None);
    dev.device.destroy_descriptor_set_layout(desc_layout, None);

    Ok(())
}
