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

/// Phase G: bitmap-selected sum of up to 64 affine G1 points (Jacobian out). See `g1_batch_accum_bitmap1636_tail.comp`.
pub const G1_BATCH_ACC_MAX_POINTS: usize = 64;
pub const G1_BATCH_ACC_HEADER_WORDS: usize = 64;
pub const G1_BATCH_ACC_JAC_WORDS: usize = 3 * BLS12_381_FP_U32_LIMBS;
/// `d[0]` = `n`, `d[1]`/`d[2]` = bitmap lo/hi (bit *i* selects affine *i*), words `64..100` = out Jacobian, `100..` = affines × 24 words.
pub const G1_BATCH_ACC_SSBO_U32: usize =
    G1_BATCH_ACC_HEADER_WORDS + G1_BATCH_ACC_JAC_WORDS + G1_BATCH_ACC_MAX_POINTS * 2 * BLS12_381_FP_U32_LIMBS;
pub const G1_BATCH_ACC_SSBO_BYTES: usize = G1_BATCH_ACC_SSBO_U32 * 4;

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
pub const G2_BATCH_ACC_SSBO_U32: usize =
    G2_BATCH_ACC_HEADER_WORDS + G2_BATCH_ACC_JAC_WORDS + G2_BATCH_ACC_MAX_POINTS * 2 * crate::fp2::BLS12_381_FP2_U32_LIMBS;
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
pub fn g1_affine_montgomery_limbs(a: &G1Affine) -> ([u32; BLS12_381_FP_U32_LIMBS], [u32; BLS12_381_FP_U32_LIMBS]) {
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
pub fn g2_affine_montgomery_limbs(a: &G2Affine) -> (
    [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
    [u32; crate::fp2::BLS12_381_FP2_U32_LIMBS],
) {
    (
        fp2_montgomery_u32_limbs(&a.x()),
        fp2_montgomery_u32_limbs(&a.y()),
    )
}
