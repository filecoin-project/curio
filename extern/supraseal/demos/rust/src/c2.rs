// Copyright Supranational LLC

use anyhow::Context;
use bincode::deserialize;
use filecoin_proofs_api::{RegisteredSealProof, SectorId};
use filecoin_proofs_v1::{
    PoRepConfig, ProverId, seal_commit_phase2, SealCommitPhase1Output,
    verify_seal, with_shape
};

use std::fs::read;
use std::path::PathBuf;
use storage_proofs_core::{
    api_version::ApiVersion,
    merkle::MerkleTreeTrait,
};
use std::time::Instant;

use std::thread;
use std::sync::{Arc, Mutex};

fn run_c2<Tree: 'static + MerkleTreeTrait>(
    num_sectors: usize,
    c1_dir: &str,
    start_sector_id: usize,
    porep_config: Arc<PoRepConfig>,
) -> usize {
    // Choose some fixed values for demonstration
    // All sectors using the same prover id, ticket, and wait seed
    let prover_id: ProverId = [9u8; 32];
    let slots_sector_id = Arc::new(Mutex::new(start_sector_id));
    let successes = Arc::new(Mutex::new(0));

    let mut provers = vec![];

    let commit_phase1_output_path =
        PathBuf::from(c1_dir);

    for _ in 0..2 {
        let slots_sector_id = Arc::clone(&slots_sector_id);
        let successes = Arc::clone(&successes);
        let commit_phase1_output_base_path = commit_phase1_output_path.clone();
        let porep_config = Arc::clone(&porep_config);

        let prover = thread::spawn(move || {
            loop {
                let mut sector_lock = slots_sector_id.lock().unwrap();
                let cur_sector = *sector_lock;
                let sector_slot = cur_sector - start_sector_id;
                let sector_id = SectorId::from(cur_sector as u64);

                *sector_lock += 1;
                drop(sector_lock);
                if sector_slot >= num_sectors {
                    println!("Exiting, sector_slot {} num_sectors {}",
                             sector_slot, num_sectors);
                    break;
                }
                println!("Starting c2 sector {}", cur_sector);

                let commit_phase1_output = {
                    let mut commit_phase1_output_path = commit_phase1_output_base_path.clone();
                    commit_phase1_output_path.push(
                        format!("{:03}/commit-phase1-output", sector_slot)
                    );
                    println!("Restoring commit phase1 output file {:?}", commit_phase1_output_path);
                    let commit_phase1_output_bytes =
                        read(&commit_phase1_output_path).with_context(|| {
                            format!(
                                "couldn't read commit_phase1_output_path={:?}",
                                commit_phase1_output_path
                            )
                        }).unwrap();

                    let res: SealCommitPhase1Output<Tree> =
                        deserialize(&commit_phase1_output_bytes).unwrap();
                    res
                };

                let SealCommitPhase1Output {
                    vanilla_proofs: _,
                    comm_d,
                    comm_r,
                    replica_id: _,
                    seed,
                    ticket,
                } = commit_phase1_output;

                println!("Starting seal_commit_phase2 sector {}", sector_slot);
                let now = Instant::now();
                let commit_output = seal_commit_phase2(
                    &porep_config,
                    commit_phase1_output,
                    prover_id,
                    sector_id
                )
                .unwrap();
                println!("seal_commit_phase2 took: {:.2?}", now.elapsed());

                let result = verify_seal::<Tree>(
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
                    *successes.lock().unwrap() += 1;
                } else {
                    println!("Verification FAILED!");
                }
            }
        });
        provers.push(prover);
    }
    for prover in provers {
        prover.join().unwrap();
    }
    let count = *successes.lock().unwrap();
    count
}

fn c2_caller(
    num_sectors: usize,
    c1_dir: &str,
    start_sector_id: usize,
    sector_size: u64,
    porep_id: [u8; 32],
) -> usize {
    let porep_config = Arc::new(PoRepConfig::new_groth16(
        sector_size,
        porep_id,
        ApiVersion::V1_1_0));

    with_shape!(sector_size, run_c2, num_sectors, c1_dir, start_sector_id, porep_config)
}

fn main() {
    let num_sectors: usize = 128;
    //let num_sectors: usize = 64;
    //let num_sectors: usize = 32;

    let args: Vec<String> = std::env::args().collect();

    if args.len() < 4 {
        println!("Usage: c2 <cache_path> <start_sector_id> <sector_size> e.g 32GiB");
        std::process::exit(-1);
    }
    println!("path:         {:?}", args[1]);
    println!("start sector: {:?}", args[2]);
    println!("sector size:  {:?}", args[3]);
    let c1_dir = &args[1];
    let start_sector_id: usize = args[2].trim().parse().expect("Wanted a number");
    let sector_size_string = &args[3];

    let sector_size = match sector_size_string.as_str() {
        "2KiB"   => 2048,
        "4KiB"   => 4096,
        "16KiB"  => 16384,
        "32KiB"  => 32768,
        "8MiB"   => 8388608,
        "16MiB"  => 16777216,
        "512MiB" => 536870912,
        "1GiB"   => 1073741824,
        "32GiB"  => 34359738368,
        "64GiB"  => 68719476736,
        _ => panic!("Invalid sector size"),
    };

    let porep_id: [u8; 32] = match sector_size_string.as_str() {
        "2KiB"   => RegisteredSealProof::StackedDrg2KiBV1_1.as_v1_config().porep_id,
        "8MiB"   => RegisteredSealProof::StackedDrg8MiBV1_1.as_v1_config().porep_id,
        "512MiB" => RegisteredSealProof::StackedDrg512MiBV1_1.as_v1_config().porep_id,
        "32GiB"  => RegisteredSealProof::StackedDrg32GiBV1_1.as_v1_config().porep_id,
        "64GiB"  => RegisteredSealProof::StackedDrg64GiBV1_1.as_v1_config().porep_id,
        _ => [99u8; 32], // use an arbitrary porep_id for other sizes
    };
    
    let successes = c2_caller(num_sectors, &c1_dir, start_sector_id, sector_size, porep_id);

    std::process::exit((num_sectors - successes) as i32);
}
