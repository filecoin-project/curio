//! Configuration for the cuzk proving engine.

use serde::Deserialize;
use std::path::PathBuf;

/// Top-level daemon configuration, loaded from TOML.
#[derive(Debug, Clone, Deserialize)]
pub struct Config {
    #[serde(default)]
    pub daemon: DaemonConfig,
    #[serde(default)]
    pub memory: MemoryConfig,
    #[serde(default)]
    pub gpus: GpuConfig,
    #[serde(default)]
    pub scheduler: SchedulerConfig,
    #[serde(default)]
    pub synthesis: SynthesisConfig,
    #[serde(default)]
    pub pipeline: PipelineConfig,
    #[serde(default)]
    pub srs: SrsConfig,
    #[serde(default)]
    pub logging: LoggingConfig,
}

#[derive(Debug, Clone, Deserialize)]
pub struct DaemonConfig {
    /// Listen address. Examples:
    ///   "unix:///run/curio/cuzk.sock"
    ///   "0.0.0.0:9820"
    #[serde(default = "DaemonConfig::default_listen")]
    pub listen: String,
}

impl DaemonConfig {
    fn default_listen() -> String {
        "unix:///run/curio/cuzk.sock".to_string()
    }
}

impl Default for DaemonConfig {
    fn default() -> Self {
        Self {
            listen: Self::default_listen(),
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct MemoryConfig {
    /// Maximum CUDA pinned memory for SRS residency (e.g. "50GiB", "140GiB").
    #[serde(default = "MemoryConfig::default_pinned_budget")]
    pub pinned_budget: String,
    /// Maximum total memory for proof working set.
    #[serde(default = "MemoryConfig::default_working_memory_budget")]
    pub working_memory_budget: String,
}

impl MemoryConfig {
    fn default_pinned_budget() -> String {
        "50GiB".to_string()
    }
    fn default_working_memory_budget() -> String {
        "80GiB".to_string()
    }

    /// Parse the pinned budget string into bytes.
    pub fn pinned_budget_bytes(&self) -> u64 {
        parse_size(&self.pinned_budget)
    }
}

impl Default for MemoryConfig {
    fn default() -> Self {
        Self {
            pinned_budget: Self::default_pinned_budget(),
            working_memory_budget: Self::default_working_memory_budget(),
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct GpuConfig {
    /// GPU ordinals to use. Empty = auto-detect all.
    #[serde(default)]
    pub devices: Vec<u32>,
    /// Number of CPU threads for the GPU-side thread pool (b_g2_msm,
    /// preprocessing, bitmap population). 0 = auto-detect (all CPUs).
    ///
    /// When running parallel synthesis (synthesis_concurrency > 1), the
    /// GPU's CPU work (b_g2_msm: ~25s) contends with synthesis for CPU
    /// time. Setting this to ~1/3 of available cores reserves the rest
    /// for synthesis, reducing contention.
    ///
    /// Example for a 96-core machine with synthesis_concurrency=2:
    ///   gpu_threads = 32  (leaves 64 cores for 2 syntheses)
    ///   synthesis.threads = 0  (rayon auto = remaining cores)
    #[serde(default)]
    pub gpu_threads: u32,
}

impl Default for GpuConfig {
    fn default() -> Self {
        Self {
            devices: vec![],
            gpu_threads: 0,
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct SchedulerConfig {
    /// Max proofs to batch into a single GPU invocation (same circuit type).
    /// Phase 0: always 1.
    #[serde(default = "SchedulerConfig::default_max_batch_size")]
    pub max_batch_size: u32,
    /// Max time (ms) to wait for batch to fill before flushing.
    #[serde(default = "SchedulerConfig::default_max_batch_wait_ms")]
    pub max_batch_wait_ms: u64,
    /// Reorder NORMAL-priority queue to group by circuit type.
    #[serde(default = "SchedulerConfig::default_sort_by_type")]
    pub sort_by_type: bool,
}

impl SchedulerConfig {
    fn default_max_batch_size() -> u32 {
        1
    }
    fn default_max_batch_wait_ms() -> u64 {
        10000
    }
    fn default_sort_by_type() -> bool {
        true
    }
}

impl Default for SchedulerConfig {
    fn default() -> Self {
        Self {
            max_batch_size: Self::default_max_batch_size(),
            max_batch_wait_ms: Self::default_max_batch_wait_ms(),
            sort_by_type: Self::default_sort_by_type(),
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct SynthesisConfig {
    /// CPU threads for circuit synthesis. 0 = auto (num_cpus / 2).
    #[serde(default)]
    pub threads: u32,
}

impl Default for SynthesisConfig {
    fn default() -> Self {
        Self { threads: 0 }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct PipelineConfig {
    /// Enable pipelined synthesis → GPU proving.
    /// When enabled, synthesis and GPU compute overlap for consecutive proofs.
    /// When disabled, falls back to Phase 1 monolithic proving.
    #[serde(default = "PipelineConfig::default_enabled")]
    pub enabled: bool,
    /// Maximum number of pre-synthesized proofs waiting for GPU.
    /// Controls memory backpressure:
    /// - 0 = no pipelining (synthesis and GPU are sequential per proof)
    /// - 1 = one proof pre-synthesized (recommended for PoRep on 256+ GiB machines)
    /// - N = N proofs pre-synthesized (only for PoSt which has small intermediate state)
    ///
    /// Per-partition pipelining reduces this further: for PoRep, each
    /// in-flight unit is one partition (~13.6 GiB), not the full proof.
    #[serde(default = "PipelineConfig::default_synthesis_lookahead")]
    pub synthesis_lookahead: u32,
    /// Number of concurrent synthesis tasks.
    ///
    /// Controls how many proofs can be synthesized simultaneously on the CPU.
    /// When synthesis takes longer than GPU proving (e.g. 39s synth vs 27s GPU),
    /// the GPU idles for ~12s between proofs with a single synthesis task. With
    /// 2 concurrent synthesis tasks, the GPU can be kept fully saturated.
    ///
    /// - 1 = sequential synthesis (default, lower memory)
    /// - 2 = recommended for single-GPU machines with sufficient RAM (>400 GiB)
    /// - N = N concurrent syntheses (memory: N × ~136 GiB for PoRep 32G)
    ///
    /// The synthesis_lookahead channel still provides backpressure — even with
    /// N concurrent syntheses, only `synthesis_lookahead` completed proofs can
    /// be queued waiting for the GPU.
    #[serde(default = "PipelineConfig::default_synthesis_concurrency")]
    pub synthesis_concurrency: u32,

    /// Pipelined partition proving (Phase 6).
    ///
    /// Controls how many synthesized partitions can be queued for the GPU
    /// simultaneously. Each partition is synthesized independently (1 circuit)
    /// and all partitions run in parallel, throttled by this bound.
    ///
    /// - 0 = disabled (batch all partitions together, ~228 GiB for PoRep)
    /// - 1 = sequential pipeline (one partition at a time, ~27 GiB)
    /// - 2 = recommended for memory-constrained machines (~41 GiB)
    /// - 3 = recommended default — keeps GPU fed (~54 GiB)
    /// - >= num_partitions = batch-all (no pipeline, all partitions at once)
    ///
    /// Memory formula: (max_concurrent + 1) × ~13.6 GiB per partition.
    /// The +1 accounts for the partition currently being GPU-proved.
    ///
    /// With num_circuits=1 per GPU call, the GPU takes ~3s per partition
    /// (fast b_g2_msm). Synthesis takes ~29s per partition. So with
    /// max_concurrent=3, the pipeline keeps the GPU continuously fed.
    ///
    /// Only applies to multi-partition proof types (PoRep, SnapDeals).
    /// WinningPoSt and WindowPoSt are single-partition and bypass this.
    #[serde(default = "PipelineConfig::default_slot_size")]
    pub slot_size: u32,
}

impl PipelineConfig {
    fn default_enabled() -> bool {
        true
    }
    fn default_synthesis_lookahead() -> u32 {
        1
    }
    fn default_synthesis_concurrency() -> u32 {
        1 // sequential by default for backward compatibility
    }
    fn default_slot_size() -> u32 {
        0 // disabled by default for backward compatibility
    }
}

impl Default for PipelineConfig {
    fn default() -> Self {
        Self {
            enabled: Self::default_enabled(),
            synthesis_lookahead: Self::default_synthesis_lookahead(),
            synthesis_concurrency: Self::default_synthesis_concurrency(),
            slot_size: Self::default_slot_size(),
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct SrsConfig {
    /// Directory containing .params and .vk files.
    #[serde(default = "SrsConfig::default_param_cache")]
    pub param_cache: PathBuf,
    /// SRS entries to preload at startup (e.g. ["porep-32g"]).
    #[serde(default)]
    pub preload: Vec<String>,
}

impl SrsConfig {
    fn default_param_cache() -> PathBuf {
        PathBuf::from("/data/zk/params")
    }
}

impl Default for SrsConfig {
    fn default() -> Self {
        Self {
            param_cache: Self::default_param_cache(),
            preload: vec![],
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct LoggingConfig {
    #[serde(default = "LoggingConfig::default_level")]
    pub level: String,
    #[serde(default)]
    pub format: Option<String>,
}

impl LoggingConfig {
    fn default_level() -> String {
        "info".to_string()
    }
}

impl Default for LoggingConfig {
    fn default() -> Self {
        Self {
            level: Self::default_level(),
            format: None,
        }
    }
}

impl Default for Config {
    fn default() -> Self {
        Self {
            daemon: DaemonConfig::default(),
            memory: MemoryConfig::default(),
            gpus: GpuConfig::default(),
            scheduler: SchedulerConfig::default(),
            synthesis: SynthesisConfig::default(),
            pipeline: PipelineConfig::default(),
            srs: SrsConfig::default(),
            logging: LoggingConfig::default(),
        }
    }
}

impl Config {
    /// Load configuration from a TOML file.
    pub fn from_file(path: &std::path::Path) -> anyhow::Result<Self> {
        let content = std::fs::read_to_string(path)?;
        let config: Config = toml::from_str(&content)?;
        Ok(config)
    }
}

/// Parse a human-readable size string like "50GiB" or "140GiB" into bytes.
fn parse_size(s: &str) -> u64 {
    let s = s.trim();
    if let Some(n) = s.strip_suffix("GiB") {
        n.trim().parse::<u64>().unwrap_or(50) * 1024 * 1024 * 1024
    } else if let Some(n) = s.strip_suffix("MiB") {
        n.trim().parse::<u64>().unwrap_or(0) * 1024 * 1024
    } else if let Some(n) = s.strip_suffix("GB") {
        n.trim().parse::<u64>().unwrap_or(50) * 1_000_000_000
    } else if let Some(n) = s.strip_suffix("MB") {
        n.trim().parse::<u64>().unwrap_or(0) * 1_000_000
    } else {
        // Assume bytes
        s.parse::<u64>().unwrap_or(50 * 1024 * 1024 * 1024)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_size() {
        assert_eq!(parse_size("50GiB"), 50 * 1024 * 1024 * 1024);
        assert_eq!(parse_size("140GiB"), 140 * 1024 * 1024 * 1024);
        assert_eq!(parse_size("626MiB"), 626 * 1024 * 1024);
    }

    #[test]
    fn test_default_config() {
        let cfg = Config::default();
        assert_eq!(cfg.daemon.listen, "unix:///run/curio/cuzk.sock");
        assert_eq!(cfg.scheduler.max_batch_size, 1);
        assert!(cfg.srs.preload.is_empty());
    }

    #[test]
    fn test_parse_toml() {
        let toml_str = r#"
[daemon]
listen = "0.0.0.0:9820"

[srs]
param_cache = "/data/zk/params"
preload = ["porep-32g"]

[scheduler]
max_batch_size = 1
sort_by_type = true
"#;
        let cfg: Config = toml::from_str(toml_str).unwrap();
        assert_eq!(cfg.daemon.listen, "0.0.0.0:9820");
        assert_eq!(cfg.srs.preload, vec!["porep-32g"]);
    }
}
