//! Phase I — SRS mmap layout from SupraSeal `groth16_srs.cuh` (Filecoin Groth16 SRS binary).
//!
//! On-disk types: big-endian `u32` counts, 96-byte uncompressed G1 affine, 192-byte G2 affine.
//! Order: `alpha_g1, beta_g1, beta_g2, gamma_g2, delta_g1, delta_g2`, then `n_ic`, `ic[]`, `n_h`,
//! `h[]`, `n_l`, `l[]`, `n_a`, `a[]`, `n_b_g1`, `b_g1[]`, `n_b_g2`, `b_g2[]`.
//!
//! Host helpers: [`srs_read_file`], [`srs_read_file_spawn`] (§C.2 disk overlap), [`srs_decode_ic_g1`], [`srs_decode_h_g1`], [`srs_decode_l_g1`],
//! [`srs_decode_a_g1`], [`srs_decode_bg1_g1`], [`srs_decode_bg2_g2`], [`srs_synthetic_partition_smoke_blob`]
//! (see `tests/srs_file_ic.rs`). GPU limb staging smoke: [`crate::srs_gpu`] (G1+G2).

use std::path::{Path, PathBuf};
use std::thread::JoinHandle;

use anyhow::{Context, Result};
use blstrs::{G1Affine, G1Projective, G2Affine, G2Projective, Scalar};
use ff::Field;
use group::Group;
use rand::SeedableRng;
use rand_chacha::ChaCha20Rng;

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

/// **Milestone B (§8.3 C.2 slice):** start SRS file I/O on a background thread so the caller can
/// overlap host work (e.g. `FrNttPlan` build, buffer alloc) before [`JoinHandle::join`].
///
/// Full async upload to the GPU queue is still future work; this only overlaps **disk read** latency.
pub fn srs_read_file_spawn(path: PathBuf) -> JoinHandle<Result<Vec<u8>>> {
    std::thread::spawn(move || srs_read_file(&path))
}

/// Byte offset immediately after the `ic[]` array (first `u32` there is `n_h`).
fn srs_offset_after_ic(data: &[u8]) -> Result<usize> {
    anyhow::ensure!(
        data.len() >= SRS_VK_PREFIX_BYTES + 4,
        "SRS data shorter than VK prefix + n_ic ({})",
        SRS_VK_PREFIX_BYTES + 4
    );
    let mut o = SRS_VK_PREFIX_BYTES;
    let n_ic = read_u32_be(data, o).context("n_ic")? as usize;
    o += 4;
    o += n_ic
        .checked_mul(SRS_P1_AFFINE_BYTES)
        .context("n_ic * G1 overflow")?;
    anyhow::ensure!(o <= data.len(), "truncated ic[]");
    Ok(o)
}

/// `count_off` points at big-endian `n`; returns byte offset **after** `n` G1 affines (`4 + n*96` past `count_off`).
fn srs_skip_counted_g1_array(data: &[u8], count_off: usize, label: &'static str) -> Result<usize> {
    let n = read_u32_be(data, count_off)
        .with_context(|| format!("{label} count (off {count_off})"))?
        as usize;
    let end = count_off
        .checked_add(4)
        .and_then(|x| x.checked_add(n.checked_mul(SRS_P1_AFFINE_BYTES)?))
        .context("G1 array size overflow")?;
    anyhow::ensure!(end <= data.len(), "truncated {label}[] (need {end} bytes)");
    Ok(end)
}

fn srs_decode_g1_at_array(
    data: &[u8],
    count_off: usize,
    idx: usize,
    label: &'static str,
) -> Result<G1Affine> {
    let n = read_u32_be(data, count_off)
        .with_context(|| format!("{label} count"))?
        as usize;
    anyhow::ensure!(idx < n, "{label}[{idx}] out of range (n={n})");
    let o = count_off
        .checked_add(4)
        .and_then(|x| x.checked_add(idx.checked_mul(SRS_P1_AFFINE_BYTES)?))
        .context("G1 index overflow")?;
    anyhow::ensure!(
        o + SRS_P1_AFFINE_BYTES <= data.len(),
        "truncated {label}[{idx}]"
    );
    srs_g1_affine_from_uncompressed_bytes(&data[o..]).with_context(|| format!("decode {label}[{idx}]"))
}

/// `count_off` points at big-endian `n`; returns byte offset after `n` G2 affines.
fn srs_skip_counted_g2_array(data: &[u8], count_off: usize, label: &'static str) -> Result<usize> {
    let n = read_u32_be(data, count_off)
        .with_context(|| format!("{label} G2 count (off {count_off})"))?
        as usize;
    let end = count_off
        .checked_add(4)
        .and_then(|x| x.checked_add(n.checked_mul(SRS_P2_AFFINE_BYTES)?))
        .context("G2 array size overflow")?;
    anyhow::ensure!(end <= data.len(), "truncated {label}[] G2 (need {end} bytes)");
    Ok(end)
}

fn srs_decode_g2_at_array(
    data: &[u8],
    count_off: usize,
    idx: usize,
    label: &'static str,
) -> Result<G2Affine> {
    let n = read_u32_be(data, count_off)
        .with_context(|| format!("{label} count"))?
        as usize;
    anyhow::ensure!(idx < n, "{label}[{idx}] out of range (n={n})");
    let o = count_off
        .checked_add(4)
        .and_then(|x| x.checked_add(idx.checked_mul(SRS_P2_AFFINE_BYTES)?))
        .context("G2 index overflow")?;
    anyhow::ensure!(
        o + SRS_P2_AFFINE_BYTES <= data.len(),
        "truncated {label}[{idx}]"
    );
    srs_g2_affine_from_uncompressed_bytes(&data[o..]).with_context(|| format!("decode {label}[{idx}]"))
}

fn srs_offset_of_n_l(data: &[u8]) -> Result<usize> {
    srs_skip_counted_g1_array(data, srs_offset_after_ic(data)?, "h")
}

fn srs_offset_of_n_a(data: &[u8]) -> Result<usize> {
    srs_skip_counted_g1_array(data, srs_offset_of_n_l(data)?, "l")
}

fn srs_offset_of_n_bg1(data: &[u8]) -> Result<usize> {
    srs_skip_counted_g1_array(data, srs_offset_of_n_a(data)?, "a")
}

fn srs_offset_of_n_bg2(data: &[u8]) -> Result<usize> {
    srs_skip_counted_g1_array(data, srs_offset_of_n_bg1(data)?, "b_g1")
}

/// Decode `ic[idx]` as an uncompressed G1 affine (`0 <= idx < n_ic`).
pub fn srs_decode_ic_g1(data: &[u8], idx: usize) -> Result<G1Affine> {
    anyhow::ensure!(
        data.len() >= SRS_VK_PREFIX_BYTES + 4,
        "SRS data shorter than VK prefix + n_ic ({})",
        SRS_VK_PREFIX_BYTES + 4
    );
    srs_decode_g1_at_array(data, SRS_VK_PREFIX_BYTES, idx, "ic")
}

/// Decode `h[idx]` as an uncompressed G1 affine (`0 <= idx < n_h`).
pub fn srs_decode_h_g1(data: &[u8], idx: usize) -> Result<G1Affine> {
    srs_decode_g1_at_array(data, srs_offset_after_ic(data)?, idx, "h")
}

/// Decode `l[idx]` (`0 <= idx < n_l`).
pub fn srs_decode_l_g1(data: &[u8], idx: usize) -> Result<G1Affine> {
    srs_decode_g1_at_array(data, srs_offset_of_n_l(data)?, idx, "l")
}

/// Decode `a[idx]` (`0 <= idx < n_a`).
pub fn srs_decode_a_g1(data: &[u8], idx: usize) -> Result<G1Affine> {
    srs_decode_g1_at_array(data, srs_offset_of_n_a(data)?, idx, "a")
}

/// Decode `b_g1[idx]` (`0 <= idx < n_b_g1`).
pub fn srs_decode_bg1_g1(data: &[u8], idx: usize) -> Result<G1Affine> {
    srs_decode_g1_at_array(data, srs_offset_of_n_bg1(data)?, idx, "b_g1")
}

/// Decode `b_g2[idx]` (`0 <= idx < n_b_g2`).
pub fn srs_decode_bg2_g2(data: &[u8], idx: usize) -> Result<G2Affine> {
    srs_decode_g2_at_array(data, srs_offset_of_n_bg2(data)?, idx, "b_g2")
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
    o = srs_skip_counted_g2_array(data, o, "b_g2")?;

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

/// Synthetic SRS bytes for **Milestone B** partition smoke: valid VK prefix (same RNG as
/// `tests/srs_layout.rs`), `n_ic = 1` + one IC, `n_h = 8` distinct G1 `h[]` points, then **one** affine
/// each for `l`, `a`, `b_g1` (deterministic scalars on G1 generator) and **one** `b_g2` affine
/// (`G2` × scalar 201).
pub fn srs_synthetic_partition_smoke_blob() -> Vec<u8> {
    let mut rng = ChaCha20Rng::seed_from_u64(0x5352535f564b5f31);
    let vk = SrsVkPrefix {
        alpha_g1: G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)),
        beta_g1: G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)),
        beta_g2: G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)),
        gamma_g2: G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)),
        delta_g1: G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)),
        delta_g2: G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)),
    };
    let ic0 = G1Affine::from(G1Projective::generator() * Scalar::from(42u64));
    let mut buf = Vec::new();
    buf.extend_from_slice(&vk.alpha_g1.to_uncompressed());
    buf.extend_from_slice(&vk.beta_g1.to_uncompressed());
    buf.extend_from_slice(&vk.beta_g2.to_uncompressed());
    buf.extend_from_slice(&vk.gamma_g2.to_uncompressed());
    buf.extend_from_slice(&vk.delta_g1.to_uncompressed());
    buf.extend_from_slice(&vk.delta_g2.to_uncompressed());
    buf.extend_from_slice(&1u32.to_be_bytes());
    buf.extend_from_slice(&ic0.to_uncompressed());
    buf.extend_from_slice(&8u32.to_be_bytes());
    for i in 0..8 {
        let p = G1Affine::from(G1Projective::generator() * Scalar::from((i as u64) + 1));
        buf.extend_from_slice(&p.to_uncompressed());
    }
    for s in [101u64, 102u64, 103u64] {
        buf.extend_from_slice(&1u32.to_be_bytes());
        let p = G1Affine::from(G1Projective::generator() * Scalar::from(s));
        buf.extend_from_slice(&p.to_uncompressed());
    }
    buf.extend_from_slice(&1u32.to_be_bytes());
    let bg2_0 = G2Affine::from(G2Projective::generator() * Scalar::from(201u64));
    buf.extend_from_slice(&bg2_0.to_uncompressed());
    buf
}
