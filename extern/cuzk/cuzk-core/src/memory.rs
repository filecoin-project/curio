//! Memory budget manager for the cuzk proving engine.
//!
//! Provides a unified, budget-based memory manager that tracks all major
//! memory consumers (SRS, PCE, synthesis working set) under a single
//! byte-level budget auto-detected from system RAM.
//!
//! ## Design
//!
//! One budget covers everything: SRS (pinned), PCE (heap), and working set
//! (heap). The total is auto-detected from system RAM minus a safety margin.
//!
//! Every memory consumer acquires a [`MemoryReservation`] from the budget
//! before allocating. The reservation is an RAII guard that releases its
//! quota when dropped, with support for partial release mid-lifecycle.
//!
//! Long-lived resources (SRS, PCE) use [`MemoryReservation::into_permanent`]
//! to transfer ownership to the cache manager, which releases budget
//! explicitly on eviction via [`MemoryBudget::release_internal`].

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

use tokio::sync::Notify;
use tracing::{debug, warn};

use crate::srs_manager::CircuitId;

// ─── Constants ──────────────────────────────────────────────────────────────

pub const GIB: u64 = 1024 * 1024 * 1024;

/// Default safety margin subtracted from system RAM for auto-detection.
pub const DEFAULT_SAFETY_MARGIN: u64 = 5 * GIB;

// Per-partition estimates (PoRep 32G, 1 circuit)
pub const POREP_PARTITION_FULL_BYTES: u64 = 14 * GIB; // ~13.6, rounded up
pub const POREP_PARTITION_ABC_BYTES: u64 = 13 * GIB; // ~12.5, rounded up
pub const POREP_PARTITION_REST_BYTES: u64 = POREP_PARTITION_FULL_BYTES - POREP_PARTITION_ABC_BYTES;

// Full PoRep batch (all 10 partitions at once)
pub const POREP_BATCH_FULL_BYTES: u64 = 136 * GIB;
pub const POREP_BATCH_ABC_BYTES: u64 = 125 * GIB;

// WindowPoSt 32G (single partition, ~125M constraints)
pub const WPOST_FULL_BYTES: u64 = 14 * GIB; // ~13.2, rounded up
pub const WPOST_ABC_BYTES: u64 = 13 * GIB;

// WinningPoSt 32G (single partition, ~3.5M constraints)
pub const WINNING_FULL_BYTES: u64 = GIB / 2; // ~0.4 GiB
pub const WINNING_ABC_BYTES: u64 = GIB / 4;

// SnapDeals 32G (1 partition, ~81M constraints)
pub const SNAP_PARTITION_FULL_BYTES: u64 = 9 * GIB; // ~8.6, rounded up
pub const SNAP_PARTITION_ABC_BYTES: u64 = 8 * GIB;

// PCE extraction transient (RecordingCS synthesis)
pub const PCE_EXTRACTION_TRANSIENT_BYTES: u64 = 14 * GIB;

// ─── Estimation Helpers ─────────────────────────────────────────────────────

/// Get full synthesis estimate for a circuit type (per partition).
pub fn proof_kind_full_bytes(circuit_id: &CircuitId) -> u64 {
    match circuit_id {
        CircuitId::Porep32G | CircuitId::Porep64G => POREP_PARTITION_FULL_BYTES,
        CircuitId::WindowPost32G => WPOST_FULL_BYTES,
        CircuitId::WinningPost32G => WINNING_FULL_BYTES,
        CircuitId::SnapDeals32G | CircuitId::SnapDeals64G => SNAP_PARTITION_FULL_BYTES,
    }
}

/// Get a/b/c portion estimate for a circuit type.
pub fn proof_kind_abc_bytes(circuit_id: &CircuitId) -> u64 {
    match circuit_id {
        CircuitId::Porep32G | CircuitId::Porep64G => POREP_PARTITION_ABC_BYTES,
        CircuitId::WindowPost32G => WPOST_ABC_BYTES,
        CircuitId::WinningPost32G => WINNING_ABC_BYTES,
        CircuitId::SnapDeals32G | CircuitId::SnapDeals64G => SNAP_PARTITION_ABC_BYTES,
    }
}

// ─── System Memory Detection ────────────────────────────────────────────────

/// Read total system memory from `/proc/meminfo`.
///
/// Returns bytes, or `None` on non-Linux / parse failure.
///
/// Example `/proc/meminfo` line: `MemTotal:       263041396 kB`
pub fn detect_system_memory() -> Option<u64> {
    let content = std::fs::read_to_string("/proc/meminfo").ok()?;
    for line in content.lines() {
        if let Some(rest) = line.strip_prefix("MemTotal:") {
            let rest = rest.trim();
            if let Some(kb_str) = rest.strip_suffix("kB") {
                let kb: u64 = kb_str.trim().parse().ok()?;
                return Some(kb * 1024);
            }
        }
    }
    None
}

// ─── MemoryBudget ───────────────────────────────────────────────────────────

/// Central memory budget manager.
///
/// All memory consumers (SRS, PCE, synthesis working set) acquire reservations
/// from this budget before allocating. The budget enforces a hard cap derived
/// from system RAM.
///
/// ## Thread Safety
///
/// `MemoryBudget` is `Send + Sync`. All state is managed through atomics
/// and `tokio::sync` primitives.
pub struct MemoryBudget {
    /// Total budget in bytes (system RAM - safety margin).
    total_bytes: u64,

    /// Currently reserved bytes (sum of all live reservations + permanent).
    used_bytes: AtomicU64,

    /// Wakes blocked `acquire()` callers when reservations are released.
    notify: Notify,

    /// Callback to attempt eviction when budget is full.
    /// Takes `needed_bytes`, returns actually freed bytes.
    /// Set once by the engine after SrsManager and PceCache are created.
    evictor: tokio::sync::RwLock<Option<Arc<dyn Fn(u64) -> u64 + Send + Sync>>>,
}

impl MemoryBudget {
    /// Create a new budget.
    ///
    /// Use [`detect_system_memory`] to derive `total_bytes`:
    /// ```ignore
    /// let total = detect_system_memory().unwrap_or(256 * GIB) - safety_margin;
    /// let budget = MemoryBudget::new(total);
    /// ```
    pub fn new(total_bytes: u64) -> Self {
        Self {
            total_bytes,
            used_bytes: AtomicU64::new(0),
            notify: Notify::new(),
            evictor: tokio::sync::RwLock::new(None),
        }
    }

    /// Set the eviction callback (called once during engine init).
    ///
    /// The callback receives the number of bytes needed and should attempt
    /// to free that much by evicting idle SRS/PCE entries. Returns the
    /// number of bytes actually freed.
    pub async fn set_evictor(&self, f: Arc<dyn Fn(u64) -> u64 + Send + Sync>) {
        let mut guard = self.evictor.write().await;
        *guard = Some(f);
    }

    /// Current bytes reserved.
    pub fn used_bytes(&self) -> u64 {
        self.used_bytes.load(Ordering::Relaxed)
    }

    /// Current bytes available.
    pub fn available_bytes(&self) -> u64 {
        self.total_bytes.saturating_sub(self.used_bytes.load(Ordering::Relaxed))
    }

    /// Total budget.
    pub fn total_bytes(&self) -> u64 {
        self.total_bytes
    }

    /// Acquire a reservation of `amount` bytes.
    ///
    /// If the budget has space, returns immediately.
    /// If not, calls the evictor to try freeing SRS/PCE entries.
    /// If still not enough, blocks until other reservations are released.
    ///
    /// Returns a [`MemoryReservation`] that releases on drop.
    pub async fn acquire(self: &Arc<Self>, amount: u64) -> MemoryReservation {
        if amount == 0 {
            return MemoryReservation {
                budget: Arc::clone(self),
                remaining: AtomicU64::new(0),
            };
        }

        if amount > self.total_bytes {
            warn!(
                amount_gib = amount / GIB,
                total_gib = self.total_bytes / GIB,
                "acquire request exceeds total budget — will block until possible"
            );
        }

        loop {
            // Attempt optimistic reservation
            let old = self.used_bytes.fetch_add(amount, Ordering::AcqRel);
            if old + amount <= self.total_bytes {
                // Success — budget has room
                debug!(
                    amount_gib = amount / GIB,
                    used_gib = (old + amount) / GIB,
                    total_gib = self.total_bytes / GIB,
                    "budget acquired"
                );
                return MemoryReservation {
                    budget: Arc::clone(self),
                    remaining: AtomicU64::new(amount),
                };
            }
            // Over budget — undo
            self.used_bytes.fetch_sub(amount, Ordering::AcqRel);

            // Try eviction
            let evictor = self.evictor.read().await;
            if let Some(ref evict_fn) = *evictor {
                let freed = evict_fn(amount);
                if freed > 0 {
                    debug!(
                        freed_gib = freed / GIB,
                        needed_gib = amount / GIB,
                        "evictor freed memory, retrying acquire"
                    );
                    continue; // Retry — eviction freed some space
                }
            }
            drop(evictor);

            // Wait for a release notification, then retry
            debug!(
                needed_gib = amount / GIB,
                available_gib = self.available_bytes() / GIB,
                total_gib = self.total_bytes / GIB,
                "waiting for budget (acquire blocked)"
            );
            self.notify.notified().await;
        }
    }

    /// Try to acquire without blocking. Returns `None` if insufficient budget.
    pub fn try_acquire(self: &Arc<Self>, amount: u64) -> Option<MemoryReservation> {
        if amount == 0 {
            return Some(MemoryReservation {
                budget: Arc::clone(self),
                remaining: AtomicU64::new(0),
            });
        }

        let old = self.used_bytes.fetch_add(amount, Ordering::AcqRel);
        if old + amount <= self.total_bytes {
            Some(MemoryReservation {
                budget: Arc::clone(self),
                remaining: AtomicU64::new(amount),
            })
        } else {
            self.used_bytes.fetch_sub(amount, Ordering::AcqRel);
            None
        }
    }

    /// Release budget that was held by a permanent resource (SRS/PCE).
    ///
    /// Called by `SrsManager::evict()` and `PceCache::evict()` when
    /// long-lived resources are freed. This is the counterpart to
    /// [`MemoryReservation::into_permanent`].
    pub(crate) fn release_internal(&self, amount: u64) {
        if amount == 0 {
            return;
        }
        self.used_bytes.fetch_sub(amount, Ordering::AcqRel);
        self.notify.notify_waiters();
    }
}

// ─── MemoryReservation ──────────────────────────────────────────────────────

/// RAII guard for a memory reservation. Supports partial release.
///
/// When dropped, releases any remaining reserved bytes back to the budget.
///
/// ## Two-Phase Release
///
/// For partition synthesis, the working memory is released in two phases:
/// 1. After `prove_start`: a/b/c vectors are freed → call [`release`] with the
///    a/b/c estimate to immediately return ~12.5 GiB.
/// 2. After `prove_finish`: the reservation is dropped, releasing the remaining
///    ~1.1 GiB (aux_assignment + density bitvecs).
pub struct MemoryReservation {
    budget: Arc<MemoryBudget>,
    /// Bytes still reserved (decremented by `release()`).
    remaining: AtomicU64,
}

impl MemoryReservation {
    /// Release `amount` bytes back to the budget immediately.
    ///
    /// Use after a/b/c vectors are freed in `prove_start`.
    /// Panics if `amount > remaining`.
    pub fn release(&self, amount: u64) {
        if amount == 0 {
            return;
        }
        let prev = self.remaining.fetch_sub(amount, Ordering::AcqRel);
        assert!(
            prev >= amount,
            "MemoryReservation::release({}) exceeds remaining ({})",
            amount,
            prev
        );
        self.budget.release_internal(amount);
        debug!(
            released_gib = amount / GIB,
            remaining_gib = (prev - amount) / GIB,
            "partial reservation release"
        );
    }

    /// Bytes still held by this reservation.
    pub fn remaining(&self) -> u64 {
        self.remaining.load(Ordering::Relaxed)
    }

    /// Consume the reservation without releasing the budget.
    ///
    /// Used when the reserved memory becomes owned by a long-lived cache
    /// (SRS or PCE) that manages its own lifecycle via
    /// [`MemoryBudget::release_internal`].
    ///
    /// After this call, the reservation's `Drop` impl will not release
    /// any budget.
    pub fn into_permanent(self) {
        self.remaining.store(0, Ordering::Relaxed);
        // self is dropped, but Drop sees remaining=0 and does nothing
    }
}

impl Drop for MemoryReservation {
    fn drop(&mut self) {
        let remaining = self.remaining.load(Ordering::Relaxed);
        if remaining > 0 {
            self.budget.release_internal(remaining);
        }
    }
}

// ─── Tests ──────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[tokio::test]
    async fn test_acquire_release() {
        let budget = Arc::new(MemoryBudget::new(100 * GIB));
        assert_eq!(budget.used_bytes(), 0);
        assert_eq!(budget.available_bytes(), 100 * GIB);

        let reservation = budget.acquire(30 * GIB).await;
        assert_eq!(budget.used_bytes(), 30 * GIB);
        assert_eq!(budget.available_bytes(), 70 * GIB);
        assert_eq!(reservation.remaining(), 30 * GIB);

        drop(reservation);
        assert_eq!(budget.used_bytes(), 0);
        assert_eq!(budget.available_bytes(), 100 * GIB);
    }

    #[tokio::test]
    async fn test_partial_release() {
        let budget = Arc::new(MemoryBudget::new(100 * GIB));

        let reservation = budget.acquire(14 * GIB).await;
        assert_eq!(budget.used_bytes(), 14 * GIB);

        // Phase 1: release a/b/c portion
        reservation.release(13 * GIB);
        assert_eq!(budget.used_bytes(), 1 * GIB);
        assert_eq!(reservation.remaining(), 1 * GIB);

        // Phase 2: drop releases remainder
        drop(reservation);
        assert_eq!(budget.used_bytes(), 0);
    }

    #[tokio::test]
    async fn test_try_acquire() {
        let budget = Arc::new(MemoryBudget::new(20 * GIB));

        let r1 = budget.try_acquire(15 * GIB);
        assert!(r1.is_some());
        assert_eq!(budget.used_bytes(), 15 * GIB);

        // Should fail — only 5 GiB left
        let r2 = budget.try_acquire(10 * GIB);
        assert!(r2.is_none());
        assert_eq!(budget.used_bytes(), 15 * GIB);

        // Should succeed — exactly 5 GiB left
        let r3 = budget.try_acquire(5 * GIB);
        assert!(r3.is_some());
        assert_eq!(budget.used_bytes(), 20 * GIB);

        drop(r1);
        drop(r3);
        assert_eq!(budget.used_bytes(), 0);
    }

    #[tokio::test]
    async fn test_into_permanent() {
        let budget = Arc::new(MemoryBudget::new(100 * GIB));

        let reservation = budget.acquire(44 * GIB).await;
        assert_eq!(budget.used_bytes(), 44 * GIB);

        // Convert to permanent — budget stays consumed
        reservation.into_permanent();
        assert_eq!(budget.used_bytes(), 44 * GIB);

        // Only explicit release_internal frees it
        budget.release_internal(44 * GIB);
        assert_eq!(budget.used_bytes(), 0);
    }

    #[tokio::test]
    async fn test_acquire_blocks_then_unblocks() {
        let budget = Arc::new(MemoryBudget::new(20 * GIB));

        // Fill the budget
        let r1 = budget.acquire(20 * GIB).await;
        assert_eq!(budget.available_bytes(), 0);

        // Spawn a task that tries to acquire — will block
        let budget2 = budget.clone();
        let handle = tokio::spawn(async move {
            let _r2 = budget2.acquire(10 * GIB).await;
            true // reached here = unblocked
        });

        // Give the spawned task time to park on notify
        tokio::time::sleep(Duration::from_millis(50)).await;
        assert!(!handle.is_finished());

        // Release enough budget
        r1.release(15 * GIB);

        // The spawned task should now complete
        let result = tokio::time::timeout(Duration::from_secs(2), handle).await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_zero_amount() {
        let budget = Arc::new(MemoryBudget::new(100 * GIB));

        let r = budget.acquire(0).await;
        assert_eq!(budget.used_bytes(), 0);
        assert_eq!(r.remaining(), 0);
        drop(r);

        let r = budget.try_acquire(0);
        assert!(r.is_some());
    }

    #[test]
    fn test_detect_system_memory() {
        // On Linux CI, this should return Some
        if cfg!(target_os = "linux") {
            let mem = detect_system_memory();
            assert!(mem.is_some(), "detect_system_memory should work on Linux");
            let bytes = mem.unwrap();
            // Sanity: at least 1 GiB, at most 64 TiB
            assert!(bytes >= GIB, "system memory too low: {}", bytes);
            assert!(
                bytes <= 64 * 1024 * GIB,
                "system memory implausibly high: {}",
                bytes
            );
        }
    }

    #[test]
    fn test_estimation_helpers() {
        assert_eq!(proof_kind_full_bytes(&CircuitId::Porep32G), 14 * GIB);
        assert_eq!(proof_kind_abc_bytes(&CircuitId::Porep32G), 13 * GIB);
        assert_eq!(proof_kind_full_bytes(&CircuitId::WinningPost32G), GIB / 2);
        assert_eq!(proof_kind_abc_bytes(&CircuitId::WinningPost32G), GIB / 4);
        assert_eq!(proof_kind_full_bytes(&CircuitId::SnapDeals32G), 9 * GIB);
    }
}
