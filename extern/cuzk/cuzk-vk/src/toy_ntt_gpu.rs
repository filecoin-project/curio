//! Run the 8-point toy NTT compute shader (mod 998244353) on the Vulkan queue.

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;

use crate::device::VulkanDevice;
use crate::vk_oneshot;

const BUF_ALIGN: u64 = 256;

/// Forward toy NTT on `data` in place (see [`crate::toy_ntt::ntt_forward_8`]).
pub fn run_toy_ntt8_gpu(dev: &VulkanDevice, data: &mut [u32; 8]) -> Result<()> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/toy_ntt8.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv toy_ntt8")?;
    let mut wbytes = [0u8; 32];
    for i in 0..8 {
        wbytes[i * 4..i * 4 + 4].copy_from_slice(&data[i].to_le_bytes());
    }
    let mut out = [0u8; 32];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &wbytes,
            32,
            &mut out,
            None,
        )?;
    }
    for i in 0..8 {
        data[i] = u32::from_le_bytes(out[i * 4..i * 4 + 4].try_into().unwrap());
    }
    Ok(())
}
