//! MSM scheduling parameters (RDNA / CDNA oriented). Shader path is future work.

/// Bucket MSM configuration placeholder (window bits, batch width).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct MsmConfig {
    /// Bits per MSM window (e.g. 16 for classical Pippenger-style staging).
    pub window_bits: u32,
    /// Logical circuits batched into one dispatch (Phase D roadmap).
    pub batch_circuits: u32,
}

impl Default for MsmConfig {
    fn default() -> Self {
        Self {
            window_bits: 16,
            batch_circuits: 1,
        }
    }
}

/// Number of positive buckets for unsigned window MSM (`2^window_bits` buckets, including zero).
#[inline]
pub fn msm_bucket_count(window_bits: u32) -> u64 {
    debug_assert!(window_bits < 64);
    1u64 << window_bits
}

/// Scalar width in bits for BLS12-381 Fr (fixed; excludes leading zeros in scalars but bounds work).
pub const BLS12_381_FR_BIT_WIDTH: u32 = 255;

/// How many window blocks cover `scalar_bits` when each window (except possibly the last) uses
/// `window_bits` bits (ceiling division). Used to size MSM rounds / dispatches.
#[inline]
pub fn msm_window_block_count(scalar_bits: u32, window_bits: u32) -> u32 {
    debug_assert!(window_bits > 0);
    scalar_bits.div_ceil(window_bits)
}

/// Expected MSM rounds for Filecoin-style 255-bit scalars at the given window size.
#[inline]
pub fn msm_default_rounds_for_fr(window_bits: u32) -> u32 {
    msm_window_block_count(BLS12_381_FR_BIT_WIDTH, window_bits)
}

/// `vkCmdDispatch` grid for a dense “one thread per base / bucket column” reduction sketch.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct MsmBucketReduceDispatch {
    pub groups_x: u32,
    pub groups_y: u32,
    pub local_x: u32,
}

impl MsmBucketReduceDispatch {
    /// Total compute invocations for a `(local_x, 1, 1)` workgroup size (matches dense MSM sketches).
    #[inline]
    pub const fn invocation_count(self) -> u64 {
        self.groups_x as u64 * self.groups_y as u64 * self.local_x as u64
    }

    /// `groups_x = ceil(num_points / local_x)`, `groups_y = min(2^window_bits, u32::MAX)` (capped).
    pub fn dense(num_points: u32, window_bits: u32, local_x: u32) -> Self {
        debug_assert!(local_x > 0);
        let groups_x = num_points.div_ceil(local_x).max(1);
        let wb = window_bits.min(31);
        let groups_y = (1u32 << wb).max(1);
        Self {
            groups_x,
            groups_y,
            local_x,
        }
    }

    #[inline]
    pub fn dispatch(self) -> (u32, u32, u32) {
        (self.groups_x, self.groups_y, 1)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn dense_invocation_count_matches_grid() {
        let d = MsmBucketReduceDispatch::dense(100, 4, 64);
        assert_eq!(d.invocation_count(), 2u64 * 16 * 64);
    }

    #[test]
    fn msm_rounds_match_ceil_div() {
        assert_eq!(msm_window_block_count(255, 16), 16);
        assert_eq!(msm_window_block_count(256, 16), 16);
        assert_eq!(msm_window_block_count(1, 8), 1);
        assert_eq!(msm_bucket_count(4), 16);
    }

    #[test]
    fn msm_dispatch_grid_sane() {
        let d = MsmBucketReduceDispatch::dense(10_000, 16, 256);
        assert_eq!(d.groups_x, 40);
        assert_eq!(d.groups_y, 65536);
        assert_eq!(d.dispatch(), (40, 65536, 1));
    }
}
