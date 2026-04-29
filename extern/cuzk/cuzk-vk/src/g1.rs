//! G1 / G2 affine data layout for upcoming MSM and pairing shaders.
//!
//! Curve arithmetic in GLSL is not wired yet; callers should continue using `blstrs` on CPU.

use blstrs::{G1Affine, G2Affine};

/// Base-field limb count for BLS12-381 Fp in 32-bit little-endian Montgomery form (`ec-gpu` layout).
pub const BLS12_381_FP_U32_LIMBS: usize = 12;

/// Scalar field limb count for BLS12-381 Fr in 32-bit little-endian form.
pub const BLS12_381_FR_U32_LIMBS: usize = 8;

/// Affine G1 point as two Fp limb arrays (x then y), host-endian; GPU packing may add padding later.
#[repr(C)]
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct G1AffineLimbs {
    pub x: [u32; BLS12_381_FP_U32_LIMBS],
    pub y: [u32; BLS12_381_FP_U32_LIMBS],
}

/// G2 coordinates live in `Fp2` (two base-field elements per coordinate). For SSBO layout we
/// store each `Fp2` as two consecutive Fp limb blocks (`c0` then `c1`), each `BLS12_381_FP_U32_LIMBS` wide.
pub const BLS12_381_FP2_U32_LIMBS: usize = 2 * BLS12_381_FP_U32_LIMBS;

/// Affine G2 point (x, y) in `Fp2`, flattened to `u32` limbs for future GLSL I/O.
#[repr(C)]
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct G2AffineLimbs {
    pub x: [u32; BLS12_381_FP2_U32_LIMBS],
    pub y: [u32; BLS12_381_FP2_U32_LIMBS],
}

/// Packed `x || y` little-endian limb bytes (`12 + 12` `u32` words).
pub const G1_AFFINE_LIMB_BYTES: usize = 2 * BLS12_381_FP_U32_LIMBS * 4;

/// Packed G2 affine `x || y` (`24 + 24` `u32` words).
pub const G2_AFFINE_LIMB_BYTES: usize = 2 * BLS12_381_FP2_U32_LIMBS * 4;

#[inline]
pub fn g1_limbs_to_le_bytes(g: &G1AffineLimbs) -> [u8; G1_AFFINE_LIMB_BYTES] {
    let mut b = [0u8; G1_AFFINE_LIMB_BYTES];
    let mut o = 0usize;
    for &u in g.x.iter().chain(g.y.iter()) {
        b[o..o + 4].copy_from_slice(&u.to_le_bytes());
        o += 4;
    }
    b
}

/// Montgomery `Fp` limbs for a `blstrs::G1Affine` (GPU `g1_reverse24` / future SRS SSBO layout).
#[inline]
pub fn g1_affine_limbs_from_blstrs(a: &G1Affine) -> G1AffineLimbs {
    let (x, y) = crate::ec::g1_affine_montgomery_limbs(a);
    G1AffineLimbs { x, y }
}

/// Montgomery `Fp2` limbs for a `blstrs::G2Affine` (GPU `g2_reverse48` / SRS G2 SSBO layout).
#[inline]
pub fn g2_affine_limbs_from_blstrs(a: &G2Affine) -> G2AffineLimbs {
    let (x, y) = crate::ec::g2_affine_montgomery_limbs(a);
    G2AffineLimbs { x, y }
}

pub fn g1_limbs_from_le_bytes(bytes: &[u8; G1_AFFINE_LIMB_BYTES]) -> G1AffineLimbs {
    let mut x = [0u32; BLS12_381_FP_U32_LIMBS];
    let mut y = [0u32; BLS12_381_FP_U32_LIMBS];
    for i in 0..BLS12_381_FP_U32_LIMBS {
        x[i] = u32::from_le_bytes(bytes[i * 4..i * 4 + 4].try_into().unwrap());
    }
    let o = BLS12_381_FP_U32_LIMBS * 4;
    for i in 0..BLS12_381_FP_U32_LIMBS {
        y[i] = u32::from_le_bytes(bytes[o + i * 4..o + i * 4 + 4].try_into().unwrap());
    }
    G1AffineLimbs { x, y }
}

/// Reverse the `x || y` `u32` word order (same permutation as `shaders/g1_reverse24.comp`).
#[inline]
pub fn g1_limbs_reverse_words_inplace(g: &mut G1AffineLimbs) {
    let mut flat = [0u32; 2 * BLS12_381_FP_U32_LIMBS];
    flat[..BLS12_381_FP_U32_LIMBS].copy_from_slice(&g.x);
    flat[BLS12_381_FP_U32_LIMBS..].copy_from_slice(&g.y);
    flat.reverse();
    g.x.copy_from_slice(&flat[..BLS12_381_FP_U32_LIMBS]);
    g.y.copy_from_slice(&flat[BLS12_381_FP_U32_LIMBS..]);
}

#[inline]
pub fn g2_limbs_to_le_bytes(g: &G2AffineLimbs) -> [u8; G2_AFFINE_LIMB_BYTES] {
    let mut b = [0u8; G2_AFFINE_LIMB_BYTES];
    let mut o = 0usize;
    for &u in g.x.iter().chain(g.y.iter()) {
        b[o..o + 4].copy_from_slice(&u.to_le_bytes());
        o += 4;
    }
    b
}

#[inline]
pub fn g2_limbs_from_le_bytes(bytes: &[u8; G2_AFFINE_LIMB_BYTES]) -> G2AffineLimbs {
    let mut x = [0u32; BLS12_381_FP2_U32_LIMBS];
    let mut y = [0u32; BLS12_381_FP2_U32_LIMBS];
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        x[i] = u32::from_le_bytes(bytes[i * 4..i * 4 + 4].try_into().unwrap());
    }
    let o = BLS12_381_FP2_U32_LIMBS * 4;
    for i in 0..BLS12_381_FP2_U32_LIMBS {
        y[i] = u32::from_le_bytes(bytes[o + i * 4..o + i * 4 + 4].try_into().unwrap());
    }
    G2AffineLimbs { x, y }
}

/// Reverse flattened `x || y` word order (matches `shaders/g2_reverse48.comp`).
#[inline]
pub fn g2_limbs_reverse_words_inplace(g: &mut G2AffineLimbs) {
    let mut flat = [0u32; 2 * BLS12_381_FP2_U32_LIMBS];
    flat[..BLS12_381_FP2_U32_LIMBS].copy_from_slice(&g.x);
    flat[BLS12_381_FP2_U32_LIMBS..].copy_from_slice(&g.y);
    flat.reverse();
    g.x.copy_from_slice(&flat[..BLS12_381_FP2_U32_LIMBS]);
    g.y.copy_from_slice(&flat[BLS12_381_FP2_U32_LIMBS..]);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn g1_limbs_bytes_roundtrip() {
        let g = G1AffineLimbs {
            x: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12],
            y: [100; 12],
        };
        let b = g1_limbs_to_le_bytes(&g);
        assert_eq!(g1_limbs_from_le_bytes(&b), g);
    }

    #[test]
    fn g1_reverse_twice_is_identity() {
        let mut g = G1AffineLimbs {
            x: [7; 12],
            y: [42; 12],
        };
        let orig = g;
        g1_limbs_reverse_words_inplace(&mut g);
        assert_ne!(g, orig);
        g1_limbs_reverse_words_inplace(&mut g);
        assert_eq!(g, orig);
    }

    #[test]
    fn g2_limbs_bytes_roundtrip() {
        let mut x = [0u32; BLS12_381_FP2_U32_LIMBS];
        let mut y = [0u32; BLS12_381_FP2_U32_LIMBS];
        for i in 0..BLS12_381_FP2_U32_LIMBS {
            x[i] = i as u32;
            y[i] = (1000 + i) as u32;
        }
        let g = G2AffineLimbs { x, y };
        let b = g2_limbs_to_le_bytes(&g);
        assert_eq!(g2_limbs_from_le_bytes(&b), g);
    }

    #[test]
    fn g2_reverse_twice_is_identity() {
        let mut g = G2AffineLimbs {
            x: [3; BLS12_381_FP2_U32_LIMBS],
            y: [9; BLS12_381_FP2_U32_LIMBS],
        };
        let orig = g;
        g2_limbs_reverse_words_inplace(&mut g);
        assert_ne!(g, orig);
        g2_limbs_reverse_words_inplace(&mut g);
        assert_eq!(g, orig);
    }
}
