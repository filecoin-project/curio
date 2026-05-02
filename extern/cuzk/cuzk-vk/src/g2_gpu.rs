//! G2 limb buffer smoke on the GPU (`shaders/g2_reverse48.comp`).

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;

use crate::device::VulkanDevice;
use crate::g1::{
    g2_limbs_from_le_bytes, g2_limbs_to_le_bytes, G2AffineLimbs, G2_AFFINE_LIMB_BYTES,
};
use crate::vk_oneshot;

const BUF_ALIGN: u64 = 256;

/// Same `u32[48]` reversal as [`crate::g1::g2_limbs_reverse_words_inplace`] on-device.
pub fn run_g2_reverse48_gpu(dev: &VulkanDevice, limbs: &mut G2AffineLimbs) -> Result<()> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g2_reverse48.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv g2_reverse48")?;
    let inb = g2_limbs_to_le_bytes(limbs);
    let mut out = [0u8; G2_AFFINE_LIMB_BYTES];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &inb,
            G2_AFFINE_LIMB_BYTES,
            &mut out,
            None,
        )?;
    }
    *limbs = g2_limbs_from_le_bytes(&out);
    Ok(())
}
