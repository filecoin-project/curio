//! SRS (Structured Reference String) manager for the cuzk pipeline.
//!
//! Manages the lifecycle of Groth16 parameters loaded via SupraSeal.
//! Each `CircuitId` maps to a `.params` file on disk. The manager loads
//! parameters on demand (or via explicit preload) and caches them in memory
//! as `Arc<SuprasealParameters<Bls12>>`.
//!
//! This replaces the implicit `GROTH_PARAM_MEMORY_CACHE` used in Phase 0-1,
//! giving cuzk explicit control over parameter lifetime and memory budget.
//!
//! Only available with the `cuda-supraseal` feature.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Instant;

use anyhow::{Context, Result};
use tracing::{debug, info, warn};

#[cfg(feature = "cuda-supraseal")]
use bellperson::groth16::SuprasealParameters;
#[cfg(feature = "cuda-supraseal")]
use blstrs::Bls12;

/// Identifies a circuit type for SRS routing.
///
/// Each variant maps to a specific `.params` file containing the Groth16
/// structured reference string for that proof circuit.
#[derive(Clone, Debug, Hash, Eq, PartialEq)]
pub enum CircuitId {
    /// PoRep 32 GiB (interactive, V1/V1_1, 10 partitions × 18 challenges)
    Porep32G,
    /// PoRep 64 GiB (interactive, V1/V1_1)
    Porep64G,
    /// Window PoSt 32 GiB
    WindowPost32G,
    /// Winning PoSt 32 GiB
    WinningPost32G,
    /// SnapDeals (empty sector update) 32 GiB
    SnapDeals32G,
    /// SnapDeals (empty sector update) 64 GiB
    SnapDeals64G,
}

impl std::fmt::Display for CircuitId {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CircuitId::Porep32G => write!(f, "porep-32g"),
            CircuitId::Porep64G => write!(f, "porep-64g"),
            CircuitId::WindowPost32G => write!(f, "wpost-32g"),
            CircuitId::WinningPost32G => write!(f, "winning-32g"),
            CircuitId::SnapDeals32G => write!(f, "snap-32g"),
            CircuitId::SnapDeals64G => write!(f, "snap-64g"),
        }
    }
}

impl CircuitId {
    /// Parse a circuit ID from a string (e.g. "porep-32g").
    pub fn from_str_id(s: &str) -> Option<Self> {
        match s {
            "porep-32g" => Some(CircuitId::Porep32G),
            "porep-64g" => Some(CircuitId::Porep64G),
            "wpost-32g" => Some(CircuitId::WindowPost32G),
            "winning-32g" => Some(CircuitId::WinningPost32G),
            "snap-32g" => Some(CircuitId::SnapDeals32G),
            "snap-64g" => Some(CircuitId::SnapDeals64G),
            _ => None,
        }
    }
}

/// Map a `CircuitId` to its `.params` filename (without directory prefix).
///
/// These filenames are deterministic: they encode the tree shape, hasher,
/// and a SHA-256 hash of the public parameters identifier string. The hash
/// is stable across builds because it depends only on the circuit structure
/// (node count, layers, challenges, degree, expansion degree).
pub fn circuit_id_to_param_filename(id: &CircuitId) -> &'static str {
    match id {
        // PoRep 32G: merkletree-poseidon_hasher-8-8-0, sha256_hasher, 11 layers, 18 challenges
        CircuitId::Porep32G => "v28-stacked-proof-of-replication-merkletree-poseidon_hasher-8-8-0-sha256_hasher-82a357d2f2ca81dc61bb45f4a762807aedee1b0a53fd6c4e77b46a01bfef7820.params",
        // PoRep 64G: merkletree-poseidon_hasher-8-8-2, sha256_hasher
        CircuitId::Porep64G => "v28-stacked-proof-of-replication-merkletree-poseidon_hasher-8-8-2-sha256_hasher-96f1b4a04c5c51e4759bbf224bbc2ef5a42c7100f16ec0637123f16a845ddfb2.params",
        // Window PoSt 32G: fallback, merkletree-poseidon_hasher-8-8-0 (57 GiB file)
        CircuitId::WindowPost32G => "v28-proof-of-spacetime-fallback-merkletree-poseidon_hasher-8-8-0-0377ded656c6f524f1618760bffe4e0a1c51d5a70c4509eedae8a27555733edc.params",
        // Winning PoSt 32G: fallback, merkletree-poseidon_hasher-8-8-0 (184 MiB file)
        CircuitId::WinningPost32G => "v28-proof-of-spacetime-fallback-merkletree-poseidon_hasher-8-8-0-559e581f022bb4e4ec6e719e563bf0e026ad6de42e56c18714a2c692b1b88d7e.params",
        // SnapDeals 32G: empty-sector-update, merkletree-poseidon_hasher-8-8-0 (33 GiB file)
        CircuitId::SnapDeals32G => "v28-empty-sector-update-merkletree-poseidon_hasher-8-8-0-3b7f44a9362e3985369454947bc94022e118211e49fd672d52bec1cbfd599d18.params",
        // SnapDeals 64G: empty-sector-update, merkletree-poseidon_hasher-8-8-2
        // TODO: verify this filename on a machine with 64G params
        CircuitId::SnapDeals64G => "v28-empty-sector-update-merkletree-poseidon_hasher-8-8-2-102e1444a7e9a97ebf1e3d6855dcc77e66c011ea66f936d9b2c508f87f2f83a7.params",
    }
}

/// Map a `CircuitId` to its `.vk` (verifying key) filename.
pub fn circuit_id_to_vk_filename(id: &CircuitId) -> &'static str {
    match id {
        CircuitId::Porep32G => "v28-stacked-proof-of-replication-merkletree-poseidon_hasher-8-8-0-sha256_hasher-82a357d2f2ca81dc61bb45f4a762807aedee1b0a53fd6c4e77b46a01bfef7820.vk",
        CircuitId::Porep64G => "v28-stacked-proof-of-replication-merkletree-poseidon_hasher-8-8-2-sha256_hasher-96f1b4a04c5c51e4759bbf224bbc2ef5a42c7100f16ec0637123f16a845ddfb2.vk",
        CircuitId::WindowPost32G => "v28-proof-of-spacetime-fallback-merkletree-poseidon_hasher-8-8-0-0377ded656c6f524f1618760bffe4e0a1c51d5a70c4509eedae8a27555733edc.vk",
        CircuitId::WinningPost32G => "v28-proof-of-spacetime-fallback-merkletree-poseidon_hasher-8-8-0-559e581f022bb4e4ec6e719e563bf0e026ad6de42e56c18714a2c692b1b88d7e.vk",
        CircuitId::SnapDeals32G => "v28-empty-sector-update-merkletree-poseidon_hasher-8-8-0-3b7f44a9362e3985369454947bc94022e118211e49fd672d52bec1cbfd599d18.vk",
        CircuitId::SnapDeals64G => "v28-empty-sector-update-merkletree-poseidon_hasher-8-8-2-102e1444a7e9a97ebf1e3d6855dcc77e66c011ea66f936d9b2c508f87f2f83a7.vk",
    }
}

/// Manages SRS parameter loading and caching for the cuzk pipeline.
///
/// Parameters are loaded via `SuprasealParameters::new(path)`, which reads
/// the `.params` file and allocates CUDA pinned memory for the SRS points.
/// This is the same codepath used by bellperson internally, but we manage
/// the lifetime explicitly instead of relying on the global cache.
#[cfg(feature = "cuda-supraseal")]
pub struct SrsManager {
    /// Loaded SRS entries, keyed by circuit ID.
    loaded: HashMap<CircuitId, Arc<SuprasealParameters<Bls12>>>,
    /// Base directory containing `.params` and `.vk` files.
    param_dir: PathBuf,
    /// Approximate bytes of SRS currently loaded (based on file sizes).
    loaded_bytes: u64,
    /// Memory budget for SRS (0 = unlimited).
    budget_bytes: u64,
}

#[cfg(feature = "cuda-supraseal")]
impl SrsManager {
    /// Create a new SRS manager.
    ///
    /// # Arguments
    /// * `param_dir` — Directory containing `.params` and `.vk` files.
    /// * `budget_bytes` — Maximum total SRS memory budget (0 = unlimited).
    pub fn new(param_dir: PathBuf, budget_bytes: u64) -> Self {
        Self {
            loaded: HashMap::new(),
            param_dir,
            loaded_bytes: 0,
            budget_bytes,
        }
    }

    /// Get a loaded SRS, or load it if not cached.
    ///
    /// Returns the SRS wrapped in an Arc for shared ownership across
    /// synthesis and GPU worker threads.
    pub fn ensure_loaded(
        &mut self,
        circuit_id: &CircuitId,
    ) -> Result<Arc<SuprasealParameters<Bls12>>> {
        if let Some(srs) = self.loaded.get(circuit_id) {
            debug!(circuit_id = %circuit_id, "SRS already loaded");
            return Ok(Arc::clone(srs));
        }

        let filename = circuit_id_to_param_filename(circuit_id);
        let path = self.param_dir.join(filename);

        if !path.exists() {
            anyhow::bail!(
                "SRS param file not found for {:?}: {}",
                circuit_id,
                path.display()
            );
        }

        // Check file size for budget tracking
        let file_size = std::fs::metadata(&path)
            .with_context(|| format!("failed to stat {}", path.display()))?
            .len();

        if self.budget_bytes > 0 && self.loaded_bytes + file_size > self.budget_bytes {
            warn!(
                circuit_id = %circuit_id,
                file_size_gib = file_size / (1024 * 1024 * 1024),
                loaded_gib = self.loaded_bytes / (1024 * 1024 * 1024),
                budget_gib = self.budget_bytes / (1024 * 1024 * 1024),
                "SRS load would exceed memory budget"
            );
            // Still load — budget is advisory, not hard. The operator should
            // configure the budget or the preload list appropriately.
        }

        info!(
            circuit_id = %circuit_id,
            path = %path.display(),
            file_size_gib = file_size / (1024 * 1024 * 1024),
            "loading SRS from disk"
        );

        let start = Instant::now();
        let srs = SuprasealParameters::<Bls12>::new(path.clone())
            .with_context(|| format!("failed to load SRS from {}", path.display()))?;
        let elapsed = start.elapsed();

        info!(
            circuit_id = %circuit_id,
            elapsed_ms = elapsed.as_millis(),
            "SRS loaded successfully"
        );

        let srs = Arc::new(srs);
        self.loaded.insert(circuit_id.clone(), Arc::clone(&srs));
        self.loaded_bytes += file_size;

        Ok(srs)
    }

    /// Preload SRS for multiple circuit IDs.
    ///
    /// Errors on individual loads are logged as warnings but don't prevent
    /// loading other entries.
    pub fn preload(
        &mut self,
        circuit_ids: &[CircuitId],
    ) -> Vec<(CircuitId, Result<std::time::Duration>)> {
        let mut results = Vec::with_capacity(circuit_ids.len());
        for cid in circuit_ids {
            let start = Instant::now();
            match self.ensure_loaded(cid) {
                Ok(_) => results.push((cid.clone(), Ok(start.elapsed()))),
                Err(e) => {
                    warn!(circuit_id = %cid, error = %e, "SRS preload failed");
                    results.push((cid.clone(), Err(e)));
                }
            }
        }
        results
    }

    /// Evict an SRS entry, freeing its memory.
    ///
    /// Returns true if the entry was evicted, false if it wasn't loaded.
    /// Note: The actual memory is freed when all Arc references are dropped,
    /// which may be after this call if a GPU worker is still using it.
    pub fn evict(&mut self, circuit_id: &CircuitId) -> bool {
        if let Some(_srs) = self.loaded.remove(circuit_id) {
            let filename = circuit_id_to_param_filename(circuit_id);
            let path = self.param_dir.join(filename);
            if let Ok(meta) = std::fs::metadata(&path) {
                self.loaded_bytes = self.loaded_bytes.saturating_sub(meta.len());
            }
            info!(circuit_id = %circuit_id, "SRS evicted from cache");
            true
        } else {
            false
        }
    }

    /// Get the SRS for a circuit ID without loading it.
    ///
    /// Returns None if the SRS is not loaded.
    pub fn get(&self, circuit_id: &CircuitId) -> Option<Arc<SuprasealParameters<Bls12>>> {
        self.loaded.get(circuit_id).cloned()
    }

    /// Check if an SRS is loaded.
    pub fn is_loaded(&self, circuit_id: &CircuitId) -> bool {
        self.loaded.contains_key(circuit_id)
    }

    /// List all currently loaded circuit IDs.
    pub fn loaded_ids(&self) -> Vec<CircuitId> {
        self.loaded.keys().cloned().collect()
    }

    /// Total approximate bytes of SRS currently loaded.
    pub fn loaded_bytes(&self) -> u64 {
        self.loaded_bytes
    }

    /// The parameter directory path.
    pub fn param_dir(&self) -> &Path {
        &self.param_dir
    }
}

/// Stub SRS manager for builds without CUDA support.
///
/// This allows the code to compile and type-check without `cuda-supraseal`,
/// but all operations return errors.
#[cfg(not(feature = "cuda-supraseal"))]
pub struct SrsManager {
    param_dir: PathBuf,
}

#[cfg(not(feature = "cuda-supraseal"))]
impl SrsManager {
    pub fn new(param_dir: PathBuf, _budget_bytes: u64) -> Self {
        Self { param_dir }
    }

    pub fn param_dir(&self) -> &Path {
        &self.param_dir
    }

    pub fn is_loaded(&self, _circuit_id: &CircuitId) -> bool {
        false
    }

    pub fn loaded_ids(&self) -> Vec<CircuitId> {
        vec![]
    }

    pub fn loaded_bytes(&self) -> u64 {
        0
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_circuit_id_display() {
        assert_eq!(CircuitId::Porep32G.to_string(), "porep-32g");
        assert_eq!(CircuitId::WindowPost32G.to_string(), "wpost-32g");
        assert_eq!(CircuitId::WinningPost32G.to_string(), "winning-32g");
        assert_eq!(CircuitId::SnapDeals32G.to_string(), "snap-32g");
    }

    #[test]
    fn test_circuit_id_from_str() {
        assert_eq!(
            CircuitId::from_str_id("porep-32g"),
            Some(CircuitId::Porep32G)
        );
        assert_eq!(
            CircuitId::from_str_id("wpost-32g"),
            Some(CircuitId::WindowPost32G)
        );
        assert_eq!(
            CircuitId::from_str_id("winning-32g"),
            Some(CircuitId::WinningPost32G)
        );
        assert_eq!(
            CircuitId::from_str_id("snap-32g"),
            Some(CircuitId::SnapDeals32G)
        );
        assert_eq!(CircuitId::from_str_id("unknown"), None);
    }

    #[test]
    fn test_param_filename_mapping() {
        let f = circuit_id_to_param_filename(&CircuitId::Porep32G);
        assert!(f.starts_with("v28-stacked-proof-of-replication"));
        assert!(f.contains("8-8-0"));
        assert!(f.ends_with(".params"));

        let f = circuit_id_to_param_filename(&CircuitId::WinningPost32G);
        assert!(f.starts_with("v28-proof-of-spacetime-fallback"));
        assert!(f.ends_with(".params"));

        let f = circuit_id_to_param_filename(&CircuitId::SnapDeals32G);
        assert!(f.starts_with("v28-empty-sector-update"));
        assert!(f.ends_with(".params"));
    }

    #[test]
    fn test_vk_filename_mapping() {
        let f = circuit_id_to_vk_filename(&CircuitId::Porep32G);
        assert!(f.starts_with("v28-stacked-proof-of-replication"));
        assert!(f.ends_with(".vk"));

        // VK filename should differ from param filename only in extension
        let params = circuit_id_to_param_filename(&CircuitId::Porep32G);
        assert_eq!(f.strip_suffix(".vk"), params.strip_suffix(".params"));
    }
}
