//! Phase G: bitmap-selected sum of affine G2 points (single workgroup, shared-memory tree).

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::{G2Affine, G2Projective};
use group::Group;

use crate::device::VulkanDevice;
use crate::ec::{
    g2_affine_montgomery_limbs, g2_jacobian_limbs_from_projective, G2JacobianLimbs,
    G2_BATCH_ACC_MAX_POINTS, G2_BATCH_ACC_SSBO_BYTES,
};
use crate::fp2::BLS12_381_FP2_U32_LIMBS;
use crate::vk_oneshot;

const BUF_ALIGN: u64 = 4096;

fn put_u32(buf: &mut [u8], word: usize, v: u32) {
    let o = word * 4;
    buf[o..o + 4].copy_from_slice(&v.to_le_bytes());
}

fn put_fp2(buf: &mut [u8], word: usize, limbs: &[u32; BLS12_381_FP2_U32_LIMBS]) {
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        put_u32(buf, word + i, limbs[i]);
    }
}

fn get_fp2(buf: &[u8], word: usize) -> [u32; BLS12_381_FP2_U32_LIMBS] {
    let mut out = [0u32; BLS12_381_FP2_U32_LIMBS];
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        let o = (word + i) * 4;
        out[i] = u32::from_le_bytes(buf[o..o + 4].try_into().unwrap());
    }
    out
}

/// Sum affine `points[0..n)` where bit *i* of `bitmap` is set; `n <= 16` (low bits only).
pub fn run_g2_batch_jacobian_accum_bitmap_gpu(
    dev: &VulkanDevice,
    n: usize,
    bitmap: u32,
    points: &[G2Affine],
) -> Result<G2JacobianLimbs> {
    anyhow::ensure!(n <= G2_BATCH_ACC_MAX_POINTS);
    anyhow::ensure!(points.len() >= n);
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g2_batch_accum_aff16_904.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv g2_batch_accum_aff16_904")?;

    let mut wbytes = vec![0u8; G2_BATCH_ACC_SSBO_BYTES];
    put_u32(&mut wbytes, 0, n as u32);
    put_u32(&mut wbytes, 1, bitmap);

    let aff0 = 64 + 72;
    for i in 0..n {
        let (x, y) = g2_affine_montgomery_limbs(&points[i]);
        put_fp2(&mut wbytes, aff0 + i * 48, &x);
        put_fp2(&mut wbytes, aff0 + i * 48 + 24, &y);
    }

    let read_words = 64 + 72;
    let read_len = read_words * 4;
    let mut out = vec![0u8; read_len];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &wbytes,
            read_len,
            &mut out,
        )?;
    }
    Ok(G2JacobianLimbs {
        x: get_fp2(&out, 64),
        y: get_fp2(&out, 64 + 24),
        z: get_fp2(&out, 64 + 48),
    })
}

/// CPU oracle for [`run_g2_batch_jacobian_accum_bitmap_gpu`].
pub fn g2_batch_jacobian_accum_bitmap_cpu(n: usize, bitmap: u32, points: &[G2Affine]) -> G2JacobianLimbs {
    let mut acc = G2Projective::identity();
    for i in 0..n {
        if ((bitmap >> i) & 1) == 0 {
            continue;
        }
        acc += G2Projective::from(points[i]);
    }
    g2_jacobian_limbs_from_projective(&acc)
}
