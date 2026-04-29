//! Map `blstrs::Scalar` (Fr) to fixed little-endian `u32` limbs for SSBO uploads.
//!
//! - **Canonical** packing matches `Scalar::to_bytes_le` / `from_bytes_le` (Fr field elements as integers).
//! - **Montgomery** packing matches `blst_fr::l` little-endian `u64` limbs split into `u32` pairs (same as
//!   OpenCL / `ec-gpu` runtime values); used by CIOS multiply in `fr_gpu`.

use blst::blst_fr;
use blstrs::Scalar;

use crate::g1::BLS12_381_FR_U32_LIMBS;

/// Pack a scalar into eight little-endian `u32` words (32 bytes total).
#[inline]
pub fn scalar_to_le_u32_limbs(s: &Scalar) -> [u32; BLS12_381_FR_U32_LIMBS] {
    let b = s.to_bytes_le();
    let mut out = [0u32; BLS12_381_FR_U32_LIMBS];
    for (i, chunk) in b.chunks_exact(4).enumerate() {
        out[i] = u32::from_le_bytes(chunk.try_into().expect("4 bytes"));
    }
    out
}

/// Inverse of [`scalar_to_le_u32_limbs`]. Returns `None` if the encoding is not a valid scalar.
#[inline]
pub fn scalar_try_from_le_u32_limbs(limbs: &[u32; BLS12_381_FR_U32_LIMBS]) -> Option<Scalar> {
    let mut b = [0u8; 32];
    for (i, &w) in limbs.iter().enumerate() {
        b[i * 4..i * 4 + 4].copy_from_slice(&w.to_le_bytes());
    }
    Scalar::from_bytes_le(&b).into_option()
}

/// Montgomery `blst_fr` limbs as eight little-endian `u32` words (pairs of each `u64` in `fr.l`).
#[inline]
pub fn scalar_montgomery_u32_limbs(s: &Scalar) -> [u32; BLS12_381_FR_U32_LIMBS] {
    let fr: blst_fr = (*s).into();
    let mut out = [0u32; BLS12_381_FR_U32_LIMBS];
    for i in 0..4 {
        out[2 * i] = fr.l[i] as u32;
        out[2 * i + 1] = (fr.l[i] >> 32) as u32;
    }
    out
}

/// Decode [`scalar_montgomery_u32_limbs`] back into a `Scalar`.
#[inline]
pub fn scalar_from_montgomery_u32_limbs(limbs: &[u32; BLS12_381_FR_U32_LIMBS]) -> Scalar {
    let mut fr = blst_fr::default();
    for i in 0..4 {
        fr.l[i] = limbs[2 * i] as u64 | ((limbs[2 * i + 1] as u64) << 32);
    }
    Scalar::from(fr)
}

#[cfg(test)]
mod tests {
    use super::*;
    use ff::Field;
    use rand::SeedableRng;
    use rand_chacha::ChaCha8Rng;

    #[test]
    fn scalar_limbs_roundtrip_random() {
        let mut rng = ChaCha8Rng::from_seed([11u8; 32]);
        for _ in 0..64 {
            let s = Scalar::random(&mut rng);
            let limbs = scalar_to_le_u32_limbs(&s);
            let back = scalar_try_from_le_u32_limbs(&limbs).expect("roundtrip");
            assert_eq!(back, s);
        }
    }

    #[test]
    fn montgomery_limbs_roundtrip_random() {
        let mut rng = ChaCha8Rng::from_seed([22u8; 32]);
        for _ in 0..64 {
            let s = Scalar::random(&mut rng);
            let m = scalar_montgomery_u32_limbs(&s);
            let back = scalar_from_montgomery_u32_limbs(&m);
            assert_eq!(back, s);
        }
    }
}
