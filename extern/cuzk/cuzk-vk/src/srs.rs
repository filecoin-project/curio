//! Phase I — SRS mmap layout from SupraSeal `groth16_srs.cuh` (Filecoin Groth16 SRS binary).
//!
//! On-disk types: big-endian `u32` counts, 96-byte uncompressed G1 affine, 192-byte G2 affine.
//! Order: `alpha_g1, beta_g1, beta_g2, gamma_g2, delta_g1, delta_g2`, then `n_ic`, `ic[]`, `n_h`,
//! `h[]`, `n_l`, `l[]`, `n_a`, `a[]`, `n_b_g1`, `b_g1[]`, `n_b_g2`, `b_g2[]`.
//!
//! Host helpers: [`srs_read_file`], [`srs_decode_ic_g1`] (see `tests/srs_file_ic.rs`).

use std::path::Path;

use anyhow::{Context, Result};
use blstrs::{G1Affine, G2Affine};

/// Serialized BLS12-381 G1 affine (uncompressed) size in bytes.
pub const SRS_P1_AFFINE_BYTES: usize = 96;
/// Serialized G2 affine size in bytes.
pub const SRS_P2_AFFINE_BYTES: usize = 192;
/// Verifying key prefix: 3×G1 + 3×G2 affines, no padding.
pub const SRS_VK_PREFIX_BYTES: usize = SRS_P1_AFFINE_BYTES * 3 + SRS_P2_AFFINE_BYTES * 3;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct SrsPointCounts {
    pub n_ic: u32,
    pub n_h: u32,
    pub n_l: u32,
    pub n_a: u32,
    pub n_b_g1: u32,
    pub n_b_g2: u32,
}

/// First six affines in an SRS file (after any outer container), matching `groth16_srs.cuh` order.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct SrsVkPrefix {
    pub alpha_g1: G1Affine,
    pub beta_g1: G1Affine,
    pub beta_g2: G2Affine,
    pub gamma_g2: G2Affine,
    pub delta_g1: G1Affine,
    pub delta_g2: G2Affine,
}

/// Decode one **uncompressed** G1 affine (`blst` / `blstrs` wire format, 96 bytes).
pub fn srs_g1_affine_from_uncompressed_bytes(buf: &[u8]) -> Result<G1Affine> {
    anyhow::ensure!(
        buf.len() >= SRS_P1_AFFINE_BYTES,
        "G1 uncompressed slice too short (need {})",
        SRS_P1_AFFINE_BYTES
    );
    let a: [u8; SRS_P1_AFFINE_BYTES] = buf[..SRS_P1_AFFINE_BYTES]
        .try_into()
        .expect("length checked");
    let ct = G1Affine::from_uncompressed(&a);
    if bool::from(ct.is_some()) {
        Ok(ct.unwrap())
    } else {
        anyhow::bail!("invalid G1 uncompressed SRS bytes");
    }
}

/// Decode one **uncompressed** G2 affine (192 bytes).
pub fn srs_g2_affine_from_uncompressed_bytes(buf: &[u8]) -> Result<G2Affine> {
    anyhow::ensure!(
        buf.len() >= SRS_P2_AFFINE_BYTES,
        "G2 uncompressed slice too short (need {})",
        SRS_P2_AFFINE_BYTES
    );
    let a: [u8; SRS_P2_AFFINE_BYTES] = buf[..SRS_P2_AFFINE_BYTES]
        .try_into()
        .expect("length checked");
    let ct = G2Affine::from_uncompressed(&a);
    if bool::from(ct.is_some()) {
        Ok(ct.unwrap())
    } else {
        anyhow::bail!("invalid G2 uncompressed SRS bytes");
    }
}

/// Parse the fixed VK prefix (`alpha_g1` … `delta_g2`) from the start of an SRS blob.
pub fn srs_decode_vk_prefix(data: &[u8]) -> Result<SrsVkPrefix> {
    anyhow::ensure!(
        data.len() >= SRS_VK_PREFIX_BYTES,
        "SRS data shorter than VK prefix ({})",
        SRS_VK_PREFIX_BYTES
    );
    let mut o = 0usize;
    let alpha_g1 = srs_g1_affine_from_uncompressed_bytes(&data[o..]).context("alpha_g1")?;
    o += SRS_P1_AFFINE_BYTES;
    let beta_g1 = srs_g1_affine_from_uncompressed_bytes(&data[o..]).context("beta_g1")?;
    o += SRS_P1_AFFINE_BYTES;
    let beta_g2 = srs_g2_affine_from_uncompressed_bytes(&data[o..]).context("beta_g2")?;
    o += SRS_P2_AFFINE_BYTES;
    let gamma_g2 = srs_g2_affine_from_uncompressed_bytes(&data[o..]).context("gamma_g2")?;
    o += SRS_P2_AFFINE_BYTES;
    let delta_g1 = srs_g1_affine_from_uncompressed_bytes(&data[o..]).context("delta_g1")?;
    o += SRS_P1_AFFINE_BYTES;
    let delta_g2 = srs_g2_affine_from_uncompressed_bytes(&data[o..]).context("delta_g2")?;
    Ok(SrsVkPrefix {
        alpha_g1,
        beta_g1,
        beta_g2,
        gamma_g2,
        delta_g1,
        delta_g2,
    })
}

#[inline]
fn read_u32_be(data: &[u8], off: usize) -> Option<u32> {
    if off + 4 > data.len() {
        return None;
    }
    Some(u32::from_be_bytes(
        data[off..off + 4].try_into().ok()?,
    ))
}

/// Read an SRS file into memory (host-side; mmap can wrap this slice later).
pub fn srs_read_file(path: &Path) -> Result<Vec<u8>> {
    std::fs::read(path).with_context(|| format!("SRS read {}", path.display()))
}

/// Decode `ic[idx]` as an uncompressed G1 affine (`0 <= idx < n_ic`).
pub fn srs_decode_ic_g1(data: &[u8], idx: usize) -> Result<G1Affine> {
    anyhow::ensure!(
        data.len() >= SRS_VK_PREFIX_BYTES + 4,
        "SRS data shorter than VK prefix + n_ic ({})",
        SRS_VK_PREFIX_BYTES + 4
    );
    let mut o = SRS_VK_PREFIX_BYTES;
    let n_ic = read_u32_be(data, o).context("n_ic")? as usize;
    o += 4;
    anyhow::ensure!(idx < n_ic, "ic index {idx} out of range (n_ic={n_ic})");
    o += idx * SRS_P1_AFFINE_BYTES;
    anyhow::ensure!(
        o + SRS_P1_AFFINE_BYTES <= data.len(),
        "truncated ic[{idx}]"
    );
    srs_g1_affine_from_uncompressed_bytes(&data[o..])
}

/// Walk the SRS blob after the VK prefix: read six counts and skip the following affine blobs.
/// Returns `(counts, byte_offset_immediately_after_b_g2_array)` for bounds checks / mmap slicing.
pub fn srs_walk_counts_and_point_blobs(data: &[u8]) -> Result<(SrsPointCounts, usize)> {
    anyhow::ensure!(
        data.len() >= SRS_VK_PREFIX_BYTES,
        "SRS data shorter than VK prefix ({})",
        SRS_VK_PREFIX_BYTES
    );
    let mut o = SRS_VK_PREFIX_BYTES;

    let n_ic = read_u32_be(data, o).context("n_ic")?;
    o += 4;
    o += (n_ic as usize)
        .checked_mul(SRS_P1_AFFINE_BYTES)
        .context("n_ic overflow")?;
    anyhow::ensure!(o <= data.len(), "truncated at ic points");

    let n_h = read_u32_be(data, o).context("n_h")?;
    o += 4;
    o += (n_h as usize)
        .checked_mul(SRS_P1_AFFINE_BYTES)
        .context("n_h overflow")?;
    anyhow::ensure!(o <= data.len(), "truncated at h points");

    let n_l = read_u32_be(data, o).context("n_l")?;
    o += 4;
    o += (n_l as usize)
        .checked_mul(SRS_P1_AFFINE_BYTES)
        .context("n_l overflow")?;
    anyhow::ensure!(o <= data.len(), "truncated at l points");

    let n_a = read_u32_be(data, o).context("n_a")?;
    o += 4;
    o += (n_a as usize)
        .checked_mul(SRS_P1_AFFINE_BYTES)
        .context("n_a overflow")?;
    anyhow::ensure!(o <= data.len(), "truncated at a points");

    let n_b_g1 = read_u32_be(data, o).context("n_b_g1")?;
    o += 4;
    o += (n_b_g1 as usize)
        .checked_mul(SRS_P1_AFFINE_BYTES)
        .context("n_b_g1 overflow")?;
    anyhow::ensure!(o <= data.len(), "truncated at b_g1 points");

    let n_b_g2 = read_u32_be(data, o).context("n_b_g2")?;
    o += 4;
    o += (n_b_g2 as usize)
        .checked_mul(SRS_P2_AFFINE_BYTES)
        .context("n_b_g2 overflow")?;
    anyhow::ensure!(o <= data.len(), "truncated at b_g2 points");

    Ok((
        SrsPointCounts {
            n_ic,
            n_h,
            n_l,
            n_a,
            n_b_g1,
            n_b_g2,
        },
        o,
    ))
}
