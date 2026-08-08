//! Host-side **Jacobian / XYZZ** limb layouts for G1/G2 (Montgomery base-field / Fp2 limbs for GPU shaders).
//!
//! Conventions match sppark `xyzz_t` (`X`, `Y`, `ZZZ`, `ZZ` = \(Z^3\), \(Z^2\)) and Jacobian \((X,Y,Z)\) with
//! short Weierstrass \(a=0\). See `shaders/PORTING_NOTES.md`.

use blstrs::{G1Affine, G1Projective, G2Affine, G2Projective};
use ff::Field;

use crate::fp::{fp_from_montgomery_u32_limbs, fp_montgomery_u32_limbs};
use crate::fp2::{fp2_from_montgomery_u32_limbs, fp2_montgomery_u32_limbs};
use crate::g1::BLS12_381_FP_U32_LIMBS;

/// G1 Jacobian point: Montgomery limbs for `X, Y, Z` (matches `blst_p1` / `G1Projective`).
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct G1JacobianLimbs {
    pub x: [u32; BLS12_381_FP_U32_LIMBS],
    pub y: [u32; BLS12_381_FP_U32_LIMBS],
    pub z: [u32; BLS12_381_FP_U32_LIMBS],
}

impl G1JacobianLimbs {
    /// Point-at-infinity sentinel (`Z = 0`), used by the host-side dispatcher
    /// in [`crate::g1_ec_gpu::run_g1_jacobian_add_gpu`] to short-circuit the
    /// identity input case without touching the GPU.
    pub fn zero() -> Self {
        Self {
            x: [0u32; BLS12_381_FP_U32_LIMBS],
            y: [0u32; BLS12_381_FP_U32_LIMBS],
            z: [0u32; BLS12_381_FP_U32_LIMBS],
        }
    }
}

/// G1 XYZZ bucket state: `X`, `Y`, `ZZZ` (\(Z^3\)), `ZZ` (\(Z^2\)) — sppark member order.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct G1XyzzLimbs {
    pub x: [u32; BLS12_381_FP_U32_LIMBS],
    pub y: [u32; BLS12_381_FP_U32_LIMBS],
    pub zzz: [u32; BLS12_381_FP_U32_LIMBS],
    pub zz: [u32; BLS12_381_FP_U32_LIMBS],
}

/// One Jacobian point (`X||Y||Z`) in Montgomery limbs.
pub const G1_JACOBIAN_POINT_BYTES: usize = 3 * BLS12_381_FP_U32_LIMBS * 4;
/// SSBO for [`crate::g1_ec_gpu::run_g1_jacobian_add_gpu`]: `out || a || b` (108 × `u32`).
pub const G1_JACOBIAN_ADD_SSBO_BYTES: usize = 3 * 3 * BLS12_381_FP_U32_LIMBS * 4;

/// `X||Y||ZZZ||ZZ` for one XYZZ state (Montgomery).
pub const G1_XYZZ_POINT_BYTES: usize = 4 * BLS12_381_FP_U32_LIMBS * 4;
/// SSBO for mixed add: XYZZ (48 words) + affine `x||y` (24 words) = 72 × `u32`.
pub const G1_XYZZ_ADD_MIXED_SSBO_BYTES: usize = 72 * 4;

/// §8.1 Pippenger bucket accumulate — one thread per bucket, iterates packed affine points.
/// See `g1_pippenger_bucket_acc_tail.comp`. Host [`VkSpecializationInfo`] passes this as `local_size_x`
/// (id **0**) when **`cfg(g1_pippenger_bucket_acc_spec)`**; keep in sync with `local_size_x = 256` default in
/// `g1_pippenger_bucket_acc_spec_tail.comp`.
pub const G1_PIPP_LOCAL_X: u32 = 256;
/// Maximum packed affine points + digits in one SSBO / dispatch (`batch` circuits concatenated).
pub const G1_PIPP_MAX_N: usize = 16384;
/// Maximum bucket count (= 1 << 16 for w=16).
pub const G1_PIPP_MAX_BUCKET_COUNT: usize = 65536;
/// Max unsigned MSM **`window_bits`** for [`crate::g1_pippenger_bucket_gpu`] (`bucket_count = 2^w` ≤ [`G1_PIPP_MAX_BUCKET_COUNT`]).
pub const G1_PIPP_MAX_WINDOW_BITS: u32 = 16;
const _: () = assert!(1usize << G1_PIPP_MAX_WINDOW_BITS as usize == G1_PIPP_MAX_BUCKET_COUNT);
/// Words **`d[0..64)`** — dispatch metadata (bucket count, batch size, total points, reserved).
pub const G1_PIPP_HEADER: usize = 64;
/// Reserved / pad (write **0**).
pub const G1_PIPP_OFF_RESERVED: usize = 0;
pub const G1_PIPP_OFF_BUCKET_COUNT: usize = 1;
/// Circuits in this dispatch (`≥ 1`).
pub const G1_PIPP_OFF_BATCH: usize = 2;
/// Total packed points **`N_tot`** (same as last entry of `circuit_start`).
pub const G1_PIPP_OFF_POINT_TOTAL: usize = 3;
/// **0** = dense affine rows (`slot pi` is row `pi`). **1** = indexed pool (**§8.4 D.2**): row `pi` uses `base_idx[pi]` into [`G1_PIPP_OFF_AFF_POOL`].
pub const G1_PIPP_OFF_AFFINE_MODE: usize = 4;
/// Rows populated in [`G1_PIPP_OFF_AFF_POOL`] (`≤ N_tot`; equals `N_tot` in dense mode).
pub const G1_PIPP_OFF_UNIQUE_AFF_COUNT: usize = 5;
/// **`circuit_start[c]`** = digit/affine base index for circuit **`c`**; **`circuit_start[batch]=N_tot`**.
/// Uses **`G1_PIPP_MAX_BATCH_CIRCUITS + 1`** slots (`u32`), zero-padded.
pub const G1_PIPP_OFF_CIRCUIT_START: usize = G1_PIPP_HEADER;
/// Max circuits per **single** GPU dispatch (shader constant — change requires recompiling SPIR-V).
pub const G1_PIPP_MAX_BATCH_CIRCUITS: usize = 128;
pub const G1_PIPP_CIRCUIT_PREFIX_SLOTS: usize = G1_PIPP_MAX_BATCH_CIRCUITS + 1;
pub const G1_PIPP_OFF_DIG: usize = G1_PIPP_OFF_CIRCUIT_START + G1_PIPP_CIRCUIT_PREFIX_SLOTS;
/// Parallel **`base_idx[pi]`** table (**[`G1_PIPP_OFF_AFFINE_MODE`] = 1**); unused words remain **0** in dense mode.
pub const G1_PIPP_OFF_BASE_IDX: usize = G1_PIPP_OFF_DIG + G1_PIPP_MAX_N;
/// Montgomery **`x‖y`** pool (**24 × `u32`** per row); indexed by `pi` (dense) or **`base_idx[pi]`** (shared).
pub const G1_PIPP_OFF_AFF_POOL: usize = G1_PIPP_OFF_BASE_IDX + G1_PIPP_MAX_N;
pub const G1_PIPP_OFF_BUCKET_JAC: usize =
    G1_PIPP_OFF_AFF_POOL + G1_PIPP_MAX_N * 2 * BLS12_381_FP_U32_LIMBS;
pub const G1_PIPP_SSBO_U32: usize =
    G1_PIPP_OFF_BUCKET_JAC + G1_PIPP_MAX_BUCKET_COUNT * 3 * BLS12_381_FP_U32_LIMBS;
pub const G1_PIPP_SSBO_BYTES: usize = G1_PIPP_SSBO_U32 * 4;

/// Deprecated alias for [`G1_PIPP_OFF_AFF_POOL`] (pre–indexed-layout naming).
pub const G1_PIPP_OFF_AFF: usize = G1_PIPP_OFF_AFF_POOL;

/// Deprecated alias (was uniform **n** per circuit). Layout uses [`G1_PIPP_OFF_POINT_TOTAL`] + prefix.
pub const G1_PIPP_OFF_N: usize = G1_PIPP_OFF_RESERVED;

/// Phase G: bitmap-selected sum of up to 64 affine G1 points (Jacobian out). See `g1_batch_accum_bitmap1636_tail.comp`.
pub const G1_BATCH_ACC_MAX_POINTS: usize = 64;
pub const G1_BATCH_ACC_HEADER_WORDS: usize = 64;
pub const G1_BATCH_ACC_JAC_WORDS: usize = 3 * BLS12_381_FP_U32_LIMBS;
/// `d[0]` = `n`, `d[1]`/`d[2]` = bitmap lo/hi (bit *i* selects affine *i*), words `64..100` = out Jacobian, `100..` = affines × 24 words.
pub const G1_BATCH_ACC_SSBO_U32: usize = G1_BATCH_ACC_HEADER_WORDS
    + G1_BATCH_ACC_JAC_WORDS
    + G1_BATCH_ACC_MAX_POINTS * 2 * BLS12_381_FP_U32_LIMBS;
pub const G1_BATCH_ACC_SSBO_BYTES: usize = G1_BATCH_ACC_SSBO_U32 * 4;

/// Single-window **bucket sums** (Jacobian per bucket) for Pippenger-style MSM; see `g1_bucket_sum_window4_n32_tail.comp`.
pub const G1_BUCKET_MSM_WINDOW_BITS: u32 = 4;
pub const G1_BUCKET_MSM_BUCKETS: usize = 1 << G1_BUCKET_MSM_WINDOW_BITS;
pub const G1_BUCKET_MSM_MAX_POINTS: usize = 32;
pub const G1_BUCKET_MSM_HEADER_WORDS: usize = 64;
pub const G1_BUCKET_MSM_JAC_PER_BUCKET_WORDS: usize = G1_BATCH_ACC_JAC_WORDS;
pub const G1_BUCKET_MSM_OFF_N: usize = 0;
pub const G1_BUCKET_MSM_OFF_BUCKET_JAC: usize = G1_BUCKET_MSM_HEADER_WORDS;
pub const G1_BUCKET_MSM_OFF_DIGITS: usize =
    G1_BUCKET_MSM_OFF_BUCKET_JAC + G1_BUCKET_MSM_BUCKETS * G1_BUCKET_MSM_JAC_PER_BUCKET_WORDS;
pub const G1_BUCKET_MSM_OFF_AFF: usize = G1_BUCKET_MSM_OFF_DIGITS + G1_BUCKET_MSM_MAX_POINTS;
pub const G1_BUCKET_MSM_SSBO_U32: usize =
    G1_BUCKET_MSM_OFF_AFF + G1_BUCKET_MSM_MAX_POINTS * 2 * BLS12_381_FP_U32_LIMBS;
pub const G1_BUCKET_MSM_SSBO_BYTES: usize = G1_BUCKET_MSM_SSBO_U32 * 4;

/// G2 Jacobian: Montgomery `Fp2` limbs for each of `X, Y, Z` (`c0||c1` per [`crate::fp2`]).
pub const G2_JACOBIAN_U32: usize = 3 * crate::fp2::BLS12_381_FP2_U32_LIMBS;

#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct G2JacobianLimbs {
    pub x: [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
    pub y: [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
    pub z: [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
}

#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct G2XyzzLimbs {
    pub x: [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
    pub y: [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
    pub zzz: [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
    pub zz: [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
}

pub const G2_JACOBIAN_POINT_BYTES: usize = G2_JACOBIAN_U32 * 4;
pub const G2_JACOBIAN_ADD_SSBO_BYTES: usize = 3 * G2_JACOBIAN_POINT_BYTES;
pub const G2_XYZZ_POINT_BYTES: usize = 4 * crate::fp2::BLS12_381_FP2_U32_LIMBS * 4;
pub const G2_XYZZ_ADD_MIXED_SSBO_BYTES: usize = (4 * 24 + 2 * 24) * 4;

/// Phase G: bitmap-selected sum of up to 16 affine G2 points (Jacobian out). See `g2_batch_accum_aff16_904_tail.comp`.
pub const G2_BATCH_ACC_MAX_POINTS: usize = 16;
pub const G2_BATCH_ACC_HEADER_WORDS: usize = 64;
pub const G2_BATCH_ACC_JAC_WORDS: usize = G2_JACOBIAN_U32;
pub const G2_BATCH_ACC_SSBO_U32: usize = G2_BATCH_ACC_HEADER_WORDS
    + G2_BATCH_ACC_JAC_WORDS
    + G2_BATCH_ACC_MAX_POINTS * 2 * crate::fp2::BLS12_381_FP2_U32_LIMBS;
pub const G2_BATCH_ACC_SSBO_BYTES: usize = G2_BATCH_ACC_SSBO_U32 * 4;

#[inline]
pub fn g1_jacobian_limbs_from_projective(p: &G1Projective) -> G1JacobianLimbs {
    G1JacobianLimbs {
        x: fp_montgomery_u32_limbs(&p.x()),
        y: fp_montgomery_u32_limbs(&p.y()),
        z: fp_montgomery_u32_limbs(&p.z()),
    }
}

#[inline]
pub fn g1_projective_from_jacobian_limbs(j: &G1JacobianLimbs) -> G1Projective {
    G1Projective::from_raw_unchecked(
        fp_from_montgomery_u32_limbs(&j.x),
        fp_from_montgomery_u32_limbs(&j.y),
        fp_from_montgomery_u32_limbs(&j.z),
    )
}

#[inline]
pub fn g1_xyzz_limbs_from_projective(p: &G1Projective) -> G1XyzzLimbs {
    let x = p.x();
    let y = p.y();
    let z = p.z();
    let zz = z * z;
    let zzz = zz * z;
    G1XyzzLimbs {
        x: fp_montgomery_u32_limbs(&x),
        y: fp_montgomery_u32_limbs(&y),
        zzz: fp_montgomery_u32_limbs(&zzz),
        zz: fp_montgomery_u32_limbs(&zz),
    }
}

/// Decode sppark XYZZ → affine (CPU oracle). Returns `None` if `ZZZ` has no inverse.
pub fn g1_affine_from_xyzz_limbs(w: &G1XyzzLimbs) -> Option<G1Affine> {
    let x_j = fp_from_montgomery_u32_limbs(&w.x);
    let y_j = fp_from_montgomery_u32_limbs(&w.y);
    let zz = fp_from_montgomery_u32_limbs(&w.zz);
    let zzz = fp_from_montgomery_u32_limbs(&w.zzz);
    if bool::from(zz.is_zero()) && bool::from(zzz.is_zero()) {
        return Some(G1Affine::default());
    }
    let ya = zzz.invert().into_option()?;
    let xa = ya * zz;
    let x_aff = x_j * xa.square();
    let y_aff = y_j * ya;
    Some(G1Affine::from_raw_unchecked(x_aff, y_aff, false))
}

#[inline]
pub fn g1_affine_montgomery_limbs(
    a: &G1Affine,
) -> ([u32; BLS12_381_FP_U32_LIMBS], [u32; BLS12_381_FP_U32_LIMBS]) {
    (
        fp_montgomery_u32_limbs(&a.x()),
        fp_montgomery_u32_limbs(&a.y()),
    )
}

#[inline]
pub fn g2_jacobian_limbs_from_projective(p: &G2Projective) -> G2JacobianLimbs {
    G2JacobianLimbs {
        x: fp2_montgomery_u32_limbs(&p.x()),
        y: fp2_montgomery_u32_limbs(&p.y()),
        z: fp2_montgomery_u32_limbs(&p.z()),
    }
}

#[inline]
pub fn g2_projective_from_jacobian_limbs(j: &G2JacobianLimbs) -> G2Projective {
    G2Projective::from_raw_unchecked(
        fp2_from_montgomery_u32_limbs(&j.x),
        fp2_from_montgomery_u32_limbs(&j.y),
        fp2_from_montgomery_u32_limbs(&j.z),
    )
}

#[inline]
pub fn g2_xyzz_limbs_from_projective(p: &G2Projective) -> G2XyzzLimbs {
    let x = p.x();
    let y = p.y();
    let z = p.z();
    let zz = z * z;
    let zzz = zz * z;
    G2XyzzLimbs {
        x: fp2_montgomery_u32_limbs(&x),
        y: fp2_montgomery_u32_limbs(&y),
        zzz: fp2_montgomery_u32_limbs(&zzz),
        zz: fp2_montgomery_u32_limbs(&zz),
    }
}

pub fn g2_affine_from_xyzz_limbs(w: &G2XyzzLimbs) -> Option<G2Affine> {
    let x_j = fp2_from_montgomery_u32_limbs(&w.x);
    let y_j = fp2_from_montgomery_u32_limbs(&w.y);
    let zz = fp2_from_montgomery_u32_limbs(&w.zz);
    let zzz = fp2_from_montgomery_u32_limbs(&w.zzz);
    if bool::from(zz.is_zero()) && bool::from(zzz.is_zero()) {
        return Some(G2Affine::default());
    }
    let ya = zzz.invert().into_option()?;
    let xa = ya * zz;
    let x_aff = x_j * xa.square();
    let y_aff = y_j * ya;
    Some(G2Affine::from_raw_unchecked(x_aff, y_aff, false))
}

#[inline]
pub fn g2_affine_montgomery_limbs(
    a: &G2Affine,
) -> (
    [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
    [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
) {
    (
        fp2_montgomery_u32_limbs(&a.x()),
        fp2_montgomery_u32_limbs(&a.y()),
    )
}
