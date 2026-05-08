//! Prover module — wraps calls into `filecoin-proofs-api`.
//!
//! Phase 0-1: Calls directly into `filecoin-proofs-api` proving functions.
//! Each call hits `GROTH_PARAM_MEMORY_CACHE` for SRS residency.
//!
//! Supported proof types:
//! - PoRep C2 (`seal_commit_phase2`)
//! - WinningPoSt (`generate_winning_post_with_vanilla`)
//! - WindowPoSt per-partition (`generate_single_window_post_with_vanilla`)
//! - SnapDeals (`generate_empty_sector_update_proof_with_vanilla`)

use anyhow::{ensure, Context, Result};
use base64::Engine as _;
use std::path::Path;
use std::time::Instant;
use tracing::{debug, info, info_span, warn};

use filecoin_proofs_api::seal::{self, SealCommitPhase1Output};
use filecoin_proofs_api::{
    post, update, ChallengeSeed, Commitment, PartitionProofBytes, RegisteredPoStProof,
    RegisteredUpdateProof, SectorId,
};

use filecoin_proofs_api::RegisteredSealProof;

use crate::types::ProofTimings;

/// Opaque wrapper for the C1 output JSON format used by Curio.
///
/// The c1.json has structure:
/// ```json
/// {
///   "SectorNum": 1,
///   "Phase1Out": "<base64-encoded JSON SealCommitPhase1Output>",
///   "SectorSize": 34359738368
/// }
/// ```
///
/// `Phase1Out` is base64 because Go's `encoding/json` encodes `[]byte` as base64.
/// The underlying bytes are JSON (serde_json) of `SealCommitPhase1Output`.
#[derive(Debug, serde::Deserialize)]
pub struct C1OutputWrapper {
    #[serde(rename = "SectorNum")]
    pub sector_num: u64,
    #[serde(rename = "Phase1Out")]
    pub phase1_out: String,
    #[serde(rename = "SectorSize")]
    pub sector_size: u64,
}

/// Construct a ProverId from a miner ID.
///
/// Mirrors the Go `toProverID(minerID)` which creates a Filecoin address
/// from the actor ID and uses its payload bytes, left-aligned in a [u8; 32].
///
/// Filecoin ID addresses have a 1-byte protocol prefix (0x00) followed by
/// the actor ID as a varint. The payload is just the varint portion.
pub fn make_prover_id(miner_id: u64) -> [u8; 32] {
    let mut prover_id = [0u8; 32];
    // Encode miner_id as unsigned LEB128 / varint (same as Go address payload)
    let mut val = miner_id;
    let mut i = 0;
    loop {
        let byte = (val & 0x7F) as u8;
        val >>= 7;
        if val == 0 {
            prover_id[i] = byte;
            break;
        }
        prover_id[i] = byte | 0x80;
        i += 1;
    }
    prover_id
}

/// Pre-load SRS parameters for the given circuit ID into the in-process cache.
///
/// This triggers `filecoin-proofs`' `GROTH_PARAM_MEMORY_CACHE` to load and retain
/// the Groth16 parameters. On subsequent proof calls within this process, the cache
/// is hit and SRS loading is skipped.
///
/// Phase 0 approach: We cannot directly call `get_stacked_params()` (it's `pub(crate)`).
/// Instead, we note that the cache is populated lazily on the first `seal_commit_phase2()`
/// call. The preload_srs function sets up the environment and logs intent. The actual
/// cache population happens on the first proof call.
///
/// # Arguments
/// * `circuit_id` — e.g. "porep-32g", "snap-32g", "wpost-32g", "winning-32g"
/// * `param_cache` — path to the directory containing .params/.vk files
pub fn preload_srs(circuit_id: &str, param_cache: &Path) -> Result<std::time::Duration> {
    let start = Instant::now();

    // Set the parameter cache path so filecoin-proofs finds the .params files.
    // This is the standard mechanism: FIL_PROOFS_PARAMETER_CACHE env var.
    std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", param_cache);

    info!(
        circuit_id = circuit_id,
        param_cache = %param_cache.display(),
        "SRS preload: environment configured, cache will populate on first proof"
    );

    // Phase 0: Cache population is deferred to the first proof call for each circuit type.
    // The GROTH_PARAM_MEMORY_CACHE in filecoin-proofs is populated lazily.
    //
    // Phase 1 will add explicit cache pre-population by calling the proving functions
    // with constructed configs, or by directly accessing the cache internals.
    warn!(
        circuit_id = circuit_id,
        "SRS preload is lazy in Phase 0 (will load on first proof call)"
    );

    let elapsed = start.elapsed();
    Ok(elapsed)
}

/// Execute a PoRep C2 proof.
///
/// Deserializes the C1 output (JSON), calls `seal_commit_phase2()`, returns proof bytes.
/// SRS is loaded from `GROTH_PARAM_MEMORY_CACHE` (cached after first call in this process).
///
/// `job_id` is threaded through for log correlation.
pub fn prove_porep_c2(
    vanilla_proof_json: &[u8],
    sector_number: u64,
    miner_id: u64,
    job_id: &str,
) -> Result<(Vec<u8>, ProofTimings)> {
    let _span = info_span!("prove_porep_c2", job_id = job_id).entered();
    let total_start = Instant::now();
    let mut timings = ProofTimings::default();

    // --- Phase: Deserialize ---
    let deser_start = Instant::now();

    // Parse the C1 wrapper JSON (outer Curio format)
    debug!(
        input_len = vanilla_proof_json.len(),
        "parsing C1 outer wrapper JSON"
    );
    let wrapper: C1OutputWrapper = serde_json::from_slice(vanilla_proof_json)
        .context("failed to parse C1 output wrapper JSON")?;

    info!(
        sector_num = wrapper.sector_num,
        sector_size = wrapper.sector_size,
        phase1_out_b64_len = wrapper.phase1_out.len(),
        "parsed C1 wrapper"
    );

    // Decode the base64-encoded Phase1Output.
    // Go encodes []byte as base64 in JSON. The underlying bytes are JSON (serde_json).
    let phase1_json_bytes = base64::engine::general_purpose::STANDARD
        .decode(&wrapper.phase1_out)
        .context("failed to decode base64 Phase1Output")?;

    debug!(
        phase1_json_len = phase1_json_bytes.len(),
        "decoded Phase1Output from base64"
    );

    // Deserialize the JSON into the Rust SealCommitPhase1Output struct.
    let c1_output: SealCommitPhase1Output = serde_json::from_slice(&phase1_json_bytes)
        .context("failed to deserialize SealCommitPhase1Output from JSON")?;

    timings.deserialize = deser_start.elapsed();
    info!(
        registered_proof = ?c1_output.registered_proof,
        deser_ms = timings.deserialize.as_millis(),
        "deserialized SealCommitPhase1Output"
    );

    // --- Phase: Prove (SRS load + synthesis + GPU + verify, monolithic in Phase 0) ---
    let prover_id = make_prover_id(miner_id);
    let sector_id = SectorId::from(sector_number);

    info!(
        sector_id = sector_number,
        miner_id = miner_id,
        "calling seal_commit_phase2 (includes SRS load + synthesis + GPU + verify)"
    );

    let prove_start = Instant::now();
    let output = seal::seal_commit_phase2(c1_output, prover_id, sector_id)
        .context("seal_commit_phase2 failed")?;
    timings.proving = prove_start.elapsed();

    // Phase 0: We cannot split synthesis from GPU time. Report the total
    // proving duration under gpu_compute for proto compat, and zero out
    // the parts we can't measure.
    timings.srs_load = std::time::Duration::ZERO;
    timings.synthesis = std::time::Duration::ZERO;
    timings.gpu_compute = timings.proving;

    timings.total = total_start.elapsed();
    timings.queue_wait = std::time::Duration::ZERO; // Set by engine

    info!(
        proof_len = output.proof.len(),
        deser_ms = timings.deserialize.as_millis(),
        prove_ms = timings.proving.as_millis(),
        total_ms = timings.total.as_millis(),
        "PoRep C2 proof completed"
    );

    Ok((output.proof, timings))
}

/// Verify a PoRep proof using `filecoin-proofs-api::seal::verify_seal`.
///
/// This provides a self-check for pipeline-generated proofs. It re-parses
/// the C1 output wrapper to extract `registered_proof`, `comm_r`, `comm_d`,
/// `seed`, and `ticket`, then calls `verify_seal` with the given proof bytes.
///
/// Returns `Ok(true)` if the proof verifies, `Ok(false)` if it is invalid,
/// or an error if the verification call itself fails.
pub fn verify_porep_proof(
    vanilla_proof_json: &[u8],
    proof_bytes: &[u8],
    sector_number: u64,
    miner_id: u64,
    job_id: &str,
) -> Result<bool> {
    let _span = info_span!("verify_porep_proof", job_id = job_id).entered();

    // Parse the C1 wrapper to extract verification inputs
    let wrapper: C1OutputWrapper = serde_json::from_slice(vanilla_proof_json)
        .context("verify: failed to parse C1 output wrapper JSON")?;
    let phase1_json_bytes = base64::engine::general_purpose::STANDARD
        .decode(&wrapper.phase1_out)
        .context("verify: failed to decode base64 Phase1Output")?;
    let c1_output: SealCommitPhase1Output = serde_json::from_slice(&phase1_json_bytes)
        .context("verify: failed to deserialize SealCommitPhase1Output from JSON")?;

    let prover_id = make_prover_id(miner_id);
    let sector_id = SectorId::from(sector_number);

    info!(
        registered_proof = ?c1_output.registered_proof,
        proof_len = proof_bytes.len(),
        sector_number = sector_number,
        miner_id = miner_id,
        "verifying PoRep proof (self-check)"
    );

    let result = seal::verify_seal(
        c1_output.registered_proof,
        c1_output.comm_r,
        c1_output.comm_d,
        prover_id,
        sector_id,
        c1_output.ticket,
        c1_output.seed,
        proof_bytes,
    )
    .context("verify_seal call failed")?;

    if result {
        info!(job_id = job_id, "PoRep proof self-check PASSED");
    } else {
        warn!(
            job_id = job_id,
            registered_proof = ?c1_output.registered_proof,
            proof_len = proof_bytes.len(),
            sector_number = sector_number,
            miner_id = miner_id,
            "PoRep proof self-check FAILED — proof is invalid"
        );
    }

    Ok(result)
}

/// Verify each partition of a PoRep proof individually.
///
/// This is a diagnostic function to determine whether individual partition
/// proofs are valid (suggesting an ordering/assembly bug) or invalid
/// (suggesting a `num_circuits=1` GPU proving bug).
///
/// For each partition k (0..num_partitions):
///   1. Extracts the 192-byte Groth16 proof at `proof_bytes[k*192..(k+1)*192]`
///   2. Generates the public inputs for partition k via `StackedCompound::generate_public_inputs`
///   3. Verifies the single proof with `bellperson::groth16::verify_proof`
///
/// Returns a Vec of booleans, one per partition, indicating validity.
pub fn verify_porep_partitions(
    vanilla_proof_json: &[u8],
    proof_bytes: &[u8],
    sector_number: u64,
    miner_id: u64,
    job_id: &str,
) -> Result<Vec<bool>> {
    use bellperson::groth16;
    use blstrs::Bls12;
    use filecoin_hashers::Hasher;
    use filecoin_proofs::parameters::setup_params;
    use filecoin_proofs::{
        as_safe_commitment, DefaultPieceDomain, DefaultPieceHasher, SectorShape32GiB,
    };
    use rand_core::OsRng;
    use storage_proofs_core::compound_proof::{CompoundProof, SetupParams};
    use storage_proofs_porep::stacked::{
        generate_replica_id, PublicInputs, StackedCompound, StackedDrg, Tau,
    };

    let _span = info_span!("verify_porep_partitions", job_id = job_id).entered();

    type Tree = SectorShape32GiB;
    const GROTH_PROOF_BYTES: usize = 192;

    // Parse the C1 wrapper
    let wrapper: C1OutputWrapper = serde_json::from_slice(vanilla_proof_json)
        .context("verify_partitions: failed to parse C1 output wrapper JSON")?;
    let phase1_json_bytes = base64::engine::general_purpose::STANDARD
        .decode(&wrapper.phase1_out)
        .context("verify_partitions: failed to decode base64 Phase1Output")?;
    let c1_output: SealCommitPhase1Output = serde_json::from_slice(&phase1_json_bytes)
        .context("verify_partitions: failed to deserialize SealCommitPhase1Output from JSON")?;

    let porep_config = c1_output.registered_proof.as_v1_config();
    let num_partitions = usize::from(porep_config.partitions);

    let expected_total = num_partitions * GROTH_PROOF_BYTES;
    ensure!(
        proof_bytes.len() == expected_total,
        "proof bytes len {} != expected {} ({} partitions * {})",
        proof_bytes.len(),
        expected_total,
        num_partitions,
        GROTH_PROOF_BYTES,
    );

    // Derive replica_id (same as verify_seal does internally)
    let prover_id = make_prover_id(miner_id);
    type TreeHasher = <SectorShape32GiB as storage_proofs_core::merkle::MerkleTreeTrait>::Hasher;
    let comm_r_safe: <TreeHasher as Hasher>::Domain =
        as_safe_commitment(&c1_output.comm_r, "comm_r")?;
    let comm_d_safe: DefaultPieceDomain = as_safe_commitment(&c1_output.comm_d, "comm_d")?;
    let replica_id = generate_replica_id::<TreeHasher, _>(
        &prover_id,
        sector_number,
        &c1_output.ticket,
        comm_d_safe,
        &porep_config.porep_id,
    );

    // Build compound public params
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

    let public_inputs = PublicInputs {
        replica_id,
        tau: Some(Tau {
            comm_r: comm_r_safe,
            comm_d: comm_d_safe,
        }),
        k: None,
        seed: Some(c1_output.seed),
    };

    // Get verifying key via the CompoundProof trait method (loads from .vk param cache)
    info!(
        job_id = job_id,
        num_partitions = num_partitions,
        "loading verifying key for per-partition verification"
    );
    let vk = <StackedCompound<Tree, DefaultPieceHasher> as CompoundProof<
        StackedDrg<'_, Tree, DefaultPieceHasher>,
        _,
    >>::verifying_key::<OsRng>(None, &compound_public_params.vanilla_params)
    .context("failed to load verifying key for per-partition verification")?;
    let pvk = groth16::prepare_verifying_key(&vk);

    info!(
        job_id = job_id,
        num_partitions = num_partitions,
        "starting per-partition verification"
    );

    let mut results = Vec::with_capacity(num_partitions);

    for k in 0..num_partitions {
        // Extract this partition's 192 bytes and deserialize
        let partition_proof_bytes =
            &proof_bytes[k * GROTH_PROOF_BYTES..(k + 1) * GROTH_PROOF_BYTES];
        let proof = groth16::Proof::<Bls12>::read(&partition_proof_bytes[..])
            .context(format!("failed to deserialize proof for partition {}", k))?;

        // Generate public inputs for this partition
        let partition_inputs = <StackedCompound<Tree, DefaultPieceHasher> as CompoundProof<
            StackedDrg<'_, Tree, DefaultPieceHasher>,
            _,
        >>::generate_public_inputs(
            &public_inputs,
            &compound_public_params.vanilla_params,
            Some(k),
        )
        .context(format!(
            "failed to generate public inputs for partition {}",
            k,
        ))?;

        // Verify single partition
        let valid = groth16::verify_proof(&pvk, &proof, &partition_inputs)
            .context(format!("verify_proof call failed for partition {}", k))?;

        if valid {
            info!(
                job_id = job_id,
                partition = k,
                "partition {} proof VALID",
                k
            );
        } else {
            warn!(
                job_id = job_id,
                partition = k,
                num_public_inputs = partition_inputs.len(),
                "partition {} proof INVALID",
                k
            );
        }

        results.push(valid);
    }

    let valid_count = results.iter().filter(|&&v| v).count();
    let invalid_count = num_partitions - valid_count;

    if invalid_count == 0 {
        info!(
            job_id = job_id,
            "ALL {} partition proofs are individually valid — likely an ordering/assembly bug",
            num_partitions
        );
    } else {
        warn!(
            job_id = job_id,
            valid = valid_count,
            invalid = invalid_count,
            results = ?results,
            "PER-PARTITION VERIFICATION: {}/{} valid — likely a num_circuits=1 GPU proving bug",
            valid_count,
            num_partitions
        );
    }

    Ok(results)
}

/// Convert a numeric registered proof value (from gRPC) to a `RegisteredPoStProof`.
///
/// These values match the FFI `#[repr(i32)]` enum used by Go's `abi.RegisteredPoStProof`.
/// Note: FFI V1_1 (Go side) maps to filecoin-proofs-api V1_2 (grindability fix).
pub fn registered_post_proof_from_u64(v: u64) -> Result<RegisteredPoStProof> {
    match v {
        0 => Ok(RegisteredPoStProof::StackedDrgWinning2KiBV1),
        1 => Ok(RegisteredPoStProof::StackedDrgWinning8MiBV1),
        2 => Ok(RegisteredPoStProof::StackedDrgWinning512MiBV1),
        3 => Ok(RegisteredPoStProof::StackedDrgWinning32GiBV1),
        4 => Ok(RegisteredPoStProof::StackedDrgWinning64GiBV1),
        5 => Ok(RegisteredPoStProof::StackedDrgWindow2KiBV1),
        6 => Ok(RegisteredPoStProof::StackedDrgWindow8MiBV1),
        7 => Ok(RegisteredPoStProof::StackedDrgWindow512MiBV1),
        8 => Ok(RegisteredPoStProof::StackedDrgWindow32GiBV1),
        9 => Ok(RegisteredPoStProof::StackedDrgWindow64GiBV1),
        // FFI V1_1 → proofs-api V1_2 (grindability fix)
        10 => Ok(RegisteredPoStProof::StackedDrgWindow2KiBV1_2),
        11 => Ok(RegisteredPoStProof::StackedDrgWindow8MiBV1_2),
        12 => Ok(RegisteredPoStProof::StackedDrgWindow512MiBV1_2),
        13 => Ok(RegisteredPoStProof::StackedDrgWindow32GiBV1_2),
        14 => Ok(RegisteredPoStProof::StackedDrgWindow64GiBV1_2),
        _ => anyhow::bail!("unknown RegisteredPoStProof value: {}", v),
    }
}

/// Convert a numeric registered proof value (from gRPC) to a `RegisteredUpdateProof`.
///
/// These values match the FFI `#[repr(i32)]` enum used by Go's `abi.RegisteredUpdateProof`.
pub fn registered_update_proof_from_u64(v: u64) -> Result<RegisteredUpdateProof> {
    match v {
        0 => Ok(RegisteredUpdateProof::StackedDrg2KiBV1),
        1 => Ok(RegisteredUpdateProof::StackedDrg8MiBV1),
        2 => Ok(RegisteredUpdateProof::StackedDrg512MiBV1),
        3 => Ok(RegisteredUpdateProof::StackedDrg32GiBV1),
        4 => Ok(RegisteredUpdateProof::StackedDrg64GiBV1),
        _ => anyhow::bail!("unknown RegisteredUpdateProof value: {}", v),
    }
}

/// Convert a byte slice to a 32-byte array (for commitments, randomness, etc.).
pub fn to_array32(bytes: &[u8], name: &str) -> Result<[u8; 32]> {
    if bytes.len() != 32 {
        anyhow::bail!("{} must be exactly 32 bytes, got {}", name, bytes.len());
    }
    let mut arr = [0u8; 32];
    arr.copy_from_slice(bytes);
    Ok(arr)
}

/// Execute a WinningPoSt proof.
///
/// Calls `generate_winning_post_with_vanilla()` with the provided vanilla proofs.
/// Each element of `vanilla_proofs` is a bincode-serialized `FallbackPoStSectorProof`
/// for one challenged sector.
///
/// Returns the concatenated SNARK proof bytes.
pub fn prove_winning_post(
    vanilla_proofs: &[Vec<u8>],
    registered_proof: u64,
    miner_id: u64,
    randomness: &[u8],
    job_id: &str,
) -> Result<(Vec<u8>, ProofTimings)> {
    let _span = info_span!("prove_winning_post", job_id = job_id).entered();
    let total_start = Instant::now();
    let mut timings = ProofTimings::default();

    // --- Phase: Deserialize / validate ---
    let deser_start = Instant::now();

    let post_proof_type = registered_post_proof_from_u64(registered_proof)
        .context("invalid registered proof for WinningPoSt")?;
    let prover_id = make_prover_id(miner_id);
    let challenge_seed: ChallengeSeed = to_array32(randomness, "randomness")?;

    info!(
        proof_type = ?post_proof_type,
        miner_id = miner_id,
        num_vanilla_proofs = vanilla_proofs.len(),
        "proving WinningPoSt"
    );

    timings.deserialize = deser_start.elapsed();

    // --- Phase: Prove ---
    let prove_start = Instant::now();
    let results = post::generate_winning_post_with_vanilla(
        post_proof_type,
        &challenge_seed,
        prover_id,
        vanilla_proofs,
    )
    .context("generate_winning_post_with_vanilla failed")?;
    timings.proving = prove_start.elapsed();

    // Collect proof bytes from all results (typically one)
    let mut proof_bytes = Vec::new();
    for (_reg_proof, snark_proof) in &results {
        proof_bytes.extend_from_slice(snark_proof);
    }

    timings.srs_load = std::time::Duration::ZERO;
    timings.synthesis = std::time::Duration::ZERO;
    timings.gpu_compute = timings.proving;
    timings.total = total_start.elapsed();

    info!(
        proof_len = proof_bytes.len(),
        prove_ms = timings.proving.as_millis(),
        total_ms = timings.total.as_millis(),
        "WinningPoSt proof completed"
    );

    Ok((proof_bytes, timings))
}

/// Execute a WindowPoSt single-partition proof.
///
/// Calls `generate_single_window_post_with_vanilla()` for one partition.
/// Each element of `vanilla_proofs` is a bincode-serialized `FallbackPoStSectorProof`
/// for one sector in this partition.
///
/// Returns the `PartitionSnarkProof` bytes for this partition.
pub fn prove_window_post(
    vanilla_proofs: &[Vec<u8>],
    registered_proof: u64,
    miner_id: u64,
    randomness: &[u8],
    partition_index: u32,
    job_id: &str,
) -> Result<(Vec<u8>, ProofTimings)> {
    let _span = info_span!("prove_window_post", job_id = job_id).entered();
    let total_start = Instant::now();
    let mut timings = ProofTimings::default();

    // --- Phase: Deserialize / validate ---
    let deser_start = Instant::now();

    let post_proof_type = registered_post_proof_from_u64(registered_proof)
        .context("invalid registered proof for WindowPoSt")?;
    let prover_id = make_prover_id(miner_id);
    let challenge_seed: ChallengeSeed = to_array32(randomness, "randomness")?;

    info!(
        proof_type = ?post_proof_type,
        miner_id = miner_id,
        partition_index = partition_index,
        num_vanilla_proofs = vanilla_proofs.len(),
        "proving WindowPoSt partition"
    );

    timings.deserialize = deser_start.elapsed();

    // --- Phase: Prove ---
    let prove_start = Instant::now();
    let partition_proof = post::generate_single_window_post_with_vanilla(
        post_proof_type,
        &challenge_seed,
        prover_id,
        vanilla_proofs,
        partition_index as usize,
    )
    .context("generate_single_window_post_with_vanilla failed")?;
    timings.proving = prove_start.elapsed();

    let proof_bytes = partition_proof.0;

    timings.srs_load = std::time::Duration::ZERO;
    timings.synthesis = std::time::Duration::ZERO;
    timings.gpu_compute = timings.proving;
    timings.total = total_start.elapsed();

    info!(
        proof_len = proof_bytes.len(),
        prove_ms = timings.proving.as_millis(),
        total_ms = timings.total.as_millis(),
        "WindowPoSt partition proof completed"
    );

    Ok((proof_bytes, timings))
}

/// Execute a SnapDeals (empty sector update) proof.
///
/// Calls `generate_empty_sector_update_proof_with_vanilla()` with the provided
/// partition vanilla proofs. Each element of `vanilla_proofs` is a bincode-serialized
/// `PartitionProof` for one partition.
///
/// Returns the SNARK proof bytes.
pub fn prove_snap_deals(
    vanilla_proofs: Vec<Vec<u8>>,
    registered_proof: u64,
    comm_r_old: &[u8],
    comm_r_new: &[u8],
    comm_d_new: &[u8],
    job_id: &str,
) -> Result<(Vec<u8>, ProofTimings)> {
    let _span = info_span!("prove_snap_deals", job_id = job_id).entered();
    let total_start = Instant::now();
    let mut timings = ProofTimings::default();

    // --- Phase: Deserialize / validate ---
    let deser_start = Instant::now();

    let update_proof_type = registered_update_proof_from_u64(registered_proof)
        .context("invalid registered proof for SnapDeals")?;
    let comm_r_old: Commitment = to_array32(comm_r_old, "comm_r_old")?;
    let comm_r_new: Commitment = to_array32(comm_r_new, "comm_r_new")?;
    let comm_d_new: Commitment = to_array32(comm_d_new, "comm_d_new")?;

    // Wrap each vanilla proof in PartitionProofBytes
    let partition_proofs: Vec<PartitionProofBytes> = vanilla_proofs
        .into_iter()
        .map(PartitionProofBytes)
        .collect();

    info!(
        proof_type = ?update_proof_type,
        num_partition_proofs = partition_proofs.len(),
        "proving SnapDeals update"
    );

    timings.deserialize = deser_start.elapsed();

    // --- Phase: Prove ---
    let prove_start = Instant::now();
    let update_proof = update::generate_empty_sector_update_proof_with_vanilla(
        update_proof_type,
        partition_proofs,
        comm_r_old,
        comm_r_new,
        comm_d_new,
    )
    .context("generate_empty_sector_update_proof_with_vanilla failed")?;
    timings.proving = prove_start.elapsed();

    let proof_bytes = update_proof.0;

    timings.srs_load = std::time::Duration::ZERO;
    timings.synthesis = std::time::Duration::ZERO;
    timings.gpu_compute = timings.proving;
    timings.total = total_start.elapsed();

    info!(
        proof_len = proof_bytes.len(),
        prove_ms = timings.proving.as_millis(),
        total_ms = timings.total.as_millis(),
        "SnapDeals proof completed"
    );

    Ok((proof_bytes, timings))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_c1_wrapper() {
        let json = r#"{"SectorNum": 1, "Phase1Out": "aGVsbG8=", "SectorSize": 34359738368}"#;
        let wrapper: C1OutputWrapper = serde_json::from_str(json).unwrap();
        assert_eq!(wrapper.sector_num, 1);
        assert_eq!(wrapper.sector_size, 34359738368);
        // "aGVsbG8=" is base64 for "hello"
        let decoded = base64::engine::general_purpose::STANDARD
            .decode(&wrapper.phase1_out)
            .unwrap();
        assert_eq!(decoded, b"hello");
    }

    #[test]
    fn test_registered_post_proof_from_u64() {
        // Winning 32G = 3
        let p = registered_post_proof_from_u64(3).unwrap();
        assert_eq!(p, RegisteredPoStProof::StackedDrgWinning32GiBV1);

        // Window 32G V1 = 8
        let p = registered_post_proof_from_u64(8).unwrap();
        assert_eq!(p, RegisteredPoStProof::StackedDrgWindow32GiBV1);

        // Window 32G V1_1 (FFI) -> V1_2 (proofs-api) = 13
        let p = registered_post_proof_from_u64(13).unwrap();
        assert_eq!(p, RegisteredPoStProof::StackedDrgWindow32GiBV1_2);

        // Invalid
        assert!(registered_post_proof_from_u64(100).is_err());
    }

    #[test]
    fn test_registered_update_proof_from_u64() {
        // 32G = 3
        let p = registered_update_proof_from_u64(3).unwrap();
        assert_eq!(p, RegisteredUpdateProof::StackedDrg32GiBV1);

        // Invalid
        assert!(registered_update_proof_from_u64(99).is_err());
    }

    #[test]
    fn test_to_array32() {
        let bytes = vec![0u8; 32];
        let arr = to_array32(&bytes, "test").unwrap();
        assert_eq!(arr, [0u8; 32]);

        // Too short
        assert!(to_array32(&[0u8; 31], "test").is_err());

        // Too long
        assert!(to_array32(&[0u8; 33], "test").is_err());
    }

    #[test]
    fn test_make_prover_id() {
        // Miner ID 1000 = 0xe8 0x07 in unsigned varint
        let id = make_prover_id(1000);
        assert_eq!(id[0], 0xe8);
        assert_eq!(id[1], 0x07);
        assert_eq!(id[2], 0x00);

        // Miner ID 0
        let id = make_prover_id(0);
        assert_eq!(id[0], 0x00);
        assert_eq!(id[1], 0x00);

        // Miner ID 1
        let id = make_prover_id(1);
        assert_eq!(id[0], 0x01);
        assert_eq!(id[1], 0x00);

        // Miner ID 127 (single byte)
        let id = make_prover_id(127);
        assert_eq!(id[0], 0x7F);
        assert_eq!(id[1], 0x00);

        // Miner ID 128 (two bytes)
        let id = make_prover_id(128);
        assert_eq!(id[0], 0x80);
        assert_eq!(id[1], 0x01);
    }
}
