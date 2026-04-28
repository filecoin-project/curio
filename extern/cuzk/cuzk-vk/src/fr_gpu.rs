//! Fr modular **add**, **sub**, and **mul** on the GPU (canonical `u32[8]` LE limbs, [`crate::scalar_limbs`]).
//!
//! Mul uses **CIOS Montgomery** multiplication on `blst_fr` limbs (see `ec-gpu-gen` `FIELD_mul_default`);
//! canonical `Scalar` values are converted to / from Montgomery packing in this module.

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::Scalar;

use crate::device::VulkanDevice;
use crate::g1::BLS12_381_FR_U32_LIMBS;
use crate::scalar_limbs::{
    scalar_from_montgomery_u32_limbs, scalar_montgomery_u32_limbs, scalar_to_le_u32_limbs,
    scalar_try_from_le_u32_limbs,
};
use crate::vk_oneshot;

const BUF_ALIGN: u64 = 256;

fn pack_fr_ab_io(a: &Scalar, b: &Scalar) -> [u8; 96] {
    let la = scalar_to_le_u32_limbs(a);
    let lb = scalar_to_le_u32_limbs(b);
    let mut wbytes = [0u8; 96];
    for i in 0..BLS12_381_FR_U32_LIMBS {
        wbytes[32 + i * 4..32 + i * 4 + 4].copy_from_slice(&la[i].to_le_bytes());
        wbytes[64 + i * 4..64 + i * 4 + 4].copy_from_slice(&lb[i].to_le_bytes());
    }
    wbytes
}

fn unpack_fr_out(out32: &[u8; 32]) -> Result<Scalar> {
    let mut limbs = [0u32; BLS12_381_FR_U32_LIMBS];
    for i in 0..BLS12_381_FR_U32_LIMBS {
        limbs[i] = u32::from_le_bytes(out32[i * 4..i * 4 + 4].try_into().unwrap());
    }
    scalar_try_from_le_u32_limbs(&limbs).context("GPU Fr bytes not canonical")
}

fn pack_fr_mont_mul_io(a: &Scalar, b: &Scalar) -> [u8; 96] {
    let la = scalar_montgomery_u32_limbs(a);
    let lb = scalar_montgomery_u32_limbs(b);
    let mut wbytes = [0u8; 96];
    for i in 0..BLS12_381_FR_U32_LIMBS {
        wbytes[32 + i * 4..32 + i * 4 + 4].copy_from_slice(&la[i].to_le_bytes());
        wbytes[64 + i * 4..64 + i * 4 + 4].copy_from_slice(&lb[i].to_le_bytes());
    }
    wbytes
}

fn unpack_fr_mont_mul_out(out32: &[u8; 32]) -> Scalar {
    let mut limbs = [0u32; BLS12_381_FR_U32_LIMBS];
    for i in 0..BLS12_381_FR_U32_LIMBS {
        limbs[i] = u32::from_le_bytes(out32[i * 4..i * 4 + 4].try_into().unwrap());
    }
    scalar_from_montgomery_u32_limbs(&limbs)
}

/// `(a + b) mod r` with `a`, `b` already in Fr; matches CPU `a + b`.
pub fn run_fr_add_mod_gpu(dev: &VulkanDevice, a: &Scalar, b: &Scalar) -> Result<Scalar> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fr_add8.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fr_add8")?;
    let wbytes = pack_fr_ab_io(a, b);
    let mut out32 = [0u8; 32];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &wbytes,
            32,
            &mut out32,
        )?;
    }
    unpack_fr_out(&out32)
}

/// `(a - b) mod r`; matches CPU `a - b`.
pub fn run_fr_sub_mod_gpu(dev: &VulkanDevice, a: &Scalar, b: &Scalar) -> Result<Scalar> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fr_sub8.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fr_sub8")?;
    let wbytes = pack_fr_ab_io(a, b);
    let mut out32 = [0u8; 32];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &wbytes,
            32,
            &mut out32,
        )?;
    }
    unpack_fr_out(&out32)
}

/// `(a * b) mod r`; matches CPU `a * b` (Montgomery CIOS on-device).
pub fn run_fr_mul_mod_gpu(dev: &VulkanDevice, a: &Scalar, b: &Scalar) -> Result<Scalar> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fr_mul8.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fr_mul8")?;
    let wbytes = pack_fr_mont_mul_io(a, b);
    let mut out32 = [0u8; 32];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &wbytes,
            32,
            &mut out32,
        )?;
    }
    Ok(unpack_fr_mont_mul_out(&out32))
}
