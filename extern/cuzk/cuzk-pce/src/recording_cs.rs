//! RecordingCS: a ConstraintSystem that captures R1CS structure into CSR matrices.
//!
//! Used once per circuit topology to extract the fixed constraint structure.
//! The resulting `PreCompiledCircuit` is serialized to disk and reused for all
//! subsequent proofs of the same circuit type.
//!
//! Unlike `KeypairAssembly` (which stores CSC format), `RecordingCS` builds
//! CSR directly — each `enforce()` call appends one row to each of A, B, C.

use bellpepper_core::{
    Circuit, ConstraintSystem, Index, LinearCombination, SynthesisError, Variable,
};
use ff::PrimeField;
use tracing::info;

use crate::csr::{CsrMatrix, PreCompiledCircuit};
use crate::density::PreComputedDensity;

/// Bit flag used in column indices during recording to distinguish aux variables
/// from input variables. During `enforce()`, input columns are stored as their
/// raw index, and aux columns are stored as `index | AUX_FLAG`. In
/// `into_precompiled()`, all columns are remapped to the unified index space
/// using the *final* `num_inputs` value.
///
/// This is necessary because `alloc_input()` and `enforce()` are interleaved
/// during circuit synthesis — `num_inputs` is not yet final when early
/// `enforce()` calls are recorded.
const AUX_FLAG: u32 = 0x8000_0000;

/// A `ConstraintSystem` that records R1CS structure into CSR format.
///
/// Usage:
/// ```ignore
/// let mut cs = RecordingCS::<Fr>::new();
/// cs.alloc_input(|| "one", || Ok(Fr::ONE))?;
/// circuit.synthesize(&mut cs)?;
/// let pce = cs.into_precompiled();
/// ```
///
/// Note: The `alloc()` closures ARE evaluated (to count variables correctly),
/// but the witness values are discarded. Only the constraint structure is kept.
///
/// **Column encoding during recording**: Input variable indices are stored as-is
/// (bit 31 = 0). Aux variable indices are stored with bit 31 set (`index | AUX_FLAG`).
/// The `into_precompiled()` method remaps all aux columns to `final_num_inputs + index`.
pub struct RecordingCS<Scalar: PrimeField> {
    num_inputs: usize,
    num_aux: usize,
    num_constraints: usize,

    // CSR builders for A, B, C — accumulate rows incrementally.
    // Column indices use tagged encoding: bit 31 distinguishes input vs aux.
    a_row_ptrs: Vec<u32>,
    a_cols: Vec<u32>,
    a_vals: Vec<Scalar>,

    b_row_ptrs: Vec<u32>,
    b_cols: Vec<u32>,
    b_vals: Vec<Scalar>,

    c_row_ptrs: Vec<u32>,
    c_cols: Vec<u32>,
    c_vals: Vec<Scalar>,
}

impl<Scalar: PrimeField> RecordingCS<Scalar> {
    /// Create a new empty RecordingCS.
    fn new_empty() -> Self {
        Self {
            num_inputs: 0,
            num_aux: 0,
            num_constraints: 0,
            // row_ptrs start with [0] — the first row starts at index 0
            a_row_ptrs: vec![0],
            a_cols: Vec::new(),
            a_vals: Vec::new(),
            b_row_ptrs: vec![0],
            b_cols: Vec::new(),
            b_vals: Vec::new(),
            c_row_ptrs: vec![0],
            c_cols: Vec::new(),
            c_vals: Vec::new(),
        }
    }

    /// Remap tagged column indices to unified column space.
    ///
    /// During `enforce()`, input columns are stored as raw index (bit 31 = 0),
    /// and aux columns are stored as `index | AUX_FLAG` (bit 31 = 1).
    /// This method replaces each tagged aux column with `final_num_inputs + index`.
    fn remap_cols(cols: &mut [u32], final_num_inputs: u32) {
        for col in cols.iter_mut() {
            if *col & AUX_FLAG != 0 {
                // Aux variable: strip flag, add final_num_inputs offset
                *col = final_num_inputs + (*col & !AUX_FLAG);
            }
            // Input variables: already correct (raw index, no flag)
        }
    }

    /// Finalize into a `PreCompiledCircuit`.
    ///
    /// Consumes the RecordingCS and produces the pre-compiled circuit with
    /// CSR matrices and pre-computed density bitmaps. All tagged column indices
    /// are remapped to the unified `[inputs | aux]` column space using the
    /// final `num_inputs` value.
    pub fn into_precompiled(mut self) -> PreCompiledCircuit<Scalar> {
        let num_inputs = self.num_inputs as u32;
        let num_aux = self.num_aux as u32;
        let num_constraints = self.num_constraints as u32;

        // Remap all tagged column indices to unified space
        Self::remap_cols(&mut self.a_cols, num_inputs);
        Self::remap_cols(&mut self.b_cols, num_inputs);
        Self::remap_cols(&mut self.c_cols, num_inputs);

        let a = CsrMatrix {
            row_ptrs: self.a_row_ptrs,
            cols: self.a_cols,
            vals: self.a_vals,
        };
        let b = CsrMatrix {
            row_ptrs: self.b_row_ptrs,
            cols: self.b_cols,
            vals: self.b_vals,
        };
        let c = CsrMatrix {
            row_ptrs: self.c_row_ptrs,
            cols: self.c_cols,
            vals: self.c_vals,
        };

        // Compute density bitmaps from the CSR matrices
        let density = PreComputedDensity::from_csr(&a, &b, self.num_inputs, self.num_aux);

        info!(
            num_inputs = self.num_inputs,
            num_aux = self.num_aux,
            num_constraints = self.num_constraints,
            a_nnz = a.nnz(),
            b_nnz = b.nnz(),
            c_nnz = c.nnz(),
            a_aux_density = density.a_aux_popcount,
            b_input_density = density.b_input_popcount,
            b_aux_density = density.b_aux_popcount,
            "RecordingCS: extracted pre-compiled circuit"
        );

        PreCompiledCircuit {
            num_inputs,
            num_aux,
            num_constraints,
            a,
            b,
            c,
            density,
        }
    }
}

impl<Scalar: PrimeField> ConstraintSystem<Scalar> for RecordingCS<Scalar> {
    type Root = Self;

    fn new() -> Self {
        Self::new_empty()
    }

    fn alloc<F, A, AR>(&mut self, _: A, f: F) -> Result<Variable, SynthesisError>
    where
        F: FnOnce() -> Result<Scalar, SynthesisError>,
        A: FnOnce() -> AR,
        AR: Into<String>,
    {
        // We evaluate the closure to maintain correct variable counting
        // (some circuits conditionally alloc based on prior alloc results).
        // The value is discarded — we only care about structure.
        let _val = f()?;
        let index = self.num_aux;
        self.num_aux += 1;
        Ok(Variable(Index::Aux(index)))
    }

    fn alloc_input<F, A, AR>(&mut self, _: A, f: F) -> Result<Variable, SynthesisError>
    where
        F: FnOnce() -> Result<Scalar, SynthesisError>,
        A: FnOnce() -> AR,
        AR: Into<String>,
    {
        let _val = f()?;
        let index = self.num_inputs;
        self.num_inputs += 1;
        Ok(Variable(Index::Input(index)))
    }

    fn enforce<A, AR, LA, LB, LC>(&mut self, _: A, a: LA, b: LB, c: LC)
    where
        A: FnOnce() -> AR,
        AR: Into<String>,
        LA: FnOnce(LinearCombination<Scalar>) -> LinearCombination<Scalar>,
        LB: FnOnce(LinearCombination<Scalar>) -> LinearCombination<Scalar>,
        LC: FnOnce(LinearCombination<Scalar>) -> LinearCombination<Scalar>,
    {
        let a_lc = a(LinearCombination::zero());
        let b_lc = b(LinearCombination::zero());
        let c_lc = c(LinearCombination::zero());

        // Record A row — input cols stored as raw index, aux cols tagged with AUX_FLAG
        for (index, coeff) in a_lc.input_terms_slice() {
            if !coeff.is_zero_vartime() {
                self.a_cols.push(*index as u32);
                self.a_vals.push(*coeff);
            }
        }
        for (index, coeff) in a_lc.aux_terms_slice() {
            if !coeff.is_zero_vartime() {
                self.a_cols.push(*index as u32 | AUX_FLAG);
                self.a_vals.push(*coeff);
            }
        }
        self.a_row_ptrs.push(self.a_cols.len() as u32);

        // Record B row
        for (index, coeff) in b_lc.input_terms_slice() {
            if !coeff.is_zero_vartime() {
                self.b_cols.push(*index as u32);
                self.b_vals.push(*coeff);
            }
        }
        for (index, coeff) in b_lc.aux_terms_slice() {
            if !coeff.is_zero_vartime() {
                self.b_cols.push(*index as u32 | AUX_FLAG);
                self.b_vals.push(*coeff);
            }
        }
        self.b_row_ptrs.push(self.b_cols.len() as u32);

        // Record C row
        for (index, coeff) in c_lc.input_terms_slice() {
            if !coeff.is_zero_vartime() {
                self.c_cols.push(*index as u32);
                self.c_vals.push(*coeff);
            }
        }
        for (index, coeff) in c_lc.aux_terms_slice() {
            if !coeff.is_zero_vartime() {
                self.c_cols.push(*index as u32 | AUX_FLAG);
                self.c_vals.push(*coeff);
            }
        }
        self.c_row_ptrs.push(self.c_cols.len() as u32);

        self.num_constraints += 1;
    }

    fn push_namespace<NR, N>(&mut self, _: N)
    where
        NR: Into<String>,
        N: FnOnce() -> NR,
    {
    }

    fn pop_namespace(&mut self) {}

    fn get_root(&mut self) -> &mut Self::Root {
        self
    }
}

/// Extract a pre-compiled circuit from a circuit instance.
///
/// Runs the circuit's `synthesize()` method with a `RecordingCS` to capture
/// the full R1CS structure. This is a one-time operation per circuit topology.
///
/// The circuit should be constructed with dummy/default witness values — the
/// actual values don't matter, only the constraint structure.
///
/// # Arguments
/// * `circuit` - A circuit instance (can use any witness values)
///
/// # Returns
/// `PreCompiledCircuit` containing CSR matrices and density bitmaps.
pub fn extract_precompiled_circuit<Scalar, C>(
    circuit: C,
) -> Result<PreCompiledCircuit<Scalar>, SynthesisError>
where
    Scalar: PrimeField,
    C: Circuit<Scalar>,
{
    let mut cs = RecordingCS::<Scalar>::new();

    // Allocate the "one" input variable (matches bellperson prover convention)
    cs.alloc_input(|| "one", || Ok(Scalar::ONE))?;

    // Synthesize the circuit to capture R1CS structure
    circuit.synthesize(&mut cs)?;

    // Add input constraints (matches bellperson: input_i * 1 = 0, for density)
    let num_inputs = cs.num_inputs;
    for i in 0..num_inputs {
        cs.enforce(|| "", |lc| lc + Variable(Index::Input(i)), |lc| lc, |lc| lc);
    }

    Ok(cs.into_precompiled())
}
