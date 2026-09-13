//! §8.3 **C.2 slice:** [`cuzk_vk::srs::srs_read_file_spawn`] overlaps disk read with the caller thread.

use std::path::PathBuf;

use cuzk_vk::srs::{srs_read_file, srs_read_file_spawn, srs_synthetic_partition_smoke_blob};

#[test]
fn srs_read_file_spawn_matches_sync_read() {
    let blob = srs_synthetic_partition_smoke_blob();
    let dir = std::env::temp_dir();
    let path: PathBuf = dir.join(format!("cuzk_srs_async_{}.bin", std::process::id()));
    std::fs::write(&path, &blob).expect("write temp srs");

    let handle = srs_read_file_spawn(path.clone());
    let sync = srs_read_file(&path).expect("sync read");
    let async_result = handle.join().expect("thread join").expect("async read");

    std::fs::remove_file(&path).ok();
    assert_eq!(async_result, blob);
    assert_eq!(sync, blob);
}
