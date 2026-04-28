//! MSM-related GPU validation: dispatch grid vs [`crate::msm::MsmBucketReduceDispatch`] (atomics).
//!
//! Bucket MSM / curve arithmetic is still future work; see repo root `cuzk-vulkan-optimization-roadmap.md` §8.1.

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;

use crate::device::VulkanDevice;
use crate::msm::MsmBucketReduceDispatch;
use crate::pipelines::align_up_u64;
use crate::vk_oneshot;

/// Must match `shaders/msm_dispatch_grid_smoke.comp` `layout(local_size_x = …)`.
pub const MSM_DISPATCH_SMOKE_LOCAL_X: u32 = 64;

/// Must match `payload[…]` array length in `msm_dispatch_grid_smoke.comp`.
pub const MSM_DISPATCH_SMOKE_MAX_THREADS: u32 = 4096;

const BUF_ALIGN: u64 = 256;

/// Writes `1u` into `payload[0..invocation_count)` (unique `flat` per thread). Returns that count.
/// `dispatch.local_x` must equal [`MSM_DISPATCH_SMOKE_LOCAL_X`].
pub fn run_msm_dispatch_hitcount_smoke(
    dev: &VulkanDevice,
    dispatch: MsmBucketReduceDispatch,
) -> Result<u32> {
    anyhow::ensure!(
        dispatch.local_x == MSM_DISPATCH_SMOKE_LOCAL_X,
        "msm_dispatch_grid_smoke.comp uses local_size_x={}",
        MSM_DISPATCH_SMOKE_LOCAL_X
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
        )?;
    }
    let sum: u64 = read[4..buf_len]
        .chunks_exact(4)
        .map(|c| u32::from_le_bytes(c.try_into().unwrap()) as u64)
        .sum();
    anyhow::ensure!(sum == n, "payload sum {sum} != expected {n}");
    Ok(n32)
}
