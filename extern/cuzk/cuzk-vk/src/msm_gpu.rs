//! MSM-related GPU validation: dispatch grid vs [`crate::msm::MsmBucketReduceDispatch`] (atomics).
//!
//! Bucket MSM / curve arithmetic is still future work; see repo root `cuzk-vulkan-optimization-roadmap.md` §8.1.

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;

use crate::device::VulkanDevice;
use crate::msm::MsmBucketReduceDispatch;
use crate::pipelines::{align_up_u64, spec_constant_u32_le};
use crate::vk_oneshot::{self, ComputeShaderStageSpec};

/// Default `local_x` for tests / smoke (must lie in `MSM_DISPATCH_SMOKE_LOCAL_X_MIN..=MSM_DISPATCH_SMOKE_LOCAL_X_MAX`).
pub const MSM_DISPATCH_SMOKE_LOCAL_X: u32 = 64;

/// Allowed `MsmBucketReduceDispatch::local_x` for [`run_msm_dispatch_hitcount_smoke`] (must match SPIR-V `local_size_x_id = 0`).
pub const MSM_DISPATCH_SMOKE_LOCAL_X_MIN: u32 = 1;
pub const MSM_DISPATCH_SMOKE_LOCAL_X_MAX: u32 = 256;

/// Must match `payload[…]` array length in `msm_dispatch_grid_smoke.comp`.
pub const MSM_DISPATCH_SMOKE_MAX_THREADS: u32 = 4096;

const BUF_ALIGN: u64 = 256;

/// Writes `1u` into `payload[0..invocation_count)` (unique `flat` per thread). Returns that count.
/// `dispatch.local_x` is passed as **specialization constant id 0** (workgroup X size in SPIR-V).
pub fn run_msm_dispatch_hitcount_smoke(
    dev: &VulkanDevice,
    dispatch: MsmBucketReduceDispatch,
) -> Result<u32> {
    anyhow::ensure!(
        (MSM_DISPATCH_SMOKE_LOCAL_X_MIN..=MSM_DISPATCH_SMOKE_LOCAL_X_MAX)
            .contains(&dispatch.local_x),
        "msm_dispatch_grid_smoke local_x must be in {}..={} (got {})",
        MSM_DISPATCH_SMOKE_LOCAL_X_MIN,
        MSM_DISPATCH_SMOKE_LOCAL_X_MAX,
        dispatch.local_x
    );
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/msm_dispatch_grid_smoke.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv msm_dispatch_grid_smoke")?;

    let n = dispatch.invocation_count();
    let n32 = u32::try_from(n).map_err(|_| anyhow::anyhow!("invocation count overflow"))?;
    anyhow::ensure!(
        n32 <= MSM_DISPATCH_SMOKE_MAX_THREADS,
        "msm dispatch smoke supports at most {} threads (got {n32})",
        MSM_DISPATCH_SMOKE_MAX_THREADS
    );
    let buf_len = 4usize + (n as usize).saturating_mul(4);
    let buffer_size = align_up_u64(buf_len as u64, BUF_ALIGN).max(BUF_ALIGN);

    let mut write = vec![0u8; buffer_size as usize];
    write[..4].copy_from_slice(&n32.to_le_bytes());
    let mut read = vec![0u8; buffer_size as usize];
    let (gx, gy, gz) = dispatch.dispatch();
    let lx_le = spec_constant_u32_le(dispatch.local_x);
    let map = [vk_oneshot::spec_map_u32(0, 0)];
    let stage_spec = ComputeShaderStageSpec {
        map_entries: &map,
        data: lx_le.as_slice(),
    };
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            buffer_size,
            buffer_size,
            (gx, gy, gz),
            &write,
            buf_len,
            &mut read,
            Some(&stage_spec),
        )?;
    }
    let sum: u64 = read[4..buf_len]
        .chunks_exact(4)
        .map(|c| u32::from_le_bytes(c.try_into().unwrap()) as u64)
        .sum();
    anyhow::ensure!(sum == n, "payload sum {sum} != expected {n}");
    Ok(n32)
}
