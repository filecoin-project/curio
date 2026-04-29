//! BLS12-381 **Fp** modular add / sub / mul on the GPU (Montgomery mul; canonical add/sub I/O).

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::Fp;

use crate::device::VulkanDevice;
use crate::fp::{fp_from_montgomery_u32_limbs, fp_montgomery_u32_limbs, fp_to_le_u32_limbs, fp_try_from_le_u32_limbs};
use crate::g1::BLS12_381_FP_U32_LIMBS;
use crate::vk_oneshot;

const FP_IO_U32: usize = 3 * BLS12_381_FP_U32_LIMBS;
const FP_IO_BYTES: usize = FP_IO_U32 * 4;
const BUF_ALIGN: u64 = 256;

fn pack_fp_ab_io(a: &Fp, b: &Fp) -> [u8; FP_IO_BYTES] {
    let la = fp_to_le_u32_limbs(a);
    let lb = fp_to_le_u32_limbs(b);
    let mut wbytes = [0u8; FP_IO_BYTES];
    for i in 0..BLS12_381_FP_U32_LIMBS {
        wbytes[48 + i * 4..48 + i * 4 + 4].copy_from_slice(&la[i].to_le_bytes());
        wbytes[96 + i * 4..96 + i * 4 + 4].copy_from_slice(&lb[i].to_le_bytes());
    }
    wbytes
}

fn unpack_fp_out(out48: &[u8; 48]) -> Result<Fp> {
    let mut limbs = [0u32; BLS12_381_FP_U32_LIMBS];
    for i in 0..BLS12_381_FP_U32_LIMBS {
        limbs[i] = u32::from_le_bytes(out48[i * 4..i * 4 + 4].try_into().unwrap());
    }
    fp_try_from_le_u32_limbs(&limbs).context("GPU Fp bytes not canonical")
}

fn pack_fp_mont_mul_io(a: &Fp, b: &Fp) -> [u8; FP_IO_BYTES] {
    let la = fp_montgomery_u32_limbs(a);
    let lb = fp_montgomery_u32_limbs(b);
    let mut wbytes = [0u8; FP_IO_BYTES];
    for i in 0..BLS12_381_FP_U32_LIMBS {
        wbytes[48 + i * 4..48 + i * 4 + 4].copy_from_slice(&la[i].to_le_bytes());
        wbytes[96 + i * 4..96 + i * 4 + 4].copy_from_slice(&lb[i].to_le_bytes());
    }
    wbytes
}

fn unpack_fp_mont_mul_out(out48: &[u8; 48]) -> Fp {
    let mut limbs = [0u32; BLS12_381_FP_U32_LIMBS];
    for i in 0..BLS12_381_FP_U32_LIMBS {
        limbs[i] = u32::from_le_bytes(out48[i * 4..i * 4 + 4].try_into().unwrap());
    }
    fp_from_montgomery_u32_limbs(&limbs)
}

/// `(a + b) mod p` with canonical `Fp` I/O.
pub fn run_fp_add_mod_gpu(dev: &VulkanDevice, a: &Fp, b: &Fp) -> Result<Fp> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fp_add12.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fp_add12")?;
    let wbytes = pack_fp_ab_io(a, b);
    let mut out48 = [0u8; 48];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &wbytes,
            48,
            &mut out48,
        )?;
    }
    unpack_fp_out(&out48)
}

/// `(a - b) mod p` with canonical `Fp` I/O.
pub fn run_fp_sub_mod_gpu(dev: &VulkanDevice, a: &Fp, b: &Fp) -> Result<Fp> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fp_sub12.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fp_sub12")?;
    let wbytes = pack_fp_ab_io(a, b);
    let mut out48 = [0u8; 48];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &wbytes,
            48,
            &mut out48,
        )?;
    }
    unpack_fp_out(&out48)
}

/// `(a * b) mod p` with Montgomery CIOS on-device; canonical `Fp` API.
pub fn run_fp_mul_mod_gpu(dev: &VulkanDevice, a: &Fp, b: &Fp) -> Result<Fp> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fp_mul12.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fp_mul12")?;
    let wbytes = pack_fp_mont_mul_io(a, b);
    let mut out48 = [0u8; 48];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &wbytes,
            48,
            &mut out48,
        )?;
    }
    Ok(unpack_fp_mont_mul_out(&out48))
}
