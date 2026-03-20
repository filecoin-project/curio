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
