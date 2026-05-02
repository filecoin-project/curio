//! SupraSeal-style **Fr** pointwise kernels: coefficient-wise multiply and `a - c*b`.

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::Scalar;

use crate::device::VulkanDevice;
use crate::fr_ntt_general_gpu::FR_NTT_GENERAL_MAX_N;
use crate::g1::BLS12_381_FR_U32_LIMBS;
use crate::scalar_limbs::{scalar_from_montgomery_u32_limbs, scalar_montgomery_u32_limbs};
use crate::vk_oneshot;

/// Maximum vector length for [`run_fr_coeff_wise_mult_gpu`] / [`run_fr_sub_mult_constant_gpu`].
/// Kept equal to [`crate::fr_ntt_general_gpu::FR_NTT_GENERAL_MAX_N`] so coset / H-term pipelines can
/// use the same `n` bound as general Fr NTT without a host round-trip between distribute and FFT.
pub const FR_POINTWISE_MAX: usize = FR_NTT_GENERAL_MAX_N;

/// SSBO size in `u32` words for coefficient-wise multiply (header + `out` + `a` + `b` regions).
pub const FR_COEFF_WISE_BUFFER_U32: usize = 64 + FR_POINTWISE_MAX * BLS12_381_FR_U32_LIMBS * 3;

/// SSBO size for `sub_mult` including Montgomery `c` (8 words) after `a`/`b`.
pub const FR_SUB_MULT_BUFFER_U32: usize = 64 + FR_POINTWISE_MAX * BLS12_381_FR_U32_LIMBS * 3 + BLS12_381_FR_U32_LIMBS;

fn coeff_base_a() -> usize {
    64 + FR_POINTWISE_MAX * BLS12_381_FR_U32_LIMBS
}

fn coeff_base_b() -> usize {
    64 + 2 * FR_POINTWISE_MAX * BLS12_381_FR_U32_LIMBS
}

fn coeff_base_c() -> usize {
    64 + 3 * FR_POINTWISE_MAX * BLS12_381_FR_U32_LIMBS
}

fn write_mont_region(buf: &mut [u32], base_word: usize, idx: usize, limbs: &[u32; BLS12_381_FR_U32_LIMBS]) {
    let o = base_word + idx * BLS12_381_FR_U32_LIMBS;
    buf[o..o + BLS12_381_FR_U32_LIMBS].copy_from_slice(limbs);
}

fn read_mont_region(buf: &[u32], base_word: usize, idx: usize) -> [u32; BLS12_381_FR_U32_LIMBS] {
    let o = base_word + idx * BLS12_381_FR_U32_LIMBS;
    let mut out = [0u32; BLS12_381_FR_U32_LIMBS];
    out.copy_from_slice(&buf[o..o + BLS12_381_FR_U32_LIMBS]);
    out
}

fn u32s_to_bytes(words: &[u32]) -> Vec<u8> {
    words.iter().flat_map(|w| w.to_le_bytes()).collect()
}

fn bytes_to_u32s(bytes: &[u8], n_words: usize) -> Vec<u32> {
    let mut out = vec![0u32; n_words];
    for i in 0..n_words {
        let o = i * 4;
        out[i] = u32::from_le_bytes(bytes[o..o + 4].try_into().unwrap());
    }
    out
}

/// `out[i] = a[i] * b[i]` in Montgomery on the GPU. `out.len()` must equal `a` and `b`; `n = 0` is a no-op.
pub fn run_fr_coeff_wise_mult_gpu(
    dev: &VulkanDevice,
    out: &mut [Scalar],
    a: &[Scalar],
    b: &[Scalar],
) -> Result<()> {
    let n = out.len();
    if n == 0 {
        return Ok(());
    }
    anyhow::ensure!(n == a.len() && n == b.len(), "length mismatch");
    anyhow::ensure!(n <= FR_POINTWISE_MAX, "n > FR_POINTWISE_MAX");

    let mut d = vec![0u32; FR_COEFF_WISE_BUFFER_U32];
    d[0] = n as u32;
    for i in 0..n {
        write_mont_region(&mut d, coeff_base_a(), i, &scalar_montgomery_u32_limbs(&a[i]));
        write_mont_region(&mut d, coeff_base_b(), i, &scalar_montgomery_u32_limbs(&b[i]));
    }
    let write_bytes = u32s_to_bytes(&d);
    let read_words = 64 + n * BLS12_381_FR_U32_LIMBS;
    let read_bytes = read_words * 4;
    let mut read_back = vec![0u8; read_bytes];
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fr_coeff_wise_mult.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fr_coeff_wise_mult")?;
    let gx = ((n as u32) + 63) / 64;
    let buf_bytes = (FR_COEFF_WISE_BUFFER_U32 * 4) as u64;
    let alloc = buf_bytes.max(256 * 1024);
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            alloc,
            alloc,
            (gx, 1, 1),
            &write_bytes,
            read_bytes,
            &mut read_back,
            None,
        )?;
    }
    let dr = bytes_to_u32s(&read_back, read_words);
    for i in 0..n {
        let limbs = read_mont_region(&dr, 64, i);
        out[i] = scalar_from_montgomery_u32_limbs(&limbs);
    }
    Ok(())
}

/// `out[i] = a[i] - c * b[i]` (Montgomery). `n = 0` is a no-op.
pub fn run_fr_sub_mult_constant_gpu(
    dev: &VulkanDevice,
    out: &mut [Scalar],
    a: &[Scalar],
    b: &[Scalar],
    c: &Scalar,
) -> Result<()> {
    let n = out.len();
    if n == 0 {
        return Ok(());
    }
    anyhow::ensure!(n == a.len() && n == b.len(), "length mismatch");
    anyhow::ensure!(n <= FR_POINTWISE_MAX, "n > FR_POINTWISE_MAX");

    let mut d = vec![0u32; FR_SUB_MULT_BUFFER_U32];
    d[0] = n as u32;
    for i in 0..n {
        write_mont_region(&mut d, coeff_base_a(), i, &scalar_montgomery_u32_limbs(&a[i]));
        write_mont_region(&mut d, coeff_base_b(), i, &scalar_montgomery_u32_limbs(&b[i]));
    }
    write_mont_region(&mut d, coeff_base_c(), 0, &scalar_montgomery_u32_limbs(c));

    let write_bytes = u32s_to_bytes(&d);
    let read_words = 64 + n * BLS12_381_FR_U32_LIMBS;
    let read_bytes = read_words * 4;
    let mut read_back = vec![0u8; read_bytes];
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/fr_sub_mult_constant.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv fr_sub_mult_constant")?;
    let gx = ((n as u32) + 63) / 64;
    let buf_bytes = (FR_SUB_MULT_BUFFER_U32 * 4) as u64;
    let alloc = buf_bytes.max(256 * 1024);
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            alloc,
            alloc,
            (gx, 1, 1),
            &write_bytes,
            read_bytes,
            &mut read_back,
            None,
        )?;
    }
    let dr = bytes_to_u32s(&read_back, read_words);
    for i in 0..n {
        let limbs = read_mont_region(&dr, 64, i);
        out[i] = scalar_from_montgomery_u32_limbs(&limbs);
    }
    Ok(())
}
