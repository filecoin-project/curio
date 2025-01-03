// Copyright Supranational LLC

// This is a basic demonstration of the sealing pipeline, the bindings
// interface and order of operations from a rust perspective

#![feature(vec_into_raw_parts)]

use filecoin_proofs_api::RegisteredSealProof;
use filecoin_proofs_v1::{
    ProverId,
    Ticket,
    with_shape,
};

use sha2::{Digest, Sha256};
use std::ffi::CString;
use std::os::raw::c_char;
use std::os::unix::ffi::OsStrExt;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};
use std::thread;
use storage_proofs_core::{
    merkle::MerkleTreeTrait,
};

extern crate chrono;

pub type ReplicaId = Vec<u8>;

// C Bindings
extern "C" {
    // Optional init function. Default config file is supra_config.cfg
    fn supra_seal_init(sector_size: usize, config_filename: *const c_char);

    fn get_max_block_offset(sector_size: usize) -> usize;

    fn get_slot_size(num_sectors: usize, sector_size: usize) -> usize;

    fn pc1(block_offset: usize,
           num_sectors: usize,
           replica_ids: *const u8,
           parents_filename: *const c_char,
           sector_size: usize) -> u32;

    fn pc2(block_offset: usize,
           num_sectors: usize,
           output_dir: *const c_char,
           data_filenames: *const *const c_char,
           sector_size: usize) -> u32;

    fn pc2_cleanup(num_sectors: usize,
                   output_dir: *const c_char,
                   sector_size: usize) -> u32;

    fn c1(block_offset: usize,
          num_sectors: usize,
          sector_slot: usize,
          replica_id: *const u8,
          seed: *const u8,
          ticket: *const u8,
          cache_path: *const c_char,
          parents_filename: *const c_char,
          replica_path: *const c_char,
          sector_size: usize) -> u32;
}

pub fn init_wrapper<T: AsRef<std::path::Path>>(sector_size: usize, config: T) {
    let config_c = CString::new(config.as_ref().as_os_str().as_bytes()).unwrap();
    unsafe {
        supra_seal_init(sector_size, config_c.as_ptr());
    };
}

// Rust wrappers around unsafe C calls
pub fn get_max_block_offset_wrapper(sector_size: usize) -> usize {
    let max_offset = unsafe { get_max_block_offset(sector_size) };
    println!("Max Offset returned {:x}", max_offset);
    return max_offset;
}

pub fn get_slot_size_wrapper(num_sectors: usize, sector_size: usize) -> usize {
    let slot_size = unsafe { get_slot_size(num_sectors, sector_size) };
    println!("Slot size returned {:x} for {} sectors sized {}", slot_size, num_sectors, sector_size);
    return slot_size;
}

pub fn pc1_wrapper<T: AsRef<std::path::Path>>(
    block_offset: usize,
    num_sectors: usize,
    replica_ids: Vec<ReplicaId>,
    path: T,
    sector_size: usize) -> u32 {

    let f = replica_ids.into_iter().flatten().collect::<Vec<u8>>();
    let path_c = CString::new(path.as_ref().as_os_str().as_bytes()).unwrap();
    let pc1_status = unsafe {
        pc1(block_offset, num_sectors, f.as_ptr(), path_c.as_ptr(), sector_size)
    };
    println!("PC1 returned {}", pc1_status);
    return pc1_status;
}

pub fn pc2_wrapper<T: AsRef<std::path::Path>>(
    block_offset: usize,
    num_sectors: usize,
    path: T,
    sector_size: usize) -> u32 {

    let path_c = CString::new(path.as_ref().as_os_str().as_bytes()).unwrap();
    let pc2_status = unsafe { pc2(block_offset, num_sectors,
                                  path_c.as_ptr(), std::ptr::null(),
                                  sector_size) };
    println!("PC2 returned {}", pc2_status);
    return pc2_status;
}

pub fn pc2_cleanup_wrapper<T: AsRef<std::path::Path>>(
    num_sectors: usize,
    path: T,
    sector_size: usize) -> u32 {

    let path_c = CString::new(path.as_ref().as_os_str().as_bytes()).unwrap();
    let pc2_status = unsafe { pc2_cleanup(num_sectors, path_c.as_ptr(), sector_size) };
    println!("PC2 cleanup returned {}", pc2_status);
    return pc2_status;
}

pub fn c1_wrapper<T: AsRef<std::path::Path>>(
    block_offset: usize,
    num_sectors: usize,
    sector_id: usize,
    replica_id: *const u8,
    seed: *const u8,
    ticket: *const u8,
    cache_path: T,
    parents_filename: T,
    replica_path: T,
    sector_size: usize) -> u32 {

    let cache_path_c =
        CString::new(cache_path.as_ref().as_os_str().as_bytes()).unwrap();
    let parents_c =
        CString::new(parents_filename.as_ref().as_os_str().as_bytes()).unwrap();
    let replica_path_c =
        CString::new(replica_path.as_ref().as_os_str().as_bytes()).unwrap();

    let c1_status = unsafe {
        c1(block_offset,
           num_sectors,
           sector_id,
           replica_id,
           seed,
           ticket,
           cache_path_c.as_ptr(),
           parents_c.as_ptr(),
           replica_path_c.as_ptr(),
           sector_size)
    };
    println!("C1 returned {}", c1_status);
    return c1_status;
}

// Helper function to create replica ids
fn create_replica_id(
    prover_id: ProverId,
    sector_size_string: &str,
    sector_id: u64,
    comm_d: &[u8; 32],
    ticket: Ticket,
) -> ReplicaId {
    let porep_id: [u8; 32] = match sector_size_string {
        "2KiB"   => RegisteredSealProof::StackedDrg2KiBV1_1.as_v1_config().porep_id,
        "8MiB"   => RegisteredSealProof::StackedDrg8MiBV1_1.as_v1_config().porep_id,
        "512MiB" => RegisteredSealProof::StackedDrg512MiBV1_1.as_v1_config().porep_id,
        "32GiB"  => RegisteredSealProof::StackedDrg32GiBV1_1.as_v1_config().porep_id,
        "64GiB"  => RegisteredSealProof::StackedDrg64GiBV1_1.as_v1_config().porep_id,
        _ => [99u8; 32], // use an arbitrary porep_id for other sizes
    };

    // Print porep_id
    println!("PoRep ID for sector size {}: {:?}", sector_size_string, porep_id);

    let hash = Sha256::new()
        .chain_update(&prover_id)
        .chain_update(sector_id.to_be_bytes())
        .chain_update(&ticket)
        .chain_update(comm_d)
        .chain_update(&porep_id)
        .finalize();

    let mut id = [0u8; 32];
    id.copy_from_slice(&hash);
    id[31] &= 0b0011_1111;

    // Print id as hex
    println!("Replica ID (hex): {:x?}", id);

    id.to_vec()
}

fn run_pipeline<Tree: 'static + MerkleTreeTrait>(
    num_sectors: usize,
    c2_cores: &str,
    parents_cache_filename: &str,
    comm_d: [u8; 32],
    sector_size: usize,
    sector_size_str: &str,
) {
    let wait_seed_time = match sector_size_str {
        "32GiB"  => Duration::from_secs(60 * 75), // 75 min
        "64GiB"  => Duration::from_secs(60 * 75),
        _ => Duration::from_secs(30),             // 30 sec
    };

    let mut parents_cache_file = PathBuf::new();
    parents_cache_file.push(parents_cache_filename);

    // This is optional but if present must be the first call into the library
    //init_wrapper("supra_seal_zen.cfg");

    let max_offset = get_max_block_offset_wrapper(sector_size);
    let slot_size = get_slot_size_wrapper(num_sectors, sector_size);

    println!("max_offset {} and slot_size {}", max_offset, slot_size);

    // Choose some fixed values for demonstration
    // All sectors using the same prover id, ticket, and wait seed
    let prover_id: ProverId = [ 9u8; 32];
    let ticket: Ticket = [ 1u8; 32];
    let seed: Ticket = [ 1u8; 32];

    // Example showing operations running in parallel
    let pc1_counter = Arc::new(Mutex::new((0, 0)));
    let pc2_counter = Arc::new(Mutex::new(0));
    let c1_counter = Arc::new(Mutex::new(0));
    let c2_counter = Arc::new(Mutex::new(0));

    //let passed_counter = Arc::new(Mutex::new(0));
    let failed_counter = Arc::new(Mutex::new(0));
    let gpu_counter = Arc::new(Mutex::new(0));

    let pipeline_start = Arc::new(Mutex::new(Instant::now()));
    let gpu_lock = Arc::new(Mutex::new(false));

    // Specify the number of slots that can be run at a time
    // This will come down to resources on the machine and number of sectors
    //   being sealed in parallel.
    let num_slots = 2; // This matches mutex array below
    let slot_counter = Arc::new([Mutex::new(0), Mutex::new(0)]);
    let mut pipelines = vec![];

    // Demonstrate three passes through the pipeline
    // Batch 0  PC1  PC2   C1   C2
    // Batch 1       PC1  PC2   C1   C2
    // Batch 2            PC1  PC2   C1   C2
    let num_batches = 3;
    for batch_num in 0..num_batches {
        let pc1_counter = Arc::clone(&pc1_counter);
        let pc2_counter = Arc::clone(&pc2_counter);
        let c1_counter = Arc::clone(&c1_counter);
        let c2_counter = Arc::clone(&c2_counter);
        let c2_cores = c2_cores.to_string();

        let failed_counter = Arc::clone(&failed_counter);
        let gpu_counter = Arc::clone(&gpu_counter);
        let slot_counter = Arc::clone(&slot_counter);
        let parents_cache_file = parents_cache_file.clone();

        let pipeline_start = Arc::clone(&pipeline_start);
        let gpu_lock = Arc::clone(&gpu_lock);

        let sector_size_string = sector_size_str.to_string();

        let pipeline = thread::spawn(move || {
            let pipe_dir = "/var/tmp/supra_seal/".to_owned() + &batch_num.to_string();
            let output_dir = Path::new(&pipe_dir);

            // Grab unique sector ids for each sector in the batch
            let batch_sector_start = batch_num * num_sectors;

            // Create replica_ids
            let mut cur_sector_id = batch_sector_start;
            let mut replica_ids: Vec<ReplicaId> = Vec::new();
            for _ in 0..num_sectors {
                let replica_id = create_replica_id(
                    prover_id,
                    &sector_size_string,
                    cur_sector_id as u64,
                    &comm_d,
                    ticket);

                cur_sector_id += 1; // Increment sector id for each sector
                replica_ids.push(replica_id);
            }

            // Indent based on batch number
            let mut indent: String = "".to_owned();
            for _x in 0..batch_num {
                indent += "    ";
            }

            //if batch_num > 0 { // TODO SNP: testing only, remove
            // Wait until it's time for this batch's pc1 to start
            {
                let mut pc1_counter_lock = pc1_counter.lock().unwrap();
                while (*pc1_counter_lock).0 != batch_num {
                    drop(pc1_counter_lock);
                    thread::sleep(Duration::from_millis(100));
                    pc1_counter_lock = pc1_counter.lock().unwrap();
                }
            }

            // Lock the slot
            let cur_slot = batch_num % num_slots;
            let mut slot_count = slot_counter[cur_slot].lock().unwrap();
            println!("{}**** {} Batch {} locked slot {}", indent,
                     chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                     batch_num, cur_slot);
            *slot_count += 1;

            // Lock PC1
            let cur_offset;
            let cp_replica_ids = replica_ids.clone();
            {
                let mut pc1_count = pc1_counter.lock().unwrap();
                println!("{}**** {} Batch {} start PC1", indent,
                         chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                         batch_num);
                cur_offset = (*pc1_count).1;
                (*pc1_count).1 += slot_size;

                // Check if pc1 will overflow available disk space
                if ((*pc1_count).1 + slot_size) >  max_offset {
                    (*pc1_count).1 = 0;
                }

                // Do PC1
                //if batch_num > 0 { // TODO SNP: testing only, remove
                    pc1_wrapper(cur_offset, num_sectors,
                                replica_ids, parents_cache_file.clone(),
                                sector_size);
                //}
                (*pc1_count).0 += 1;
                println!("{}**** {} Batch {} done with PC1", indent,
                         chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                         batch_num);
            }

            // PC2
            {
                let mut gpu_lock = gpu_lock.lock().unwrap();
                *gpu_lock = true;

                if batch_num == 1 {
                    let mut pipeline_start_lock = pipeline_start.lock().unwrap();
                    *pipeline_start_lock = Instant::now();
                    drop(pipeline_start_lock);
                    println!("\n**** {} Pipeline start\n",
                             chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"));
                }

                let mut pc2_count = pc2_counter.lock().unwrap();
                let mut gpu_count = gpu_counter.lock().unwrap();
                println!("{}**** {} Batch {} start PC2", indent,
                         chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                         batch_num);
                *gpu_count += 1;

                pc2_wrapper(cur_offset, num_sectors, output_dir, sector_size);
                *pc2_count += 1;
                *gpu_lock = false;
                println!("{}**** {} Batch {} done with PC2", indent,
                         chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                         batch_num);
            }

            println!("{}**** {} Batch {} Wait Seed sleeping {:?}", indent,
                     chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                     batch_num, wait_seed_time);
            thread::sleep(wait_seed_time);
            println!("{}**** {} Batch {} Wait Seed done sleeping", indent,
                     chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                     batch_num);

            // C1
            // Each of these can be parallelized, however the function only
            //  operates on a single sector at a time as opposed to PC1/PC2
            println!("{}**** {} Batch {} start C1", indent,
                     chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                     batch_num);
            for sector_slot in 0..num_sectors {
                let mut cur_cache_path = PathBuf::from(output_dir);
                cur_cache_path.push(format!("{:03}", sector_slot));
                let mut cur_replica_dir = PathBuf::from(output_dir);
                cur_replica_dir.push(format!("{:03}", sector_slot));

                c1_wrapper(cur_offset,
                           num_sectors,
                           sector_slot,
                           cp_replica_ids[sector_slot].as_ptr(),
                           seed.as_ptr(),
                           ticket.as_ptr(),
                           cur_cache_path,
                           //parents_cache_file.to_path_buf(),
                           parents_cache_file.clone(),
                           cur_replica_dir,
                           sector_size);
            }
            *c1_counter.lock().unwrap() += 1;
            println!("{}**** {} Batch {} done with C1", indent,
                     chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                     batch_num);

            // At this point the layers on NVME for this batch can be reused
            println!("{}**** {} Batch {} dropping lock on slot {}", indent,
                     chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                     batch_num, cur_slot);
            drop(slot_count);

            // Delete pc2 content
            println!("{}**** {} Batch {} cleanup pc2 on slot {}", indent,
                     chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                     batch_num, cur_slot);
            pc2_cleanup_wrapper(num_sectors, output_dir, sector_size);

            // Wait until it's time for this batch's c2 to start, which is after
            // the next batch's pc2
            if batch_num < num_batches - 1 {
                let mut pc2_counter_lock = pc2_counter.lock().unwrap();
                while *pc2_counter_lock != batch_num + 2 {
                    drop(pc2_counter_lock);
                    thread::sleep(Duration::from_millis(100));
                    pc2_counter_lock = pc2_counter.lock().unwrap();
                }
            }

            // TODO SNP: testing only, remove
            // } else {
            //     let cur_slot = batch_num % num_slots;
            //     *slot_counter[cur_slot].lock().unwrap() += 1;
            //     let mut pc1_count = pc1_counter.lock().unwrap();
            //     (*pc1_count).1 += slot_size;
            //     // Check if pc1 will overflow available disk space
            //     if ((*pc1_count).1 + slot_size) >  max_offset {
            //         (*pc1_count).1 = 0;
            //     }
            //     (*pc1_count).0 += 1;

            //     *pc2_counter.lock().unwrap() += 1;
            //     *c1_counter.lock().unwrap() += 1;
            // }

            // C2
            let mut gpu_lock = gpu_lock.lock().unwrap();
            *gpu_lock = true;
            println!("{}**** {} Batch {} start C2", indent,
                     chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                     batch_num);
            let mut c2_count = c2_counter.lock().unwrap();
            *gpu_counter.lock().unwrap() += 1;

            let now = Instant::now();
            let status = std::process::Command::new("/usr/bin/taskset")
                .arg("-c").arg(c2_cores).arg("./target/release/c2")
                .arg(output_dir).arg(batch_sector_start.to_string())
                .arg(sector_size_string)
                .status().expect("failed to execute process");
            println!("status: {}", status);
            println!("{}**** {} Batch {} C2 done took {:.2?}", indent,
                     chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                     batch_num, now.elapsed());
            *failed_counter.lock().unwrap() += status.code().unwrap();

            if batch_num == 0 {
                let pipeline_start_lock = pipeline_start.lock().unwrap();
                println!("**** {} Pipeline took {:?}\n",
                         chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                         (*pipeline_start_lock).elapsed());
            }
            println!("{}**** {} Batch {} done with C2", indent,
                     chrono::Local::now().format("%Y-%m-%d %H:%M:%S %s"),
                     batch_num);
            *c2_count += 1;
            drop(c2_count);
            *gpu_lock = false;
            drop(gpu_lock);
        });
        pipelines.push(pipeline);
    }

    for pipeline in pipelines {
        pipeline.join().unwrap();
    }

    let pc1_count = pc1_counter.lock().unwrap();
    println!("PC1 counter: {} {}", (*pc1_count).0, (*pc1_count).1);
    println!("PC2 counter: {}", *pc2_counter.lock().unwrap());
    println!("C1 counter:  {}", *c1_counter.lock().unwrap());
    println!("C2 counter:  {}", *c2_counter.lock().unwrap());
    println!("GPU counter:  {}", *gpu_counter.lock().unwrap());
    println!("Failed counter:  {}", *failed_counter.lock().unwrap());
    for i in 0..num_slots {
      println!("Slot counter[{}]: {}", i, *slot_counter[i].lock().unwrap());
    }
}

fn pipeline_caller(num_sectors: usize, c2_cores: &str, sector_size_string: &str) {
    let parents_cache_filename = match sector_size_string {
        "2KiB"   => "/var/tmp/filecoin-parents/v28-sdr-parent-652bae61e906c0732e9eb95b1217cfa6afcce221ff92a8aedf62fa778fa765bc.cache",
        "4KiB"   => "/var/tmp/filecoin-parents/v28-sdr-parent-56d4865ec3476221fd1412409b5d9439182d71bf5e2078d0ecde76c0f7e33986.cache",
        "16KiB"  => "/var/tmp/filecoin-parents/v28-sdr-parent-cd17f936869de64be8cb1ae4496e788f6af982bc65f78bec83e33c42c7210a41.cache",
        "32KiB"  => "/var/tmp/filecoin-parents/v28-sdr-parent-81a0489b0dd6c7755cdce0917dd436288b6e82e17d596e5a23836e7a602ab9be.cache",
        "8MiB"   => "/var/tmp/filecoin-parents/v28-sdr-parent-1139cb33af3e3c24eb644da64ee8bc43a8df0f29fc96b5337bee369345884cdc.cache",
        "16MiB"  => "/var/tmp/filecoin-parents/v28-sdr-parent-7fa3ff8ffb57106211c4be413eb15ea072ebb363fa5a1316fe341ac8d7a03d51.cache",
        "512MiB" => "/var/tmp/filecoin-parents/v28-sdr-parent-7ba215a1d2345774ab90b8cb1158d296e409d6068819d7b8c7baf0b25d63dc34.cache",
        "1GiB"   => "/var/tmp/filecoin-parents/v28-sdr-parent-637f021bceb5248f0d1dcf4dbf132fedc025d0b3b55d3e7ac171c02676a96ccb.cache",
        "32GiB"  => "/var/tmp/filecoin-parents/v28-sdr-parent-21981246c370f9d76c7a77ab273d94bde0ceb4e938292334960bce05585dc117.cache",
        "64GiB"  => "/var/tmp/filecoin-parents/v28-sdr-parent-767ee5400732ee77b8762b9d0dd118e88845d28bfa7aee875dc751269f7d0b87.cache",
        _ => panic!("Invalid sector size"),
    };

    let comm_d: [u8; 32] = match sector_size_string {
        "2KiB"   => [252, 126, 146, 130, 150, 229, 22, 250, 173, 233, 134, 178, 143, 146, 212, 74, 79, 36, 185, 53, 72, 82, 35, 55, 106, 121, 144, 39, 188, 24, 248, 51],
        "4KiB"   => [8, 196, 123, 56, 238, 19, 188, 67, 244, 27, 145, 92, 14, 237, 153, 17, 162, 96, 134, 179, 237, 98, 64, 27, 249, 213, 139, 141, 25, 223, 246, 36],
        "16KiB"  => [249, 34, 97, 96, 200, 249, 39, 191, 220, 196, 24, 205, 242, 3, 73, 49, 70, 0, 142, 174, 251, 125, 2, 25, 77, 94, 84, 129, 137, 0, 81, 8],
        "32KiB"  => [44, 26, 150, 75, 185, 11, 89, 235, 254, 15, 109, 162, 154, 214, 90, 227, 228, 23, 114, 74, 143, 124, 17, 116, 90, 64, 202, 193, 229, 231, 64, 17],
        "8MiB"   => [101, 242, 158, 93, 152, 210, 70, 195, 139, 56, 140, 252, 6, 219, 31, 107, 2, 19, 3, 197, 162, 137, 0, 11, 220, 232, 50, 169, 195, 236, 66, 28],
        "16MiB"  => [162, 36, 117, 8, 40, 88, 80, 150, 91, 126, 51, 75, 49, 39, 176, 192, 66, 177, 208, 70, 220, 84, 64, 33, 55, 98, 124, 216, 121, 156, 225, 58],
        "512MiB" => [57, 86, 14, 123, 19, 169, 59, 7, 162, 67, 253, 39, 32, 255, 167, 203, 62, 29, 46, 80, 90, 179, 98, 158, 121, 244, 99, 19, 81, 44, 218, 6],
        "1GiB"   => [204, 195, 192, 18, 245, 176, 94, 129, 26, 43, 191, 221, 15, 104, 51, 184, 66, 117, 180, 123, 242, 41, 192, 5, 42, 130, 72, 79, 60, 26, 91, 61],
        "32GiB"  => [7, 126, 95, 222, 53, 197, 10, 147, 3, 165, 80, 9, 227, 73, 138, 78, 190, 223, 243, 156, 66, 183, 16, 183, 48, 216, 236, 122, 199, 175, 166, 62],
        "64GiB"  => [230, 64, 5, 166, 191, 227, 119, 121, 83, 184, 173, 110, 249, 63, 15, 202, 16, 73, 178, 4, 22, 84, 242, 164, 17, 247, 112, 39, 153, 206, 206, 2],
        _ => panic!("Invalid sector size"),
    };

    let sector_size: usize = match sector_size_string {
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

    with_shape!(
        sector_size as u64,
        run_pipeline,
        num_sectors,
        c2_cores,
        parents_cache_filename,
        comm_d,
        sector_size,
        sector_size_string
    );
}

fn main() {
    let num_sectors: usize = 128;
    //let num_sectors: usize = 64;
    //let num_sectors: usize = 32;

    // Cores for C2 use, in a string that can be passed to taskset
    let c2_cores = "4-7,45-47,48-63";

    let args: Vec<String> = std::env::args().collect();

    if args.len() < 2 {
        println!("Usage: supra-seal-demo <sector_size> e.g 32GiB");
        std::process::exit(-1);
    }
    println!("sector size:  {:?}", args[1]);
    let sector_size_string = &args[1];

    pipeline_caller(num_sectors, c2_cores, &sector_size_string.as_str());
}
