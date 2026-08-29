//! Dense **A / B / C** R1CS evaluation vectors (one `Fr` per constraint) — same lengths as bellperson’s
//! internal proving assignment `a` / `b` / `c` after synthesis. `bellperson` does not export that type
//! without the `cuda-supraseal` feature; Vulkan staging uses these counts regardless.

use crate::g1::BLS12_381_FR_U32_LIMBS;

/// Number of `Fr` values in each of `a`, `b`, and `c` for a circuit with `num_constraints` rows.
#[inline]
pub const fn dense_abc_fr_count(num_constraints: usize) -> usize {
    num_constraints
}

/// Bytes for one dense `Fr` column in Montgomery `u32` limb layout (matches GPU Fr SSBO packing).
#[inline]
pub const fn dense_fr_column_bytes(num_constraints: usize) -> usize {
    num_constraints * BLS12_381_FR_U32_LIMBS * 4
}

/// Total bytes for `a || b || c` when each is stored as `u32[BLS12_381_FR_U32_LIMBS]` per constraint.
#[inline]
pub const fn dense_abc_triple_bytes(num_constraints: usize) -> usize {
    dense_fr_column_bytes(num_constraints) * 3
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn dense_abc_sizes_match_fr_limbs() {
        let n = 100usize;
        assert_eq!(dense_abc_fr_count(n), n);
        assert_eq!(dense_fr_column_bytes(n), n * 32);
        assert_eq!(dense_abc_triple_bytes(n), n * 32 * 3);
    }
}
