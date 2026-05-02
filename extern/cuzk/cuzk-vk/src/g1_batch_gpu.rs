//! Phase G: bitmap-selected sum of affine G1 points (single workgroup, shared-memory tree).

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::{G1Affine, G1Projective};
use group::Group;

use crate::device::VulkanDevice;
use crate::ec::{
    g1_affine_montgomery_limbs, g1_jacobian_limbs_from_projective, G1JacobianLimbs,
    G1_BATCH_ACC_MAX_POINTS, G1_BATCH_ACC_SSBO_BYTES,
};
use crate::g1::BLS12_381_FP_U32_LIMBS;
use crate::vk_oneshot;

/// Storage buffer / mapped range size for the bitmap-accum kernel. Must cover the full SSBO
/// (`G1_BATCH_ACC_SSBO_BYTES` = 6544 B for `MAX_POINTS = 64`) rounded up to a 4 KiB page so the
/// host visible allocation lines up with MoltenVK's preferred backing.
const BUF_SIZE: u64 = ((G1_BATCH_ACC_SSBO_BYTES as u64) + 4095) & !4095;

fn put_u32(buf: &mut [u8], word: usize, v: u32) {
    let o = word * 4;
    buf[o..o + 4].copy_from_slice(&v.to_le_bytes());
}

fn put_fp12(buf: &mut [u8], word: usize, limbs: &[u32; BLS12_381_FP_U32_LIMBS]) {
    for i in 0..BLS12_381_FP_U32_LIMBS {
        put_u32(buf, word + i, limbs[i]);
    }
}

fn get_fp12(buf: &[u8], word: usize) -> [u32; BLS12_381_FP_U32_LIMBS] {
    let mut out = [0u32; BLS12_381_FP_U32_LIMBS];
    for i in 0..BLS12_381_FP_U32_LIMBS {
        let o = (word + i) * 4;
        out[i] = u32::from_le_bytes(buf[o..o + 4].try_into().unwrap());
    }
    out
}

/// Sum affine `points[0..n)` where bit *i* of `bitmap` is set; `n <= 64`.
/// `bitmap` is only read for indices `< n` (low word first, then high for `i >= 32`).
pub fn run_g1_batch_jacobian_accum_bitmap_gpu(
    dev: &VulkanDevice,
    n: usize,
    bitmap: u64,
    points: &[G1Affine],
) -> Result<G1JacobianLimbs> {
    anyhow::ensure!(n <= G1_BATCH_ACC_MAX_POINTS);
    anyhow::ensure!(points.len() >= n);
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g1_batch_accum_bitmap1636.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv g1_batch_accum_bitmap1636")?;

    let mut wbytes = vec![0u8; G1_BATCH_ACC_SSBO_BYTES];
    put_u32(&mut wbytes, 0, n as u32);
    put_u32(&mut wbytes, 1, bitmap as u32);
    put_u32(&mut wbytes, 2, (bitmap >> 32) as u32);

    let aff0 = 64 + 36;
    for i in 0..n {
        let (x, y) = g1_affine_montgomery_limbs(&points[i]);
        put_fp12(&mut wbytes, aff0 + i * 24, &x);
        put_fp12(&mut wbytes, aff0 + i * 24 + 12, &y);
    }

    let read_words = 64 + 36;
    let read_len = read_words * 4;
    let mut out = vec![0u8; read_len];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_SIZE,
            BUF_SIZE,
            (1, 1, 1),
            &wbytes,
            read_len,
            &mut out,
            None,
        )?;
    }
    Ok(G1JacobianLimbs {
        x: get_fp12(&out, 64),
        y: get_fp12(&out, 64 + 12),
        z: get_fp12(&out, 64 + 24),
    })
}

/// CPU oracle: same sum as [`run_g1_batch_jacobian_accum_bitmap_gpu`].
pub fn g1_batch_jacobian_accum_bitmap_cpu(n: usize, bitmap: u64, points: &[G1Affine]) -> G1JacobianLimbs {
    let mut acc = G1Projective::identity();
    for i in 0..n {
        if ((bitmap >> i) & 1) == 0 {
            continue;
        }
        acc += G1Projective::from(points[i]);
    }
    g1_jacobian_limbs_from_projective(&acc)
}
