//! Power-of-two Fr NTT for `n <= 2^14` on the GPU (Montgomery `u32[8]`, multi-dispatch).
//!
//! Matches [`crate::ntt::fr_ntt_inplace`] / [`crate::ntt::fr_intt_inplace`] with [`crate::ntt::fr_omega`].

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::Scalar;
use ff::Field;

use crate::device::VulkanDevice;
use crate::g1::BLS12_381_FR_U32_LIMBS;
use crate::ntt::{fr_stage_wlens, FrNttPlan};
use crate::scalar_limbs::{scalar_from_montgomery_u32_limbs, scalar_montgomery_u32_limbs};
use crate::vk_oneshot::{run_compute_passes_1x_storage_buffer, ComputePassDesc};

/// `buf.d` header words (matches other Fr SSBO helpers).
pub const FR_NTT_GENERAL_HEADER_WORDS: usize = 64;
/// Maximum supported length (must match GLSL `FR_NTT_MAX_N`).
pub const FR_NTT_GENERAL_MAX_N: usize = 16384;
/// Per-stage `wlen` slots reserved (must match `FR_NTT_MAX_LOG` in shaders).
pub const FR_NTT_GENERAL_MAX_LOG: usize = 32;

/// Compute passes in [`run_fr_ntt_general_forward_gpu`] (radix-2: bitrev + copy + `log_n` half butterflies).
#[inline]
pub const fn fr_ntt_general_forward_pass_count_radix2(log_n: u32) -> u32 {
    2 + log_n
}

/// Compute passes in [`run_fr_ntt_general_inverse_gpu`] (forward-shaped stages + `scale_ninv`).
#[inline]
pub const fn fr_ntt_general_inverse_pass_count_radix2(log_n: u32) -> u32 {
    3 + log_n
}

const WG: u32 = 256;

/// Total `u32` words in the general-NTT SSBO (header + data + scratch + twiddle bases).
#[inline]
pub fn fr_ntt_general_buf_u32_len() -> usize {
    FR_NTT_GENERAL_HEADER_WORDS + 2 * FR_NTT_GENERAL_MAX_N * 8 + FR_NTT_GENERAL_MAX_LOG * 8
}

#[inline]
pub fn fr_ntt_general_buf_bytes() -> usize {
    fr_ntt_general_buf_u32_len() * 4
}

#[inline]
fn off_data_w() -> usize {
    FR_NTT_GENERAL_HEADER_WORDS
}

#[inline]
fn off_wstep_w() -> usize {
    FR_NTT_GENERAL_HEADER_WORDS + 2 * FR_NTT_GENERAL_MAX_N * 8
}

fn push_zero() -> [u8; 16] {
    [0u8; 16]
}

fn push_stage(stage: u32) -> [u8; 16] {
    let mut b = [0u8; 16];
    b[0..4].copy_from_slice(&stage.to_le_bytes());
    b
}

fn div_ceil_u32(a: u32, b: u32) -> u32 {
    (a + b - 1) / b
}

fn dispatch_for_n(n: u32) -> (u32, u32, u32) {
    (div_ceil_u32(n, WG), 1, 1)
}

fn dispatch_for_half(n: u32) -> (u32, u32, u32) {
    (div_ceil_u32(n / 2, WG), 1, 1)
}

fn pack_buf(
    n: usize,
    log_n: u32,
    coeffs: &[Scalar],
    stage_wlens_mont: &[[u32; BLS12_381_FR_U32_LIMBS]],
    n_inv_mont: Option<&[u32; BLS12_381_FR_U32_LIMBS]>,
) -> Vec<u8> {
    let mut words = vec![0u32; fr_ntt_general_buf_u32_len()];
    words[0] = n as u32;
    words[1] = log_n;
    if let Some(inv) = n_inv_mont {
        for i in 0..BLS12_381_FR_U32_LIMBS {
            words[8 + i] = inv[i];
        }
    }
    let off_d = off_data_w();
    for (i, s) in coeffs.iter().enumerate().take(n) {
        let m = scalar_montgomery_u32_limbs(s);
        let base = off_d + i * BLS12_381_FR_U32_LIMBS;
        for j in 0..BLS12_381_FR_U32_LIMBS {
            words[base + j] = m[j];
        }
    }
    let off_w = off_wstep_w();
    for (si, wl) in stage_wlens_mont.iter().enumerate().take(log_n as usize) {
        let base = off_w + si * BLS12_381_FR_U32_LIMBS;
        for j in 0..BLS12_381_FR_U32_LIMBS {
            words[base + j] = wl[j];
        }
    }
    words
        .into_iter()
        .flat_map(|w| w.to_le_bytes())
        .collect()
}

fn read_u32_at_byte(buf: &[u8], byte_off: usize) -> u32 {
    u32::from_le_bytes(buf[byte_off..byte_off + 4].try_into().unwrap())
}

fn unpack_coeffs(buf: &[u8], n: usize) -> Vec<Scalar> {
    let off_d = off_data_w();
    let base_byte = off_d * 4;
    let mut v = Vec::with_capacity(n);
    for i in 0..n {
        let mut limbs = [0u32; BLS12_381_FR_U32_LIMBS];
        for j in 0..BLS12_381_FR_U32_LIMBS {
            let bo = base_byte + (i * BLS12_381_FR_U32_LIMBS + j) * 4;
            limbs[j] = read_u32_at_byte(buf, bo);
        }
        v.push(scalar_from_montgomery_u32_limbs(&limbs));
    }
    v
}

fn spirv_words(bytes: &'static [u8]) -> Result<Vec<u32>> {
    read_spv(&mut Cursor::new(bytes)).context("read_spv fr_ntt_general")
}

/// Forward NTT: same result as CPU [`crate::ntt::fr_ntt_inplace`] with [`crate::ntt::fr_omega`].
pub fn run_fr_ntt_general_forward_gpu(dev: &VulkanDevice, coeffs: &[Scalar]) -> Result<Vec<Scalar>> {
    let n = coeffs.len();
    if n == 0 {
        return Ok(vec![]);
    }
    anyhow::ensure!(n.is_power_of_two(), "n must be a power of two");
    anyhow::ensure!(
        n <= FR_NTT_GENERAL_MAX_N,
        "n {} exceeds FR_NTT_GENERAL_MAX_N",
        n
    );
    let log_n = n.trailing_zeros();
    let plan = FrNttPlan::try_new(log_n)?;

    let stage_limbs: Vec<[u32; BLS12_381_FR_U32_LIMBS]> = plan
        .stage_wlens
        .iter()
        .map(|s| scalar_montgomery_u32_limbs(s))
        .collect();

    let packed = pack_buf(n, log_n, coeffs, &stage_limbs, None);
    let buf_sz = fr_ntt_general_buf_bytes() as u64;
    let read_len = (off_data_w() + n * BLS12_381_FR_U32_LIMBS) * 4;

    let w_bitrev = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_bitrev_scatter.spv"
    )))?;
    let w_copy = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_copy_scratch_to_data.spv"
    )))?;
    let w_radix = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix2_stage_spec.spv"
    )))?;

    let dn = n as u32;
    let mut passes: Vec<ComputePassDesc<'_>> = Vec::new();
    passes.push(ComputePassDesc {
        spirv_words: &w_bitrev,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });
    passes.push(ComputePassDesc {
        spirv_words: &w_copy,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });
    #[cfg(fr_ntt_radix2_spec)]
    {
        let radix2_spec_le = WG.to_le_bytes();
        let radix2_spec_map = [crate::vk_oneshot::spec_map_u32(0, 0)];
        let radix2_spec_blob = crate::vk_oneshot::ComputeShaderStageSpec {
            map_entries: &radix2_spec_map,
            data: &radix2_spec_le,
        };
        for s in 0..log_n {
            passes.push(ComputePassDesc {
                spirv_words: &w_radix,
                dispatch: dispatch_for_half(dn),
                push_constants: push_stage(s),
                stage_spec: Some(&radix2_spec_blob),
            });
        }
    }
    #[cfg(not(fr_ntt_radix2_spec))]
    {
        for s in 0..log_n {
            passes.push(ComputePassDesc {
                spirv_words: &w_radix,
                dispatch: dispatch_for_half(dn),
                push_constants: push_stage(s),
                stage_spec: None,
            });
        }
    }

    let mut readback = vec![0u8; read_len];
    unsafe {
        run_compute_passes_1x_storage_buffer(
            dev,
            buf_sz,
            buf_sz,
            &packed,
            &passes,
            read_len,
            &mut readback,
        )?;
    }
    Ok(unpack_coeffs(&readback, n))
}

/// Inverse NTT: same result as CPU [`crate::ntt::fr_intt_inplace`] with [`crate::ntt::fr_omega`].
pub fn run_fr_ntt_general_inverse_gpu(dev: &VulkanDevice, evals: &[Scalar]) -> Result<Vec<Scalar>> {
    let n = evals.len();
    if n == 0 {
        return Ok(vec![]);
    }
    anyhow::ensure!(n.is_power_of_two(), "n must be a power of two");
    anyhow::ensure!(
        n <= FR_NTT_GENERAL_MAX_N,
        "n {} exceeds FR_NTT_GENERAL_MAX_N",
        n
    );
    let log_n = n.trailing_zeros();
    let plan = FrNttPlan::try_new(log_n)?;
    let inv_wlens = fr_stage_wlens(n, plan.omega_inv);
    let stage_limbs: Vec<[u32; BLS12_381_FR_U32_LIMBS]> = inv_wlens
        .iter()
        .map(|s| scalar_montgomery_u32_limbs(s))
        .collect();
    let n_inv = Scalar::from(n as u64).invert().unwrap();
    let n_inv_limbs = scalar_montgomery_u32_limbs(&n_inv);

    let packed = pack_buf(n, log_n, evals, &stage_limbs, Some(&n_inv_limbs));
    let buf_sz = fr_ntt_general_buf_bytes() as u64;
    let read_len = (off_data_w() + n * BLS12_381_FR_U32_LIMBS) * 4;

    let w_bitrev = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_bitrev_scatter.spv"
    )))?;
    let w_copy = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_copy_scratch_to_data.spv"
    )))?;
    let w_radix = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix2_stage_spec.spv"
    )))?;
    let w_scale = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_scale_ninv.spv"
    )))?;

    let dn = n as u32;
    let mut passes: Vec<ComputePassDesc<'_>> = Vec::new();
    passes.push(ComputePassDesc {
        spirv_words: &w_bitrev,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });
    passes.push(ComputePassDesc {
        spirv_words: &w_copy,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });
    #[cfg(fr_ntt_radix2_spec)]
    {
        let radix2_spec_le = WG.to_le_bytes();
        let radix2_spec_map = [crate::vk_oneshot::spec_map_u32(0, 0)];
        let radix2_spec_blob = crate::vk_oneshot::ComputeShaderStageSpec {
            map_entries: &radix2_spec_map,
            data: &radix2_spec_le,
        };
        for s in 0..log_n {
            passes.push(ComputePassDesc {
                spirv_words: &w_radix,
                dispatch: dispatch_for_half(dn),
                push_constants: push_stage(s),
                stage_spec: Some(&radix2_spec_blob),
            });
        }
    }
    #[cfg(not(fr_ntt_radix2_spec))]
    {
        for s in 0..log_n {
            passes.push(ComputePassDesc {
                spirv_words: &w_radix,
                dispatch: dispatch_for_half(dn),
                push_constants: push_stage(s),
                stage_spec: None,
            });
        }
    }
    passes.push(ComputePassDesc {
        spirv_words: &w_scale,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });

    let mut readback = vec![0u8; read_len];
    unsafe {
        run_compute_passes_1x_storage_buffer(
            dev,
            buf_sz,
            buf_sz,
            &packed,
            &passes,
            read_len,
            &mut readback,
        )?;
    }
    Ok(unpack_coeffs(&readback, n))
}

/// Forward then inverse on the GPU (round-trip sanity).
pub fn run_fr_ntt_general_roundtrip_gpu(dev: &VulkanDevice, coeffs: &[Scalar]) -> Result<Vec<Scalar>> {
    let fwd = run_fr_ntt_general_forward_gpu(dev, coeffs)?;
    run_fr_ntt_general_inverse_gpu(dev, &fwd)
}

#[cfg(test)]
mod radix2_pass_count_tests {
    use super::*;

    #[test]
    fn forward_pass_count_matches_gpu_dispatch_list() {
        for log_n in 1u32..=14 {
            assert_eq!(fr_ntt_general_forward_pass_count_radix2(log_n), 2 + log_n);
        }
    }

    #[test]
    fn inverse_pass_count_matches_gpu_dispatch_list() {
        for log_n in 1u32..=14 {
            assert_eq!(fr_ntt_general_inverse_pass_count_radix2(log_n), 3 + log_n);
        }
    }
}
