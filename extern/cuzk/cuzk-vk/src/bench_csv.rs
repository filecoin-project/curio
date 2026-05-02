//! Optional CSV append + **ceiling** checks for [`crate::prover::prove_groth16_partition`] (roadmap **§1**).
//!
//! Set **`CUZK_VK_BENCH_CSV`** to a filesystem path. On each successful partition run, one data row
//! is appended; a header row is written when the file is missing or empty.
//! CI opt-in: set **`CUZK_VK_CI_TIMINGS_ROW=1`** with the same variable and run `tests/ci_partition_csv_row.rs`
//! (see `.github/workflows/cuzk-vulkan.yml` §1 Lavapipe step).
//!
//! Optional regression ceilings (milliseconds, `u64`): **`CUZK_VK_BENCH_MAX_TOTAL_MS`**, **`CUZK_VK_BENCH_MAX_FR_NTT_MS`**,
//! **`CUZK_VK_BENCH_MAX_MSM_GRID_MS`**, and when Vulkan smoke runs **`CUZK_VK_BENCH_MAX_SRS_G2_MS`**, **`CUZK_VK_BENCH_MAX_G1_MSM_MS`**,
//! **`CUZK_VK_BENCH_MAX_H_TERM_MS`**. Missing, empty, or **`0`** disables that check.
//!
//! Optional **`CUZK_VK_BENCH_HARDWARE_MD`**: append a markdown section per successful run (see [`append_partition_hardware_md`]).
//! Optional **`CUZK_VK_BENCH_TAG`**: short label included in that section (e.g. git SHA or batch id).
//!
//! **CI baseline header:** `benchmarks/cuzk-vk/results-ci-host-baseline.csv` in the repo must match [`PARTITION_BENCH_CSV_HEADER`] (schema drift test).

use std::fs::OpenOptions;
use std::io::Write;
use std::path::Path;
use std::time::Duration;

/// Canonical CSV header for [`append_partition_benchmark_csv`]. Kept in sync with `benchmarks/cuzk-vk/results-ci-host-baseline.csv`.
pub const PARTITION_BENCH_CSV_HEADER: &str = "unix_ms,circuit_log_n,partition_max_log,vulkan_smoke,fr_ntt_ms,msm_grid_ms,srs_g2_ms,g1_msm_ms,h_term_ms,total_ms,gpu_name,driver_version,api_version,vendor_id,device_id";

/// RFC-4180-style cell: quote when the value contains comma, quote, or newline.
pub fn csv_escape_cell(s: &str) -> String {
    if s.contains(',') || s.contains('"') || s.contains('\n') || s.contains('\r') {
        let escaped = s.replace('"', "\"\"");
        format!("\"{escaped}\"")
    } else {
        s.to_string()
    }
}

/// One sample row (milliseconds are wall-clock; optional stages use an empty field when absent).
#[derive(Clone, Debug)]
pub struct PartitionBenchRow {
    pub unix_ms: u128,
    pub circuit_log_n: u32,
    pub partition_max_log: u32,
    pub vulkan_smoke: bool,
    pub fr_ntt_ms: f64,
    pub msm_grid_ms: f64,
    pub srs_g2_ms: Option<f64>,
    pub g1_msm_ms: Option<f64>,
    pub h_term_ms: Option<f64>,
    pub total_ms: f64,
    /// From [`crate::device::VulkanDevice::physical_device_info`] (empty when unavailable).
    pub gpu_name: String,
    pub driver_version: String,
    pub api_version: String,
    pub vendor_id: u32,
    pub device_id: u32,
}

fn opt_ms(o: Option<f64>) -> String {
    o.map(|ms| format!("{ms:.3}")).unwrap_or_default()
}

/// `true` iff `d` is strictly longer than `max_ms` milliseconds.
#[inline]
pub fn duration_ms_exceeds_u64_ceiling(d: Duration, max_ms: u64) -> bool {
    d.as_millis() > u128::from(max_ms)
}

/// Parse `var` as a positive `u64` millisecond ceiling; missing, empty, unparsable, or zero → `None`.
pub fn bench_max_ms_from_env(var: &str) -> Option<u64> {
    std::env::var(var)
        .ok()
        .and_then(|s| s.parse::<u64>().ok())
        .filter(|&v| v > 0)
}

/// Append one CSV line to `path`. Creates parent directories and writes [`PARTITION_BENCH_CSV_HEADER`] if needed.
pub fn append_partition_benchmark_csv(path: &Path, row: &PartitionBenchRow) -> std::io::Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let need_header = match std::fs::metadata(path) {
        Ok(m) => m.len() == 0,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => true,
        Err(e) => return Err(e),
    };
    let mut f = OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)?;
    if need_header {
        writeln!(f, "{PARTITION_BENCH_CSV_HEADER}")?;
    }
    let vs = if row.vulkan_smoke { 1 } else { 0 };
    writeln!(
        f,
        "{},{},{},{},{:.3},{:.3},{},{},{},{:.3},{},{},{},0x{:08x},0x{:08x}",
        row.unix_ms,
        row.circuit_log_n,
        row.partition_max_log,
        vs,
        row.fr_ntt_ms,
        row.msm_grid_ms,
        opt_ms(row.srs_g2_ms),
        opt_ms(row.g1_msm_ms),
        opt_ms(row.h_term_ms),
        row.total_ms,
        csv_escape_cell(&row.gpu_name),
        csv_escape_cell(&row.driver_version),
        csv_escape_cell(&row.api_version),
        row.vendor_id,
        row.device_id,
    )?;
    Ok(())
}

/// Append a markdown block to `path` (creates parent dirs). Intended for **`CUZK_VK_BENCH_HARDWARE_MD`** §1 logs.
pub fn append_partition_hardware_md(path: &Path, markdown_section: &str) -> std::io::Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let mut f = OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)?;
    writeln!(f)?;
    write!(f, "{markdown_section}")?;
    writeln!(f)?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    #[test]
    fn append_partition_benchmark_csv_writes_header_and_row() {
        let dir = std::env::temp_dir().join(format!(
            "cuzk_vk_bench_csv_{}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("results-test.csv");
        let row = PartitionBenchRow {
            unix_ms: 1_700_000_000_000,
            circuit_log_n: 6,
            partition_max_log: 6,
            vulkan_smoke: true,
            fr_ntt_ms: 12.0,
            msm_grid_ms: 0.5,
            srs_g2_ms: Some(1.25),
            g1_msm_ms: None,
            h_term_ms: Some(3.0),
            total_ms: 20.0,
            gpu_name: "Test GPU".into(),
            driver_version: "1.2.3".into(),
            api_version: "1.3.0".into(),
            vendor_id: 0x1002,
            device_id: 0xabcd1234,
        };
        append_partition_benchmark_csv(&path, &row).unwrap();
        let s = std::fs::read_to_string(&path).unwrap();
        assert!(s.contains(PARTITION_BENCH_CSV_HEADER));
        assert!(s.contains("1700000000000,6,6,1,"));
        assert!(s.contains("20.000"));
        assert!(s.contains("Test GPU"));
        assert!(s.contains("0x00001002"));
        assert!(s.contains("0xabcd1234"));
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn csv_escape_cell_quotes_commas() {
        assert_eq!(csv_escape_cell("ok"), "ok");
        assert_eq!(csv_escape_cell("a,b"), "\"a,b\"");
        assert_eq!(csv_escape_cell("say \"hi\""), "\"say \"\"hi\"\"\"");
    }

    #[test]
    fn duration_ms_exceeds_u64_ceiling_edges() {
        assert!(!duration_ms_exceeds_u64_ceiling(Duration::from_millis(10), 10));
        assert!(duration_ms_exceeds_u64_ceiling(Duration::from_millis(11), 10));
    }

    #[test]
    fn partition_csv_header_matches_ci_baseline_artifact() {
        let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("../../../benchmarks/cuzk-vk/results-ci-host-baseline.csv");
        let got = std::fs::read_to_string(&path)
            .unwrap_or_else(|e| panic!("read {}: {e}", path.display()));
        assert_eq!(
            got.trim_end_matches(['\n', '\r']),
            PARTITION_BENCH_CSV_HEADER,
            "update benchmarks/cuzk-vk/results-ci-host-baseline.csv when CSV columns change"
        );
    }

    #[test]
    fn append_partition_hardware_md_appends_block() {
        let dir = std::env::temp_dir().join(format!(
            "cuzk_vk_bench_md_{}",
            std::process::id()
        ));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("hw.md");
        append_partition_hardware_md(&path, "## once\n\nhello.").unwrap();
        append_partition_hardware_md(&path, "## twice\n\nworld.").unwrap();
        let s = std::fs::read_to_string(&path).unwrap();
        assert!(s.contains("## once"));
        assert!(s.contains("## twice"));
        let _ = std::fs::remove_dir_all(&dir);
    }
}
