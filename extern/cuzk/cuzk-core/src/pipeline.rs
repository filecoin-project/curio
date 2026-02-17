//! Pipelined synthesis/GPU proving engine (Phase 2).
//!
//! Splits the monolithic `seal_commit_phase2()` (and analogous PoSt/SnapDeals
//! functions) into two phases:
//!
//! 1. **Synthesis** (CPU-bound): Builds circuits from vanilla proofs, runs
//!    `bellperson::synthesize_circuits_batch()` to produce intermediate state
//!    (a/b/c evaluations + density trackers + witness assignments).
//!
//! 2. **GPU Prove** (GPU-bound): Takes the synthesized state and SRS, runs
//!    `bellperson::prove_from_assignments()` for NTT + MSM on the GPU.
//!
//! This allows CPU synthesis for job N+1 to overlap with GPU proving for job N,
//! achieving ~1.5x throughput improvement when there's a continuous workload.
//!
//! For PoRep 32G, we use **per-partition pipelining**: each of the 10 partitions
//! is synthesized and proven individually, keeping intermediate memory at ~13.6 GiB
//! (vs ~136 GiB for all 10 at once). This allows pipelining on 128 GiB machines.

#[cfg(feature = "cuda-supraseal")]
use std::sync::Arc;
use std::time::{Duration, Instant};

use anyhow::{Context, Result};
use tracing::{debug, info, info_span};

use crate::prover::C1OutputWrapper;
use crate::srs_manager::CircuitId;

// ─── Bellperson split API (only with cuda-supraseal) ────────────────────────

#[cfg(feature = "cuda-supraseal")]
use bellperson::groth16::{
    prove_from_assignments, synthesize_circuits_batch, Proof, ProvingAssignment,
    SuprasealParameters,
};
#[cfg(feature = "cuda-supraseal")]
use blstrs::{Bls12, Scalar as Fr};
#[cfg(feature = "cuda-supraseal")]
use ff::Field;

// ─── Filecoin proving stack (direct circuit construction) ───────────────────

#[cfg(feature = "cuda-supraseal")]
use filecoin_proofs::parameters::setup_params;
#[cfg(feature = "cuda-supraseal")]
use filecoin_proofs::{
    as_safe_commitment, DefaultPieceDomain, DefaultPieceHasher, SectorShape32GiB,
    SECTOR_SIZE_32_GIB,
};
#[cfg(feature = "cuda-supraseal")]
use filecoin_proofs_api::seal::SealCommitPhase1Output;
#[cfg(feature = "cuda-supraseal")]
use storage_proofs_core::compound_proof::CompoundProof;
#[cfg(feature = "cuda-supraseal")]
use storage_proofs_porep::stacked::{StackedCompound, StackedDrg};

// ─── Constants ──────────────────────────────────────────────────────────────

/// Number of Groth16 proof bytes per partition (2 × G1 + 1 × G2 compressed).
const GROTH_PROOF_BYTES: usize = 192;

// ═══════════════════════════════════════════════════════════════════════════
// Types
// ═══════════════════════════════════════════════════════════════════════════

/// Intermediate state between synthesis (CPU) and GPU proving.
///
/// This struct holds the output of `bellperson::synthesize_circuits_batch()`:
/// the a/b/c constraint evaluations, density trackers, and witness assignments.
/// It is produced by the synthesis phase and consumed by the GPU phase.
///
/// For PoRep 32G with per-partition pipelining, each `SynthesizedProof` holds
/// one partition (~13.6 GiB). For PoSt, it holds the full proof.
#[cfg(feature = "cuda-supraseal")]
pub struct SynthesizedProof {
    /// Circuit type (determines which SRS to use).
    pub circuit_id: CircuitId,

    /// Bellperson proving assignments (a/b/c evaluations + density trackers).
    /// One per circuit in the batch (1 for per-partition, up to 10 for full batch).
    pub provers: Vec<ProvingAssignment<Fr>>,

    /// Input witness vectors (extracted from provers via `std::mem::take`).
    pub input_assignments: Vec<Arc<Vec<Fr>>>,

    /// Aux witness vectors (extracted from provers via `std::mem::take`).
    pub aux_assignments: Vec<Arc<Vec<Fr>>>,

    /// Randomization r values (one per circuit).
    pub r_s: Vec<Fr>,

    /// Randomization s values (one per circuit).
    pub s_s: Vec<Fr>,

    /// How long the synthesis step took.
    pub synthesis_duration: Duration,

    /// For multi-partition proofs (PoRep): which partition index this is.
    /// None for single-partition proofs (PoSt).
    pub partition_index: Option<usize>,

    /// Total number of partitions expected for the complete proof.
    pub total_partitions: usize,
}

/// Stub type for builds without CUDA support.
#[cfg(not(feature = "cuda-supraseal"))]
pub struct SynthesizedProof {
    pub circuit_id: CircuitId,
    pub synthesis_duration: Duration,
    pub partition_index: Option<usize>,
    pub total_partitions: usize,
}

/// Result of GPU proving: raw proof bytes for one or more partitions.
#[derive(Debug)]
pub struct GpuProveResult {
    /// Raw Groth16 proof bytes (192 bytes per partition).
    pub proof_bytes: Vec<u8>,
    /// How long the GPU phase took.
    pub gpu_duration: Duration,
    /// Partition index if this is a per-partition result.
    pub partition_index: Option<usize>,
}

// ═══════════════════════════════════════════════════════════════════════════
// PoRep C2 Synthesis (CPU phase)
// ═══════════════════════════════════════════════════════════════════════════

/// Synthesize a single partition of a PoRep C2 proof (CPU-only).
///
/// This replicates the circuit construction logic from
/// `seal_commit_phase2_circuit_proofs` in filecoin-proofs, but stops after
/// synthesis — the GPU proving is done separately via `gpu_prove()`.
///
/// # Per-Partition Pipeline
///
/// Instead of building all 10 partition circuits at once (~136 GiB intermediate),
/// this function builds ONE partition circuit (~13.6 GiB). The caller invokes
/// this 10 times (one per partition) and feeds each result to the GPU worker.
///
/// # Arguments
/// * `vanilla_proof_json` — The raw C1 JSON output (with base64 wrapper)
/// * `sector_number` — Sector ID
/// * `miner_id` — Miner actor ID
/// * `partition_index` — Which partition (0..9) to synthesize
/// * `job_id` — For log correlation
#[cfg(feature = "cuda-supraseal")]
pub fn synthesize_porep_c2_partition(
    vanilla_proof_json: &[u8],
    _sector_number: u64,
    _miner_id: u64,
    partition_index: usize,
    job_id: &str,
) -> Result<SynthesizedProof> {
    let _span = info_span!(
        "synthesize_porep_c2",
        job_id = job_id,
        partition = partition_index
    )
    .entered();

    // ── Step 1: Deserialize C1 output ──────────────────────────────────
    let deser_start = Instant::now();

    let wrapper: C1OutputWrapper = serde_json::from_slice(vanilla_proof_json)
        .context("failed to parse C1 output wrapper JSON")?;

    let phase1_json_bytes = base64::Engine::decode(
        &base64::engine::general_purpose::STANDARD,
        &wrapper.phase1_out,
    )
    .context("failed to decode base64 Phase1Output")?;

    let c1_output: SealCommitPhase1Output = serde_json::from_slice(&phase1_json_bytes)
        .context("failed to deserialize SealCommitPhase1Output from JSON")?;

    let deser_duration = deser_start.elapsed();
    debug!(
        deser_ms = deser_duration.as_millis(),
        "deserialized C1 output"
    );

    // ── Step 2: Build PoRep config from the registered proof ───────────
    let porep_config = c1_output.registered_proof.as_v1_config();
    let num_partitions = usize::from(porep_config.partitions);

    anyhow::ensure!(
        partition_index < num_partitions,
        "partition_index {} >= num_partitions {}",
        partition_index,
        num_partitions,
    );

    // ── Step 3: Call the typed inner function ──────────────────────────
    // We use SectorShape32GiB directly for 32G sectors.
    // For other sizes, we'd dispatch via with_shape! — but Phase 2 focuses on 32G.
    let sector_size = u64::from(porep_config.sector_size);
    anyhow::ensure!(
        sector_size == SECTOR_SIZE_32_GIB,
        "Phase 2 pipelined synthesis currently supports only 32 GiB sectors, got {} bytes",
        sector_size,
    );

    // ── Step 3: Unpack C1 output ─────────────────────────────────────
    //
    // The VanillaSealProof enum wraps Vec<Vec<stacked::Proof<Tree>>>.
    // We convert from the API-level type to the concrete SectorShape32GiB type.
    type Tree = SectorShape32GiB;

    let vanilla_proofs: Vec<Vec<storage_proofs_porep::stacked::Proof<Tree, DefaultPieceHasher>>> =
        c1_output
            .vanilla_proofs
            .try_into()
            .map_err(|e| anyhow::anyhow!("failed to convert vanilla proofs: {:?}", e))?;

    let comm_r = c1_output.comm_r;
    let comm_d = c1_output.comm_d;
    let replica_id = c1_output.replica_id;
    let seed = c1_output.seed;

    anyhow::ensure!(comm_d != [0; 32], "Invalid all zero commitment (comm_d)");
    anyhow::ensure!(comm_r != [0; 32], "Invalid all zero commitment (comm_r)");
    anyhow::ensure!(seed != [0; 32], "Invalid porep challenge seed");

    let comm_r_safe = as_safe_commitment(&comm_r, "comm_r")?;
    let comm_d_safe: DefaultPieceDomain = as_safe_commitment(&comm_d, "comm_d")?;

    // ── Step 4: Build stacked public inputs ────────────────────────────
    use storage_proofs_porep::stacked::{PublicInputs, Tau};

    let public_inputs = PublicInputs {
        replica_id,
        tau: Some(Tau {
            comm_d: comm_d_safe,
            comm_r: comm_r_safe,
        }),
        k: None,
        seed: Some(seed),
    };

    // ── Step 5: Build compound public params ───────────────────────────
    use storage_proofs_core::compound_proof::SetupParams;

    let vanilla_setup = setup_params(&porep_config)?;
    let compound_setup = SetupParams {
        vanilla_params: vanilla_setup,
        partitions: Some(num_partitions),
        priority: false,
    };

    let compound_public_params = <StackedCompound<Tree, DefaultPieceHasher> as CompoundProof<
        StackedDrg<'_, Tree, DefaultPieceHasher>,
        _,
    >>::setup(&compound_setup)?;

    // ── Step 6: Build circuit for this one partition ────────────────────
    //
    // CompoundProof::circuit() builds a circuit instance from the vanilla
    // proof for partition `k`. The vanilla_proofs[partition_index] is the
    // Vec<stacked::Proof> for this partition's challenges.

    anyhow::ensure!(
        partition_index < vanilla_proofs.len(),
        "partition_index {} >= vanilla_proofs.len() {}",
        partition_index,
        vanilla_proofs.len(),
    );

    let partition_vanilla = &vanilla_proofs[partition_index];

    // StackedCircuit::ComponentPrivateInputs = () (unit type)
    let circuit = <StackedCompound<Tree, DefaultPieceHasher> as CompoundProof<
        StackedDrg<'_, Tree, DefaultPieceHasher>,
        _,
    >>::circuit(
        &public_inputs,
        Default::default(), // ComponentPrivateInputs = ()
        partition_vanilla,
        &compound_public_params.vanilla_params,
        Some(partition_index),
    )?;

    debug!(partition = partition_index, "built circuit for partition");

    // ── Step 7: Synthesize (CPU phase) ─────────────────────────────────
    //
    // This runs `circuit.synthesize(&mut prover)` which generates the
    // a/b/c constraint evaluations and witness assignments. CPU-intensive,
    // uses rayon for parallel constraint evaluation.

    let synth_start = Instant::now();

    let (_start, provers, input_assignments, aux_assignments) =
        synthesize_circuits_batch(vec![circuit])?;

    let synthesis_duration = synth_start.elapsed();

    info!(
        partition = partition_index,
        synth_ms = synthesis_duration.as_millis(),
        num_constraints = provers[0].a.len(),
        "partition synthesis complete"
    );

    // ── Step 8: Generate randomization values ──────────────────────────
    //
    // Each circuit gets random r and s for zero-knowledge. We generate
    // them here and carry them through to the GPU phase.
    let mut rng = rand_core::OsRng;
    let r_s = vec![Fr::random(&mut rng)];
    let s_s = vec![Fr::random(&mut rng)];

    Ok(SynthesizedProof {
        circuit_id: CircuitId::Porep32G,
        provers,
        input_assignments,
        aux_assignments,
        r_s,
        s_s,
        synthesis_duration,
        partition_index: Some(partition_index),
        total_partitions: num_partitions,
    })
}

// ═══════════════════════════════════════════════════════════════════════════
// GPU Prove (GPU phase)
// ═══════════════════════════════════════════════════════════════════════════

/// Execute the GPU phase of proving on a pre-synthesized witness.
///
/// Takes a `SynthesizedProof` (output of synthesis) and SRS parameters,
/// runs NTT + MSM on the GPU via the SupraSeal C++ backend, and returns
/// the raw Groth16 proof bytes.
///
/// # Arguments
/// * `synth` — Synthesized proof state (from `synthesize_porep_c2_partition` etc.)
/// * `params` — SupraSeal SRS parameters for the circuit type
#[cfg(feature = "cuda-supraseal")]
pub fn gpu_prove(
    synth: SynthesizedProof,
    params: &SuprasealParameters<Bls12>,
) -> Result<GpuProveResult> {
    let _span = info_span!("gpu_prove",
        circuit_id = %synth.circuit_id,
        partition = ?synth.partition_index,
    )
    .entered();

    let gpu_start = Instant::now();

    let proofs: Vec<Proof<Bls12>> = prove_from_assignments(
        synth.provers,
        synth.input_assignments,
        synth.aux_assignments,
        params,
        synth.r_s,
        synth.s_s,
    )
    .map_err(|e| anyhow::anyhow!("GPU prove failed: {:?}", e))?;

    let gpu_duration = gpu_start.elapsed();

    // Serialize proofs to bytes (same format as MultiProof::write)
    let mut proof_bytes = Vec::with_capacity(proofs.len() * GROTH_PROOF_BYTES);
    for proof in &proofs {
        proof
            .write(&mut proof_bytes)
            .context("failed to serialize groth16 proof")?;
    }

    info!(
        proof_count = proofs.len(),
        proof_bytes = proof_bytes.len(),
        gpu_ms = gpu_duration.as_millis(),
        "GPU prove complete"
    );

    Ok(GpuProveResult {
        proof_bytes,
        gpu_duration,
        partition_index: synth.partition_index,
    })
}

// ═══════════════════════════════════════════════════════════════════════════
// Full pipelined PoRep C2 (all partitions, sequential for now)
// ═══════════════════════════════════════════════════════════════════════════

/// Execute a full PoRep C2 proof using the pipelined synthesis → GPU approach.
///
/// Iterates over all partitions, synthesizing each one and proving it on the GPU.
/// In Phase 2, this is called from a single worker — true overlap (synthesis of
/// partition N+1 while GPU proves partition N) is handled by the engine's pipeline
/// scheduler.
///
/// This function is the pipelined equivalent of `prover::prove_porep_c2()`.
///
/// Returns the concatenated proof bytes (10 × 192 = 1920 bytes for 32G).
#[cfg(feature = "cuda-supraseal")]
pub fn prove_porep_c2_pipelined(
    vanilla_proof_json: &[u8],
    sector_number: u64,
    miner_id: u64,
    params: &SuprasealParameters<Bls12>,
    job_id: &str,
) -> Result<(Vec<u8>, PipelinedTimings)> {
    let _span = info_span!("prove_porep_c2_pipelined", job_id = job_id).entered();
    let total_start = Instant::now();

    // First, determine the number of partitions by parsing the config from the C1 output.
    // We parse the wrapper once to get registered_proof, then pass the raw JSON to each
    // partition synthesis call (which will re-parse — acceptable overhead since deser is <200ms
    // total and we avoid holding the full deserialized C1 output in memory).
    let wrapper: C1OutputWrapper = serde_json::from_slice(vanilla_proof_json)
        .context("failed to parse C1 output wrapper JSON")?;
    let phase1_json_bytes = base64::Engine::decode(
        &base64::engine::general_purpose::STANDARD,
        &wrapper.phase1_out,
    )
    .context("failed to decode base64 Phase1Output")?;
    let c1_output: SealCommitPhase1Output = serde_json::from_slice(&phase1_json_bytes)
        .context("failed to deserialize SealCommitPhase1Output")?;
    let porep_config = c1_output.registered_proof.as_v1_config();
    let num_partitions = usize::from(porep_config.partitions);
    drop(c1_output);

    info!(
        sector = sector_number,
        num_partitions = num_partitions,
        "starting pipelined PoRep C2"
    );

    let mut all_proof_bytes = Vec::with_capacity(num_partitions * GROTH_PROOF_BYTES);
    let mut total_synth_duration = Duration::ZERO;
    let mut total_gpu_duration = Duration::ZERO;

    for partition in 0..num_partitions {
        // Synthesis phase (CPU)
        let synth = synthesize_porep_c2_partition(
            vanilla_proof_json,
            sector_number,
            miner_id,
            partition,
            job_id,
        )?;
        total_synth_duration += synth.synthesis_duration;

        // GPU phase
        let gpu_result = gpu_prove(synth, params)?;
        total_gpu_duration += gpu_result.gpu_duration;

        all_proof_bytes.extend_from_slice(&gpu_result.proof_bytes);
    }

    let timings = PipelinedTimings {
        synthesis: total_synth_duration,
        gpu_compute: total_gpu_duration,
        total: total_start.elapsed(),
    };

    info!(
        proof_len = all_proof_bytes.len(),
        synth_ms = timings.synthesis.as_millis(),
        gpu_ms = timings.gpu_compute.as_millis(),
        total_ms = timings.total.as_millis(),
        "pipelined PoRep C2 complete"
    );

    Ok((all_proof_bytes, timings))
}

/// Timing breakdown for pipelined proving.
#[derive(Debug, Clone, Default)]
pub struct PipelinedTimings {
    /// Total time spent in CPU synthesis across all partitions.
    pub synthesis: Duration,
    /// Total time spent in GPU compute across all partitions.
    pub gpu_compute: Duration,
    /// Wall-clock total.
    pub total: Duration,
}

// ═══════════════════════════════════════════════════════════════════════════
// Stub implementations for non-CUDA builds
// ═══════════════════════════════════════════════════════════════════════════

#[cfg(not(feature = "cuda-supraseal"))]
pub fn synthesize_porep_c2_partition(
    _vanilla_proof_json: &[u8],
    _sector_number: u64,
    _miner_id: u64,
    _partition_index: usize,
    _job_id: &str,
) -> Result<SynthesizedProof> {
    anyhow::bail!("pipelined synthesis requires cuda-supraseal feature")
}

#[cfg(not(feature = "cuda-supraseal"))]
pub fn prove_porep_c2_pipelined(
    _vanilla_proof_json: &[u8],
    _sector_number: u64,
    _miner_id: u64,
    _job_id: &str,
) -> Result<(Vec<u8>, PipelinedTimings)> {
    anyhow::bail!("pipelined proving requires cuda-supraseal feature")
}

// ═══════════════════════════════════════════════════════════════════════════
// Tests
// ═══════════════════════════════════════════════════════════════════════════

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_pipelined_timings_default() {
        let t = PipelinedTimings::default();
        assert_eq!(t.synthesis, Duration::ZERO);
        assert_eq!(t.gpu_compute, Duration::ZERO);
        assert_eq!(t.total, Duration::ZERO);
    }

    #[test]
    fn test_gpu_prove_result_size() {
        // A 32G PoRep partition should produce exactly 192 bytes
        assert_eq!(GROTH_PROOF_BYTES, 192);
    }

    #[test]
    fn test_synthesized_proof_stub() {
        // Verify the stub struct compiles and can be constructed
        let _sp = SynthesizedProof {
            circuit_id: CircuitId::Porep32G,
            synthesis_duration: Duration::from_secs(1),
            partition_index: Some(0),
            total_partitions: 10,
            #[cfg(feature = "cuda-supraseal")]
            provers: vec![],
            #[cfg(feature = "cuda-supraseal")]
            input_assignments: vec![],
            #[cfg(feature = "cuda-supraseal")]
            aux_assignments: vec![],
            #[cfg(feature = "cuda-supraseal")]
            r_s: vec![],
            #[cfg(feature = "cuda-supraseal")]
            s_s: vec![],
        };
    }
}
