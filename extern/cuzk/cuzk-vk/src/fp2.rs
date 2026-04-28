//! BLS12-381 **Fp2** limb packing for SSBOs: `c0` limbs then `c1` limbs (24 × `u32` LE).

use blstrs::Fp2;

use crate::fp::{
    fp_from_montgomery_u32_limbs, fp_montgomery_u32_limbs, fp_to_le_u32_limbs, fp_try_from_le_u32_limbs,
};
use crate::g1::BLS12_381_FP_U32_LIMBS;

/// Total little-endian `u32` limbs for one `Fp2` (`c0` then `c1`).
pub const BLS12_381_FP2_U32_LIMBS: usize = 2 * BLS12_381_FP_U32_LIMBS;

/// Pack `Fp2` into 24 little-endian `u32` words (canonical `Fp` encoding per component).
#[inline]
pub fn fp2_to_le_u32_limbs(x: &Fp2) -> [u32; BLS12_381_FP2_U32_LIMBS] {
    let c0 = fp_to_le_u32_limbs(&x.c0());
    let c1 = fp_to_le_u32_limbs(&x.c1());
    let mut out = [0u32; BLS12_381_FP2_U32_LIMBS];
    out[..BLS12_381_FP_U32_LIMBS].copy_from_slice(&c0);
    out[BLS12_381_FP_U32_LIMBS..].copy_from_slice(&c1);
    out
}

/// Inverse of [`fp2_to_le_u32_limbs`].
#[inline]
pub fn fp2_try_from_le_u32_limbs(limbs: &[u32; BLS12_381_FP2_U32_LIMBS]) -> Option<Fp2> {
    let mut c0 = [0u32; BLS12_381_FP_U32_LIMBS];
    let mut c1 = [0u32; BLS12_381_FP_U32_LIMBS];
    c0.copy_from_slice(&limbs[..BLS12_381_FP_U32_LIMBS]);
    c1.copy_from_slice(&limbs[BLS12_381_FP_U32_LIMBS..]);
    Some(Fp2::new(
        fp_try_from_le_u32_limbs(&c0)?,
        fp_try_from_le_u32_limbs(&c1)?,
    ))
}

/// Montgomery limbs for both `Fp2` components (96 bytes as `u32[24]`).
#[inline]
pub fn fp2_montgomery_u32_limbs(x: &Fp2) -> [u32; BLS12_381_FP2_U32_LIMBS] {
    let c0 = fp_montgomery_u32_limbs(&x.c0());
    let c1 = fp_montgomery_u32_limbs(&x.c1());
    let mut out = [0u32; BLS12_381_FP2_U32_LIMBS];
    out[..BLS12_381_FP_U32_LIMBS].copy_from_slice(&c0);
    out[BLS12_381_FP_U32_LIMBS..].copy_from_slice(&c1);
    out
}

/// Decode [`fp2_montgomery_u32_limbs`] back into `Fp2`.
#[inline]
pub fn fp2_from_montgomery_u32_limbs(limbs: &[u32; BLS12_381_FP2_U32_LIMBS]) -> Fp2 {
    let mut c0 = [0u32; BLS12_381_FP_U32_LIMBS];
    let mut c1 = [0u32; BLS12_381_FP_U32_LIMBS];
    c0.copy_from_slice(&limbs[..BLS12_381_FP_U32_LIMBS]);
    c1.copy_from_slice(&limbs[BLS12_381_FP_U32_LIMBS..]);
    Fp2::new(
        fp_from_montgomery_u32_limbs(&c0),
        fp_from_montgomery_u32_limbs(&c1),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use ff::Field;
    use rand::SeedableRng;
    use rand_chacha::ChaCha8Rng;

    #[test]
    fn fp2_limbs_roundtrip_random() {
        let mut rng = ChaCha8Rng::from_seed([0xE1u8; 32]);
        for _ in 0..32 {
            let x = Fp2::random(&mut rng);
            let limbs = fp2_to_le_u32_limbs(&x);
            let back = fp2_try_from_le_u32_limbs(&limbs).expect("roundtrip");
            assert_eq!(back, x);
        }
    }

    #[test]
    fn fp2_montgomery_limbs_roundtrip_random() {
        let mut rng = ChaCha8Rng::from_seed([0xE2u8; 32]);
        for _ in 0..32 {
            let x = Fp2::random(&mut rng);
            let m = fp2_montgomery_u32_limbs(&x);
            let back = fp2_from_montgomery_u32_limbs(&m);
            assert_eq!(back, x);
        }
    }
}
