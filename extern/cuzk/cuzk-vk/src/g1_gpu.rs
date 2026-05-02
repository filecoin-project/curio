//! G1 limb buffer smoke on the GPU (`shaders/g1_reverse24.comp`).

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;

use crate::device::VulkanDevice;
use crate::g1::{g1_limbs_from_le_bytes, g1_limbs_to_le_bytes, G1AffineLimbs, G1_AFFINE_LIMB_BYTES};
use crate::vk_oneshot;

const BUF_ALIGN: u64 = 256;

/// Applies the same `u32[24]` reversal as [`crate::g1::g1_limbs_reverse_words_inplace`] on-device.
pub fn run_g1_reverse24_gpu(dev: &VulkanDevice, limbs: &mut G1AffineLimbs) -> Result<()> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g1_reverse24.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv g1_reverse24")?;
    let inb = g1_limbs_to_le_bytes(limbs);
    let mut out = [0u8; G1_AFFINE_LIMB_BYTES];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &inb,
            G1_AFFINE_LIMB_BYTES,
            &mut out,
            None,
        )?;
    }
    *limbs = g1_limbs_from_le_bytes(&out);
    Ok(())
}
