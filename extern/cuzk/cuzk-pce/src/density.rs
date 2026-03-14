//! Pre-computed density bitmaps extracted from R1CS constraint structure.
//!
//! Density bitmaps track which variables appear in which polynomial evaluations.
//! They are purely a function of circuit topology — identical across all witnesses.
//!
//! The GPU MSM pipeline (supraseal) requires three density bitmaps:
//! - `a_aux_density`: which aux variables appear in any A constraint
//! - `b_input_density`: which input variables appear in any B constraint
//! - `b_aux_density`: which aux variables appear in any B constraint
//!
//! These are stored as `Vec<usize>` raw bitvec words (64-bit on x86_64),
//! matching the layout expected by `ec_gpu_gen::DensityTracker::bv.as_raw_slice()`.

use bitvec::prelude::*;
use serde::{Deserialize, Serialize};

/// Pre-computed density bitmaps for the GPU MSM pipeline.
///
/// These are circuit-topology constants: computed once from the R1CS matrices,
/// reused for every proof of the same circuit type.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct PreComputedDensity {
    /// Bitvec raw words: bit `i` = 1 if aux variable `i` appears in any A constraint.
    /// Length: ceil(num_aux / 64) words.
    pub a_aux_density_words: Vec<u64>,
    /// Number of aux variables (= bit length of a_aux_density).
    pub a_aux_bit_len: usize,
    /// Popcount of a_aux_density.
    pub a_aux_popcount: usize,

    /// Bitvec raw words: bit `i` = 1 if input variable `i` appears in any B constraint.
    pub b_input_density_words: Vec<u64>,
    /// Number of input variables (= bit length of b_input_density).
    pub b_input_bit_len: usize,
    /// Popcount of b_input_density.
    pub b_input_popcount: usize,

    /// Bitvec raw words: bit `i` = 1 if aux variable `i` appears in any B constraint.
    pub b_aux_density_words: Vec<u64>,
    /// Number of aux variables (= bit length of b_aux_density).
    pub b_aux_bit_len: usize,
    /// Popcount of b_aux_density.
    pub b_aux_popcount: usize,
}

impl PreComputedDensity {
    /// Build density bitmaps from pre-compiled CSR matrices.
    ///
    /// Scans the A and B matrices to determine which variables appear
    /// in at least one constraint with a non-zero coefficient.
    pub fn from_csr<Scalar: ff::PrimeField>(
        a: &super::csr::CsrMatrix<Scalar>,
        b: &super::csr::CsrMatrix<Scalar>,
        num_inputs: usize,
        num_aux: usize,
    ) -> Self {
        let mut a_aux_bv = BitVec::<u64, Lsb0>::repeat(false, num_aux);
        let mut b_input_bv = BitVec::<u64, Lsb0>::repeat(false, num_inputs);
        let mut b_aux_bv = BitVec::<u64, Lsb0>::repeat(false, num_aux);

        // Scan A matrix for aux variable density
        for col_idx in a.cols.iter() {
            let col = *col_idx as usize;
            if col >= num_inputs {
                let aux_idx = col - num_inputs;
                a_aux_bv.set(aux_idx, true);
            }
            // A input density is not tracked (always full density)
        }

        // Scan B matrix for input and aux variable density
        for col_idx in b.cols.iter() {
            let col = *col_idx as usize;
            if col < num_inputs {
                b_input_bv.set(col, true);
            } else {
                let aux_idx = col - num_inputs;
                b_aux_bv.set(aux_idx, true);
            }
        }

        let a_aux_popcount = a_aux_bv.count_ones();
        let b_input_popcount = b_input_bv.count_ones();
        let b_aux_popcount = b_aux_bv.count_ones();

        // Extract raw u64 words
        let a_aux_density_words = a_aux_bv.as_raw_slice().to_vec();
        let b_input_density_words = b_input_bv.as_raw_slice().to_vec();
        let b_aux_density_words = b_aux_bv.as_raw_slice().to_vec();

        Self {
            a_aux_density_words,
            a_aux_bit_len: num_aux,
            a_aux_popcount,
            b_input_density_words,
            b_input_bit_len: num_inputs,
            b_input_popcount,
            b_aux_density_words,
            b_aux_bit_len: num_aux,
            b_aux_popcount,
        }
    }

    /// Memory footprint in bytes.
    pub fn memory_bytes(&self) -> usize {
        (self.a_aux_density_words.len()
            + self.b_input_density_words.len()
            + self.b_aux_density_words.len())
            * 8
            + 6 * 8 // 6 usize fields
    }

    /// Convert a_aux_density words to a DensityTracker-compatible raw slice.
    ///
    /// The supraseal FFI expects `&[usize]` from `BitVec<usize, Lsb0>::as_raw_slice()`.
    /// On x86_64, `usize` == `u64`, so we can transmute. This function returns a
    /// reference to the underlying data reinterpreted as `&[usize]`.
    ///
    /// # Safety
    /// Only safe on platforms where `size_of::<usize>() == 8`.
    pub fn a_aux_as_usize_slice(&self) -> &[usize] {
        assert_eq!(
            std::mem::size_of::<usize>(),
            8,
            "PCE density requires 64-bit platform"
        );
        // SAFETY: usize == u64 on x86_64, same alignment and representation
        unsafe {
            std::slice::from_raw_parts(
                self.a_aux_density_words.as_ptr() as *const usize,
                self.a_aux_density_words.len(),
            )
        }
    }

    /// Convert b_input_density words to `&[usize]`.
    pub fn b_input_as_usize_slice(&self) -> &[usize] {
        assert_eq!(std::mem::size_of::<usize>(), 8);
        unsafe {
            std::slice::from_raw_parts(
                self.b_input_density_words.as_ptr() as *const usize,
                self.b_input_density_words.len(),
            )
        }
    }

    /// Convert b_aux_density words to `&[usize]`.
    pub fn b_aux_as_usize_slice(&self) -> &[usize] {
        assert_eq!(std::mem::size_of::<usize>(), 8);
        unsafe {
            std::slice::from_raw_parts(
                self.b_aux_density_words.as_ptr() as *const usize,
                self.b_aux_density_words.len(),
            )
        }
    }
}
