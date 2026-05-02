//! G2 Jacobian / XYZZ point ops on the GPU (Montgomery `Fp2` limbs).

#[allow(dead_code)]
const _G2_JAC_ADD216_SPIRV_STAMP: &str = env!("CUZK_VK_G2_JAC_ADD216_STAMP");

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::G2Affine;

use crate::device::VulkanDevice;
use crate::ec::{
    g2_affine_montgomery_limbs, G2JacobianLimbs, G2XyzzLimbs, G2_JACOBIAN_ADD_SSBO_BYTES,
    G2_XYZZ_ADD_MIXED_SSBO_BYTES,
};
use crate::fp2::BLS12_381_FP2_U32_LIMBS;
use crate::vk_oneshot;

const J2_BUF: u64 = 1024;
const XYZZ2_BUF: u64 = 1024;

fn put_fp24(buf: &mut [u8], off_u32: usize, limbs: &[u32; BLS12_381_FP2_U32_LIMBS]) {
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        let o = (off_u32 + i) * 4;
        buf[o..o + 4].copy_from_slice(&limbs[i].to_le_bytes());
    }
}

fn get_fp24(buf: &[u8], off_u32: usize) -> [u32; BLS12_381_FP2_U32_LIMBS] {
    let mut out = [0u32; BLS12_381_FP2_U32_LIMBS];
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        let o = (off_u32 + i) * 4;
        out[i] = u32::from_le_bytes(buf[o..o + 4].try_into().unwrap());
    }
    out
}

fn is_g2_jac_identity(p: &G2JacobianLimbs) -> bool {
    p.z.iter().all(|&w| w == 0)
}

/// `a + b` on G2 Jacobian points (Montgomery `Fp2` I/O).
///
/// Identity inputs (`Z == 0`) are short-circuited on the host so callers (notably
/// [`crate::g2_batch_gpu::run_g2_batch_jacobian_accum_bitmap_gpu`]) can safely seed the
/// accumulator with the point at infinity. The kernel itself does not handle identity
/// operands.
pub fn run_g2_jacobian_add_gpu(
    dev: &VulkanDevice,
    a: &G2JacobianLimbs,
    b: &G2JacobianLimbs,
) -> Result<G2JacobianLimbs> {
    if is_g2_jac_identity(a) {
        return Ok(*b);
    }
    if is_g2_jac_identity(b) {
        return Ok(*a);
    }
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g2_jacobian_add216.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv g2_jacobian_add216")?;
    let mut wbytes = [0u8; G2_JACOBIAN_ADD_SSBO_BYTES];
    put_fp24(&mut wbytes, 72, &a.x);
    put_fp24(&mut wbytes, 96, &a.y);
    put_fp24(&mut wbytes, 120, &a.z);
    put_fp24(&mut wbytes, 144, &b.x);
    put_fp24(&mut wbytes, 168, &b.y);
    put_fp24(&mut wbytes, 192, &b.z);
    let mut out = [0u8; BLS12_381_FP2_U32_LIMBS * 3 * 4];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            J2_BUF,
            J2_BUF,
            (1, 1, 1),
            &wbytes,
            out.len(),
            &mut out,
            None,
        )?;
    }
    Ok(G2JacobianLimbs {
        x: get_fp24(&out, 0),
        y: get_fp24(&out, 24),
        z: get_fp24(&out, 48),
    })
}

/// G2 XYZZ += affine `p2` (Montgomery I/O). `p2` must not be the point at infinity.
pub fn run_g2_xyzz_add_mixed_gpu(dev: &VulkanDevice, xyzz: &mut G2XyzzLimbs, p2: &G2Affine) -> Result<()> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g2_xyzz_add_mixed144.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv g2_xyzz_add_mixed144")?;
    let (p2x, p2y) = g2_affine_montgomery_limbs(p2);
    let mut wbytes = [0u8; G2_XYZZ_ADD_MIXED_SSBO_BYTES];
    put_fp24(&mut wbytes, 0, &xyzz.x);
    put_fp24(&mut wbytes, 24, &xyzz.y);
    put_fp24(&mut wbytes, 48, &xyzz.zzz);
    put_fp24(&mut wbytes, 72, &xyzz.zz);
    put_fp24(&mut wbytes, 96, &p2x);
    put_fp24(&mut wbytes, 120, &p2y);
    let read_len = 96 * 4;
    let mut out = [0u8; 96 * 4];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            XYZZ2_BUF,
            XYZZ2_BUF,
            (1, 1, 1),
            &wbytes,
            read_len,
            &mut out,
            None,
        )?;
    }
    xyzz.x = get_fp24(&out, 0);
    xyzz.y = get_fp24(&out, 24);
    xyzz.zzz = get_fp24(&out, 48);
    xyzz.zz = get_fp24(&out, 72);
    Ok(())
}
