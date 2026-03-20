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
use tracing::{debug, info};

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
///
/// Budget integration: SRS memory is tracked in the unified [`MemoryBudget`].
/// The caller pre-acquires budget before calling [`ensure_loaded`], and
/// eviction releases budget via [`MemoryBudget::release_internal`].
#[cfg(feature = "cuda-supraseal")]
pub struct SrsManager {
    /// Loaded SRS entries, keyed by circuit ID.
    loaded: HashMap<CircuitId, Arc<SuprasealParameters<Bls12>>>,
    /// Base directory containing `.params` and `.vk` files.
    param_dir: PathBuf,
    /// Approximate bytes of SRS currently loaded (based on file sizes).
    loaded_bytes: u64,
    /// Unified memory budget shared with PCE and working set.
    budget: Arc<crate::memory::MemoryBudget>,
    /// Last time each circuit's SRS was accessed (for eviction ordering).
    last_used: HashMap<CircuitId, Instant>,
}

#[cfg(feature = "cuda-supraseal")]
impl SrsManager {
    /// Create a new SRS manager with unified budget reference.
    pub fn new(param_dir: PathBuf, budget: Arc<crate::memory::MemoryBudget>) -> Self {
        Self {
            loaded: HashMap::new(),
            param_dir,
            loaded_bytes: 0,
            budget,
            last_used: HashMap::new(),
        }
    }

    /// Get a loaded SRS, or load it if not cached.
    ///
    /// If the SRS is not loaded and `budget_reservation` is provided, the
    /// reservation is consumed (converted to permanent) to cover the SRS
    /// memory. If no reservation is provided and the SRS needs loading,
    /// a non-blocking `try_acquire` is attempted.
    ///
    /// The caller (in async context) should pre-acquire budget via
    /// `budget.acquire(srs_file_size).await` when `is_loaded()` returns false,
    /// then pass the reservation here.
    pub fn ensure_loaded(
        &mut self,
        circuit_id: &CircuitId,
        budget_reservation: Option<crate::memory::MemoryReservation>,
    ) -> Result<Arc<SuprasealParameters<Bls12>>> {
        // Fast path: already loaded
        if let Some(srs) = self.loaded.get(circuit_id) {
            self.last_used.insert(circuit_id.clone(), Instant::now());
            debug!(circuit_id = %circuit_id, "SRS already loaded (cache hit)");
            // Reservation (if any) is dropped here, releasing budget back
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

        let file_size = std::fs::metadata(&path)
            .with_context(|| format!("failed to stat {}", path.display()))?
            .len();

        // Ensure we have budget for this SRS
        let reservation = match budget_reservation {
            Some(r) => r,
            None => {
                // Caller didn't pre-reserve; try non-blocking acquire
                self.budget.try_acquire(file_size).ok_or_else(|| {
                    anyhow::anyhow!(
                        "insufficient memory budget for SRS {} ({} GiB), \
                         available: {} GiB, total: {} GiB",
                        circuit_id,
                        file_size / crate::memory::GIB,
                        self.budget.available_bytes() / crate::memory::GIB,
                        self.budget.total_bytes() / crate::memory::GIB,
                    )
                })?
            }
        };

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

        let srs = Arc::new(srs);
        self.loaded.insert(circuit_id.clone(), Arc::clone(&srs));
        self.loaded_bytes += file_size;
        self.last_used.insert(circuit_id.clone(), Instant::now());

        // Make reservation permanent — SrsManager owns this budget now.
        // It will be released via evict() → budget.release_internal().
        reservation.into_permanent();

        info!(
            circuit_id = %circuit_id,
            elapsed_ms = elapsed.as_millis(),
            budget_used_gib = self.budget.used_bytes() / crate::memory::GIB,
            budget_available_gib = self.budget.available_bytes() / crate::memory::GIB,
            "SRS loaded successfully"
        );

        Ok(srs)
    }

    /// Return evictable SRS entries (idle with refcount == 1).
    ///
    /// Only entries where `Arc::strong_count() == 1` (only the cache holds
    /// a reference — no in-flight proof) are eligible.
    ///
    /// Returns `(circuit_id, file_size_bytes, last_used)`.
    pub fn evictable_entries(&self) -> Vec<(CircuitId, u64, Instant)> {
        let mut entries = Vec::new();
        for (cid, srs_arc) in &self.loaded {
            if Arc::strong_count(srs_arc) == 1 {
                if let Some(&last_used) = self.last_used.get(cid) {
                    let file_size =
                        std::fs::metadata(self.param_dir.join(circuit_id_to_param_filename(cid)))
                            .map(|m| m.len())
                            .unwrap_or(0);
                    entries.push((cid.clone(), file_size, last_used));
                }
            }
        }
        entries
    }

    /// Evict a specific SRS entry, freeing its memory and budget.
    ///
    /// Returns the number of bytes freed (0 if entry not found or in use).
    /// The actual pinned memory is freed when all `Arc` references are dropped.
    pub fn evict(&mut self, circuit_id: &CircuitId) -> u64 {
        // Check refcount before removing
        if let Some(srs_arc) = self.loaded.get(circuit_id) {
            if Arc::strong_count(srs_arc) > 1 {
                debug!(
                    circuit_id = %circuit_id,
                    refcount = Arc::strong_count(srs_arc),
                    "SRS cannot be evicted — in use by in-flight proof"
                );
                return 0;
            }
        }
        if let Some(_srs) = self.loaded.remove(circuit_id) {
            let filename = circuit_id_to_param_filename(circuit_id);
            let path = self.param_dir.join(filename);
            let file_size = std::fs::metadata(&path).map(|m| m.len()).unwrap_or(0);
            self.loaded_bytes = self.loaded_bytes.saturating_sub(file_size);
            self.last_used.remove(circuit_id);
            // Release budget — the Arc<SuprasealParameters> is dropped here.
            // If this was the last reference, Drop calls cudaFreeHost.
            self.budget.release_internal(file_size);
            info!(
                circuit_id = %circuit_id,
                freed_gib = file_size / crate::memory::GIB,
                budget_available_gib = self.budget.available_bytes() / crate::memory::GIB,
                "SRS evicted"
            );
            file_size
        } else {
            0
        }
    }

    /// Get the SRS for a circuit ID without loading it.
    ///
    /// Returns None if the SRS is not loaded. Updates last_used if found.
    pub fn get(&mut self, circuit_id: &CircuitId) -> Option<Arc<SuprasealParameters<Bls12>>> {
        if let Some(srs) = self.loaded.get(circuit_id) {
            self.last_used.insert(circuit_id.clone(), Instant::now());
            Some(Arc::clone(srs))
        } else {
            None
        }
    }

    /// Check if an SRS is loaded.
    pub fn is_loaded(&self, circuit_id: &CircuitId) -> bool {
        self.loaded.contains_key(circuit_id)
    }

    /// Get the file size for an SRS entry (for pre-budget-acquisition).
    pub fn srs_file_size(&self, circuit_id: &CircuitId) -> u64 {
        let filename = circuit_id_to_param_filename(circuit_id);
        let path = self.param_dir.join(filename);
        std::fs::metadata(&path).map(|m| m.len()).unwrap_or(0)
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
    pub fn new(param_dir: PathBuf, _budget: Arc<crate::memory::MemoryBudget>) -> Self {
        Self { param_dir }
    }

    pub fn param_dir(&self) -> &Path {
        &self.param_dir
    }

    pub fn is_loaded(&self, _circuit_id: &CircuitId) -> bool {
        false
    }

    pub fn srs_file_size(&self, _circuit_id: &CircuitId) -> u64 {
        0
    }

    pub fn evictable_entries(&self) -> Vec<(CircuitId, u64, Instant)> {
        vec![]
    }

    pub fn evict(&mut self, _circuit_id: &CircuitId) -> u64 {
        0
    }

    pub fn loaded_ids(&self) -> Vec<CircuitId> {
        vec![]
    }

    pub fn loaded_bytes(&self) -> u64 {
        0
    }

    pub fn ensure_loaded(
        &mut self,
        _circuit_id: &CircuitId,
        _budget_reservation: Option<crate::memory::MemoryReservation>,
    ) -> anyhow::Result<()> {
        anyhow::bail!("SRS loading requires cuda-supraseal feature")
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
