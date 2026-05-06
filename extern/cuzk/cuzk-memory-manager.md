# cuzk Memory Manager — Implementation Specification

## Goal

Replace the static `partition_workers` semaphore with a unified, budget-based
memory manager that tracks all major memory consumers (SRS, PCE, synthesis
working set) under a single byte-level budget auto-detected from system RAM.
SRS and PCE are loaded on demand, consume quota, and are evicted under pressure
when idle for ≥5 minutes.

---

## 1. Background: Current Memory Architecture

### 1.1 Resident Memory (process lifetime today)

| Component | Size (PoRep 32G) | Memory Kind | Lifetime |
|---|---|---|---|
| SRS (Groth16 params) | ~44 GiB | CUDA pinned (`cudaHostAlloc`) | Process — never evicted |
| PCE (pre-compiled R1CS) | ~26 GiB | Heap (`Vec`) | Process — `OnceLock`, never freed |
| GPU d_a buffer | ~4 GiB | VRAM (`cudaMalloc`) | Process — cached per GPU |
| GPU memory pool (d_b, NTT scratch) | ~8 GiB | VRAM (CUDA async pool) | Process — pool threshold=MAX |

Baseline RSS before any proof ≈ 70 GiB (SRS + PCE).

### 1.2 Per-Partition Working Memory (PoRep 32G)

Each of the 10 partitions in a 32G PoRep proof produces this during synthesis:

| Buffer | Size | Type |
|---|---|---|
| `ProvingAssignment.a` | ~4.17 GiB | `Vec<Fr>` (~130M × 32 bytes) |
| `ProvingAssignment.b` | ~4.17 GiB | `Vec<Fr>` |
| `ProvingAssignment.c` | ~4.17 GiB | `Vec<Fr>` |
| `aux_assignment` | ~0.74 GiB | `Arc<Vec<Fr>>` (~23M × 32 bytes) |
| `input_assignment` | negligible | `Arc<Vec<Fr>>` (~1 element) |
| Density bitvecs | ~48 MB | BitVec inside ProvingAssignment |
| **Total per partition** | **~13.6 GiB** | |

#### Two-phase release during GPU proving

1. **After `prove_start`** (bellperson `supraseal.rs:230-235`): `prover.a/b/c = Vec::new()`
   drops the a/b/c buffers synchronously. Frees **~12.5 GiB**.
2. **After `gpu_prove_finish`** (bellperson `supraseal.rs:276-292`): async dealloc thread
   drops prover shells + aux_assignment. Frees remaining **~1.1 GiB**.

### 1.3 SRS Sizes by Circuit Type

| Circuit | .params file size | Pinned memory |
|---|---|---|
| PoRep 32G | ~44 GiB | ~44 GiB |
| WindowPoSt 32G | ~57 GiB | ~57 GiB |
| WinningPoSt 32G | ~184 MiB | ~184 MiB |
| SnapDeals 32G | ~33 GiB | ~33 GiB |

### 1.4 PCE Sizes by Circuit Type (approximate)

| Circuit | In-memory size | Disk file size |
|---|---|---|
| PoRep 32G | ~26 GiB | ~25 GiB |
| WindowPoSt 32G | ~26 GiB | ~25 GiB |
| WinningPoSt 32G | ~0.4 GiB | ~0.4 GiB |
| SnapDeals 32G | ~17 GiB | ~16 GiB |

### 1.5 Current Throttling (being replaced)

| Mechanism | Location | What it does |
|---|---|---|
| `partition_semaphore` | `engine.rs:1074-1078` | Limits concurrent partition synthesis to `partition_workers` count |
| `synth_semaphore` | `engine.rs:1072` | Limits concurrent full-proof syntheses to `synthesis_concurrency` |
| Bounded channel | `engine.rs:1021-1028` | Backpressure: `max(lookahead, partition_workers)` capacity |
| `working_memory_budget` | `config.rs:56-57` | **Dead code** — configured but never checked |
| `pinned_budget` | `srs_manager.rs:173` | Advisory only — logs warning, loads anyway |
| Buffer flight counters | `pipeline.rs:39-80` | Diagnostic only — not enforced |

---

## 2. Design

### 2.1 Single Unified Memory Budget

One budget covers everything: SRS (pinned), PCE (heap), and working set (heap).
The total is auto-detected from system RAM.

```
total_budget = MemTotal (from /proc/meminfo) - safety_margin
```

Default `safety_margin` = 5 GiB (configurable). This reserves space for the OS,
kernel buffers, stack, and other non-cuzk allocations.

**Note:** VRAM (GPU d_a, d_b, memory pool) is NOT tracked by this budget.
It lives on the GPU and doesn't compete with host RAM.

### 2.2 Reservation Lifecycle

Every memory consumer acquires a `MemoryReservation` from the budget before
allocating. The reservation is an RAII guard that releases its quota when
dropped, with support for partial release mid-lifecycle.

```
  ┌─────────────────────────────────────────────────────────┐
  │                   MemoryBudget                          │
  │  total: 251 GiB                                        │
  │  used:  AtomicU64  ────────────────────────────┐       │
  │  notify: tokio::sync::Notify                   │       │
  │  evictor: Box<dyn Fn(u64)->u64 + Send + Sync>  │       │
  └─────────────────────────────────────────────────│───────┘
                                                    │
       ┌────────────────────────────────────────────┼───────┐
       │ used = SRS(44) + PCE(26) + partitions(N×13.6) ... │
       └────────────────────────────────────────────────────┘
```

### 2.3 On-Demand Loading with Budget Gating

**SRS**: Loaded inside `SrsManager::ensure_loaded()` when first proof of a
circuit type arrives. The method calls `budget.acquire(file_size)` before
calling `SuprasealParameters::new(path)`. If the budget is full, this blocks
(and may trigger eviction of other SRS/PCE entries).

**PCE**: Loaded from disk (if file exists) or extracted from scratch on first
proof. `PceCache::get_or_load()` calls `budget.acquire(pce_size)` before
loading. Background extraction also acquires budget for its transient memory.

No configurable preload list. Everything is on-demand.

### 2.4 Eviction Under Pressure

When `budget.acquire(amount)` cannot be fulfilled:

1. Call the evictor callback with the `amount` needed.
2. The evictor scans SRS and PCE caches for entries that are:
   - **Idle ≥ 5 minutes** (`Instant::now() - last_used >= Duration::from_secs(300)`)
   - **Not in use** (`Arc::strong_count() == 1`, meaning no in-flight proof holds a clone)
3. Evict oldest-first (LRU) until enough space is freed or no more candidates.
4. If eviction freed enough, retry acquire.
5. If not, block on `notify.notified().await` until some other reservation is released.

**SRS eviction**: Remove from `SrsManager.loaded` HashMap. If refcount drops to 0,
`SuprasealParameters` Drop impl calls C++ destructor which calls `cudaFreeHost(pinned)`.

**PCE eviction**: Remove from `PceCache.entries` HashMap. If refcount drops to 0,
the `PreCompiledCircuit` Vecs are freed. PCE can be reloaded from disk in ~5s.
SRS reloads take 30-60s (mmap + cudaHostAlloc + deserialize).

### 2.5 Working Set: Two-Phase Release

For partition-level pipeline (the primary code path for PoRep/SnapDeals):

```
Step 1: budget.acquire(FULL_EST)           // e.g. 13.6 GiB for PoRep partition
        → returns MemoryReservation
Step 2: synthesis runs on spawn_blocking
Step 3: SynthesizedJob sent through channel (reservation travels with it)
Step 4: GPU worker calls gpu_prove_start()
        → inside prove_start: a/b/c Vecs dropped (synchronous)
Step 5: reservation.release(ABC_EST)       // e.g. 12.5 GiB — immediate
Step 6: GPU worker spawns finalizer task
Step 7: gpu_prove_finish() completes
Step 8: drop(reservation)                  // releases remaining ~1.1 GiB
```

For monolithic/batch synthesis (non-partitioned path): same pattern but with
larger FULL_EST (e.g. 136 GiB for full 10-partition PoRep batch).

### 2.6 synthesis_concurrency Stays

The `synth_semaphore` (controlled by `synthesis_concurrency` config) is kept.
It limits CPU/memory-bandwidth contention between concurrent rayon synthesis
tasks — a separate constraint from the memory budget. The budget prevents OOM;
the semaphore prevents CPU thrashing.

### 2.7 partition_workers Removed

The `partition_semaphore` is removed entirely. The memory budget replaces it:
partition tasks call `budget.acquire(PARTITION_EST)` which naturally limits
concurrency based on available memory. On a 256 GiB machine with 44 GiB SRS +
26 GiB PCE loaded, ~13 partitions can run concurrently. On a 512 GiB machine,
~26. No manual tuning.

---

## 3. New Types

### 3.1 `MemoryBudget` (new file: `cuzk-core/src/memory.rs`)

```rust
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use tokio::sync::Notify;

/// Central memory budget manager.
///
/// All memory consumers (SRS, PCE, synthesis working set) acquire reservations
/// from this budget before allocating. The budget enforces a hard cap derived
/// from system RAM.
pub struct MemoryBudget {
    /// Total budget in bytes (system RAM - safety margin).
    total_bytes: u64,

    /// Currently reserved bytes (sum of all live reservations).
    used_bytes: AtomicU64,

    /// Wakes blocked `acquire()` callers when reservations are released.
    notify: Notify,

    /// Callback to attempt eviction when budget is full.
    /// Takes needed_bytes, returns actually freed bytes.
    /// Set once by the engine after SrsManager and PceCache are created.
    evictor: tokio::sync::RwLock<Option<Arc<dyn Fn(u64) -> u64 + Send + Sync>>>,
}
```

#### Key methods

```rust
impl MemoryBudget {
    /// Create a new budget. Call `detect_system_memory()` for `total_bytes`.
    pub fn new(total_bytes: u64) -> Self;

    /// Set the eviction callback (called once during engine init).
    pub async fn set_evictor(&self, f: Arc<dyn Fn(u64) -> u64 + Send + Sync>);

    /// Current bytes reserved.
    pub fn used_bytes(&self) -> u64;

    /// Current bytes available.
    pub fn available_bytes(&self) -> u64;

    /// Total budget.
    pub fn total_bytes(&self) -> u64;

    /// Acquire a reservation of `amount` bytes.
    ///
    /// If the budget has space, returns immediately.
    /// If not, calls the evictor to try freeing SRS/PCE entries.
    /// If still not enough, blocks until other reservations are released.
    ///
    /// Returns a `MemoryReservation` that releases on drop.
    pub async fn acquire(self: &Arc<Self>, amount: u64) -> MemoryReservation;

    /// Try to acquire without blocking. Returns None if insufficient budget.
    pub fn try_acquire(self: &Arc<Self>, amount: u64) -> Option<MemoryReservation>;

    /// Internal: called by MemoryReservation::release and Drop.
    fn release_internal(&self, amount: u64);
}
```

#### `MemoryReservation`

```rust
/// RAII guard for a memory reservation. Supports partial release.
///
/// When dropped, releases any remaining reserved bytes back to the budget.
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
    pub fn release(&self, amount: u64);

    /// Bytes still held by this reservation.
    pub fn remaining(&self) -> u64;
}

impl Drop for MemoryReservation {
    fn drop(&mut self) {
        let remaining = self.remaining.load(Ordering::Relaxed);
        if remaining > 0 {
            self.budget.release_internal(remaining);
        }
    }
}
```

#### System memory detection

```rust
/// Read total system memory from /proc/meminfo.
/// Returns bytes, or None on non-Linux / parse failure.
pub fn detect_system_memory() -> Option<u64> {
    // Read /proc/meminfo, find "MemTotal:" line, parse kB value, convert to bytes.
    // Example line: "MemTotal:       263041396 kB"
}
```

### 3.2 `PceCache` (new struct in `cuzk-core/src/pipeline.rs`, replaces static `OnceLock`s)

```rust
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::Instant;

/// Evictable PCE cache, budget-integrated.
pub struct PceCache {
    entries: Mutex<HashMap<CircuitId, PceCacheEntry>>,
    budget: Arc<MemoryBudget>,
    param_cache: PathBuf,
}

struct PceCacheEntry {
    pce: Arc<PreCompiledCircuit<Fr>>,
    size_bytes: u64,
    last_used: Instant,
}
```

#### Key methods

```rust
impl PceCache {
    pub fn new(budget: Arc<MemoryBudget>, param_cache: PathBuf) -> Self;

    /// Get a cached PCE, updating last_used. Returns None if not loaded.
    pub fn get(&self, circuit_id: &CircuitId) -> Option<Arc<PreCompiledCircuit<Fr>>>;

    /// Insert a PCE into the cache. Acquires budget for its size.
    /// If already cached, returns without re-inserting.
    pub async fn insert(&self, circuit_id: CircuitId, pce: PreCompiledCircuit<Fr>);

    /// Try to load PCE from disk into cache. Returns Ok(true) if loaded,
    /// Ok(false) if file not found. Acquires budget before loading.
    pub async fn load_from_disk(&self, circuit_id: &CircuitId) -> anyhow::Result<bool>;

    /// Return evictable entries (idle ≥ 5 min, refcount == 1).
    /// Used by the evictor callback.
    pub fn evictable_entries(&self) -> Vec<(CircuitId, u64, Instant)>;

    /// Evict a specific entry. Returns freed bytes (0 if not found or in use).
    pub fn evict(&self, circuit_id: &CircuitId) -> u64;
}
```

### 3.3 Modified `SrsManager`

Add to existing struct:

```rust
pub struct SrsManager {
    loaded: HashMap<CircuitId, Arc<SuprasealParameters<Bls12>>>,
    param_dir: PathBuf,
    loaded_bytes: u64,
    // REMOVED: budget_bytes (old advisory budget)
    // NEW:
    budget: Arc<MemoryBudget>,
    last_used: HashMap<CircuitId, Instant>,
}
```

#### Changed methods

```rust
impl SrsManager {
    /// Create with budget reference. No more budget_bytes parameter.
    pub fn new(param_dir: PathBuf, budget: Arc<MemoryBudget>) -> Self;

    /// Load SRS, gated by budget. Blocks if budget full (may trigger eviction).
    /// Updates last_used on cache hit.
    ///
    /// IMPORTANT: This is called from blocking threads (spawn_blocking), so
    /// it must use budget.try_acquire() in a spin/sleep loop rather than
    /// the async budget.acquire(). Alternatively, accept a pre-acquired
    /// MemoryReservation from the caller.
    ///
    /// Recommended approach: have ensure_loaded return a "needs N bytes" error
    /// when budget is insufficient, and let the async caller handle acquire().
    /// See section 5 for details.
    pub fn ensure_loaded(
        &mut self,
        circuit_id: &CircuitId,
    ) -> Result<Arc<SuprasealParameters<Bls12>>>;

    /// Return evictable entries (idle ≥ 5 min, refcount == 1).
    pub fn evictable_entries(&self) -> Vec<(CircuitId, u64, Instant)>;

    /// Evict a specific entry. Returns freed bytes.
    pub fn evict(&mut self, circuit_id: &CircuitId) -> u64;
}
```

---

## 4. Config Changes

### 4.1 Remove

- `[srs] preload` — all loading is on-demand now
- `[memory] pinned_budget` — subsumed by unified budget
- `[memory] working_memory_budget` — subsumed by unified budget
- `[synthesis] partition_workers` — subsumed by memory budget

### 4.2 Add / Change

```toml
[memory]
# Total memory budget for all cuzk allocations (SRS + PCE + working set).
# "auto" = detect from /proc/meminfo MemTotal (recommended).
# Or specify explicitly: "256GiB", "512GiB", etc.
total_budget = "auto"

# Safety margin subtracted from total_budget when using "auto".
# Reserves space for OS, kernel, other processes.
safety_margin = "5GiB"

# Minimum idle time before SRS/PCE entries become eviction candidates.
# Entries are only evicted when the budget is under pressure AND idle
# for at least this duration.
eviction_min_idle = "5m"
```

### 4.3 Keep (unchanged)

```toml
[synthesis]
threads = 0                    # rayon thread pool size

[pipeline]
enabled = true
synthesis_lookahead = 1        # bounded channel capacity
synthesis_concurrency = 1      # CPU contention limit (separate from memory)
slot_size = 0                  # Phase 6 slotted pipeline (0 = disabled)

[gpus]
devices = []
gpu_threads = 32
gpu_workers_per_device = 2

[scheduler]
max_batch_size = 1
max_batch_wait_ms = 10000
sort_by_type = true
```

### 4.4 Config Structs

```rust
// config.rs

#[derive(Debug, Clone, Deserialize)]
pub struct MemoryConfig {
    /// Total budget: "auto" or explicit like "256GiB".
    #[serde(default = "MemoryConfig::default_total_budget")]
    pub total_budget: String,

    /// Safety margin for auto-detection.
    #[serde(default = "MemoryConfig::default_safety_margin")]
    pub safety_margin: String,

    /// Minimum idle time for eviction eligibility.
    #[serde(default = "MemoryConfig::default_eviction_min_idle")]
    pub eviction_min_idle: String,
}

impl MemoryConfig {
    fn default_total_budget() -> String { "auto".to_string() }
    fn default_safety_margin() -> String { "5GiB".to_string() }
    fn default_eviction_min_idle() -> String { "5m".to_string() }

    /// Resolve total budget in bytes.
    pub fn resolve_total_budget(&self) -> u64 {
        if self.total_budget == "auto" {
            let total = detect_system_memory().unwrap_or(256 * GIB);
            let margin = parse_size(&self.safety_margin);
            total.saturating_sub(margin)
        } else {
            parse_size(&self.total_budget)
        }
    }

    /// Parse eviction_min_idle into Duration.
    pub fn eviction_min_idle_duration(&self) -> Duration {
        parse_duration(&self.eviction_min_idle)  // "5m" -> 300s
    }
}

// Remove SynthesisConfig::partition_workers.
// Remove SrsConfig::preload.
// Remove MemoryConfig::pinned_budget, working_memory_budget.
```

Add `parse_duration` helper to config.rs:
```rust
fn parse_duration(s: &str) -> Duration {
    let s = s.trim();
    if let Some(n) = s.strip_suffix("m") {
        Duration::from_secs(n.trim().parse::<u64>().unwrap_or(5) * 60)
    } else if let Some(n) = s.strip_suffix("s") {
        Duration::from_secs(n.trim().parse::<u64>().unwrap_or(300))
    } else {
        Duration::from_secs(s.parse::<u64>().unwrap_or(300))
    }
}
```

---

## 5. Integration Points (engine.rs changes)

### 5.1 Engine Startup (`start()`)

**Remove** (lines 866-937):
- SRS preload loop (Phase 1 and Phase 2 paths)
- PCE preload call (`preload_pce_from_disk`)

**Add** after GPU detection:
```rust
// Create memory budget
let total_budget = self.config.memory.resolve_total_budget();
let budget = Arc::new(MemoryBudget::new(total_budget));
info!(
    total_budget_gib = total_budget / GIB,
    "memory budget initialized"
);

// Create PCE cache (replaces static OnceLocks)
let pce_cache = Arc::new(PceCache::new(
    budget.clone(),
    self.config.srs.param_cache.clone(),
));

// Wire evictor callback
let srs_for_evict = self.srs_manager.clone();
let pce_for_evict = pce_cache.clone();
let min_idle = self.config.memory.eviction_min_idle_duration();
budget.set_evictor(Arc::new(move |needed: u64| -> u64 {
    let mut freed = 0u64;

    // Collect all candidates from both caches
    let mut candidates: Vec<(String, CircuitId, u64, Instant, bool)> = Vec::new();

    // SRS candidates
    {
        let mgr = srs_for_evict.blocking_lock();
        for (cid, size, last_used) in mgr.evictable_entries() {
            if last_used.elapsed() >= min_idle {
                candidates.push((format!("srs:{}", cid), cid, size, last_used, true));
            }
        }
    }

    // PCE candidates
    for (cid, size, last_used) in pce_for_evict.evictable_entries() {
        if last_used.elapsed() >= min_idle {
            candidates.push((format!("pce:{}", cid), cid, size, last_used, false));
        }
    }

    // Sort by last_used ascending (oldest first = best eviction candidate)
    candidates.sort_by_key(|c| c.3);

    for (label, cid, _size, _last_used, is_srs) in candidates {
        if freed >= needed { break; }
        let bytes = if is_srs {
            let mut mgr = srs_for_evict.blocking_lock();
            mgr.evict(&cid)
        } else {
            pce_for_evict.evict(&cid)
        };
        if bytes > 0 {
            info!(entry = %label, freed_gib = bytes / GIB, "evicted for memory pressure");
        }
        freed += bytes;
    }

    freed
})).await;
```

### 5.2 Bounded Channel Sizing

**Current** (`engine.rs:1021-1028`):
```rust
let pw = self.config.synthesis.partition_workers as usize;
let lookahead = if pw > 0 { configured_lookahead.max(pw) } else { configured_lookahead };
```

**New**: `partition_workers` is gone. The channel capacity should be large enough
that completed syntheses can drain without blocking. Use a reasonable fixed value
or derive from the budget:

```rust
// Channel capacity: enough to buffer all partitions that could fit in memory.
// Use budget / partition_size as upper bound, capped at a sensible max.
let max_partitions_in_budget = (budget.total_bytes() / POREP_PARTITION_FULL_BYTES) as usize;
let lookahead = configured_lookahead.max(max_partitions_in_budget.min(32));
```

The `min(32)` cap prevents absurdly large channels on high-RAM machines.

### 5.3 Partition Dispatch (PoRep, lines ~1409-1568)

**Current**:
```rust
let partition_sem = partition_semaphore.clone();
tokio::spawn(async move {
    let permit = partition_sem.acquire_owned().await?;
    // ... synthesis ...
    synth_tx.send(job).await;
    drop(permit);
});
```

**New**:
```rust
let budget = budget.clone();
tokio::spawn(async move {
    // Acquire memory reservation (blocks until budget available, may evict SRS/PCE)
    let reservation = budget.acquire(POREP_PARTITION_FULL_BYTES).await;

    // Check if job already failed (skip if so — reservation drops, releasing budget)
    { /* existing failed-job check */ }

    // Synthesis
    let synth_result = tokio::task::spawn_blocking(move || {
        pipeline::synthesize_porep_c2_partition(...)
    }).await;

    // Build SynthesizedJob with reservation attached
    let job = SynthesizedJob {
        synth: synth_result,
        reservation: Some(reservation),  // travels through channel to GPU worker
        // ... other fields ...
    };
    synth_tx.send(job).await;
    // NOTE: no drop(permit) — reservation ownership transferred to SynthesizedJob
});
```

Same pattern for SnapDeals partitions (lines ~1693-1831), using
`SNAP_PARTITION_FULL_BYTES` instead.

### 5.4 SRS Loading in Partition Dispatch

**Current** (`engine.rs:1327-1339`): `mgr.ensure_loaded(&CircuitId::Porep32G)?` called
inside `spawn_blocking`.

The SRS load may need to acquire budget, which is async. Two approaches:

**Approach A** (recommended): Pre-check and acquire budget in the async context
before `spawn_blocking`:

```rust
// Before spawn_blocking:
let srs_size = srs_file_size(&CircuitId::Porep32G);
let srs_reservation = if !srs_manager_has(&CircuitId::Porep32G) {
    Some(budget.acquire(srs_size).await)
} else {
    None
};

// Inside spawn_blocking:
let srs = {
    let mut mgr = srs_mgr.blocking_lock();
    // ensure_loaded now trusts that budget was pre-acquired
    mgr.ensure_loaded(&CircuitId::Porep32G)?
};
// srs_reservation is stored inside SrsManager (transferred on insert)
```

**Approach B**: `SrsManager::ensure_loaded()` uses `budget.try_acquire()` (non-blocking).
If it returns None, propagate an error up to the async caller which does the async
acquire and retries. More complex control flow.

Go with **Approach A**. The async dispatch code checks `srs_manager.is_loaded()`,
and if not, acquires budget before entering the blocking context.

Similarly for PCE: `pce_cache.get()` returns immediately if cached. If not cached,
the first proof uses the slow synthesis path, and the background extraction thread
calls `pce_cache.insert()` which internally does budget acquisition (the background
thread can use `try_acquire` + sleep retry since it's not async).

### 5.5 SynthesizedJob Struct

**Add field** (engine.rs, ~line 703):

```rust
pub(crate) struct SynthesizedJob {
    // ... existing fields ...

    /// Memory budget reservation for this job's working set.
    /// Released in two phases: partial after prove_start (a/b/c freed),
    /// remainder after prove_finish (shell + aux freed).
    pub reservation: Option<crate::memory::MemoryReservation>,
}
```

### 5.6 GPU Worker Loop

**After `gpu_prove_start`** (engine.rs, ~line 2386):

```rust
let (pending, partition_index, gpu_start) = result?;

// a/b/c are freed inside prove_start — release that portion of the reservation
if let Some(ref reservation) = synth_job.reservation {
    reservation.release(proof_kind_abc_bytes(synth_job.circuit_id));
}
```

**After `gpu_prove_finish`** (in finalizer task, ~line 2403-2448):

```rust
let result = gpu_prove_finish(pending, partition_index, gpu_start)?;

// Drop reservation to release remaining budget (shell + aux)
drop(reservation);  // moved into finalizer task
```

The reservation must be moved from `SynthesizedJob` into the finalizer's
closure so it is dropped when the finalizer completes.

### 5.7 Monolithic Synthesis Path (lines ~1983-2168)

Same pattern. Before synthesis:
```rust
let reservation = budget.acquire(proof_kind_full_bytes(circuit_id)).await;
```
Attach to `SynthesizedJob`. GPU worker releases in two phases.

### 5.8 PCE Background Extraction

**Current** (`engine.rs:1399-1405`):
```rust
std::thread::spawn(move || {
    pipeline::extract_and_cache_pce_from_c1(&c1_data, sn, mid)
});
```

**New**: The extraction thread must acquire budget for the transient memory
(~14 GiB for circuit synthesis during recording) AND the final PCE size (~26 GiB).

```rust
std::thread::spawn(move || {
    // Try to acquire budget for transient + final
    // Use try_acquire + sleep loop since we're on a std::thread, not async
    let transient_bytes = PCE_EXTRACTION_TRANSIENT_BYTES;
    loop {
        if let Some(reservation) = budget.try_acquire(transient_bytes) {
            // Extract PCE (uses ~14 GiB transiently)
            match pipeline::extract_pce_circuit(&c1_data, sn, mid) {
                Ok(pce) => {
                    // Release transient reservation
                    drop(reservation);
                    // Insert into cache (acquires its own budget for final PCE size)
                    pce_cache.insert_blocking(circuit_id, pce);
                }
                Err(e) => {
                    warn!(error = %e, "PCE extraction failed");
                    // reservation dropped automatically
                }
            }
            break;
        }
        std::thread::sleep(Duration::from_secs(1));
    }
});
```

Note: `pce_cache.insert_blocking()` is a non-async variant that uses `try_acquire`
+ sleep internally.

---

## 6. Memory Estimation Constants

Define in `cuzk-core/src/memory.rs`:

```rust
pub const GIB: u64 = 1024 * 1024 * 1024;

// Per-partition estimates (PoRep 32G, 1 circuit)
pub const POREP_PARTITION_FULL_BYTES: u64 = 14 * GIB;          // ~13.6, rounded up
pub const POREP_PARTITION_ABC_BYTES: u64 = 13 * GIB;           // ~12.5, rounded up
pub const POREP_PARTITION_REST_BYTES: u64 = POREP_PARTITION_FULL_BYTES - POREP_PARTITION_ABC_BYTES;

// Full PoRep batch (all 10 partitions at once)
pub const POREP_BATCH_FULL_BYTES: u64 = 136 * GIB;
pub const POREP_BATCH_ABC_BYTES: u64 = 125 * GIB;

// WindowPoSt 32G (single partition, ~125M constraints)
pub const WPOST_FULL_BYTES: u64 = 14 * GIB;                   // ~13.2, rounded up
pub const WPOST_ABC_BYTES: u64 = 13 * GIB;

// WinningPoSt 32G (single partition, ~3.5M constraints)
pub const WINNING_FULL_BYTES: u64 = GIB / 2;                  // ~0.4 GiB
pub const WINNING_ABC_BYTES: u64 = GIB / 4;

// SnapDeals 32G (1 partition, ~81M constraints)
pub const SNAP_PARTITION_FULL_BYTES: u64 = 9 * GIB;           // ~8.6, rounded up
pub const SNAP_PARTITION_ABC_BYTES: u64 = 8 * GIB;

// PCE extraction transient (RecordingCS synthesis)
pub const PCE_EXTRACTION_TRANSIENT_BYTES: u64 = 14 * GIB;
```

Provide helper functions:

```rust
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
```

---

## 7. Files to Modify

| File | Changes |
|---|---|
| **`cuzk-core/src/memory.rs`** | **NEW** — `MemoryBudget`, `MemoryReservation`, `detect_system_memory()`, estimation constants and helpers |
| **`cuzk-core/src/lib.rs`** | Add `pub mod memory;` |
| **`cuzk-core/src/config.rs`** | Remove `SrsConfig::preload`, `MemoryConfig::pinned_budget`, `MemoryConfig::working_memory_budget`, `SynthesisConfig::partition_workers`. Add `MemoryConfig::total_budget`, `safety_margin`, `eviction_min_idle`. Add `parse_duration()`. Update `Default` impls and tests. |
| **`cuzk-core/src/srs_manager.rs`** | Replace `budget_bytes: u64` with `budget: Arc<MemoryBudget>`. Add `last_used: HashMap<CircuitId, Instant>`. Add `evictable_entries()`, budget-gated `ensure_loaded()`. Change `evict()` to call `budget.release_internal()`. Update `new()` signature. |
| **`cuzk-core/src/pipeline.rs`** | Remove 4 static `OnceLock<PreCompiledCircuit<Fr>>` (lines 293-305). Remove `get_pce()`, `get_pce_lock()`, `load_pce_from_disk()`, `preload_pce_from_disk()`. Add `PceCache` struct. Change `synthesize_auto()` to take `&PceCache` parameter. Change `extract_and_cache_pce()` to take `&PceCache`. Keep buffer flight counters for diagnostics. |
| **`cuzk-core/src/engine.rs`** | Remove SRS preload (lines 866-906). Remove PCE preload (lines 929-937). Remove `partition_semaphore` creation and all its uses. Create `MemoryBudget` and `PceCache` in `start()`. Wire evictor. Add `reservation` field to `SynthesizedJob`. Partition dispatch: replace `partition_sem.acquire()` with `budget.acquire()`. GPU worker: partial release after `prove_start`, drop reservation after `prove_finish`. Pass `PceCache` to synthesis and dispatch functions. Update channel sizing. Gate PCE background extraction with budget. |
| **`cuzk.example.toml`** | Update `[memory]` section. Remove `[srs] preload`. Remove `[synthesis] partition_workers`. Update documentation comments. |
| **`cuzk-core/Cargo.toml`** | May need `libc` dependency for `/proc/meminfo` reading (already present for `malloc_trim`). |
| **`cuzk-daemon/src/main.rs`** | Update if it references removed config fields. |
| **`cuzk-bench/src/main.rs`** | Update if it references `preload_pce_from_disk`, `partition_workers`, or SRS preload. |
| **`cuzk-server/src/service.rs`** | Update `PreloadSRS` / `EvictSRS` gRPC handlers to work with new budget-aware SrsManager. |

---

## 8. Acquire Loop Pseudocode

The core `budget.acquire()` method:

```rust
pub async fn acquire(self: &Arc<Self>, amount: u64) -> MemoryReservation {
    loop {
        // Attempt optimistic reservation
        let old = self.used_bytes.fetch_add(amount, Ordering::AcqRel);
        if old + amount <= self.total_bytes {
            // Success — budget has room
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
                continue;  // Retry — eviction freed some space
            }
        }
        drop(evictor);

        // Wait for a release notification, then retry
        self.notify.notified().await;
    }
}
```

And `release_internal`:

```rust
fn release_internal(&self, amount: u64) {
    self.used_bytes.fetch_sub(amount, Ordering::AcqRel);
    self.notify.notify_waiters();
}
```

---

## 9. Eviction Details

### 9.1 SrsManager::evictable_entries

```rust
pub fn evictable_entries(&self) -> Vec<(CircuitId, u64, Instant)> {
    let mut entries = Vec::new();
    for (cid, srs_arc) in &self.loaded {
        // Only evictable if nobody else holds a reference
        // (strong_count == 1 means only the cache holds it)
        if Arc::strong_count(srs_arc) == 1 {
            if let Some(&last_used) = self.last_used.get(cid) {
                let file_size = std::fs::metadata(
                    self.param_dir.join(circuit_id_to_param_filename(cid))
                ).map(|m| m.len()).unwrap_or(0);
                entries.push((cid.clone(), file_size, last_used));
            }
        }
    }
    entries
}
```

### 9.2 SrsManager::evict (budget-aware)

```rust
pub fn evict(&mut self, circuit_id: &CircuitId) -> u64 {
    if let Some(srs_arc) = self.loaded.get(circuit_id) {
        if Arc::strong_count(srs_arc) > 1 {
            return 0;  // In use by an in-flight proof — can't evict
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
        info!(circuit_id = %circuit_id, freed_gib = file_size / GIB, "SRS evicted");
        file_size
    } else {
        0
    }
}
```

### 9.3 PceCache::evict

```rust
pub fn evict(&self, circuit_id: &CircuitId) -> u64 {
    let mut entries = self.entries.lock().unwrap();
    if let Some(entry) = entries.get(circuit_id) {
        if Arc::strong_count(&entry.pce) > 1 {
            return 0;  // In use
        }
        let size = entry.size_bytes;
        entries.remove(circuit_id);
        self.budget.release_internal(size);
        info!(circuit_id = %circuit_id, freed_gib = size / GIB, "PCE evicted");
        size
    } else {
        0
    }
}
```

---

## 10. Logging

At startup:
```
memory budget: 251 GiB (256 total - 5 safety)
```

On SRS load:
```
SRS porep-32g loaded: 44 GiB, budget 207/251 GiB remaining
```

On PCE load:
```
PCE porep-32g loaded from disk: 26 GiB, budget 181/251 GiB remaining
```

On partition acquire:
```
partition[3] acquired 14 GiB, budget 167/251 GiB remaining (12 partitions possible)
```

On a/b/c release:
```
partition[3] released 13 GiB (a/b/c), budget 180/251 GiB remaining
```

On eviction:
```
evicting SRS snap-32g (idle 8m12s), freed 33 GiB, budget 214/251 GiB remaining
```

On budget wait:
```
partition[7] waiting for 14 GiB (budget 4/251 GiB available)
```

---

## 11. Testing Strategy

### Unit tests (`memory.rs`)

- `test_acquire_release`: acquire + drop releases budget
- `test_partial_release`: acquire, release half, drop releases remainder
- `test_acquire_blocks`: acquire more than budget → blocks; release from another task → unblocks
- `test_detect_system_memory`: verify `/proc/meminfo` parsing

### Unit tests (`srs_manager.rs`)

- `test_evictable_entries`: insert SRS, advance clock past 5 min, verify evictable
- `test_evict_in_use`: hold Arc clone, verify eviction returns 0
- `test_evict_releases_budget`: verify budget.used decreases after eviction

### Unit tests (`pipeline.rs` / `PceCache`)

- `test_pce_cache_insert_get`: insert, get, verify last_used updated
- `test_pce_cache_evict`: insert, advance clock, evict, verify get returns None
- `test_pce_cache_evict_in_use`: hold Arc clone, verify eviction blocked

### Integration tests

- `test_budget_limits_concurrency`: with small budget (e.g. 30 GiB), verify only 2 partitions
  can be acquired simultaneously (14 GiB each)
- `test_eviction_frees_for_partition`: load SRS (44 GiB), fill budget, verify partition
  acquire triggers SRS eviction after idle timeout
- `test_srs_reload_after_eviction`: evict SRS, submit proof, verify SRS reloads and proof succeeds

---

## 12. Migration Notes

### Backward Compatibility

Old config files with `preload`, `pinned_budget`, `working_memory_budget`, or
`partition_workers` should NOT cause parse errors. Use `#[serde(default)]` on all
fields and add `#[serde(flatten)]` or ignore unknown fields via
`#[serde(deny_unknown_fields)]` being absent (which is the current behavior —
serde ignores unknown keys by default with `Deserialize`).

The removed fields simply have no effect. Log a warning if they're present:

```rust
if config_str.contains("partition_workers") {
    warn!("partition_workers is deprecated — concurrency is now managed by the memory budget");
}
if config_str.contains("preload") {
    warn!("srs.preload is deprecated — SRS is loaded on demand");
}
```

### gRPC API

`PreloadSRS` RPC should still work — it calls `srs_manager.ensure_loaded()` which
now acquires budget. The response should include the budget impact.

`EvictSRS` RPC should still work — it calls `srs_manager.evict()` which now
releases budget.

`GetStatus` should report budget usage:
```
total_budget: 251 GiB
used_budget: 181 GiB
available_budget: 70 GiB
srs_loaded: [porep-32g (44 GiB, last used 30s ago)]
pce_loaded: [porep-32g (26 GiB, last used 30s ago)]
working_set: 111 GiB (8 partitions in flight)
```
