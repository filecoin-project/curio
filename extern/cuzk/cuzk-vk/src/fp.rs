//! BLS12-381 **Fp** limb packing for SSBOs (mirrors [`crate::scalar_limbs`] for Fr).

use blst::blst_fp;
use blstrs::Fp;

use crate::g1::BLS12_381_FP_U32_LIMBS;

/// Pack `Fp` into twelve little-endian `u32` words (48 bytes), canonical integer encoding (`to_bytes_le`).
#[inline]
pub fn fp_to_le_u32_limbs(x: &Fp) -> [u32; BLS12_381_FP_U32_LIMBS] {
    let b = x.to_bytes_le();
    let mut out = [0u32; BLS12_381_FP_U32_LIMBS];
    for (i, chunk) in b.chunks_exact(4).enumerate() {
        out[i] = u32::from_le_bytes(chunk.try_into().expect("4 bytes"));
    }
    out
}

/// Inverse of [`fp_to_le_u32_limbs`].
#[inline]
pub fn fp_try_from_le_u32_limbs(limbs: &[u32; BLS12_381_FP_U32_LIMBS]) -> Option<Fp> {
    let mut b = [0u8; 48];
    for (i, &w) in limbs.iter().enumerate() {
        b[i * 4..i * 4 + 4].copy_from_slice(&w.to_le_bytes());
    }
    Fp::from_bytes_le(&b).into_option()
}

/// Montgomery `blst_fp` limbs as twelve little-endian `u32` words (pairs of each `fp.l[i]`).
#[inline]
pub fn fp_montgomery_u32_limbs(x: &Fp) -> [u32; BLS12_381_FP_U32_LIMBS] {
    let raw: blst_fp = (*x).into();
    let mut out = [0u32; BLS12_381_FP_U32_LIMBS];
    for i in 0..6 {
        out[2 * i] = raw.l[i] as u32;
        out[2 * i + 1] = (raw.l[i] >> 32) as u32;
    }
    out
}

/// Decode [`fp_montgomery_u32_limbs`] back into `Fp`.
#[inline]
pub fn fp_from_montgomery_u32_limbs(limbs: &[u32; BLS12_381_FP_U32_LIMBS]) -> Fp {
    let mut raw = blst_fp::default();
    for i in 0..6 {
        raw.l[i] = limbs[2 * i] as u64 | ((limbs[2 * i + 1] as u64) << 32);
    }
    Fp::from(raw)
}

#[cfg(test)]
mod tests {
    use super::*;
    use ff::Field;
    use rand::SeedableRng;
    use rand_chacha::ChaCha8Rng;

    #[test]
    fn fp_limbs_roundtrip_random() {
        let mut rng = ChaCha8Rng::from_seed([0xF1u8; 32]);
        for _ in 0..64 {
            let x = Fp::random(&mut rng);
            let limbs = fp_to_le_u32_limbs(&x);
            let back = fp_try_from_le_u32_limbs(&limbs).expect("roundtrip");
            assert_eq!(back, x);
        }
    }

    #[test]
    fn fp_montgomery_limbs_roundtrip_random() {
        let mut rng = ChaCha8Rng::from_seed([0xF2u8; 32]);
        for _ in 0..64 {
            let x = Fp::random(&mut rng);
            let m = fp_montgomery_u32_limbs(&x);
            let back = fp_from_montgomery_u32_limbs(&m);
            assert_eq!(back, x);
        }
    }
}
