//! §8.3 C.3 smoke: `vk::SpecializationInfo` on the one-buffer compute path ([`crate::vk_oneshot`]).
//!
//! Real kernels (NTT `log_n`, MSM window) can reuse the same plumbing with `layout(constant_id = …)`.

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;

use crate::device::VulkanDevice;
use crate::pipelines::{align_up_u64, spec_constant_u32_le};
use crate::vk_oneshot::{self, ComputeShaderStageSpec};

const BUF_ALIGN: u64 = 256;

/// Runs `spec_constant_smoke.comp` with `constant_id = 0` set to `k`; returns SSBO word `v[0]`.
pub fn run_spec_constant_smoke_gpu(dev: &VulkanDevice, k: u32) -> Result<u32> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/spec_constant_smoke.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv spec_constant_smoke")?;
    let buf_len = 4usize;
    let buffer_size = align_up_u64(buf_len as u64, BUF_ALIGN).max(BUF_ALIGN);
    let write = [0u8; 4];
    let mut read = vec![0u8; buffer_size as usize];
    let map = [vk_oneshot::spec_map_u32(0, 0)];
    let data = spec_constant_u32_le(k);
    let spec = ComputeShaderStageSpec {
        map_entries: &map,
        data: data.as_slice(),
    };
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
            Some(&spec),
        )?;
    }
    Ok(u32::from_le_bytes(read[..4].try_into().unwrap()))
}
