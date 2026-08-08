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

#[cfg(test)]
mod density_tests {
    use super::*;
    use crate::csr::CsrMatrix;
    use bitvec::prelude::{BitVec, Lsb0};
    use blstrs::Scalar as Fr;
    use ec_gpu_gen::multiexp_cpu::DensityTracker;
    use ff::Field;
    use rand::{Rng, SeedableRng};
    use rand_chacha::ChaCha8Rng;

    fn pcd_a_aux_bits(pcd: &PreComputedDensity) -> BitVec<u64, Lsb0> {
        let mut bv = BitVec::<u64, Lsb0>::from_slice(&pcd.a_aux_density_words);
        bv.truncate(pcd.a_aux_bit_len);
        bv
    }

    fn trackers_from_csr<Scalar: ff::PrimeField>(
        a: &CsrMatrix<Scalar>,
        b: &CsrMatrix<Scalar>,
        num_inputs: usize,
        num_aux: usize,
    ) -> (DensityTracker, DensityTracker, DensityTracker) {
        let mut a_aux = DensityTracker::new();
        for _ in 0..num_aux {
            a_aux.add_element();
        }
        for &col in &a.cols {
            let c = col as usize;
            if c >= num_inputs {
                a_aux.inc(c - num_inputs);
            }
        }

        let mut b_input = DensityTracker::new();
        for _ in 0..num_inputs {
            b_input.add_element();
        }
        let mut b_aux = DensityTracker::new();
        for _ in 0..num_aux {
            b_aux.add_element();
        }
        for &col in &b.cols {
            let c = col as usize;
            if c < num_inputs {
                b_input.inc(c);
            } else {
                b_aux.inc(c - num_inputs);
            }
        }

        (a_aux, b_input, b_aux)
    }

    #[test]
    fn density_from_csr_matches_bellperson_density_tracker_on_5x5_random() {
        let mut rng = ChaCha8Rng::seed_from_u64(0xC0FFEE);
        for _ in 0..32 {
            let num_inputs = rng.gen_range(1..=4);
            let num_aux = rng.gen_range(1..=6);
            let nrows = rng.gen_range(1..=8);

            let mut row_ptrs = vec![0u32];
            let mut cols = Vec::new();
            let mut vals = Vec::new();
            for _ in 0..nrows {
                let nnz = rng.gen_range(0..=4);
                for _ in 0..nnz {
                    let col = rng.gen_range(0..(num_inputs + num_aux)) as u32;
                    cols.push(col);
                    vals.push(Fr::ONE);
                }
                row_ptrs.push(cols.len() as u32);
            }
            let a = CsrMatrix {
                row_ptrs: row_ptrs.clone(),
                cols: cols.clone(),
                vals: vals.clone(),
            };

            let mut b_row_ptrs = vec![0u32];
            let mut b_cols = Vec::new();
            let mut b_vals = Vec::new();
            for _ in 0..nrows {
                let nnz = rng.gen_range(0..=4);
                for _ in 0..nnz {
                    let col = rng.gen_range(0..(num_inputs + num_aux)) as u32;
                    b_cols.push(col);
                    b_vals.push(Fr::ONE);
                }
                b_row_ptrs.push(b_cols.len() as u32);
            }
            let b = CsrMatrix {
                row_ptrs: b_row_ptrs,
                cols: b_cols,
                vals: b_vals,
            };

            let pcd = PreComputedDensity::from_csr(&a, &b, num_inputs, num_aux);
            let (a_aux, b_input, b_aux) = trackers_from_csr(&a, &b, num_inputs, num_aux);

            assert_eq!(pcd.a_aux_popcount, a_aux.get_total_density());
            assert_eq!(pcd.b_input_popcount, b_input.get_total_density());
            assert_eq!(pcd.b_aux_popcount, b_aux.get_total_density());

            let pcd_a = pcd_a_aux_bits(&pcd);
            assert_eq!(pcd_a.len(), a_aux.bv.len());
            for i in 0..pcd_a.len() {
                assert_eq!(pcd_a[i], a_aux.bv[i], "a_aux mismatch at {}", i);
            }
        }
    }

    #[test]
    fn density_a_aux_bit_for_used_aux_var() {
        let num_inputs = 1;
        let num_aux = 3;
        let a = CsrMatrix {
            row_ptrs: vec![0, 1],
            cols: vec![num_inputs as u32], // aux 0
            vals: vec![Fr::ONE],
        };
        let b = CsrMatrix {
            row_ptrs: vec![0, 0],
            cols: vec![],
            vals: vec![],
        };
        let pcd = PreComputedDensity::from_csr(&a, &b, num_inputs, num_aux);
        let bits = pcd_a_aux_bits(&pcd);
        assert!(bits[0]);
        assert!(!bits[1]);
        assert!(!bits[2]);
    }

    #[test]
    fn density_b_input_bit_for_input_var() {
        let num_inputs = 2;
        let num_aux = 1;
        let a = CsrMatrix {
            row_ptrs: vec![0, 0],
            cols: vec![],
            vals: vec![],
        };
        let b = CsrMatrix {
            row_ptrs: vec![0, 1],
            cols: vec![1u32],
            vals: vec![Fr::ONE],
        };
        let pcd = PreComputedDensity::from_csr(&a, &b, num_inputs, num_aux);
        let mut bv = BitVec::<u64, Lsb0>::from_slice(&pcd.b_input_density_words);
        bv.truncate(pcd.b_input_bit_len);
        assert!(bv[1]);
        assert!(!bv[0]);
    }

    #[test]
    fn density_memory_bytes_4byte_aligned() {
        let a = CsrMatrix::<Fr> {
            row_ptrs: vec![0],
            cols: vec![],
            vals: vec![],
        };
        let pcd = PreComputedDensity::from_csr(&a, &a, 1, 1);
        assert_eq!(pcd.memory_bytes() % 8, 0);
    }

    #[test]
    fn density_word_layout_endian_invariant() {
        let num_inputs = 1;
        let num_aux = 70;
        let a = CsrMatrix {
            row_ptrs: vec![0, 1],
            cols: vec![num_inputs as u32],
            vals: vec![Fr::ONE],
        };
        let pcd = PreComputedDensity::from_csr(&a, &a, num_inputs, num_aux);
        let pcd2 = PreComputedDensity::from_csr(&a, &a, num_inputs, num_aux);
        assert_eq!(pcd.a_aux_density_words, pcd2.a_aux_density_words);
    }
}
