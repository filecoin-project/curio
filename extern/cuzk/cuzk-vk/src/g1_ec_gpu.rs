//! G1 Jacobian / XYZZ point ops on the GPU (Montgomery `Fp` limbs).

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::G1Affine;

use crate::device::VulkanDevice;
use crate::ec::{
    g1_affine_montgomery_limbs, G1JacobianLimbs, G1XyzzLimbs, G1_JACOBIAN_ADD_SSBO_BYTES,
    G1_XYZZ_ADD_MIXED_SSBO_BYTES,
};
use crate::g1::BLS12_381_FP_U32_LIMBS;
use crate::vk_oneshot;

const JAC_BUF: u64 = 512;
const XYZZ_BUF: u64 = 512;

fn put_fp12(buf: &mut [u8], off_u32: usize, limbs: &[u32; BLS12_381_FP_U32_LIMBS]) {
    for i in 0..BLS12_381_FP_U32_LIMBS {
        let o = (off_u32 + i) * 4;
        buf[o..o + 4].copy_from_slice(&limbs[i].to_le_bytes());
    }
}

fn get_fp12(buf: &[u8], off_u32: usize) -> [u32; BLS12_381_FP_U32_LIMBS] {
    let mut out = [0u32; BLS12_381_FP_U32_LIMBS];
    for i in 0..BLS12_381_FP_U32_LIMBS {
        let o = (off_u32 + i) * 4;
        out[i] = u32::from_le_bytes(buf[o..o + 4].try_into().unwrap());
    }
    out
}

/// `a + b` on Jacobian points (Montgomery I/O).
pub fn run_g1_jacobian_add_gpu(
    dev: &VulkanDevice,
    a: &G1JacobianLimbs,
    b: &G1JacobianLimbs,
) -> Result<G1JacobianLimbs> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g1_jacobian_add108.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv g1_jacobian_add108")?;
    let mut wbytes = [0u8; G1_JACOBIAN_ADD_SSBO_BYTES];
    put_fp12(&mut wbytes, 36, &a.x);
    put_fp12(&mut wbytes, 48, &a.y);
    put_fp12(&mut wbytes, 60, &a.z);
    put_fp12(&mut wbytes, 72, &b.x);
    put_fp12(&mut wbytes, 84, &b.y);
    put_fp12(&mut wbytes, 96, &b.z);
    let mut out = [0u8; (BLS12_381_FP_U32_LIMBS * 3) * 4];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            JAC_BUF,
            JAC_BUF,
            (1, 1, 1),
            &wbytes,
            out.len(),
            &mut out,
        )?;
    }
    Ok(G1JacobianLimbs {
        x: get_fp12(&out, 0),
        y: get_fp12(&out, 12),
        z: get_fp12(&out, 24),
    })
}

/// XYZZ += affine `p2` (Montgomery I/O). `p2` must not be the point at infinity.
pub fn run_g1_xyzz_add_mixed_gpu(dev: &VulkanDevice, xyzz: &mut G1XyzzLimbs, p2: &G1Affine) -> Result<()> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g1_xyzz_add_mixed72.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv g1_xyzz_add_mixed72")?;
    let (p2x, p2y) = g1_affine_montgomery_limbs(p2);
    let mut wbytes = [0u8; G1_XYZZ_ADD_MIXED_SSBO_BYTES];
    put_fp12(&mut wbytes, 0, &xyzz.x);
    put_fp12(&mut wbytes, 12, &xyzz.y);
    put_fp12(&mut wbytes, 24, &xyzz.zzz);
    put_fp12(&mut wbytes, 36, &xyzz.zz);
    put_fp12(&mut wbytes, 48, &p2x);
    put_fp12(&mut wbytes, 60, &p2y);
    let read_len = 48 * 4;
    let mut out = [0u8; 48 * 4];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            XYZZ_BUF,
            XYZZ_BUF,
            (1, 1, 1),
            &wbytes,
            read_len,
            &mut out,
        )?;
    }
    xyzz.x = get_fp12(&out, 0);
    xyzz.y = get_fp12(&out, 12);
    xyzz.zzz = get_fp12(&out, 24);
    xyzz.zz = get_fp12(&out, 36);
    Ok(())
}
