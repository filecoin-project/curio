//! Roadmap **§1**: append one real [`cuzk_vk::prove_groth16_partition`] timing row when opted in.
//!
//! GitHub Actions sets **`CUZK_VK_CI_TIMINGS_ROW=1`**, **`CUZK_VK_BENCH_CSV`**, and (after Mesa install)
//! **`VK_ICD_FILENAMES`** so hosted runners can record Lavapipe timings without enabling full Vulkan smoke.

use std::path::Path;
use std::sync::Arc;

use cuzk_vk::{
    prove_groth16_partition, PARTITION_BENCH_CSV_HEADER, VkGroth16Job, VkProofKind, VkProverContext,
    VulkanDevice,
};

#[test]
fn ci_append_partition_timing_csv_row() {
    if !matches!(std::env::var("CUZK_VK_CI_TIMINGS_ROW").as_deref(), Ok("1")) {
        return;
    }
    let csv_path = std::env::var("CUZK_VK_BENCH_CSV").expect(
        "CUZK_VK_CI_TIMINGS_ROW=1 requires CUZK_VK_BENCH_CSV pointing at the CSV file to append",
    );
    let path = Path::new(&csv_path);
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).expect("create CSV parent dirs");
    }

    let lines_before = match std::fs::read_to_string(path) {
        Ok(s) => s.lines().count(),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => 0,
        Err(e) => panic!("read {}: {e}", path.display()),
    };

    std::env::set_var("CUZK_VK_SKIP_SMOKE", "1");
    let dev = Arc::new(
        VulkanDevice::new().unwrap_or_else(|e| {
            panic!("Vulkan init failed ({e}); install a Vulkan ICD (e.g. Mesa Lavapipe) and set VK_ICD_FILENAMES for CI timings")
        }),
    );
    let ctx = VkProverContext::new(dev);
    let job = VkGroth16Job {
        kind: VkProofKind::WindowPost,
        circuit_log_n: 3,
        partition_index: Some(0),
        witness_ntt_coeffs: None,
    };
    prove_groth16_partition(&ctx, &job).expect("partition prove");

    let body = std::fs::read_to_string(path).expect("read CSV after prove");
    let lines_after = body.lines().count();
    assert!(
        lines_after >= lines_before.max(1),
        "expected at least one line appended; before={lines_before} after={lines_after}"
    );
    assert!(
        body.contains(PARTITION_BENCH_CSV_HEADER),
        "CSV must contain canonical header {:?}",
        PARTITION_BENCH_CSV_HEADER
    );
    let last = body.lines().last().expect("non-empty csv");
    assert_ne!(
        last.trim(),
        PARTITION_BENCH_CSV_HEADER,
        "last line should be a data row, not only the header"
    );
    let expected = PARTITION_BENCH_CSV_HEADER.split(',').count();
    assert_eq!(
        last.split(',').count(),
        expected,
        "column count should match header (quoted GPU names are not used in smoke rows)"
    );
    assert!(
        last.split(',').next().unwrap().parse::<u128>().is_ok(),
        "first column should be unix_ms"
    );
}
