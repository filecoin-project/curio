# `cuzk-vk` benchmarks (Milestone B₂ — §1 protocol)

Place `results-<gpu>-<date>.csv` here when running parity measurements (see repo-root `cuzk-vulkan-optimization-roadmap.md` §1 and **§3.4**). For prose you can paste into commits, use [`HARDWARE_MATRIX.md`](HARDWARE_MATRIX.md). **Milestone B** integration vs parity backlog: [`extern/cuzk/MILESTONE_B.md`](../../extern/cuzk/MILESTONE_B.md).

## Partition smoke CSV (`prove_groth16_partition`)

Set **`CUZK_VK_BENCH_CSV`** to a path under this directory (or anywhere writable). Each successful call to [`prove_groth16_partition`](../../extern/cuzk/cuzk-vk/src/prover.rs) appends one row with wall-clock milliseconds per stage (`fr_ntt_ms`, `msm_grid_ms`, optional SRS/G1/H columns when `CUZK_VK_SKIP_SMOKE=0`), plus **`gpu_name`**, **`driver_version`**, **`api_version`**, **`vendor_id`**, **`device_id`** (hex) from [`VulkanDevice::physical_device_info`](../../extern/cuzk/cuzk-vk/src/device.rs). GPU names with commas are CSV-escaped ([`csv_escape_cell`](../../extern/cuzk/cuzk-vk/src/bench_csv.rs)). When **`VkGroth16Job::witness_ntt_coeffs`** is set (B₂), the Fr NTT leg uses those coefficients; CSV columns are unchanged.

Example (Vulkan ICD required for full columns):

```bash
export CUZK_VK_SKIP_SMOKE=0
export CUZK_VK_BENCH_CSV="$(pwd)/results-$(uname -n)-$(date +%Y%m%d).csv"
cargo test -p cuzk-vk prove_partition_smoke_matches_roundtrip -- --nocapture
```

The file grows one line per run; a CSV header is written automatically when the file is new or empty. If the header layout changes between crate versions, **delete the CSV** or use a new path so columns stay consistent. Implementation: [`bench_csv`](../../extern/cuzk/cuzk-vk/src/bench_csv.rs).

### MSM window hint (§8.1 A.1, host-only)

[`msm_config_for_device`](../../extern/cuzk/cuzk-vk/src/device_profile.rs) picks a default `window_bits` from `vendor_id` (AMD/NVIDIA **16**, Apple **12**). Override with **`CUZK_VK_MSM_WINDOW_BITS`** (`1..=31`). The value is recorded in optional **`CUZK_VK_BENCH_HARDWARE_MD`** logs from [`prove_groth16_partition`](../../extern/cuzk/cuzk-vk/src/prover.rs); full bucket MSM shaders still use their own smoke parameters.

### Regression ceilings (optional)

After a successful run, [`prove_groth16_partition`](../../extern/cuzk/cuzk-vk/src/prover.rs) checks optional **`CUZK_VK_BENCH_MAX_*_MS`** variables (positive `u64` milliseconds). If a measured stage exceeds its ceiling, the prove returns an error (CSV is not appended when the run fails).

| Variable | Stage |
|----------|--------|
| `CUZK_VK_BENCH_MAX_FR_NTT_MS` | Fr NTT GPU round-trip |
| `CUZK_VK_BENCH_MAX_MSM_GRID_MS` | MSM dispatch grid smoke |
| `CUZK_VK_BENCH_MAX_SRS_G2_MS` | SRS `b_g2` + G2 reverse48 (Vulkan smoke only) |
| `CUZK_VK_BENCH_MAX_G1_MSM_MS` | SRS `h[]` G1 bit-plane MSM (Vulkan smoke only) |
| `CUZK_VK_BENCH_MAX_H_TERM_MS` | H quotient + commit check (Vulkan smoke only) |
| `CUZK_VK_BENCH_MAX_TOTAL_MS` | End-to-end partition smoke |

On Apple Silicon, [`apple-m2-vulkan-smoke.sh`](../../extern/cuzk/apple-m2-vulkan-smoke.sh) can set **`CUZK_VK_RECORD_BENCH_CSV=1`** to pick a default CSV path under this directory (unless **`CUZK_VK_BENCH_CSV`** is already set), and a companion **`CUZK_VK_BENCH_HARDWARE_MD`** log (same timestamp stem, `.md`).

### Hardware paragraph log (optional)

Set **`CUZK_VK_BENCH_HARDWARE_MD`** to a path. Each successful [`prove_groth16_partition`](../../extern/cuzk/cuzk-vk/src/prover.rs) appends a short markdown section: [`PhysicalDeviceInfo::measurement_paragraph`](../../extern/cuzk/cuzk-vk/src/device.rs), workload fields, and per-stage ms. **`CUZK_VK_BENCH_TAG`** (optional) is included in the heading line (for example `export CUZK_VK_BENCH_TAG="$(git rev-parse --short HEAD)"`).

This does not require **`CUZK_VK_BENCH_CSV`**; you can log hardware + timings to markdown only.

### CI CSV header baseline

[`results-ci-host-baseline.csv`](results-ci-host-baseline.csv) contains only the canonical CSV header line. `cuzk-vk` unit tests assert it matches [`PARTITION_BENCH_CSV_HEADER`](../../extern/cuzk/cuzk-vk/src/bench_csv.rs) so CI catches accidental column drift when the append format changes.
