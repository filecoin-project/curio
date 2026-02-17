//! Prover module — wraps calls into `filecoin-proofs-api`.
//!
//! Phase 0: Calls directly into `seal_commit_phase2()` etc.
//! Each call hits `GROTH_PARAM_MEMORY_CACHE` for SRS residency.

use anyhow::{Context, Result};
use base64::Engine as _;
use std::path::Path;
use std::time::Instant;
use tracing::{info, warn};

use filecoin_proofs_api::seal::{self, SealCommitPhase1Output};
use filecoin_proofs_api::SectorId;

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
fn make_prover_id(miner_id: u64) -> [u8; 32] {
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
pub fn prove_porep_c2(
    vanilla_proof_json: &[u8],
    sector_number: u64,
    miner_id: u64,
) -> Result<(Vec<u8>, ProofTimings)> {
    let total_start = Instant::now();
    let mut timings = ProofTimings::default();

    // Parse the C1 wrapper JSON (outer Curio format)
    let wrapper: C1OutputWrapper = serde_json::from_slice(vanilla_proof_json)
        .context("failed to parse C1 output wrapper JSON")?;

    info!(
        sector_num = wrapper.sector_num,
        sector_size = wrapper.sector_size,
        phase1_out_b64_len = wrapper.phase1_out.len(),
        "starting PoRep C2 proof"
    );

    // Decode the base64-encoded Phase1Output.
    // Go encodes []byte as base64 in JSON. The underlying bytes are JSON (serde_json).
    let phase1_json_bytes = base64::engine::general_purpose::STANDARD
        .decode(&wrapper.phase1_out)
        .context("failed to decode base64 Phase1Output")?;

    info!(
        phase1_json_len = phase1_json_bytes.len(),
        "decoded Phase1Output from base64"
    );

    // Deserialize the JSON into the Rust SealCommitPhase1Output struct.
    // This is the same format that the Rust FFI layer uses: serde_json.
    let srs_start = Instant::now();
    let c1_output: SealCommitPhase1Output = serde_json::from_slice(&phase1_json_bytes)
        .context("failed to deserialize SealCommitPhase1Output from JSON")?;
    timings.srs_load = srs_start.elapsed(); // First call includes SRS load time

    info!(
        registered_proof = ?c1_output.registered_proof,
        "deserialized SealCommitPhase1Output"
    );

    // Construct prover_id and sector_id
    let prover_id = make_prover_id(miner_id);
    let sector_id = SectorId::from(sector_number);

    info!(
        sector_id = sector_number,
        miner_id = miner_id,
        "calling seal_commit_phase2"
    );

    // Call into filecoin-proofs-api. This is the real proving call.
    // On first call: GROTH_PARAM_MEMORY_CACHE loads the SRS (~30-90s for 32G PoRep).
    // On subsequent calls: cache hit, SRS load is ~0.
    // The function internally performs circuit synthesis (CPU) + GPU compute (NTT/MSM).
    let prove_start = Instant::now();
    let output = seal::seal_commit_phase2(c1_output, prover_id, sector_id)
        .context("seal_commit_phase2 failed")?;
    let prove_elapsed = prove_start.elapsed();

    // We can't separate synthesis from GPU time at this level — seal_commit_phase2
    // is a monolithic call. Report the total as "gpu_compute" for Phase 0.
    // Phase 2+ with the split API will separate these.
    timings.gpu_compute = prove_elapsed;
    timings.total = total_start.elapsed();
    timings.queue_wait = std::time::Duration::ZERO; // Set by engine

    info!(
        proof_len = output.proof.len(),
        prove_ms = prove_elapsed.as_millis(),
        total_ms = timings.total.as_millis(),
        "PoRep C2 proof completed"
    );

    Ok((output.proof, timings))
}

/// Execute a WinningPoSt proof (stub for Phase 1).
pub fn prove_winning_post(
    _vanilla_proof: &[u8],
    _miner_id: u64,
    _randomness: &[u8],
) -> Result<(Vec<u8>, ProofTimings)> {
    warn!("WinningPoSt proof is a STUB — will be implemented in Phase 1");
    anyhow::bail!("WinningPoSt not yet implemented in Phase 0")
}

/// Execute a WindowPoSt partition proof (stub for Phase 1).
pub fn prove_window_post(
    _vanilla_proof: &[u8],
    _miner_id: u64,
    _randomness: &[u8],
    _partition_index: u32,
) -> Result<(Vec<u8>, ProofTimings)> {
    warn!("WindowPoSt proof is a STUB — will be implemented in Phase 1");
    anyhow::bail!("WindowPoSt not yet implemented in Phase 0")
}

/// Execute a SnapDeals update proof (stub for Phase 1).
pub fn prove_snap_deals(
    _vanilla_proof: &[u8],
    _sector_key_cid: &[u8],
    _new_sealed_cid: &[u8],
    _new_unsealed_cid: &[u8],
) -> Result<(Vec<u8>, ProofTimings)> {
    warn!("SnapDeals proof is a STUB — will be implemented in Phase 1");
    anyhow::bail!("SnapDeals not yet implemented in Phase 0")
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
