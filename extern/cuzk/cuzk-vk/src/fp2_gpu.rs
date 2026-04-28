//! BLS12-381 **Fp2** modular add / sub / mul / square on the GPU (Montgomery mul/sqr; canonical add/sub).

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::Fp2;

use crate::device::VulkanDevice;
use crate::fp2::{
    fp2_from_montgomery_u32_limbs, fp2_montgomery_u32_limbs, fp2_to_le_u32_limbs, fp2_try_from_le_u32_limbs,
};
use crate::fp2::BLS12_381_FP2_U32_LIMBS;
use crate::vk_oneshot;

const FP2_IO_U32: usize = 3 * BLS12_381_FP2_U32_LIMBS; // out + a + b
const FP2_IO_BYTES: usize = FP2_IO_U32 * 4;
/// Storage buffer must hold 72 `u32` (288 B); map range must cover full host write.
const BUF_BYTES: u64 = 512;
const OUT_BYTES: usize = BLS12_381_FP2_U32_LIMBS * 4;

fn pack_fp2_ab_canonical(a: &Fp2, b: &Fp2) -> [u8; FP2_IO_BYTES] {
    let la = fp2_to_le_u32_limbs(a);
    let lb = fp2_to_le_u32_limbs(b);
    let mut w = [0u8; FP2_IO_BYTES];
    let base_a = BLS12_381_FP2_U32_LIMBS * 4;
    let base_b = 2 * BLS12_381_FP2_U32_LIMBS * 4;
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        w[base_a + i * 4..base_a + i * 4 + 4].copy_from_slice(&la[i].to_le_bytes());
        w[base_b + i * 4..base_b + i * 4 + 4].copy_from_slice(&lb[i].to_le_bytes());
    }
    w
}

fn unpack_fp2_out(out96: &[u8; OUT_BYTES]) -> Result<Fp2> {
    let mut limbs = [0u32; BLS12_381_FP2_U32_LIMBS];
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        limbs[i] = u32::from_le_bytes(out96[i * 4..i * 4 + 4].try_into().unwrap());
    }
    fp2_try_from_le_u32_limbs(&limbs).context("GPU Fp2 canonical bytes invalid")
}

fn pack_fp2_mont_mul_io(a: &Fp2, b: &Fp2) -> [u8; FP2_IO_BYTES] {
    let la = fp2_montgomery_u32_limbs(a);
    let lb = fp2_montgomery_u32_limbs(b);
    let mut w = [0u8; FP2_IO_BYTES];
    let base_a = BLS12_381_FP2_U32_LIMBS * 4;
    let base_b = 2 * BLS12_381_FP2_U32_LIMBS * 4;
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        w[base_a + i * 4..base_a + i * 4 + 4].copy_from_slice(&la[i].to_le_bytes());
        w[base_b + i * 4..base_b + i * 4 + 4].copy_from_slice(&lb[i].to_le_bytes());
    }
    w
}

fn unpack_fp2_mont_out(out96: &[u8; OUT_BYTES]) -> Fp2 {
    let mut limbs = [0u32; BLS12_381_FP2_U32_LIMBS];
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        limbs[i] = u32::from_le_bytes(out96[i * 4..i * 4 + 4].try_into().unwrap());
    }
    fp2_from_montgomery_u32_limbs(&limbs)
}

fn pack_fp2_mont_sqr_io(a: &Fp2) -> [u8; FP2_IO_BYTES] {
    let la = fp2_montgomery_u32_limbs(a);
    let mut w = [0u8; FP2_IO_BYTES];
    let base_a = BLS12_381_FP2_U32_LIMBS * 4;
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        w[base_a + i * 4..base_a + i * 4 + 4].copy_from_slice(&la[i].to_le_bytes());
    }
    w
}

/// `(a + b)` in `Fp2` with canonical component I/O.
pub fn run_fp2_add_mod_gpu(dev: &VulkanDevice, a: &Fp2, b: &Fp2) -> Result<Fp2> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fp2_add24.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fp2_add24")?;
    let wbytes = pack_fp2_ab_canonical(a, b);
    let mut out96 = [0u8; OUT_BYTES];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_BYTES,
            BUF_BYTES,
            (1, 1, 1),
            &wbytes,
            OUT_BYTES,
            &mut out96,
        )?;
    }
    unpack_fp2_out(&out96)
}

/// `(a - b)` in `Fp2` with canonical component I/O.
pub fn run_fp2_sub_mod_gpu(dev: &VulkanDevice, a: &Fp2, b: &Fp2) -> Result<Fp2> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fp2_sub24.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fp2_sub24")?;
    let wbytes = pack_fp2_ab_canonical(a, b);
    let mut out96 = [0u8; OUT_BYTES];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_BYTES,
            BUF_BYTES,
            (1, 1, 1),
            &wbytes,
            OUT_BYTES,
            &mut out96,
        )?;
    }
    unpack_fp2_out(&out96)
}

/// `(a * b)` in `Fp2` with Montgomery CIOS on base field limbs; canonical `Fp2` API.
pub fn run_fp2_mul_mod_gpu(dev: &VulkanDevice, a: &Fp2, b: &Fp2) -> Result<Fp2> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fp2_mul24.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fp2_mul24")?;
    let wbytes = pack_fp2_mont_mul_io(a, b);
    let mut out96 = [0u8; OUT_BYTES];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_BYTES,
            BUF_BYTES,
            (1, 1, 1),
            &wbytes,
            OUT_BYTES,
            &mut out96,
        )?;
    }
    Ok(unpack_fp2_mont_out(&out96))
}

/// `a^2` in `Fp2` (Montgomery mul internally).
pub fn run_fp2_sqr_mod_gpu(dev: &VulkanDevice, a: &Fp2) -> Result<Fp2> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fp2_sqr24.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fp2_sqr24")?;
    let wbytes = pack_fp2_mont_sqr_io(a);
    let mut out96 = [0u8; OUT_BYTES];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_BYTES,
            BUF_BYTES,
            (1, 1, 1),
            &wbytes,
            OUT_BYTES,
            &mut out96,
        )?;
    }
    Ok(unpack_fp2_mont_out(&out96))
}
