//! Sparse MatVec evaluator for PCE constraint evaluation.
//!
//! Computes `a = A * w`, `b = B * w`, `c = C * w` using row-parallel
//! sparse matrix-vector multiplication. Each row is independent, so
//! threads process disjoint row ranges with zero contention.
//!
//! The witness vector `w` uses unified variable indexing:
//!   w[0..num_inputs] = input_assignment
//!   w[num_inputs..num_inputs+num_aux] = aux_assignment

use std::sync::Arc;
use std::time::Instant;

use ec_gpu_gen::multiexp_cpu::DensityTracker;
use ff::PrimeField;
use rayon::prelude::*;
use tracing::info;

use crate::csr::{CsrMatrix, PreCompiledCircuit};

/// Result of PCE evaluation for one circuit.
///
/// Contains the a, b, c polynomial evaluation vectors and the
/// witness assignments (input + aux), ready to pass to the GPU prover.
pub struct PceEvalResult<Scalar: PrimeField> {
    /// A polynomial evaluations (one per constraint).
    pub a: Vec<Scalar>,
    /// B polynomial evaluations (one per constraint).
    pub b: Vec<Scalar>,
    /// C polynomial evaluations (one per constraint).
    pub c: Vec<Scalar>,
    /// Input witness vector.
    pub input_assignment: Arc<Vec<Scalar>>,
    /// Aux witness vector.
    pub aux_assignment: Arc<Vec<Scalar>>,
}

/// Evaluate a single CSR matrix-vector product: `result[i] = sum_j M[i,j] * w[j]`
///
/// Uses rayon parallel iterators to split rows across threads.
/// Each thread writes to a disjoint range of the output vector.
fn spmv_parallel<Scalar: PrimeField>(
    matrix: &CsrMatrix<Scalar>,
    witness: &[Scalar],
) -> Vec<Scalar> {
    let num_rows = matrix.num_rows();
    if num_rows == 0 {
        return Vec::new();
    }

    // Use a chunk size that balances parallelism with cache locality.
    // Each thread processes a contiguous block of rows.
    let chunk_size = 8192; // ~256 KiB of output per chunk at 32 bytes/Fr

    let mut result = vec![Scalar::ZERO; num_rows];

    result
        .par_chunks_mut(chunk_size)
        .enumerate()
        .for_each(|(chunk_idx, out_chunk)| {
            let row_start = chunk_idx * chunk_size;
            for (local_i, out) in out_chunk.iter_mut().enumerate() {
                let row = row_start + local_i;
                let start = matrix.row_ptrs[row] as usize;
                let end = matrix.row_ptrs[row + 1] as usize;

                let mut acc = Scalar::ZERO;
                let cols = &matrix.cols[start..end];
                let vals = &matrix.vals[start..end];

                for j in 0..cols.len() {
                    let w = witness[cols[j] as usize];
                    acc += vals[j] * w;
                }

                *out = acc;
            }
        });

    result
}

/// Evaluate the pre-compiled circuit against a witness vector.
///
/// Computes `a = A * w`, `b = B * w`, `c = C * w` where `w` is the
/// concatenated `[input_assignment | aux_assignment]` witness vector.
///
/// # Arguments
/// * `pce` - Pre-compiled circuit (CSR matrices + density)
/// * `input_assignment` - Input (public) witness values
/// * `aux_assignment` - Auxiliary (private) witness values
///
/// # Returns
/// `PceEvalResult` with a, b, c vectors ready for GPU proving.
pub fn evaluate_pce<Scalar: PrimeField>(
    pce: &PreCompiledCircuit<Scalar>,
    input_assignment: Vec<Scalar>,
    aux_assignment: Vec<Scalar>,
) -> PceEvalResult<Scalar> {
    let num_inputs = pce.num_inputs as usize;
    let num_aux = pce.num_aux as usize;

    assert_eq!(
        input_assignment.len(),
        num_inputs,
        "input_assignment length mismatch: got {}, expected {}",
        input_assignment.len(),
        num_inputs,
    );
    assert_eq!(
        aux_assignment.len(),
        num_aux,
        "aux_assignment length mismatch: got {}, expected {}",
        aux_assignment.len(),
        num_aux,
    );

    // Build unified witness vector: [inputs | aux]
    let eval_start = Instant::now();
    let mut witness = Vec::with_capacity(num_inputs + num_aux);
    witness.extend_from_slice(&input_assignment);
    witness.extend_from_slice(&aux_assignment);

    // Evaluate all three matrices in parallel using rayon join
    let (a, (b, c)) = rayon::join(
        || spmv_parallel(&pce.a, &witness),
        || {
            rayon::join(
                || spmv_parallel(&pce.b, &witness),
                || spmv_parallel(&pce.c, &witness),
            )
        },
    );

    let eval_duration = eval_start.elapsed();
    info!(
        eval_ms = eval_duration.as_millis(),
        a_len = a.len(),
        b_len = b.len(),
        c_len = c.len(),
        "PCE MatVec evaluation complete"
    );

    PceEvalResult {
        a,
        b,
        c,
        input_assignment: Arc::new(input_assignment),
        aux_assignment: Arc::new(aux_assignment),
    }
}

/// Build a `DensityTracker` from pre-computed density words.
///
/// This constructs an `ec_gpu_gen::DensityTracker` with the correct
/// BitVec and popcount, suitable for passing to the GPU prover via
/// the existing `ProvingAssignment` fields.
pub fn density_tracker_from_words(
    words: &[u64],
    bit_len: usize,
    popcount: usize,
) -> DensityTracker {
    use bitvec::prelude::*;

    // Reconstruct BitVec<usize, Lsb0> from u64 words.
    // On x86_64, usize == u64, so we reinterpret the words.
    assert_eq!(
        std::mem::size_of::<usize>(),
        8,
        "density tracker reconstruction requires 64-bit platform"
    );
    let usize_words: &[usize] =
        unsafe { std::slice::from_raw_parts(words.as_ptr() as *const usize, words.len()) };

    let mut bv = BitVec::<usize, Lsb0>::from_slice(usize_words);
    bv.truncate(bit_len);

    DensityTracker {
        bv,
        total_density: popcount,
    }
}
