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
}

impl Default for GpuConfig {
    fn default() -> Self {
        Self { devices: vec![] }
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
