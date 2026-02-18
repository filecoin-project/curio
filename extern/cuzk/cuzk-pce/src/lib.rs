//! Pre-Compiled Constraint Evaluator (PCE) for Filecoin Groth16 proving.
//!
//! The PoRep circuit has a fixed R1CS structure: the constraint matrices (A, B, C)
//! are identical for every proof — only the witness vector changes. Yet the current
//! pipeline rebuilds ~130M LinearCombination objects per partition per proof.
//!
//! PCE eliminates this redundancy:
//!
//! 1. **Extract** (once per circuit topology): Run `RecordingCS` to capture R1CS
//!    into CSR (Compressed Sparse Row) matrices. Serialize to disk.
//!
//! 2. **Witness** (per proof): Run circuit synthesis with `WitnessCS` (no-op
//!    `enforce()`). This computes only witness values via `alloc()` closures.
//!
//! 3. **Evaluate** (per proof): Compute `a = A * w`, `b = B * w`, `c = C * w`
//!    via optimized sparse matrix-vector multiplication. The density bitmaps
//!    are pre-computed constants from the extraction phase.
//!
//! Expected speedup: 3-5x on the synthesis phase (~50s -> ~10-20s for 32 GiB PoRep).

pub mod csr;
pub mod density;
pub mod eval;
pub mod recording_cs;

pub use csr::{CsrMatrix, PreCompiledCircuit};
pub use density::PreComputedDensity;
pub use eval::evaluate_pce;
pub use recording_cs::{extract_precompiled_circuit, RecordingCS};
