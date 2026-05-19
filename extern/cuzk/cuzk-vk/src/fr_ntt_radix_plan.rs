//! Host-side **pass / layer counts** for higher-radix Fr NTT kernels (GPU shaders still TBD).
//!
//! Stockham / Pease radix-4 and radix-8 reduce the number of global butterfly passes versus radix-2
//! at the cost of larger register pressure and twiddle tables. Counts here are planning targets for
//! `n = 2^k` DIT after bit-reversal (same high-level shape as [`crate::fr_ntt_general_gpu`]).

/// Radix-4 DIT stages after bit-reversal: `ceil(log_n / 2)` butterfly passes (each fuses two radix-2 layers).
#[inline]
pub const fn fr_ntt_radix4_forward_butterfly_passes(log_n: u32) -> u32 {
    log_n.div_ceil(2)
}

/// Radix-4 inverse matches forward-shaped butterflies plus one `n_inv` scaling pass.
#[inline]
pub const fn fr_ntt_radix4_inverse_butterfly_passes(log_n: u32) -> u32 {
    fr_ntt_radix4_forward_butterfly_passes(log_n).saturating_add(1)
}

/// Radix-8 DIT stages after bit-reversal: `ceil(log_n / 3)` passes.
#[inline]
pub const fn fr_ntt_radix8_forward_butterfly_passes(log_n: u32) -> u32 {
    log_n.div_ceil(3)
}

/// Full forward GPU pass count sketch: bitrev + copy-to-workspace + radix-R butterflies (same +2 overhead as radix-2 path).
#[inline]
pub const fn fr_ntt_general_forward_pass_count_radix4_sketch(log_n: u32) -> u32 {
    2 + fr_ntt_radix4_forward_butterfly_passes(log_n)
}

#[inline]
pub const fn fr_ntt_general_forward_pass_count_radix8_sketch(log_n: u32) -> u32 {
    2 + fr_ntt_radix8_forward_butterfly_passes(log_n)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn radix4_fewer_layers_than_radix2() {
        assert_eq!(fr_ntt_radix4_forward_butterfly_passes(12), 6);
        assert_eq!(fr_ntt_radix8_forward_butterfly_passes(12), 4);
    }
}
