//! Sparse MatVec evaluator for PCE constraint evaluation.
//!
//! Computes `a = A * w`, `b = B * w`, `c = C * w` using row-parallel
//! sparse matrix-vector multiplication. Each row is independent, so
//! threads process disjoint row ranges with zero contention.
//!
//! The witness vector `w` uses unified variable indexing:
//!   w[0..num_inputs] = input_assignment
//!   w[num_inputs..num_inputs+num_aux] = aux_assignment

use std::sync::atomic::{AtomicI32, Ordering};
use std::sync::Arc;
use std::time::Instant;

use ec_gpu_gen::multiexp_cpu::DensityTracker;
use ff::PrimeField;
use rayon::prelude::*;
use tracing::info;

use crate::csr::{CsrMatrix, PreCompiledCircuit};

// Phase 11 Intervention 3: Global memory-bandwidth throttle flag.
// When > 0, synthesis SpMV threads should yield periodically to reduce
// L3 cache contention with b_g2_msm (CPU Pippenger MSM).
//
// Set by C++ via `set_membw_throttle(1)` before b_g2_msm, cleared after.
// Checked by `spmv_parallel()` every 64 chunks (~524K rows).
// Cost when throttle==0: one relaxed atomic load per 64 chunks (~1ns per 500µs).
static MEMBW_THROTTLE: AtomicI32 = AtomicI32::new(0);

/// FFI entry point for C++ to set the memory-bandwidth throttle flag.
/// Called from `groth16_cuda.cu` before/after b_g2_msm.
///
/// # Safety
/// Thread-safe (atomic). Can be called from any thread.
#[no_mangle]
pub extern "C" fn set_membw_throttle(value: i32) {
    MEMBW_THROTTLE.store(value, Ordering::Release);
}

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
            // Phase 11 Intervention 3: Yield briefly if b_g2_msm is active.
            // Check every 64 chunks (~524K rows) to limit overhead.
            // yield_now() is a hint — returns immediately if no other thread
            // is waiting. Cost when throttle==0: one atomic load (~1ns).
            if chunk_idx % 64 == 0 && MEMBW_THROTTLE.load(Ordering::Relaxed) > 0 {
                std::thread::yield_now();
            }

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

#[cfg(test)]
mod eval_tests {
    use super::{density_tracker_from_words, evaluate_pce, spmv_parallel};
    use crate::csr::{CsrMatrix, PreCompiledCircuit};
    use crate::density::PreComputedDensity;
    use bitvec::prelude::*;
    use blstrs::Scalar as Fr;
    use ff::{Field, PrimeField};

    fn spmv_serial<Scalar: PrimeField>(matrix: &CsrMatrix<Scalar>, witness: &[Scalar]) -> Vec<Scalar> {
        let n = matrix.num_rows();
        let mut out = vec![Scalar::ZERO; n];
        for row in 0..n {
            let (cols, vals) = matrix.row(row);
            let mut acc = Scalar::ZERO;
            for j in 0..cols.len() {
                acc += vals[j] * witness[cols[j] as usize];
            }
            out[row] = acc;
        }
        out
    }

    #[test]
    fn spmv_parallel_matches_serial_on_random_csr() {
        let witness: Vec<Fr> = (0..8).map(|i| Fr::from(i as u64)).collect();
        let m = CsrMatrix {
            row_ptrs: vec![0, 2, 4, 7],
            cols: vec![0, 3, 1, 4, 0, 5, 6],
            vals: vec![Fr::ONE, Fr::ONE, Fr::ONE, Fr::ONE, Fr::ONE, Fr::ONE, Fr::ONE],
        };
        let par = spmv_parallel(&m, &witness);
        let ser = spmv_serial(&m, &witness);
        assert_eq!(par, ser);
    }

    #[test]
    fn evaluate_pce_zero_witness_returns_zero() {
        let num_inputs = 2u32;
        let num_aux = 2u32;
        let a = CsrMatrix {
            row_ptrs: vec![0, 1, 2],
            cols: vec![0, 1],
            vals: vec![Fr::ONE, Fr::ONE],
        };
        let b = CsrMatrix {
            row_ptrs: vec![0, 0, 0],
            cols: vec![],
            vals: vec![],
        };
        let c = CsrMatrix {
            row_ptrs: vec![0, 0, 0],
            cols: vec![],
            vals: vec![],
        };
        let density = PreComputedDensity::from_csr(&a, &b, num_inputs as usize, num_aux as usize);
        let pce = PreCompiledCircuit {
            num_inputs,
            num_aux,
            num_constraints: 2,
            a,
            b,
            c,
            density,
        };
        let inputs = vec![Fr::ZERO; num_inputs as usize];
        let aux = vec![Fr::ZERO; num_aux as usize];
        let r = evaluate_pce(&pce, inputs, aux);
        assert!(r.a.iter().all(|x| x.is_zero_vartime()));
        assert!(r.b.iter().all(|x| x.is_zero_vartime()));
        assert!(r.c.iter().all(|x| x.is_zero_vartime()));
    }

    #[test]
    fn evaluate_pce_matches_serial_matvec_5x5_style() {
        // 2 constraints, 2 inputs + 2 aux
        let num_inputs = 2u32;
        let num_aux = 2u32;
        let a = CsrMatrix {
            row_ptrs: vec![0, 2, 3],
            cols: vec![0, 2, 3],
            vals: vec![Fr::ONE, Fr::from(2u64), Fr::ONE],
        };
        let b = CsrMatrix {
            row_ptrs: vec![0, 1, 2],
            cols: vec![1, 2],
            vals: vec![Fr::ONE, Fr::ONE],
        };
        let c = CsrMatrix {
            row_ptrs: vec![0, 0, 1],
            cols: vec![3],
            vals: vec![Fr::ONE],
        };
        let density = PreComputedDensity::from_csr(&a, &b, num_inputs as usize, num_aux as usize);
        let pce = PreCompiledCircuit {
            num_inputs,
            num_aux,
            num_constraints: 2,
            a: a.clone(),
            b: b.clone(),
            c: c.clone(),
            density,
        };
        let inputs = vec![Fr::ONE, Fr::from(3u64)];
        let aux = vec![Fr::from(5u64), Fr::from(7u64)];
        let mut w = inputs.clone();
        w.extend_from_slice(&aux);

        let r = evaluate_pce(&pce, inputs.clone(), aux.clone());
        assert_eq!(r.a, spmv_serial(&a, &w));
        assert_eq!(r.b, spmv_serial(&b, &w));
        assert_eq!(r.c, spmv_serial(&c, &w));
    }

    #[test]
    fn evaluate_pce_vs_recording_cs_match_small() {
        use bellpepper_core::{ConstraintSystem, Index, Variable};
        use crate::recording_cs::RecordingCS;

        let mut cs = RecordingCS::<Fr>::new();
        cs.alloc_input(|| "one", || Ok(Fr::ONE)).unwrap();
        let x = cs
            .alloc(|| "x", || Ok(Fr::from(11u64)))
            .unwrap();
        // 1 * x = x
        cs.enforce(
            || "mul",
            |lc| lc + Variable(Index::Input(0)),
            |lc| lc + x,
            |lc| lc + x,
        );
        let pce = cs.into_precompiled();
        let inputs = vec![Fr::ONE];
        let aux = vec![Fr::from(11u64)];
        let r = evaluate_pce(&pce, inputs.clone(), aux.clone());
        let mut w = inputs;
        w.extend(aux);
        assert_eq!(r.a, spmv_serial(&pce.a, &w));
        assert_eq!(r.b, spmv_serial(&pce.b, &w));
        assert_eq!(r.c, spmv_serial(&pce.c, &w));
    }

    #[test]
    fn density_tracker_from_words_round_trips_through_save_load() {
        let num_inputs = 2usize;
        let num_aux = 5usize;
        let a = CsrMatrix {
            row_ptrs: vec![0, 1],
            cols: vec![num_inputs as u32],
            vals: vec![Fr::ONE],
        };
        let b = CsrMatrix {
            row_ptrs: vec![0, 1],
            cols: vec![0u32],
            vals: vec![Fr::ONE],
        };
        let pcd = PreComputedDensity::from_csr(&a, &b, num_inputs, num_aux);
        let dt = density_tracker_from_words(
            &pcd.a_aux_density_words,
            pcd.a_aux_bit_len,
            pcd.a_aux_popcount,
        );
        assert_eq!(dt.get_total_density(), pcd.a_aux_popcount);
        assert_eq!(dt.bv.len(), pcd.a_aux_bit_len);
        let mut expected = BitVec::<u64, Lsb0>::from_slice(&pcd.a_aux_density_words);
        expected.truncate(pcd.a_aux_bit_len);
        for i in 0..pcd.a_aux_bit_len {
            assert_eq!(dt.bv[i], expected[i]);
        }
    }
}
