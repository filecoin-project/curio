//! §8.2 **B.3** plumbing preview: **`subgroupShuffle`** via **WGSL → naga → SPIR-V** (see `shaders/subgroup_shuffle_smoke.wgsl`).
//!
//! Fr NTT kernels can migrate to WGSL for shuffle-heavy stages while GLSL remains the default for portable Montgomery math.

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;

use crate::device::VulkanDevice;
use crate::pipelines::align_up_u64;
use crate::vk_oneshot;

const BUF_ALIGN: u64 = 256;
const WG: u32 = 128;
const WORDS: usize = WG as usize;

/// Runs `subgroup_shuffle_smoke.wgsl`: each lane writes `subgroupShuffle(42u, 0u)` — broadcast within subgroup.
///
/// **Requires** ICD support for Vulkan subgroup shuffle (some stacks reject pipeline creation).
pub fn run_subgroup_shuffle_smoke_gpu(dev: &VulkanDevice) -> Result<[u32; WORDS]> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/subgroup_shuffle_smoke.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv subgroup_shuffle_smoke")?;

    let buf_len = WORDS * 4;
    let buffer_size = align_up_u64(buf_len as u64, BUF_ALIGN).max(BUF_ALIGN);
    let write = vec![0xffu8; buf_len];
    let mut read = vec![0u8; buffer_size as usize];

    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            buffer_size,
            buffer_size,
            (1, 1, 1),
            &write,
            buf_len,
            &mut read,
            None,
        )?;
    }

    let mut out = [0u32; WORDS];
    for i in 0..WORDS {
        let off = i * 4;
        out[i] = u32::from_le_bytes(read[off..off + 4].try_into().unwrap());
    }
    Ok(out)
}
