// Copyright Supranational LLC

const COMMIT_PHASE1_OUTPUT_FILE: &str = "resources/test/commit-phase1-output";

use anyhow::Context;
use bincode::deserialize;
use std::fs::read;
use std::path::PathBuf;
use std::time::Instant;

use filecoin_proofs::{
    constants::SECTOR_SIZE_32_GIB, seal_commit_phase2, verify_seal,
    PoRepConfig, SealCommitPhase1Output, SectorShape32GiB,
};
use storage_proofs_core::{api_version::ApiVersion, sector::SectorId};

#[test]
fn run_seal() {
    let commit_phase1_output = {
        let mut commit_phase1_output_path = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        commit_phase1_output_path.push(COMMIT_PHASE1_OUTPUT_FILE);
        println!("*** Restoring commit phase1 output file");
        let commit_phase1_output_bytes = read(&commit_phase1_output_path)
            .with_context(|| {
                format!(
                    "couldn't read file commit_phase1_output_path={:?}",
                    commit_phase1_output_path
                )
            })
            .unwrap();
        println!(
            "commit_phase1_output_bytes len {}",
            commit_phase1_output_bytes.len()
        );

        let res: SealCommitPhase1Output<SectorShape32GiB> =
            deserialize(&commit_phase1_output_bytes).unwrap();
        res
    };

    let sector_id = SectorId::from(0);
    let prover_id: [u8; 32] = [9u8; 32];
    let arbitrary_porep_id = [99; 32];

    let porep_config =
        PoRepConfig::new_groth16(SECTOR_SIZE_32_GIB, arbitrary_porep_id, ApiVersion::V1_1_0);

    let SealCommitPhase1Output {
        vanilla_proofs: _,
        comm_d,
        comm_r,
        replica_id: _,
        seed,
        ticket,
    } = commit_phase1_output;

    println!("Starting seal_commit_phase2");
    let now = Instant::now();
    let commit_output =
        seal_commit_phase2(&porep_config, commit_phase1_output, prover_id, sector_id).unwrap();
    println!("seal_commit_phase2 took: {:.2?}", now.elapsed());

    println!("Verifying result");
    let result = verify_seal::<SectorShape32GiB>(
        &porep_config,
        comm_r,
        comm_d,
        prover_id,
        sector_id,
        ticket,
        seed,
        &commit_output.proof,
    )
    .unwrap();

    if result == true {
        println!("Verification PASSED!");
    } else {
        println!("Verification FAILED!");
    }

    assert!(result, "Verification FAILED");
}
