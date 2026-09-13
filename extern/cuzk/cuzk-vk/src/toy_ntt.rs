//! Toy NTT over the 998244353 field (32-bit limbs). Used to validate the Vulkan compute path
//! before BLS12-381 `Scalar` NTT shaders land.

pub const TOY_MOD: u32 = 998244353;
pub const TOY_ALPHA: u32 = 301989884; // 2^32 mod TOY_MOD

#[inline]
pub fn addm(a: u32, b: u32) -> u32 {
    let s = a.wrapping_add(b);
    if s >= TOY_MOD {
        s - TOY_MOD
    } else {
        s
    }
}

#[inline]
pub fn subm(a: u32, b: u32) -> u32 {
    if a >= b {
        a - b
    } else {
        a + TOY_MOD - b
    }
}

#[inline]
pub fn mulm(a: u32, b: u32) -> u32 {
    ((a as u64 * b as u64) % TOY_MOD as u64) as u32
}

/// Same modular product as [`mulm`], matching `shaders/toy_ntt8.comp` (wide `lo` + `hi*ALPHA`).
#[inline]
pub fn combine_hi_lo_mod(hi: u32, lo: u32) -> u32 {
    let s = (hi as u64) * (TOY_ALPHA as u64) + (lo as u64);
    (s % TOY_MOD as u64) as u32
}

/// Same as [`mulm`], using the same reduction as the GPU shader.
pub fn mulm_glsl_style(a: u32, b: u32) -> u32 {
    let p = (a as u64) * (b as u64);
    let lo = p as u32;
    let hi = (p >> 32) as u32;
    combine_hi_lo_mod(hi, lo)
}

#[inline]
pub fn mul_hi_alpha_mod(hi: u32) -> u32 {
    let mut tpow = TOY_ALPHA;
    let mut acc = 0u32;
    for j in 0..32 {
        if (hi & (1u32 << j)) != 0 {
            acc = addm(acc, tpow);
        }
        tpow = addm(tpow, tpow);
    }
    acc
}

/// Primitive 8th root of unity for `TOY_MOD` (generator `3`).
pub fn root_omega_8() -> u32 {
    const G: u32 = 3;
    powm(G, (TOY_MOD - 1) / 8)
}

#[inline]
pub fn powm(mut a: u32, mut e: u32) -> u32 {
    let mut r = 1u32;
    while e > 0 {
        if e & 1 != 0 {
            r = mulm(r, a);
        }
        a = mulm(a, a);
        e >>= 1;
    }
    r
}

/// Forward NTT, in place, `n == 8`, coefficients already reduced mod `TOY_MOD`.
pub fn ntt_forward_8(a: &mut [u32; 8]) {
    bit_reverse_8(a);
    let omega = root_omega_8();
    let mut m = 1usize;
    while m < 8 {
        m *= 2;
        let wlen = powm(omega, (8 / m) as u32);
        for i in (0..8).step_by(m) {
            let mut w = 1u32;
            for j in 0..m / 2 {
                let u = a[i + j];
                let v = mulm(a[i + j + m / 2], w);
                a[i + j] = addm(u, v);
                a[i + j + m / 2] = subm(u, v);
                w = mulm(w, wlen);
            }
        }
    }
}

fn bit_reverse_8(a: &mut [u32; 8]) {
    a.swap(1, 4);
    a.swap(3, 6);
}

/// Inverse NTT (scale by `1/8`).
pub fn intt_8(a: &mut [u32; 8]) {
    let omega = root_omega_8();
    let omega_inv = powm(omega, TOY_MOD - 2); // Fermat: p prime, a^(p-2) mod p
    ntt_forward_8_with_root(a, omega_inv);
    let inv8 = powm(8, TOY_MOD - 2);
    for x in a.iter_mut() {
        *x = mulm(*x, inv8);
    }
}

fn ntt_forward_8_with_root(a: &mut [u32; 8], root: u32) {
    bit_reverse_8(a);
    let mut m = 1usize;
    while m < 8 {
        m *= 2;
        let wlen = powm(root, (8 / m) as u32);
        for i in (0..8).step_by(m) {
            let mut w = 1u32;
            for j in 0..m / 2 {
                let u = a[i + j];
                let v = mulm(a[i + j + m / 2], w);
                a[i + j] = addm(u, v);
                a[i + j + m / 2] = subm(u, v);
                w = mulm(w, wlen);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use rand::Rng;
    use rand::SeedableRng;

    #[test]
    fn mulm_matches_glsl_style() {
        for a in [0u32, 1, 123456, TOY_MOD - 1] {
            for b in [0u32, 1, 987654, TOY_MOD - 1] {
                assert_eq!(mulm(a, b), mulm_glsl_style(a, b), "a={a} b={b}");
            }
        }
    }

    #[test]
    fn mul_hi_alpha_mod_matches_u64() {
        for hi in [0u32, 1, 28, 0xffff_fffe] {
            let w = ((hi as u64 * TOY_ALPHA as u64) % TOY_MOD as u64) as u32;
            assert_eq!(mul_hi_alpha_mod(hi), w, "hi={hi}");
        }
    }

    #[test]
    fn intt_roundtrip_random() {
        let mut rng = rand_chacha::ChaCha8Rng::from_seed([7u8; 32]);
        for _ in 0..200 {
            let mut v = [0u32; 8];
            for x in &mut v {
                *x = rng.gen::<u32>() % TOY_MOD;
            }
            let orig = v;
            let mut t = v;
            ntt_forward_8(&mut t);
            intt_8(&mut t);
            assert_eq!(t, orig);
        }
    }
}
