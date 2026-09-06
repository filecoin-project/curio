//! CSR (Compressed Sparse Row) matrix representation for R1CS constraint matrices.
//!
//! CSR is chosen over CSC because:
//! - Output vectors (a, b, c) are indexed by constraint (row) — sequential writes
//! - Each row's entries are contiguous in memory — excellent L1/L2 cache locality
//! - Trivial parallelization: partition rows across threads, zero contention
//!
//! Variable indexing: inputs and aux are stored in a unified column space:
//!   col < num_inputs  =>  Input(col)
//!   col >= num_inputs =>  Aux(col - num_inputs)

use ff::PrimeField;
use serde::{Deserialize, Serialize};

use crate::density::PreComputedDensity;

/// CSR sparse matrix for one of A, B, or C.
///
/// Row `i` corresponds to constraint `i`.
/// Entries for row `i` are at indices `row_ptrs[i]..row_ptrs[i+1]`.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct CsrMatrix<Scalar: PrimeField> {
    /// Row pointer array. Length: `num_constraints + 1`.
    /// `row_ptrs[i]` is the index into `cols`/`vals` where row `i` starts.
    pub row_ptrs: Vec<u32>,

    /// Column indices of nonzero entries. Length: `nnz`.
    /// Unified variable index: `col < num_inputs` => Input, else Aux.
    pub cols: Vec<u32>,

    /// Coefficient values. Length: `nnz`.
    pub vals: Vec<Scalar>,
}

impl<Scalar: PrimeField> CsrMatrix<Scalar> {
    /// Number of constraints (rows).
    pub fn num_rows(&self) -> usize {
        if self.row_ptrs.is_empty() {
            0
        } else {
            self.row_ptrs.len() - 1
        }
    }

    /// Number of nonzero entries.
    pub fn nnz(&self) -> usize {
        self.cols.len()
    }

    /// Average number of nonzero entries per row.
    pub fn avg_nnz_per_row(&self) -> f64 {
        if self.num_rows() == 0 {
            0.0
        } else {
            self.nnz() as f64 / self.num_rows() as f64
        }
    }

    /// Get the column indices and values for a single row.
    #[inline]
    pub fn row(&self, i: usize) -> (&[u32], &[Scalar]) {
        let start = self.row_ptrs[i] as usize;
        let end = self.row_ptrs[i + 1] as usize;
        (&self.cols[start..end], &self.vals[start..end])
    }

    /// Memory footprint in bytes (approximate).
    pub fn memory_bytes(&self) -> usize {
        self.row_ptrs.len() * 4
            + self.cols.len() * 4
            + self.vals.len() * std::mem::size_of::<Scalar>()
    }
}

/// Complete pre-compiled circuit, serializable to disk.
///
/// Contains the A, B, C constraint matrices in CSR format, plus
/// pre-computed density bitmaps and circuit dimensions.
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct PreCompiledCircuit<Scalar: PrimeField> {
    /// Number of public input variables (including the constant "one" variable).
    pub num_inputs: u32,

    /// Number of auxiliary (witness) variables.
    pub num_aux: u32,

    /// Number of R1CS constraints.
    pub num_constraints: u32,

    /// A constraint matrix (CSR).
    pub a: CsrMatrix<Scalar>,

    /// B constraint matrix (CSR).
    pub b: CsrMatrix<Scalar>,

    /// C constraint matrix (CSR).
    pub c: CsrMatrix<Scalar>,

    /// Pre-computed density bitmaps (circuit topology constants).
    pub density: PreComputedDensity,
}

impl<Scalar: PrimeField> PreCompiledCircuit<Scalar> {
    /// Total nonzeros across A, B, C.
    pub fn total_nnz(&self) -> usize {
        self.a.nnz() + self.b.nnz() + self.c.nnz()
    }

    /// Total memory footprint in bytes.
    pub fn memory_bytes(&self) -> usize {
        self.a.memory_bytes()
            + self.b.memory_bytes()
            + self.c.memory_bytes()
            + self.density.memory_bytes()
            + 12 // 3 x u32 dimensions
    }

    /// Print summary statistics.
    pub fn summary(&self) -> String {
        format!(
            "PreCompiledCircuit {{ inputs: {}, aux: {}, constraints: {}, \
             A: {{nnz: {}, avg/row: {:.1}}}, B: {{nnz: {}, avg/row: {:.1}}}, \
             C: {{nnz: {}, avg/row: {:.1}}}, total_nnz: {}, mem: {:.1} GiB }}",
            self.num_inputs,
            self.num_aux,
            self.num_constraints,
            self.a.nnz(),
            self.a.avg_nnz_per_row(),
            self.b.nnz(),
            self.b.avg_nnz_per_row(),
            self.c.nnz(),
            self.c.avg_nnz_per_row(),
            self.total_nnz(),
            self.memory_bytes() as f64 / (1024.0 * 1024.0 * 1024.0),
        )
    }
}

#[cfg(test)]
mod csr_tests {
    use super::*;
    use crate::density::PreComputedDensity;
    use blstrs::Scalar as Fr;
    use ff::Field;

    fn sample_csr() -> CsrMatrix<Fr> {
        // 3 rows, 4 cols unified index; row_ptrs length 4
        CsrMatrix {
            row_ptrs: vec![0, 2, 3, 5],
            cols: vec![0, 1, 2, 0, 3],
            vals: vec![Fr::ONE; 5],
        }
    }

    #[test]
    fn csr_num_rows_matches_row_offsets() {
        let m = sample_csr();
        assert_eq!(m.num_rows(), 3);
        assert_eq!(m.row_ptrs.len(), m.num_rows() + 1);
    }

    #[test]
    fn csr_nnz_matches_values_len() {
        let m = sample_csr();
        assert_eq!(m.nnz(), m.cols.len());
        assert_eq!(m.nnz(), m.vals.len());
    }

    #[test]
    fn csr_avg_nnz_handles_empty_rows() {
        let empty: CsrMatrix<Fr> = CsrMatrix {
            row_ptrs: vec![0],
            cols: vec![],
            vals: vec![],
        };
        assert_eq!(empty.num_rows(), 0);
        assert_eq!(empty.avg_nnz_per_row(), 0.0);

        let m = sample_csr();
        assert!((m.avg_nnz_per_row() - 5.0 / 3.0).abs() < 1e-9);
    }

    #[test]
    fn csr_row_returns_correct_slice_pair() {
        let m = sample_csr();
        let (c0, v0) = m.row(0);
        assert_eq!(c0, &[0u32, 1]);
        assert_eq!(v0.len(), 2);
        let (c2, v2) = m.row(2);
        assert_eq!(c2, &[0u32, 3]);
        assert_eq!(v2.len(), 2);
    }

    #[test]
    fn csr_memory_bytes_within_5pct_of_actual() {
        let m = sample_csr();
        let reported = m.memory_bytes();
        let exact = m.row_ptrs.len() * 4 + m.cols.len() * 4 + m.vals.len() * std::mem::size_of::<Fr>();
        let diff = (reported as f64 - exact as f64).abs() / exact as f64;
        assert!(diff < 0.05, "reported {} exact {} diff {}", reported, exact, diff);
    }

    #[test]
    fn precompiled_circuit_total_nnz_sums_three_matrices() {
        let a = sample_csr();
        let b = CsrMatrix {
            row_ptrs: vec![0, 1, 2, 2],
            cols: vec![0, 1],
            vals: vec![Fr::ONE; 2],
        };
        let c = CsrMatrix {
            row_ptrs: vec![0, 0, 0, 0],
            cols: vec![],
            vals: vec![],
        };
        let d = PreComputedDensity::from_csr(&a, &b, 2, 2);
        let p = PreCompiledCircuit {
            num_inputs: 2,
            num_aux: 2,
            num_constraints: 3,
            a,
            b,
            c,
            density: d,
        };
        assert_eq!(p.total_nnz(), p.a.nnz() + p.b.nnz() + p.c.nnz());
    }

    #[test]
    fn precompiled_circuit_summary_includes_dimensions() {
        let a = sample_csr();
        let b = CsrMatrix {
            row_ptrs: vec![0, 0, 0, 0],
            cols: vec![],
            vals: vec![],
        };
        let c = CsrMatrix {
            row_ptrs: vec![0, 0, 0, 0],
            cols: vec![],
            vals: vec![],
        };
        let d = PreComputedDensity::from_csr(&a, &b, 2, 2);
        let p = PreCompiledCircuit {
            num_inputs: 2,
            num_aux: 2,
            num_constraints: 3,
            a,
            b,
            c,
            density: d,
        };
        let s = p.summary();
        assert!(s.contains("inputs: 2"));
        assert!(s.contains("aux: 2"));
        assert!(s.contains("constraints: 3"));
        assert!(s.contains("total_nnz"));
    }
}
