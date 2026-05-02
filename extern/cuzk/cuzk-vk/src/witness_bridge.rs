//! **B₂ — witness bridge:** trait boundary so upstream crates (`bellperson`, `cuzk-core`) can supply
//! Fr field data into `cuzk-vk` **without** a cyclic `path` dependency on those crates.
//!
//! Implement [`FrNttWitnessSource`] in the integration crate or daemon and set
//! [`crate::prover::VkGroth16Job::witness_ntt_coeffs`] from `witness_ntt_coeffs()` when ready.

use blstrs::Scalar;

/// Host-side source for the partition **Fr NTT** coefficient vector (length `2^L` for effective `L`).
pub trait FrNttWitnessSource {
    /// `None` keeps the built-in synthetic partition coefficients.
    fn witness_ntt_coeffs(&self) -> Option<&[Scalar]>;
}
