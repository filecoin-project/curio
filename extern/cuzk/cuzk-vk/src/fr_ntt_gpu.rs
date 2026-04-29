//! Fixed `n = 8` forward / inverse Fr NTT on the GPU (Montgomery `u32[8]` per element).
//!
//! Forward matches [`crate::ntt::fr_ntt_inplace`] with [`crate::ntt::fr_omega`] for `n = 8`; inverse
//! matches [`crate::ntt::fr_intt_inplace`] with the same primitive root.

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::Scalar;
use ff::Field;

use crate::device::VulkanDevice;
use crate::g1::BLS12_381_FR_U32_LIMBS;
use crate::scalar_limbs::{scalar_from_montgomery_u32_limbs, scalar_montgomery_u32_limbs};
use crate::vk_oneshot;

const NTT8_BYTES: usize = 8 * BLS12_381_FR_U32_LIMBS * 4;
const BUF_ALIGN: u64 = 256;

fn pack_ntt8_mont(coeffs: &[Scalar; 8]) -> [u8; NTT8_BYTES] {
    let mut out = [0u8; NTT8_BYTES];
    for e in 0..8 {
        let limbs = scalar_montgomery_u32_limbs(&coeffs[e]);
        for i in 0..BLS12_381_FR_U32_LIMBS {
            out[e * 32 + i * 4..e * 32 + i * 4 + 4].copy_from_slice(&limbs[i].to_le_bytes());
        }
    }
    out
}

fn unpack_ntt8_mont(bytes: &[u8; NTT8_BYTES]) -> [Scalar; 8] {
    let mut out = [Scalar::ZERO; 8];
    for e in 0..8 {
        let mut limbs = [0u32; BLS12_381_FR_U32_LIMBS];
        for i in 0..BLS12_381_FR_U32_LIMBS {
            limbs[i] = u32::from_le_bytes(
                bytes[e * 32 + i * 4..e * 32 + i * 4 + 4]
                    .try_into()
                    .unwrap(),
            );
        }
        out[e] = scalar_from_montgomery_u32_limbs(&limbs);
    }
    out
}

fn run_ntt8_spirv(
    dev: &VulkanDevice,
    spirv: &[u8],
    coeffs: &[Scalar; 8],
) -> Result<[Scalar; 8]> {
    let spirv_words = read_spv(&mut Cursor::new(spirv)).context("read_spv ntt8")?;
    let wbytes = pack_ntt8_mont(coeffs);
    let mut readback = [0u8; NTT8_BYTES];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_ALIGN,
            BUF_ALIGN,
            (1, 1, 1),
            &wbytes,
            NTT8_BYTES,
            &mut readback,
        )?;
    }
    Ok(unpack_ntt8_mont(&readback))
}

/// Forward radix-2 NTT for eight coefficients; same result as CPU `fr_ntt_inplace` with `fr_omega(8)`.
pub fn run_fr_ntt8_forward_gpu(dev: &VulkanDevice, coeffs: &[Scalar; 8]) -> Result<[Scalar; 8]> {
    run_ntt8_spirv(
        dev,
        include_bytes!(concat!(env!("OUT_DIR"), "/fr_ntt8_forward.spv")),
        coeffs,
    )
}

/// Inverse NTT for eight coefficients; same result as CPU `fr_intt_inplace` with `fr_omega(8)`.
pub fn run_fr_ntt8_inverse_gpu(dev: &VulkanDevice, evals: &[Scalar; 8]) -> Result<[Scalar; 8]> {
    run_ntt8_spirv(
        dev,
        include_bytes!(concat!(env!("OUT_DIR"), "/fr_ntt8_inverse.spv")),
        evals,
    )
}

/// Forward then inverse on the GPU (round-trip sanity for the pair of kernels).
pub fn run_fr_ntt8_roundtrip_gpu(dev: &VulkanDevice, coeffs: &[Scalar; 8]) -> Result<[Scalar; 8]> {
    let fwd = run_fr_ntt8_forward_gpu(dev, coeffs)?;
    run_fr_ntt8_inverse_gpu(dev, &fwd)
}
