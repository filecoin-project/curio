//! gen-vanilla: Generate vanilla proof test data for PoSt and SnapDeals.
//!
//! This module calls directly into `filecoin-proofs-api` CPU-only functions
//! to produce vanilla proofs from existing sealed sector data. The vanilla
//! proofs can then be fed to the cuzk daemon for SNARK proving.
//!
//! Requires the `gen-vanilla` feature flag (pulls in `filecoin-proofs-api`).
//!
//! Output format: JSON array of base64-encoded proof bytes, matching
//! Go's `json.Marshal([][]byte)` format.

use anyhow::{Context, Result};
use base64::Engine as _;
use std::collections::BTreeMap;
use std::path::PathBuf;

use filecoin_proofs_api::{
    post, update, Commitment, PrivateReplicaInfo, RegisteredPoStProof, RegisteredUpdateProof,
    SectorId,
};

// ---------------------------------------------------------------------------
// CID → 32-byte commitment parsing
// ---------------------------------------------------------------------------

/// Parse a Filecoin commitment CID string (e.g. `bagboea4b5abc...`) to a raw
/// 32-byte commitment.
///
/// CID structure: multibase prefix ('b' = base32lower) + CIDv1 binary
/// CIDv1 binary: version(varint) | codec(varint) | multihash
/// multihash: hash_type(varint) | digest_len(varint) | digest[32]
///
/// We use the `cid` crate which handles multibase decoding and CID parsing.
pub fn parse_commitment_cid(cid_str: &str) -> Result<[u8; 32]> {
    let c: cid::Cid = cid_str
        .parse()
        .map_err(|e| anyhow::anyhow!("failed to parse CID '{}': {}", cid_str, e))?;

    let digest = c.hash().digest();
    if digest.len() != 32 {
        anyhow::bail!(
            "expected 32-byte digest in CID, got {} bytes: {}",
            digest.len(),
            cid_str
        );
    }

    let mut arr = [0u8; 32];
    arr.copy_from_slice(digest);
    Ok(arr)
}

/// Parse a "d:CID r:CID" formatted commitment file (like commdr.txt).
///
/// Returns (comm_d, comm_r) as 32-byte arrays.
pub fn parse_commdr_file(contents: &str) -> Result<(Commitment, Commitment)> {
    let parts: Vec<&str> = contents.trim().split_whitespace().collect();
    if parts.len() != 2 {
        anyhow::bail!(
            "expected 'd:<CID> r:<CID>' format, got {} parts",
            parts.len()
        );
    }

    let comm_d_str = parts[0]
        .strip_prefix("d:")
        .ok_or_else(|| anyhow::anyhow!("first part must start with 'd:', got: {}", parts[0]))?;
    let comm_r_str = parts[1]
        .strip_prefix("r:")
        .ok_or_else(|| anyhow::anyhow!("second part must start with 'r:', got: {}", parts[1]))?;

    let comm_d = parse_commitment_cid(comm_d_str)?;
    let comm_r = parse_commitment_cid(comm_r_str)?;

    Ok((comm_d, comm_r))
}

// ---------------------------------------------------------------------------
// Registered proof type conversion (matches cuzk-core/src/prover.rs)
// ---------------------------------------------------------------------------

fn registered_post_proof_from_u64(v: u64) -> Result<RegisteredPoStProof> {
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
        10 => Ok(RegisteredPoStProof::StackedDrgWindow2KiBV1_2),
        11 => Ok(RegisteredPoStProof::StackedDrgWindow8MiBV1_2),
        12 => Ok(RegisteredPoStProof::StackedDrgWindow512MiBV1_2),
        13 => Ok(RegisteredPoStProof::StackedDrgWindow32GiBV1_2),
        14 => Ok(RegisteredPoStProof::StackedDrgWindow64GiBV1_2),
        _ => anyhow::bail!("unknown RegisteredPoStProof value: {}", v),
    }
}

fn registered_update_proof_from_u64(v: u64) -> Result<RegisteredUpdateProof> {
    match v {
        0 => Ok(RegisteredUpdateProof::StackedDrg2KiBV1),
        1 => Ok(RegisteredUpdateProof::StackedDrg8MiBV1),
        2 => Ok(RegisteredUpdateProof::StackedDrg512MiBV1),
        3 => Ok(RegisteredUpdateProof::StackedDrg32GiBV1),
        4 => Ok(RegisteredUpdateProof::StackedDrg64GiBV1),
        _ => anyhow::bail!("unknown RegisteredUpdateProof value: {}", v),
    }
}

fn make_prover_id(miner_id: u64) -> [u8; 32] {
    let mut prover_id = [0u8; 32];
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

// ---------------------------------------------------------------------------
// Output helpers
// ---------------------------------------------------------------------------

/// Encode vanilla proofs as JSON array of base64 strings.
/// This matches Go's `json.Marshal([][]byte)` format.
fn encode_vanilla_proofs_json(proofs: &[Vec<u8>]) -> String {
    let b64_strings: Vec<String> = proofs
        .iter()
        .map(|p| base64::engine::general_purpose::STANDARD.encode(p))
        .collect();
    serde_json::to_string(&b64_strings).expect("JSON encoding of string array cannot fail")
}

// ---------------------------------------------------------------------------
// WinningPoSt vanilla proof generation
// ---------------------------------------------------------------------------

/// Generate vanilla proofs for WinningPoSt.
///
/// 1. Determines which sectors are challenged via `generate_winning_post_sector_challenge`
/// 2. Generates fallback challenges for those sectors
/// 3. Generates vanilla proofs for each challenged sector
///
/// For testing, we only have a single sector, so `sector_set_len` = 1 and the
/// challenged sector is always sector 0 (index into our single-sector set).
pub fn gen_winning_post_vanilla(
    registered_proof: u64,
    sector_number: u64,
    miner_id: u64,
    randomness: [u8; 32],
    comm_r: Commitment,
    cache_dir: PathBuf,
    sealed_path: PathBuf,
    output_path: PathBuf,
) -> Result<()> {
    let post_proof_type = registered_post_proof_from_u64(registered_proof)
        .context("invalid registered proof for WinningPoSt")?;
    let prover_id = make_prover_id(miner_id);
    let sector_id = SectorId::from(sector_number);

    eprintln!(
        "Generating WinningPoSt vanilla proofs (proof_type={:?}, sector={}, miner={})",
        post_proof_type, sector_number, miner_id
    );

    // Step 1: Determine challenged sectors.
    // For WinningPoSt, we need to know which sectors from the proving set are challenged.
    // With a single sector, the challenge index will be 0.
    let sector_set_len: u64 = 1; // We have a single test sector
    let challenged_indices = post::generate_winning_post_sector_challenge(
        post_proof_type,
        &randomness,
        sector_set_len,
        prover_id,
    )
    .context("generate_winning_post_sector_challenge failed")?;

    eprintln!(
        "  Challenged sector indices: {:?} (out of {} sectors)",
        challenged_indices, sector_set_len
    );

    // Map indices to our single sector. All indices should be 0 since sector_set_len=1.
    let pub_sectors: Vec<SectorId> = challenged_indices
        .iter()
        .map(|_idx| sector_id) // All map to our single sector
        .collect();

    // Step 2: Generate fallback challenges per sector.
    let sector_challenges: BTreeMap<SectorId, Vec<u64>> =
        post::generate_fallback_sector_challenges(
            post_proof_type,
            &randomness,
            &pub_sectors,
            prover_id,
        )
        .context("generate_fallback_sector_challenges failed")?;

    eprintln!(
        "  Generated challenges for {} sector(s)",
        sector_challenges.len()
    );

    // Step 3: Generate vanilla proof for each challenged sector.
    let replica_info = PrivateReplicaInfo::new(
        post_proof_type,
        comm_r,
        cache_dir.clone(),
        sealed_path.clone(),
    );

    let mut vanilla_proofs: Vec<Vec<u8>> = Vec::new();
    for (sid, challenges) in &sector_challenges {
        eprintln!(
            "  Generating vanilla proof for sector {} ({} challenges)...",
            u64::from(*sid),
            challenges.len()
        );
        let proof: Vec<u8> =
            post::generate_single_vanilla_proof(post_proof_type, *sid, &replica_info, challenges)
                .with_context(|| {
                format!(
                    "generate_single_vanilla_proof failed for sector {}",
                    u64::from(*sid)
                )
            })?;
        eprintln!("    Vanilla proof size: {} bytes", proof.len());
        vanilla_proofs.push(proof);
    }

    // Write output
    let json = encode_vanilla_proofs_json(&vanilla_proofs);
    std::fs::write(&output_path, &json)
        .with_context(|| format!("failed to write output to {}", output_path.display()))?;

    eprintln!(
        "Wrote {} vanilla proof(s) ({} bytes) to {}",
        vanilla_proofs.len(),
        json.len(),
        output_path.display()
    );
    eprintln!("Randomness (hex): {}", hex::encode(randomness));

    Ok(())
}

// ---------------------------------------------------------------------------
// WindowPoSt vanilla proof generation
// ---------------------------------------------------------------------------

/// Generate vanilla proofs for WindowPoSt.
///
/// WindowPoSt challenges all sectors. For our single-sector test setup,
/// this generates challenges and vanilla proofs for one sector.
pub fn gen_window_post_vanilla(
    registered_proof: u64,
    sector_number: u64,
    miner_id: u64,
    randomness: [u8; 32],
    comm_r: Commitment,
    cache_dir: PathBuf,
    sealed_path: PathBuf,
    output_path: PathBuf,
) -> Result<()> {
    let post_proof_type = registered_post_proof_from_u64(registered_proof)
        .context("invalid registered proof for WindowPoSt")?;
    let prover_id = make_prover_id(miner_id);
    let sector_id = SectorId::from(sector_number);

    eprintln!(
        "Generating WindowPoSt vanilla proofs (proof_type={:?}, sector={}, miner={})",
        post_proof_type, sector_number, miner_id
    );

    // WindowPoSt: all sectors are challenged. Generate fallback challenges.
    let pub_sectors = vec![sector_id];
    let sector_challenges: BTreeMap<SectorId, Vec<u64>> =
        post::generate_fallback_sector_challenges(
            post_proof_type,
            &randomness,
            &pub_sectors,
            prover_id,
        )
        .context("generate_fallback_sector_challenges failed")?;

    eprintln!(
        "  Generated challenges for {} sector(s)",
        sector_challenges.len()
    );

    let replica_info = PrivateReplicaInfo::new(
        post_proof_type,
        comm_r,
        cache_dir.clone(),
        sealed_path.clone(),
    );

    let mut vanilla_proofs: Vec<Vec<u8>> = Vec::new();
    for (sid, challenges) in &sector_challenges {
        eprintln!(
            "  Generating vanilla proof for sector {} ({} challenges)...",
            u64::from(*sid),
            challenges.len()
        );
        let proof: Vec<u8> =
            post::generate_single_vanilla_proof(post_proof_type, *sid, &replica_info, challenges)
                .with_context(|| {
                format!(
                    "generate_single_vanilla_proof failed for sector {}",
                    u64::from(*sid)
                )
            })?;
        eprintln!("    Vanilla proof size: {} bytes", proof.len());
        vanilla_proofs.push(proof);
    }

    // Write output
    let json = encode_vanilla_proofs_json(&vanilla_proofs);
    std::fs::write(&output_path, &json)
        .with_context(|| format!("failed to write output to {}", output_path.display()))?;

    eprintln!(
        "Wrote {} vanilla proof(s) ({} bytes) to {}",
        vanilla_proofs.len(),
        json.len(),
        output_path.display()
    );
    eprintln!("Randomness (hex): {}", hex::encode(randomness));

    Ok(())
}

// ---------------------------------------------------------------------------
// SnapDeals vanilla proof generation
// ---------------------------------------------------------------------------

/// Generate vanilla partition proofs for SnapDeals (sector update).
///
/// Calls `generate_partition_proofs()` which reads the original and updated
/// sector data/trees and produces partition-level vanilla proofs.
pub fn gen_snap_vanilla(
    registered_proof: u64,
    comm_r_old: Commitment,
    comm_r_new: Commitment,
    comm_d_new: Commitment,
    sector_key_path: PathBuf,
    sector_key_cache_path: PathBuf,
    replica_path: PathBuf,
    replica_cache_path: PathBuf,
    output_path: PathBuf,
) -> Result<()> {
    let update_proof_type = registered_update_proof_from_u64(registered_proof)
        .context("invalid registered proof for SnapDeals")?;

    eprintln!(
        "Generating SnapDeals vanilla partition proofs (proof_type={:?})",
        update_proof_type,
    );
    eprintln!("  sector_key:       {}", sector_key_path.display());
    eprintln!("  sector_key_cache: {}", sector_key_cache_path.display());
    eprintln!("  replica:          {}", replica_path.display());
    eprintln!("  replica_cache:    {}", replica_cache_path.display());
    eprintln!("  comm_r_old: {}", hex::encode(comm_r_old));
    eprintln!("  comm_r_new: {}", hex::encode(comm_r_new));
    eprintln!("  comm_d_new: {}", hex::encode(comm_d_new));

    let partition_proofs = update::generate_partition_proofs(
        update_proof_type,
        comm_r_old,
        comm_r_new,
        comm_d_new,
        &sector_key_path,
        &sector_key_cache_path,
        &replica_path,
        &replica_cache_path,
    )
    .context("generate_partition_proofs failed")?;

    eprintln!("  Generated {} partition proof(s)", partition_proofs.len());

    // Convert PartitionProofBytes to Vec<Vec<u8>>
    let proofs: Vec<Vec<u8>> = partition_proofs.into_iter().map(|p| p.0).collect();

    for (i, p) in proofs.iter().enumerate() {
        eprintln!("    Partition {} proof size: {} bytes", i, p.len());
    }

    // Write output
    let json = encode_vanilla_proofs_json(&proofs);
    std::fs::write(&output_path, &json)
        .with_context(|| format!("failed to write output to {}", output_path.display()))?;

    eprintln!(
        "Wrote {} partition proof(s) ({} bytes) to {}",
        proofs.len(),
        json.len(),
        output_path.display()
    );

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_commitment_cid_comm_r() {
        // CommR from golden data: bagboea4b5abcbx3jccdohrttzfneleehcpkmle4oltwuh4q3rcf5tpdodaoj6mtl
        let comm_r = parse_commitment_cid(
            "bagboea4b5abcbx3jccdohrttzfneleehcpkmle4oltwuh4q3rcf5tpdodaoj6mtl",
        )
        .unwrap();
        assert_eq!(comm_r.len(), 32);
        // The digest should be a valid non-zero commitment
        assert_ne!(comm_r, [0u8; 32]);
    }

    #[test]
    fn test_parse_commitment_cid_comm_d() {
        // CommD from golden data: baga6ea4seaqao7s73y24kcutaosvacpdjgfe5pw76ooefnyqw4ynr3d2y6x2mpq
        let comm_d = parse_commitment_cid(
            "baga6ea4seaqao7s73y24kcutaosvacpdjgfe5pw76ooefnyqw4ynr3d2y6x2mpq",
        )
        .unwrap();
        assert_eq!(comm_d.len(), 32);
        assert_ne!(comm_d, [0u8; 32]);
    }

    #[test]
    fn test_parse_commdr_file() {
        let contents =
            "d:baga6ea4seaqao7s73y24kcutaosvacpdjgfe5pw76ooefnyqw4ynr3d2y6x2mpq r:bagboea4b5abcbx3jccdohrttzfneleehcpkmle4oltwuh4q3rcf5tpdodaoj6mtl";
        let (comm_d, comm_r) = parse_commdr_file(contents).unwrap();
        assert_ne!(comm_d, [0u8; 32]);
        assert_ne!(comm_r, [0u8; 32]);
        assert_ne!(comm_d, comm_r);
    }

    #[test]
    fn test_parse_commdr_file_bad_format() {
        assert!(parse_commdr_file("single-item").is_err());
        assert!(parse_commdr_file("x:foo y:bar").is_err());
        assert!(parse_commdr_file("d:foo").is_err());
    }

    #[test]
    fn test_encode_vanilla_proofs_json() {
        let proofs = vec![vec![1, 2, 3], vec![4, 5, 6]];
        let json = encode_vanilla_proofs_json(&proofs);
        // Should be JSON array of base64 strings
        let parsed: Vec<String> = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed.len(), 2);

        // Verify round-trip
        let decoded: Vec<Vec<u8>> = parsed
            .iter()
            .map(|s| base64::engine::general_purpose::STANDARD.decode(s).unwrap())
            .collect();
        assert_eq!(decoded, proofs);
    }
}
