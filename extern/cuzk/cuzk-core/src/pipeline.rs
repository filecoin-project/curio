//! Pipelined synthesis/GPU proving engine (Phase 2).
//!
//! Splits the monolithic proving functions into two phases:
//!
//! 1. **Synthesis** (CPU-bound): Builds circuits from vanilla proofs, runs
//!    `bellperson::synthesize_circuits_batch()` to produce intermediate state
//!    (a/b/c evaluations + density trackers + witness assignments).
//!
//! 2. **GPU Prove** (GPU-bound): Takes the synthesized state and SRS, runs
//!    `bellperson::prove_from_assignments()` for NTT + MSM on the GPU.
//!
//! Supported proof types:
//! - PoRep C2 (batch all partitions — matches monolithic performance)
//! - WinningPoSt (single partition)
//! - WindowPoSt (single partition per call, as sent by Curio)
//! - SnapDeals (all partitions batched)
//!
//! The pipeline allows CPU synthesis for job N+1 to overlap with GPU proving
//! for job N, achieving ~1.5x throughput when there's a continuous workload.

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
    prove_from_assignments, synthesize_circuits_batch_with_hint, Proof, ProvingAssignment,
    SuprasealParameters, SynthesisCapacityHint,
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

// PoSt circuit construction
#[cfg(feature = "cuda-supraseal")]
use filecoin_proofs::parameters::{window_post_setup_params, winning_post_setup_params};
#[cfg(feature = "cuda-supraseal")]
use filecoin_proofs::FallbackPoStSectorProof;
#[cfg(feature = "cuda-supraseal")]
use storage_proofs_post::fallback::{self as fallback_vanilla, FallbackPoSt, FallbackPoStCompound};

// SnapDeals circuit construction
#[cfg(feature = "cuda-supraseal")]
use filecoin_proofs::types::SectorUpdateConfig;
#[cfg(feature = "cuda-supraseal")]
use storage_proofs_update::{
    EmptySectorUpdate, EmptySectorUpdateCompound, PartitionProof as UpdatePartitionProof,
    PublicInputs as UpdatePublicInputs,
};

// Common types
#[cfg(feature = "cuda-supraseal")]
use filecoin_hashers::Hasher;

// ─── Constants ──────────────────────────────────────────────────────────────

/// Number of Groth16 proof bytes per partition (2 × G1 + 1 × G2 compressed).
const GROTH_PROOF_BYTES: usize = 192;

// ─── Capacity Hint Cache ────────────────────────────────────────────────────
//
// After the first synthesis for a circuit type, we cache the actual Vec sizes
// (num_constraints, num_aux, num_inputs) so subsequent syntheses can pre-allocate
// to full capacity. This eliminates ~27 reallocation cycles per Vec and ~250 GB
// of redundant memcpy for 10-circuit 32 GiB PoRep C2 batches.

#[cfg(feature = "cuda-supraseal")]
use std::sync::OnceLock;

/// Cached hint for PoRep 32G circuits (~130M constraints, ~23M aux, ~1 input).
#[cfg(feature = "cuda-supraseal")]
static POREP_32G_HINT: OnceLock<SynthesisCapacityHint> = OnceLock::new();

/// Cached hint for WinningPoSt circuits (~3.5M constraints).
#[cfg(feature = "cuda-supraseal")]
static WINNING_POST_HINT: OnceLock<SynthesisCapacityHint> = OnceLock::new();

/// Cached hint for WindowPoSt circuits (~125M constraints).
#[cfg(feature = "cuda-supraseal")]
static WINDOW_POST_HINT: OnceLock<SynthesisCapacityHint> = OnceLock::new();

/// Cached hint for SnapDeals circuits (~81M constraints).
#[cfg(feature = "cuda-supraseal")]
static SNAP_DEALS_HINT: OnceLock<SynthesisCapacityHint> = OnceLock::new();

/// Get the cached hint for a circuit type, if available.
#[cfg(feature = "cuda-supraseal")]
fn get_hint(circuit_id: &CircuitId) -> Option<SynthesisCapacityHint> {
    match circuit_id {
        CircuitId::Porep32G | CircuitId::Porep64G => POREP_32G_HINT.get().copied(),
        CircuitId::WinningPost32G => WINNING_POST_HINT.get().copied(),
        CircuitId::WindowPost32G => WINDOW_POST_HINT.get().copied(),
        CircuitId::SnapDeals32G | CircuitId::SnapDeals64G => SNAP_DEALS_HINT.get().copied(),
    }
}

/// Cache the capacity hint after a successful synthesis.
///
/// Called after `synthesize_circuits_batch_with_hint` returns successfully.
/// On the first call, the `aux_assignment` and `input_assignment` have already
/// been moved out via `std::mem::take()`, so we get sizes from the `a` Vec
/// (constraints) and the DensityTracker BitVec lengths (which match the original
/// aux/input counts, since each `alloc()` pushes one bit).
#[cfg(feature = "cuda-supraseal")]
fn cache_hint(circuit_id: &CircuitId, provers: &[ProvingAssignment<Fr>]) {
    if provers.is_empty() {
        return;
    }
    // a_aux_density.bv has exactly one bit per aux variable (pushed in alloc())
    // b_input_density.bv has exactly one bit per input variable (pushed in alloc_input())
    let hint = SynthesisCapacityHint {
        num_constraints: provers[0].a.len(),
        num_aux: provers[0].a_aux_density.bv.len(),
        num_inputs: provers[0].b_input_density.bv.len(),
    };
    let lock = match circuit_id {
        CircuitId::Porep32G | CircuitId::Porep64G => &POREP_32G_HINT,
        CircuitId::WinningPost32G => &WINNING_POST_HINT,
        CircuitId::WindowPost32G => &WINDOW_POST_HINT,
        CircuitId::SnapDeals32G | CircuitId::SnapDeals64G => &SNAP_DEALS_HINT,
    };
    let _ = lock.set(hint); // ignore if already set
    info!(
        circuit_id = %circuit_id,
        num_constraints = hint.num_constraints,
        num_aux = hint.num_aux,
        num_inputs = hint.num_inputs,
        "cached synthesis capacity hint"
    );
}

/// Synthesize circuits with automatic capacity hinting.
///
/// Uses cached hints from previous runs (if available) to pre-allocate Vecs
/// to full capacity, eliminating ~27 reallocation cycles per Vec.
/// After the first synthesis (no hint), caches the actual sizes for next time.
#[cfg(feature = "cuda-supraseal")]
fn synthesize_with_hint<C>(
    circuits: Vec<C>,
    circuit_id: &CircuitId,
) -> Result<
    (
        Instant,
        Vec<ProvingAssignment<Fr>>,
        Vec<Arc<Vec<Fr>>>,
        Vec<Arc<Vec<Fr>>>,
    ),
    bellperson::SynthesisError,
>
where
    C: bellperson::Circuit<Fr> + Send,
{
    let hint = get_hint(circuit_id);
    if hint.is_some() {
        info!(circuit_id = %circuit_id, "using cached capacity hint for synthesis");
    } else {
        info!(circuit_id = %circuit_id, "no capacity hint cached — first synthesis will grow organically");
    }

    let (start, provers, input_assignments, aux_assignments) =
        synthesize_circuits_batch_with_hint(circuits, hint)?;

    // Cache hint for subsequent syntheses
    cache_hint(circuit_id, &provers);

    Ok((start, provers, input_assignments, aux_assignments))
}

// ═══════════════════════════════════════════════════════════════════════════
// Types
// ═══════════════════════════════════════════════════════════════════════════

/// Intermediate state between synthesis (CPU) and GPU proving.
///
/// This struct holds the output of `bellperson::synthesize_circuits_batch()`:
/// the a/b/c constraint evaluations, density trackers, and witness assignments.
/// It is produced by the synthesis phase and consumed by the GPU phase.
#[cfg(feature = "cuda-supraseal")]
pub struct SynthesizedProof {
    /// Circuit type (determines which SRS to use).
    pub circuit_id: CircuitId,

    /// Bellperson proving assignments (a/b/c evaluations + density trackers).
    /// One per circuit in the batch.
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
    /// None for batch synthesis of all partitions.
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
// GPU Prove (common GPU phase for all proof types)
// ═══════════════════════════════════════════════════════════════════════════

/// Execute the GPU phase of proving on a pre-synthesized witness.
///
/// Takes a `SynthesizedProof` (output of any synthesis function) and SRS
/// parameters, runs NTT + MSM on the GPU via the SupraSeal C++ backend,
/// and returns the raw Groth16 proof bytes.
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
// PoRep C2 Multi-Sector Synthesis (Phase 3 — cross-sector batching)
// ═══════════════════════════════════════════════════════════════════════════

/// Synthesize PoRep C2 circuits for MULTIPLE sectors in a single batch.
///
/// This is the Phase 3 cross-sector batching function. It takes N sectors'
/// C1 outputs, constructs all N×10 = N×num_partitions circuits, and
/// synthesizes them in a single `synthesize_circuits_batch()` call. The
/// resulting `SynthesizedProof` can then be passed to `gpu_prove()` for
/// a single GPU pass over all N sectors.
///
/// After GPU proving, the caller splits the resulting proof bytes back
/// into per-sector groups (each sector gets `num_partitions × 192` bytes).
///
/// ## Why This Works
///
/// All 32 GiB PoRep sectors share the same R1CS structure, constraints,
/// and SRS. The supraseal C++ code's `generate_groth16_proofs_c()` already
/// handles variable `num_circuits` — the `provers[]` array, bit vectors,
/// and MSM loops are all parameterized by circuit count.
///
/// ## Memory Analysis
///
/// With batch synthesis (all partitions of all sectors at once):
///   - 1 sector (10 circuits): ~136 GiB intermediate
///   - 2 sectors (20 circuits): ~272 GiB intermediate
///   - 3 sectors (30 circuits): ~408 GiB intermediate
///
/// This requires machines with sufficient RAM. The engine's batch_collector
/// caps max_batch_size to prevent OOM.
///
/// ## Arguments
///
/// * `sector_c1_outputs` — Vec of (vanilla_proof_json, sector_number, miner_id)
///   tuples, one per sector to batch.
/// * `job_id` — Job ID for tracing (represents the batch, not individual sectors).
///
/// ## Returns
///
/// A `SynthesizedProof` containing all N×num_partitions circuits.
/// `total_partitions` is set to `N × num_partitions_per_sector`.
/// `partition_index` is None (batch of everything).
#[cfg(feature = "cuda-supraseal")]
pub fn synthesize_porep_c2_multi(
    sector_c1_outputs: &[(&[u8], u64, u64)], // (vanilla_proof_json, sector_number, miner_id)
    job_id: &str,
) -> Result<(SynthesizedProof, Vec<usize>)> {
    let _span = info_span!(
        "synthesize_porep_c2_multi",
        job_id = job_id,
        num_sectors = sector_c1_outputs.len(),
    )
    .entered();

    let num_sectors = sector_c1_outputs.len();
    anyhow::ensure!(
        num_sectors > 0,
        "empty sector list for multi-sector synthesis"
    );

    info!(
        num_sectors = num_sectors,
        "starting multi-sector PoRep C2 synthesis"
    );

    // Deserialize all sectors' C1 outputs and build all circuits.
    // We track partition boundaries so the caller can split proofs back.
    let mut all_circuits = Vec::new();
    let mut sector_boundaries = Vec::with_capacity(num_sectors); // num_partitions per sector

    for (i, (vanilla_proof_json, _sector_number, _miner_id)) in sector_c1_outputs.iter().enumerate()
    {
        let _sector_span = info_span!("sector_deser", sector_idx = i).entered();

        // Deserialize C1 output
        let wrapper: C1OutputWrapper = serde_json::from_slice(vanilla_proof_json)
            .context("failed to parse C1 output wrapper JSON")?;
        let phase1_json_bytes = base64::Engine::decode(
            &base64::engine::general_purpose::STANDARD,
            &wrapper.phase1_out,
        )
        .context("failed to decode base64 Phase1Output")?;
        let c1_output: SealCommitPhase1Output = serde_json::from_slice(&phase1_json_bytes)
            .context("failed to deserialize SealCommitPhase1Output from JSON")?;

        let porep_config = c1_output.registered_proof.as_v1_config();
        let num_partitions = usize::from(porep_config.partitions);

        let sector_size = u64::from(porep_config.sector_size);
        anyhow::ensure!(
            sector_size == SECTOR_SIZE_32_GIB,
            "multi-sector synthesis currently supports only 32 GiB sectors, got {} bytes",
            sector_size,
        );

        type Tree = SectorShape32GiB;

        let vanilla_proofs: Vec<
            Vec<storage_proofs_porep::stacked::Proof<Tree, DefaultPieceHasher>>,
        > = c1_output
            .vanilla_proofs
            .try_into()
            .map_err(|e| anyhow::anyhow!("failed to convert vanilla proofs: {:?}", e))?;

        let comm_r_safe = as_safe_commitment(&c1_output.comm_r, "comm_r")?;
        let comm_d_safe: DefaultPieceDomain = as_safe_commitment(&c1_output.comm_d, "comm_d")?;

        anyhow::ensure!(
            c1_output.comm_d != [0; 32],
            "Invalid all zero commitment (comm_d)"
        );
        anyhow::ensure!(
            c1_output.comm_r != [0; 32],
            "Invalid all zero commitment (comm_r)"
        );
        anyhow::ensure!(c1_output.seed != [0; 32], "Invalid porep challenge seed");

        use storage_proofs_porep::stacked::{PublicInputs, Tau};
        let public_inputs = PublicInputs {
            replica_id: c1_output.replica_id,
            tau: Some(Tau {
                comm_d: comm_d_safe,
                comm_r: comm_r_safe,
            }),
            k: None,
            seed: Some(c1_output.seed),
        };

        use storage_proofs_core::compound_proof::SetupParams;
        let vanilla_setup = setup_params(&porep_config)?;
        let compound_setup = SetupParams {
            vanilla_params: vanilla_setup,
            partitions: Some(num_partitions),
            priority: false,
        };
        let compound_public_params =
            <StackedCompound<Tree, DefaultPieceHasher> as CompoundProof<
                StackedDrg<'_, Tree, DefaultPieceHasher>,
                _,
            >>::setup(&compound_setup)?;

        // Build all partition circuits for this sector
        for k in 0..num_partitions {
            anyhow::ensure!(
                k < vanilla_proofs.len(),
                "partition {} >= vanilla_proofs.len() {}",
                k,
                vanilla_proofs.len(),
            );
            let circuit = <StackedCompound<Tree, DefaultPieceHasher> as CompoundProof<
                StackedDrg<'_, Tree, DefaultPieceHasher>,
                _,
            >>::circuit(
                &public_inputs,
                Default::default(),
                &vanilla_proofs[k],
                &compound_public_params.vanilla_params,
                Some(k),
            )?;
            all_circuits.push(circuit);
        }

        sector_boundaries.push(num_partitions);

        debug!(
            sector_idx = i,
            num_partitions = num_partitions,
            total_circuits = all_circuits.len(),
            "sector circuits built"
        );
    }

    let total_circuits = all_circuits.len();
    let total_partitions = total_circuits; // all partitions across all sectors
    info!(
        num_sectors = num_sectors,
        total_circuits = total_circuits,
        "synthesizing all circuits (multi-sector batch)"
    );

    let synth_start = Instant::now();
    let (_start, provers, input_assignments, aux_assignments) =
        synthesize_with_hint(all_circuits, &CircuitId::Porep32G)?;
    let synthesis_duration = synth_start.elapsed();

    info!(
        synth_ms = synthesis_duration.as_millis(),
        num_circuits = provers.len(),
        num_constraints = provers[0].a.len(),
        "multi-sector batch synthesis complete"
    );

    // Generate r/s randomization for each circuit
    let mut rng = rand_core::OsRng;
    let r_s: Vec<Fr> = (0..total_circuits).map(|_| Fr::random(&mut rng)).collect();
    let s_s: Vec<Fr> = (0..total_circuits).map(|_| Fr::random(&mut rng)).collect();

    let synth = SynthesizedProof {
        circuit_id: CircuitId::Porep32G,
        provers,
        input_assignments,
        aux_assignments,
        r_s,
        s_s,
        synthesis_duration,
        partition_index: None, // batch — all partitions of all sectors
        total_partitions,
    };

    Ok((synth, sector_boundaries))
}

/// Split batched GPU proof output into per-sector proof byte vectors.
///
/// After `gpu_prove()` returns concatenated proof bytes for N×P circuits
/// (N sectors × P partitions), this function splits them back into
/// per-sector groups.
///
/// # Arguments
///
/// * `proof_bytes` — Concatenated Groth16 proof bytes (N×P × 192 bytes).
/// * `sector_boundaries` — Number of partitions per sector (from `synthesize_porep_c2_multi`).
///
/// # Returns
///
/// Vec of per-sector proof byte vectors, each containing P × 192 bytes.
pub fn split_batched_proofs(
    proof_bytes: &[u8],
    sector_boundaries: &[usize],
) -> Result<Vec<Vec<u8>>> {
    let total_partitions: usize = sector_boundaries.iter().sum();
    let expected_len = total_partitions * GROTH_PROOF_BYTES;
    anyhow::ensure!(
        proof_bytes.len() == expected_len,
        "proof bytes length mismatch: got {}, expected {} ({} partitions × {} bytes)",
        proof_bytes.len(),
        expected_len,
        total_partitions,
        GROTH_PROOF_BYTES,
    );

    let mut results = Vec::with_capacity(sector_boundaries.len());
    let mut offset = 0;

    for &num_partitions in sector_boundaries {
        let sector_proof_len = num_partitions * GROTH_PROOF_BYTES;
        results.push(proof_bytes[offset..offset + sector_proof_len].to_vec());
        offset += sector_proof_len;
    }

    Ok(results)
}

/// Non-CUDA stub for multi-sector synthesis.
#[cfg(not(feature = "cuda-supraseal"))]
pub fn synthesize_porep_c2_multi(
    _sector_c1_outputs: &[(&[u8], u64, u64)],
    _job_id: &str,
) -> Result<(SynthesizedProof, Vec<usize>)> {
    anyhow::bail!("multi-sector synthesis requires cuda-supraseal feature")
}

// ═══════════════════════════════════════════════════════════════════════════
// PoRep C2 Synthesis (CPU phase) — batch all partitions (single sector)
// ═══════════════════════════════════════════════════════════════════════════

/// Synthesize a single partition of a PoRep C2 proof (CPU-only).
///
/// Builds ONE partition circuit (~13.6 GiB). For per-partition pipelining.
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

    // Deserialize C1 output
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

    // Build PoRep config
    let porep_config = c1_output.registered_proof.as_v1_config();
    let num_partitions = usize::from(porep_config.partitions);
    anyhow::ensure!(
        partition_index < num_partitions,
        "partition_index {} >= num_partitions {}",
        partition_index,
        num_partitions,
    );

    let sector_size = u64::from(porep_config.sector_size);
    anyhow::ensure!(
        sector_size == SECTOR_SIZE_32_GIB,
        "pipelined synthesis currently supports only 32 GiB sectors, got {} bytes",
        sector_size,
    );

    type Tree = SectorShape32GiB;

    let vanilla_proofs: Vec<Vec<storage_proofs_porep::stacked::Proof<Tree, DefaultPieceHasher>>> =
        c1_output
            .vanilla_proofs
            .try_into()
            .map_err(|e| anyhow::anyhow!("failed to convert vanilla proofs: {:?}", e))?;

    let comm_r_safe = as_safe_commitment(&c1_output.comm_r, "comm_r")?;
    let comm_d_safe: DefaultPieceDomain = as_safe_commitment(&c1_output.comm_d, "comm_d")?;

    anyhow::ensure!(
        c1_output.comm_d != [0; 32],
        "Invalid all zero commitment (comm_d)"
    );
    anyhow::ensure!(
        c1_output.comm_r != [0; 32],
        "Invalid all zero commitment (comm_r)"
    );
    anyhow::ensure!(c1_output.seed != [0; 32], "Invalid porep challenge seed");

    use storage_proofs_porep::stacked::{PublicInputs, Tau};
    let public_inputs = PublicInputs {
        replica_id: c1_output.replica_id,
        tau: Some(Tau {
            comm_d: comm_d_safe,
            comm_r: comm_r_safe,
        }),
        k: None,
        seed: Some(c1_output.seed),
    };

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

    anyhow::ensure!(
        partition_index < vanilla_proofs.len(),
        "partition_index {} >= vanilla_proofs.len() {}",
        partition_index,
        vanilla_proofs.len(),
    );

    let circuit = <StackedCompound<Tree, DefaultPieceHasher> as CompoundProof<
        StackedDrg<'_, Tree, DefaultPieceHasher>,
        _,
    >>::circuit(
        &public_inputs,
        Default::default(),
        &vanilla_proofs[partition_index],
        &compound_public_params.vanilla_params,
        Some(partition_index),
    )?;

    let synth_start = Instant::now();
    let (_start, provers, input_assignments, aux_assignments) =
        synthesize_with_hint(vec![circuit], &CircuitId::Porep32G)?;
    let synthesis_duration = synth_start.elapsed();

    info!(
        partition = partition_index,
        synth_ms = synthesis_duration.as_millis(),
        num_constraints = provers[0].a.len(),
        "partition synthesis complete"
    );

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

/// Synthesize ALL partitions of a PoRep C2 proof in a single rayon-parallel batch.
///
/// This matches the monolithic `CompoundProof::circuit_proofs()` approach: builds
/// all 10 partition circuits in parallel via rayon, then synthesizes them all in
/// one `synthesize_circuits_batch()` call. The resulting `SynthesizedProof` holds
/// all 10 partitions (intermediate state ~136 GiB for 32G — requires 256+ GiB RAM).
///
/// For machines with >= 256 GiB RAM, this is faster than per-partition synthesis
/// because rayon parallelizes across all 10 circuits simultaneously.
#[cfg(feature = "cuda-supraseal")]
pub fn synthesize_porep_c2_batch(
    vanilla_proof_json: &[u8],
    _sector_number: u64,
    _miner_id: u64,
    job_id: &str,
) -> Result<SynthesizedProof> {
    let _span = info_span!("synthesize_porep_c2_batch", job_id = job_id).entered();

    // Deserialize C1 output
    let wrapper: C1OutputWrapper = serde_json::from_slice(vanilla_proof_json)
        .context("failed to parse C1 output wrapper JSON")?;
    let phase1_json_bytes = base64::Engine::decode(
        &base64::engine::general_purpose::STANDARD,
        &wrapper.phase1_out,
    )
    .context("failed to decode base64 Phase1Output")?;
    let c1_output: SealCommitPhase1Output = serde_json::from_slice(&phase1_json_bytes)
        .context("failed to deserialize SealCommitPhase1Output from JSON")?;

    let porep_config = c1_output.registered_proof.as_v1_config();
    let num_partitions = usize::from(porep_config.partitions);

    let sector_size = u64::from(porep_config.sector_size);
    anyhow::ensure!(
        sector_size == SECTOR_SIZE_32_GIB,
        "pipelined synthesis currently supports only 32 GiB sectors, got {} bytes",
        sector_size,
    );

    type Tree = SectorShape32GiB;

    let vanilla_proofs: Vec<Vec<storage_proofs_porep::stacked::Proof<Tree, DefaultPieceHasher>>> =
        c1_output
            .vanilla_proofs
            .try_into()
            .map_err(|e| anyhow::anyhow!("failed to convert vanilla proofs: {:?}", e))?;

    let comm_r_safe = as_safe_commitment(&c1_output.comm_r, "comm_r")?;
    let comm_d_safe: DefaultPieceDomain = as_safe_commitment(&c1_output.comm_d, "comm_d")?;

    anyhow::ensure!(
        c1_output.comm_d != [0; 32],
        "Invalid all zero commitment (comm_d)"
    );
    anyhow::ensure!(
        c1_output.comm_r != [0; 32],
        "Invalid all zero commitment (comm_r)"
    );
    anyhow::ensure!(c1_output.seed != [0; 32], "Invalid porep challenge seed");

    use storage_proofs_porep::stacked::{PublicInputs, Tau};
    let public_inputs = PublicInputs {
        replica_id: c1_output.replica_id,
        tau: Some(Tau {
            comm_d: comm_d_safe,
            comm_r: comm_r_safe,
        }),
        k: None,
        seed: Some(c1_output.seed),
    };

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

    // Build ALL partition circuits in parallel (rayon), matching circuit_proofs()
    info!(
        num_partitions = num_partitions,
        "building circuits for all partitions (parallel)"
    );
    let circuits: Vec<_> = (0..num_partitions)
        .into_iter()
        .map(|k| {
            anyhow::ensure!(
                k < vanilla_proofs.len(),
                "partition {} >= vanilla_proofs.len() {}",
                k,
                vanilla_proofs.len(),
            );
            let circuit = <StackedCompound<Tree, DefaultPieceHasher> as CompoundProof<
                StackedDrg<'_, Tree, DefaultPieceHasher>,
                _,
            >>::circuit(
                &public_inputs,
                Default::default(),
                &vanilla_proofs[k],
                &compound_public_params.vanilla_params,
                Some(k),
            )?;
            Ok(circuit)
        })
        .collect::<Result<Vec<_>>>()?;

    info!(num_circuits = circuits.len(), "synthesizing all circuits");
    let synth_start = Instant::now();
    let (_start, provers, input_assignments, aux_assignments) =
        synthesize_with_hint(circuits, &CircuitId::Porep32G)?;
    let synthesis_duration = synth_start.elapsed();

    info!(
        synth_ms = synthesis_duration.as_millis(),
        num_circuits = provers.len(),
        num_constraints = provers[0].a.len(),
        "batch synthesis complete"
    );

    // Generate r/s randomization for each partition
    let mut rng = rand_core::OsRng;
    let r_s: Vec<Fr> = (0..num_partitions).map(|_| Fr::random(&mut rng)).collect();
    let s_s: Vec<Fr> = (0..num_partitions).map(|_| Fr::random(&mut rng)).collect();

    Ok(SynthesizedProof {
        circuit_id: CircuitId::Porep32G,
        provers,
        input_assignments,
        aux_assignments,
        r_s,
        s_s,
        synthesis_duration,
        partition_index: None, // batch — all partitions
        total_partitions: num_partitions,
    })
}

/// Execute a full PoRep C2 proof using the pipelined approach.
///
/// Uses batch synthesis (all partitions at once) for optimal single-proof
/// latency. The synthesis produces all partition circuits in parallel via rayon,
/// then the GPU proves them in a single `prove_from_assignments` call.
///
/// For per-partition pipelining (across multiple proofs), the engine calls
/// `synthesize_porep_c2_partition()` + `gpu_prove()` directly.
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

    info!(
        sector = sector_number,
        "starting pipelined PoRep C2 (batch mode)"
    );

    // Batch synthesis: all partitions in one call
    let synth = synthesize_porep_c2_batch(vanilla_proof_json, sector_number, miner_id, job_id)?;
    let synth_duration = synth.synthesis_duration;

    // GPU prove: all partitions in one call
    let gpu_result = gpu_prove(synth, params)?;

    let timings = PipelinedTimings {
        synthesis: synth_duration,
        gpu_compute: gpu_result.gpu_duration,
        total: total_start.elapsed(),
    };

    info!(
        proof_len = gpu_result.proof_bytes.len(),
        synth_ms = timings.synthesis.as_millis(),
        gpu_ms = timings.gpu_compute.as_millis(),
        total_ms = timings.total.as_millis(),
        "pipelined PoRep C2 complete (batch mode)"
    );

    Ok((gpu_result.proof_bytes, timings))
}

// ═══════════════════════════════════════════════════════════════════════════
// WinningPoSt Synthesis (CPU phase)
// ═══════════════════════════════════════════════════════════════════════════

/// Synthesize a WinningPoSt proof (CPU-only).
///
/// Replicates the circuit construction from `generate_winning_post_with_vanilla`
/// but stops after synthesis. The vanilla proofs are bincode-serialized
/// `FallbackPoStSectorProof<Tree>` (one per challenged sector).
#[cfg(feature = "cuda-supraseal")]
pub fn synthesize_winning_post(
    vanilla_proofs_bytes: &[Vec<u8>],
    registered_proof: u64,
    miner_id: u64,
    randomness: &[u8],
    job_id: &str,
) -> Result<SynthesizedProof> {
    let _span = info_span!("synthesize_winning_post", job_id = job_id).entered();

    use crate::prover::{make_prover_id, registered_post_proof_from_u64, to_array32};

    let post_proof_type = registered_post_proof_from_u64(registered_proof)
        .context("invalid registered proof for WinningPoSt")?;
    let prover_id = make_prover_id(miner_id);
    let challenge_seed: [u8; 32] = to_array32(randomness, "randomness")?;

    type Tree = SectorShape32GiB;
    let post_config = post_proof_type.as_v1_config();

    // Deserialize vanilla proofs from bincode
    let fallback_proofs: Vec<FallbackPoStSectorProof<Tree>> = vanilla_proofs_bytes
        .iter()
        .map(|bytes| {
            bincode::deserialize(bytes).map_err(|e| {
                anyhow::anyhow!("failed to deserialize WinningPoSt vanilla proof: {}", e)
            })
        })
        .collect::<Result<_>>()?;

    info!(
        num_sectors = fallback_proofs.len(),
        "building WinningPoSt circuit"
    );

    // Build public inputs
    let randomness_safe: <filecoin_hashers::poseidon::PoseidonHasher as Hasher>::Domain =
        as_safe_commitment(&challenge_seed, "randomness")?;
    let prover_id_safe: <filecoin_hashers::poseidon::PoseidonHasher as Hasher>::Domain =
        as_safe_commitment(&prover_id, "prover_id")?;

    let pub_sectors: Vec<fallback_vanilla::PublicSector<_>> = fallback_proofs
        .iter()
        .map(|p| fallback_vanilla::PublicSector {
            id: p.sector_id,
            comm_r: p.comm_r,
        })
        .collect();

    let pub_inputs = fallback_vanilla::PublicInputs {
        randomness: randomness_safe,
        prover_id: prover_id_safe,
        sectors: pub_sectors,
        k: None,
    };

    // Build setup params
    let vanilla_params = winning_post_setup_params(&post_config)?;
    let setup_params = storage_proofs_core::compound_proof::SetupParams {
        vanilla_params,
        partitions: None, // WinningPoSt: single partition
        priority: post_config.priority,
    };
    let pub_params = FallbackPoStCompound::<Tree>::setup(&setup_params)?;

    // Build partitioned vanilla proofs for WinningPoSt.
    //
    // WinningPoSt has a single sector but multiple challenges. The vanilla proof
    // has one SectorProof with N inclusion proofs. We unroll each inclusion proof
    // into its own SectorProof (required by the circuit). The CompoundProof::circuit
    // is called once with k=0 for the single partition.
    let num_sectors_per_chunk = pub_params.vanilla_params.sector_count;
    anyhow::ensure!(!fallback_proofs.is_empty(), "no WinningPoSt vanilla proofs");
    anyhow::ensure!(
        fallback_proofs[0].vanilla_proof.sectors.len() == 1,
        "WinningPoSt expected 1 sector proof, got {}",
        fallback_proofs[0].vanilla_proof.sectors.len()
    );

    let cur_sector_proof = &fallback_proofs[0].vanilla_proof.sectors[0];
    let mut sector_proofs = Vec::with_capacity(post_config.challenge_count);
    for inclusion_proof in cur_sector_proof.inclusion_proofs() {
        sector_proofs.push(storage_proofs_post::fallback::SectorProof {
            inclusion_proofs: vec![inclusion_proof.clone()],
            comm_c: cur_sector_proof.comm_c,
            comm_r_last: cur_sector_proof.comm_r_last,
        });
    }
    // Pad to required sector count
    while sector_proofs.len() < num_sectors_per_chunk {
        sector_proofs.push(sector_proofs[sector_proofs.len() - 1].clone());
    }

    let partition_proof = fallback_vanilla::Proof {
        sectors: sector_proofs,
    };

    // Build circuit for the single partition
    let circuit =
        <FallbackPoStCompound<Tree> as CompoundProof<FallbackPoSt<'_, Tree>, _>>::circuit(
            &pub_inputs,
            Default::default(),
            &partition_proof,
            &pub_params.vanilla_params,
            Some(0),
        )?;
    let circuits = vec![circuit];

    let num_circuits = circuits.len();
    info!(
        num_circuits = num_circuits,
        "synthesizing WinningPoSt circuits"
    );

    let synth_start = Instant::now();
    let (_start, provers, input_assignments, aux_assignments) =
        synthesize_with_hint(circuits, &CircuitId::WinningPost32G)?;
    let synthesis_duration = synth_start.elapsed();

    info!(
        synth_ms = synthesis_duration.as_millis(),
        num_constraints = provers[0].a.len(),
        "WinningPoSt synthesis complete"
    );

    let mut rng = rand_core::OsRng;
    let r_s: Vec<Fr> = (0..num_circuits).map(|_| Fr::random(&mut rng)).collect();
    let s_s: Vec<Fr> = (0..num_circuits).map(|_| Fr::random(&mut rng)).collect();

    Ok(SynthesizedProof {
        circuit_id: CircuitId::WinningPost32G,
        provers,
        input_assignments,
        aux_assignments,
        r_s,
        s_s,
        synthesis_duration,
        partition_index: None,
        total_partitions: 1, // WinningPoSt always has 1 partition
    })
}

/// Full WinningPoSt proof via pipeline (synthesis + GPU).
#[cfg(feature = "cuda-supraseal")]
pub fn prove_winning_post_pipelined(
    vanilla_proofs_bytes: &[Vec<u8>],
    registered_proof: u64,
    miner_id: u64,
    randomness: &[u8],
    params: &SuprasealParameters<Bls12>,
    job_id: &str,
) -> Result<(Vec<u8>, PipelinedTimings)> {
    let total_start = Instant::now();

    let synth = synthesize_winning_post(
        vanilla_proofs_bytes,
        registered_proof,
        miner_id,
        randomness,
        job_id,
    )?;
    let synth_duration = synth.synthesis_duration;

    let gpu_result = gpu_prove(synth, params)?;

    let timings = PipelinedTimings {
        synthesis: synth_duration,
        gpu_compute: gpu_result.gpu_duration,
        total: total_start.elapsed(),
    };

    info!(
        proof_len = gpu_result.proof_bytes.len(),
        synth_ms = timings.synthesis.as_millis(),
        gpu_ms = timings.gpu_compute.as_millis(),
        total_ms = timings.total.as_millis(),
        "pipelined WinningPoSt complete"
    );

    Ok((gpu_result.proof_bytes, timings))
}

// ═══════════════════════════════════════════════════════════════════════════
// WindowPoSt Synthesis (CPU phase)
// ═══════════════════════════════════════════════════════════════════════════

/// Synthesize a single WindowPoSt partition (CPU-only).
///
/// Replicates the circuit construction from `generate_single_window_post_with_vanilla`.
/// Each element of `vanilla_proofs_bytes` is a bincode-serialized
/// `FallbackPoStSectorProof<Tree>` for one sector.
#[cfg(feature = "cuda-supraseal")]
pub fn synthesize_window_post(
    vanilla_proofs_bytes: &[Vec<u8>],
    registered_proof: u64,
    miner_id: u64,
    randomness: &[u8],
    partition_index: u32,
    job_id: &str,
) -> Result<SynthesizedProof> {
    let _span = info_span!(
        "synthesize_window_post",
        job_id = job_id,
        partition = partition_index
    )
    .entered();

    use crate::prover::{make_prover_id, registered_post_proof_from_u64, to_array32};

    let post_proof_type = registered_post_proof_from_u64(registered_proof)
        .context("invalid registered proof for WindowPoSt")?;
    let prover_id = make_prover_id(miner_id);
    let challenge_seed: [u8; 32] = to_array32(randomness, "randomness")?;

    type Tree = SectorShape32GiB;
    let post_config = post_proof_type.as_v1_config();

    // Deserialize vanilla proofs from bincode
    let fallback_proofs: Vec<FallbackPoStSectorProof<Tree>> = vanilla_proofs_bytes
        .iter()
        .map(|bytes| {
            bincode::deserialize(bytes).map_err(|e| {
                anyhow::anyhow!("failed to deserialize WindowPoSt vanilla proof: {}", e)
            })
        })
        .collect::<Result<_>>()?;

    let num_sectors = fallback_proofs.len();
    info!(
        num_sectors = num_sectors,
        partition = partition_index,
        "building WindowPoSt circuit"
    );

    // Build public inputs
    let randomness_safe: <filecoin_hashers::poseidon::PoseidonHasher as Hasher>::Domain =
        as_safe_commitment(&challenge_seed, "randomness")?;
    let prover_id_safe: <filecoin_hashers::poseidon::PoseidonHasher as Hasher>::Domain =
        as_safe_commitment(&prover_id, "prover_id")?;

    let pub_sectors: Vec<fallback_vanilla::PublicSector<_>> = fallback_proofs
        .iter()
        .map(|p| fallback_vanilla::PublicSector {
            id: p.sector_id,
            comm_r: p.comm_r,
        })
        .collect();

    let pub_inputs = fallback_vanilla::PublicInputs {
        randomness: randomness_safe,
        prover_id: prover_id_safe,
        sectors: pub_sectors,
        k: Some(partition_index as usize),
    };

    // Build setup params — window post uses per-partition approach
    let vanilla_params = window_post_setup_params(&post_config);
    let partitions = {
        let p = (num_sectors as f32 / post_config.sector_count as f32).ceil() as usize;
        if p > 1 {
            Some(p)
        } else {
            None
        }
    };
    let setup_params = storage_proofs_core::compound_proof::SetupParams {
        vanilla_params,
        partitions,
        priority: post_config.priority,
    };
    let pub_params = FallbackPoStCompound::<Tree>::setup(&setup_params)?;

    // Build single partition vanilla proof.
    //
    // For WindowPoSt, we find sector proofs by sector ID from the flat list,
    // matching the logic of `single_partition_vanilla_proofs` in filecoin-proofs.
    let num_sectors_per_chunk = pub_params.vanilla_params.sector_count;
    let sectors_chunk = &pub_inputs.sectors;

    let mut sector_proofs = Vec::with_capacity(num_sectors_per_chunk);
    for pub_sector in sectors_chunk.iter() {
        let cur_proof = fallback_proofs
            .iter()
            .find(|proof| proof.sector_id == pub_sector.id)
            .ok_or_else(|| {
                anyhow::anyhow!("failed to find vanilla proof for sector {}", pub_sector.id)
            })?;
        sector_proofs.extend(cur_proof.vanilla_proof.sectors.clone());
    }
    // Pad to required sector count
    while sector_proofs.len() < num_sectors_per_chunk {
        sector_proofs.push(sector_proofs[sector_proofs.len() - 1].clone());
    }

    let partitioned_proof = fallback_vanilla::Proof {
        sectors: sector_proofs,
    };

    // Build circuit for this single partition
    let circuit =
        <FallbackPoStCompound<Tree> as CompoundProof<FallbackPoSt<'_, Tree>, _>>::circuit(
            &pub_inputs,
            Default::default(),
            &partitioned_proof,
            &pub_params.vanilla_params,
            Some(partition_index as usize),
        )?;

    info!("synthesizing WindowPoSt circuit");
    let synth_start = Instant::now();
    let (_start, provers, input_assignments, aux_assignments) =
        synthesize_with_hint(vec![circuit], &CircuitId::WindowPost32G)?;
    let synthesis_duration = synth_start.elapsed();

    info!(
        synth_ms = synthesis_duration.as_millis(),
        num_constraints = provers[0].a.len(),
        "WindowPoSt synthesis complete"
    );

    let mut rng = rand_core::OsRng;
    let r_s = vec![Fr::random(&mut rng)];
    let s_s = vec![Fr::random(&mut rng)];

    Ok(SynthesizedProof {
        circuit_id: CircuitId::WindowPost32G,
        provers,
        input_assignments,
        aux_assignments,
        r_s,
        s_s,
        synthesis_duration,
        partition_index: Some(partition_index as usize),
        total_partitions: 1,
    })
}

/// Full WindowPoSt single-partition proof via pipeline (synthesis + GPU).
#[cfg(feature = "cuda-supraseal")]
pub fn prove_window_post_pipelined(
    vanilla_proofs_bytes: &[Vec<u8>],
    registered_proof: u64,
    miner_id: u64,
    randomness: &[u8],
    partition_index: u32,
    params: &SuprasealParameters<Bls12>,
    job_id: &str,
) -> Result<(Vec<u8>, PipelinedTimings)> {
    let total_start = Instant::now();

    let synth = synthesize_window_post(
        vanilla_proofs_bytes,
        registered_proof,
        miner_id,
        randomness,
        partition_index,
        job_id,
    )?;
    let synth_duration = synth.synthesis_duration;

    let gpu_result = gpu_prove(synth, params)?;

    let timings = PipelinedTimings {
        synthesis: synth_duration,
        gpu_compute: gpu_result.gpu_duration,
        total: total_start.elapsed(),
    };

    info!(
        proof_len = gpu_result.proof_bytes.len(),
        synth_ms = timings.synthesis.as_millis(),
        gpu_ms = timings.gpu_compute.as_millis(),
        total_ms = timings.total.as_millis(),
        "pipelined WindowPoSt complete"
    );

    Ok((gpu_result.proof_bytes, timings))
}

// ═══════════════════════════════════════════════════════════════════════════
// SnapDeals Synthesis (CPU phase)
// ═══════════════════════════════════════════════════════════════════════════

/// Synthesize a SnapDeals (empty sector update) proof (CPU-only).
///
/// Replicates the circuit construction from
/// `generate_empty_sector_update_proof_with_vanilla` but stops after synthesis.
/// Each element of `vanilla_proofs_bytes` is a bincode-serialized
/// `PartitionProof<Tree>` for one partition.
#[cfg(feature = "cuda-supraseal")]
pub fn synthesize_snap_deals(
    vanilla_proofs_bytes: Vec<Vec<u8>>,
    registered_proof: u64,
    comm_r_old: &[u8],
    comm_r_new: &[u8],
    comm_d_new: &[u8],
    job_id: &str,
) -> Result<SynthesizedProof> {
    let _span = info_span!("synthesize_snap_deals", job_id = job_id).entered();

    use crate::prover::{registered_update_proof_from_u64, to_array32};
    use filecoin_hashers::poseidon::PoseidonHasher;

    let update_proof_type = registered_update_proof_from_u64(registered_proof)
        .context("invalid registered proof for SnapDeals")?;
    let porep_config = update_proof_type.as_v1_config();
    let sector_size = u64::from(porep_config.sector_size);
    anyhow::ensure!(
        sector_size == SECTOR_SIZE_32_GIB,
        "pipelined synthesis currently supports only 32 GiB sectors, got {} bytes",
        sector_size,
    );

    type Tree = SectorShape32GiB;

    let comm_r_old_bytes: [u8; 32] = to_array32(comm_r_old, "comm_r_old")?;
    let comm_r_new_bytes: [u8; 32] = to_array32(comm_r_new, "comm_r_new")?;
    let comm_d_new_bytes: [u8; 32] = to_array32(comm_d_new, "comm_d_new")?;

    let comm_r_old_safe: <PoseidonHasher as Hasher>::Domain =
        as_safe_commitment(&comm_r_old_bytes, "comm_r_old")?;
    let comm_r_new_safe: <PoseidonHasher as Hasher>::Domain =
        as_safe_commitment(&comm_r_new_bytes, "comm_r_new")?;
    let comm_d_new_safe: filecoin_hashers::sha256::Sha256Domain =
        as_safe_commitment(&comm_d_new_bytes, "comm_d_new")?;

    // Deserialize vanilla partition proofs
    let vanilla_proofs: Vec<UpdatePartitionProof<Tree>> = vanilla_proofs_bytes
        .iter()
        .map(|bytes| {
            bincode::deserialize(bytes).map_err(|e| {
                anyhow::anyhow!("failed to deserialize SnapDeals partition proof: {}", e)
            })
        })
        .collect::<Result<_>>()?;

    let config = SectorUpdateConfig::from_porep_config(&porep_config);
    let num_partitions = usize::from(config.update_partitions);

    info!(
        num_partitions = num_partitions,
        num_vanilla_proofs = vanilla_proofs.len(),
        "building SnapDeals circuits"
    );

    // Build public inputs
    let public_inputs = UpdatePublicInputs {
        k: num_partitions,
        comm_r_old: comm_r_old_safe,
        comm_d_new: comm_d_new_safe,
        comm_r_new: comm_r_new_safe,
        h: config.h,
    };

    // Build setup params
    let update_vanilla_setup = storage_proofs_update::SetupParams {
        sector_bytes: u64::from(config.sector_size),
    };
    let compound_setup = storage_proofs_core::compound_proof::SetupParams {
        vanilla_params: update_vanilla_setup,
        partitions: Some(num_partitions),
        priority: false,
    };
    let pub_params = EmptySectorUpdateCompound::<Tree>::setup(&compound_setup)?;

    // Build circuits from vanilla proofs
    let circuits: Vec<_> = vanilla_proofs
        .iter()
        .enumerate()
        .map(|(k, vanilla_proof)| {
            <EmptySectorUpdateCompound<Tree> as CompoundProof<EmptySectorUpdate<Tree>, _>>::circuit(
                &public_inputs,
                Default::default(),
                vanilla_proof,
                &pub_params.vanilla_params,
                Some(k),
            )
        })
        .collect::<Result<_, _>>()
        .context("failed to build SnapDeals circuits")?;

    let num_circuits = circuits.len();
    info!(
        num_circuits = num_circuits,
        "synthesizing SnapDeals circuits"
    );

    let synth_start = Instant::now();
    let (_start, provers, input_assignments, aux_assignments) =
        synthesize_with_hint(circuits, &CircuitId::SnapDeals32G)?;
    let synthesis_duration = synth_start.elapsed();

    info!(
        synth_ms = synthesis_duration.as_millis(),
        num_circuits = provers.len(),
        num_constraints = provers[0].a.len(),
        "SnapDeals synthesis complete"
    );

    let mut rng = rand_core::OsRng;
    let r_s: Vec<Fr> = (0..num_circuits).map(|_| Fr::random(&mut rng)).collect();
    let s_s: Vec<Fr> = (0..num_circuits).map(|_| Fr::random(&mut rng)).collect();

    Ok(SynthesizedProof {
        circuit_id: CircuitId::SnapDeals32G,
        provers,
        input_assignments,
        aux_assignments,
        r_s,
        s_s,
        synthesis_duration,
        partition_index: None, // batch — all partitions
        total_partitions: num_partitions,
    })
}

/// Full SnapDeals proof via pipeline (synthesis + GPU).
#[cfg(feature = "cuda-supraseal")]
pub fn prove_snap_deals_pipelined(
    vanilla_proofs_bytes: Vec<Vec<u8>>,
    registered_proof: u64,
    comm_r_old: &[u8],
    comm_r_new: &[u8],
    comm_d_new: &[u8],
    params: &SuprasealParameters<Bls12>,
    job_id: &str,
) -> Result<(Vec<u8>, PipelinedTimings)> {
    let total_start = Instant::now();

    let synth = synthesize_snap_deals(
        vanilla_proofs_bytes,
        registered_proof,
        comm_r_old,
        comm_r_new,
        comm_d_new,
        job_id,
    )?;
    let synth_duration = synth.synthesis_duration;

    let gpu_result = gpu_prove(synth, params)?;

    let timings = PipelinedTimings {
        synthesis: synth_duration,
        gpu_compute: gpu_result.gpu_duration,
        total: total_start.elapsed(),
    };

    info!(
        proof_len = gpu_result.proof_bytes.len(),
        synth_ms = timings.synthesis.as_millis(),
        gpu_ms = timings.gpu_compute.as_millis(),
        total_ms = timings.total.as_millis(),
        "pipelined SnapDeals complete"
    );

    Ok((gpu_result.proof_bytes, timings))
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

#[cfg(not(feature = "cuda-supraseal"))]
pub fn prove_winning_post_pipelined(
    _vanilla_proofs_bytes: &[Vec<u8>],
    _registered_proof: u64,
    _miner_id: u64,
    _randomness: &[u8],
    _job_id: &str,
) -> Result<(Vec<u8>, PipelinedTimings)> {
    anyhow::bail!("pipelined proving requires cuda-supraseal feature")
}

#[cfg(not(feature = "cuda-supraseal"))]
pub fn prove_window_post_pipelined(
    _vanilla_proofs_bytes: &[Vec<u8>],
    _registered_proof: u64,
    _miner_id: u64,
    _randomness: &[u8],
    _partition_index: u32,
    _job_id: &str,
) -> Result<(Vec<u8>, PipelinedTimings)> {
    anyhow::bail!("pipelined proving requires cuda-supraseal feature")
}

#[cfg(not(feature = "cuda-supraseal"))]
pub fn prove_snap_deals_pipelined(
    _vanilla_proofs_bytes: Vec<Vec<u8>>,
    _registered_proof: u64,
    _comm_r_old: &[u8],
    _comm_r_new: &[u8],
    _comm_d_new: &[u8],
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
        assert_eq!(GROTH_PROOF_BYTES, 192);
    }

    #[test]
    fn test_synthesized_proof_stub() {
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

    #[test]
    fn test_split_batched_proofs() {
        // 3 sectors × 10 partitions = 30 proofs × 192 bytes
        let boundaries = vec![10, 10, 10];
        let total_bytes = 30 * GROTH_PROOF_BYTES;
        let mut proof_bytes = vec![0u8; total_bytes];
        // Mark each sector's first byte distinctively
        for (i, &_parts) in boundaries.iter().enumerate() {
            let offset: usize = boundaries[..i].iter().sum::<usize>() * GROTH_PROOF_BYTES;
            proof_bytes[offset] = (i + 1) as u8;
        }

        let results = split_batched_proofs(&proof_bytes, &boundaries).unwrap();
        assert_eq!(results.len(), 3);
        assert_eq!(results[0].len(), 10 * GROTH_PROOF_BYTES);
        assert_eq!(results[1].len(), 10 * GROTH_PROOF_BYTES);
        assert_eq!(results[2].len(), 10 * GROTH_PROOF_BYTES);
        assert_eq!(results[0][0], 1);
        assert_eq!(results[1][0], 2);
        assert_eq!(results[2][0], 3);
    }

    #[test]
    fn test_split_batched_proofs_mixed_partitions() {
        // PoRep: 10 partitions, SnapDeals might have 16
        let boundaries = vec![10, 16];
        let total_bytes = 26 * GROTH_PROOF_BYTES;
        let proof_bytes = vec![0xAA; total_bytes];

        let results = split_batched_proofs(&proof_bytes, &boundaries).unwrap();
        assert_eq!(results.len(), 2);
        assert_eq!(results[0].len(), 10 * GROTH_PROOF_BYTES);
        assert_eq!(results[1].len(), 16 * GROTH_PROOF_BYTES);
    }

    #[test]
    fn test_split_batched_proofs_length_mismatch() {
        let boundaries = vec![10];
        let proof_bytes = vec![0u8; 100]; // Wrong length
        assert!(split_batched_proofs(&proof_bytes, &boundaries).is_err());
    }
}
