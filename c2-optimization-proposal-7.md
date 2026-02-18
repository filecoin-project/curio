# Proposal 7: Engine-Level Per-Partition Pipeline

**Goal**: Eliminate the thundering-herd synthesis pattern by dispatching individual
partitions as independent work units through the engine's synthesis→GPU pipeline.
Instead of synthesizing all 10 PoRep partitions simultaneously and handing them to the
GPU as a monolithic batch, a pool of synth workers processes partitions one at a time and
feeds them to the GPU as they complete. This enables natural cross-sector pipelining and
eliminates the b_g2_msm CPU bottleneck.

**Impact**:
- Steady-state throughput: 42.8s/proof → ~30s/proof (1.4x faster, GPU-limited)
- b_g2_msm per partition: 25s (batch, num_circuits=10) → 0.4s (num_circuits=1)
- GPU utilization: ~78% → ~95% (GPU never idles between sectors)
- Peak memory: ~272 GiB (20 settled partitions) → bounded by channel capacity (~55 GiB)
- Cross-sector GPU idle gap: 12-14s → ~0s (next sector's partitions arrive continuously)

**Scope**: Engine dispatch refactor (partition-level `SynthesizedJob`), GPU worker
partition-aware result routing, `ProofAssembler` integration in `JobTracker`, new config
knob for synth worker pool size.

---

## Table of Contents

- [Part A: Problem Analysis](#part-a-problem-analysis)
  - [A.1 The Thundering Herd](#a1-the-thundering-herd)
  - [A.2 Per-Partition Timing (Measured)](#a2-per-partition-timing-measured)
  - [A.3 The b_g2_msm Branching Behavior](#a3-the-b_g2_msm-branching-behavior)
  - [A.4 Why the Phase 6 Partitioned Pipeline Falls Short](#a4-why-the-phase-6-partitioned-pipeline-falls-short)
- [Part B: Architecture](#part-b-architecture)
  - [B.1 Core Idea: Synth Worker Pool](#b1-core-idea-synth-worker-pool)
  - [B.2 Pipeline Topology](#b2-pipeline-topology)
  - [B.3 Steady-State Timeline](#b3-steady-state-timeline)
  - [B.4 Data Structures](#b4-data-structures)
  - [B.5 Dispatch: process_batch()](#b5-dispatch-process_batch)
  - [B.6 GPU Worker: Partition-Aware Routing](#b6-gpu-worker-partition-aware-routing)
  - [B.7 Error Handling](#b7-error-handling)
  - [B.8 Memory Model](#b8-memory-model)
  - [B.9 Thread Pool Interactions](#b9-thread-pool-interactions)
  - [B.10 Configuration](#b10-configuration)
  - [B.11 Non-PoRep Circuit Types](#b11-non-porep-circuit-types)
- [Part C: Implementation Plan](#part-c-implementation-plan)
  - [C.1 Phase Ordering](#c1-phase-ordering)
  - [C.2 Files Changed](#c2-files-changed)
  - [C.3 Risk Assessment](#c3-risk-assessment)
  - [C.4 Compatibility](#c4-compatibility)
  - [C.5 Testing Strategy](#c5-testing-strategy)
- [Appendix: Timing Derivations](#appendix-timing-derivations)

---

## Part A: Problem Analysis

### A.1 The Thundering Herd

The current PoRep C2 proving pipeline has two modes, both exhibiting a thundering-herd
pattern where all 10 partition syntheses start and finish simultaneously:

**Standard pipeline (slot_size=0, `synthesize_porep_c2_batch`):**

All 10 circuits are synthesized via `rayon::par_iter`, which fans out 10 tasks — one per
partition. Each partition's synthesis is mostly sequential (single-threaded Poseidon/SHA
witness computation). The 10 threads finish within milliseconds of each other at ~39s,
then all 10 are handed to the GPU as a single batch.

```
Synth: [P0|P1|P2|P3|P4|P5|P6|P7|P8|P9]  all 10 parallel, ~39s wall
                                          ↓ all submit at once
GPU:                                      [═══════ 10 circuits (27s, b_g2_msm=25s) ═══════]
       ← 39s GPU idle →                  ← 27s ─────────────────────────────────────────→
```

**Partitioned pipeline (slot_size>0, `prove_porep_c2_partitioned`):**

10 `std::thread::scope` threads are spawned immediately. Each calls `synthesize_partition`
(1 circuit). They all start at t=0 and finish at ~29-36s. A bounded `sync_channel` feeds
them to a single GPU consumer thread. Despite per-partition GPU calls (3s each), the GPU
cannot start until the first synthesis completes at ~29s.

```
Synth: [P0|P1|P2|P3|P4|P5|P6|P7|P8|P9]  all 10 parallel, ~29-36s wall
                                          ↓ all arrive in channel around the same time
GPU:                                      [P0][P1][P2][P3]...[P9]  sequential, 3s each
       ← 29s GPU idle →                  ← 30s ──────────────────→
```

Both modes waste ~29-39s of GPU idle time per sector, because synthesis is bursty.

Critically, the partitioned pipeline (Phase 6) runs **self-contained** inside a
`std::thread::scope` block. It does not participate in the engine-level synthesis→GPU
channel. This means:
- No cross-sector overlap: Sector B cannot start until Sector A completes entirely
- The engine's `synthesis_concurrency` semaphore has no effect on partition dispatch
- Measured result: **66-72s/proof** (worse than the 42.8s batch-all with parallel synth)

### A.2 Per-Partition Timing (Measured)

All measurements on AMD Threadripper PRO 7995WX (96C/192T), RTX 5070 Ti, 754 GiB RAM.

**Single partition synthesis breakdown** (from partitioned pipeline logs):

| Phase | Time | Notes |
|---|---|---|
| Witness generation (WitnessCS) | **25-27s** | Truly sequential — 1 thread walks Poseidon merkle tree, computing ~131M Fr elements |
| SpMV evaluation (PCE) | **4-10s** | Uses rayon (`par_chunks_mut`). 4s when alone, 10s when 10 partitions contend for rayon |
| Total per partition | **29-36s** | Depends on rayon contention in SpMV phase |

**Key observation**: The ~39s batch-all wall time is NOT 10 × 4s parallel. It is 10
sequential witness-gen threads (each ~25-27s) running concurrently on 10 OS threads,
plus a contended SpMV phase (~9-10s) where all 10 fight for rayon. The witness-gen phase
dominates and has **zero internal parallelism** per partition.

**GPU per partition** (num_circuits=1):

| Phase | Time | Notes |
|---|---|---|
| Preprocessing + NTT + MSM (G1) | **2.6s** | CUDA kernels, batch_addition, tail MSMs |
| b_g2_msm | **0.4s** | `num_circuits=1` → full CPU thread pool → multi-threaded Pippenger |
| Total GPU per partition | **~3s** | |

Compare to batch-all GPU (num_circuits=10): **27s** total, of which **25s** is b_g2_msm
running 10 single-threaded Pippengers in parallel (each ~2.5s, but the `groth16_pool`
only has ~96 threads so 10 tasks take ~2.5s × 10/96 × overhead ≈ 25s).

### A.3 The b_g2_msm Branching Behavior

The b_g2_msm cost depends critically on `num_circuits` passed to the C++ kernel
(`groth16_cuda.cu:541-560`):

```cpp
if (num_circuits > 1) {
    // N single-threaded Pippengers in parallel
    get_groth16_pool().par_map(num_circuits, [&](size_t c) {
        mult_pippenger<bucket_fp2_t>(..., nullptr);  // single-threaded
    });
} else {
    // ONE Pippenger with full thread pool
    mult_pippenger<bucket_fp2_t>(..., &get_groth16_pool());  // multi-threaded
}
```

| num_circuits | b_g2_msm time | Why |
|---|---|---|
| 1 | **0.4s** | Single multi-threaded Pippenger using all ~96 CPU cores |
| 10 | **25s** | 10 single-threaded Pippengers in parallel (each ~2.5s, serialized by pool) |

By proving one partition at a time (`num_circuits=1`), b_g2_msm drops from 25s to 0.4s —
a **62x speedup** on this single step, reducing total GPU time from 27s to ~3s per
partition.

### A.4 Why the Phase 6 Partitioned Pipeline Falls Short

The Phase 6 `prove_porep_c2_partitioned()` function (pipeline.rs:1757) achieves per-partition
GPU calls (num_circuits=1, fast b_g2_msm), but its self-contained `std::thread::scope`
design prevents cross-sector pipelining:

| Property | Phase 6 (self-contained) | Phase 7 (engine-integrated) |
|---|---|---|
| Per-partition GPU calls | Yes (num_circuits=1) | Yes (num_circuits=1) |
| Cross-sector overlap | **No** — runs in isolated scope | **Yes** — shares engine channel |
| GPU idle between sectors | ~29s (waiting for next sector's synth) | ~0s (next sector already in pipeline) |
| Memory control | `sync_channel(max_concurrent)` | Engine channel capacity + semaphore |
| Multi-GPU support | No (single GPU consumer in scope) | Yes (engine GPU workers) |
| Interleaves with other proof types | No | Yes |

The Phase 6 pipeline pays the full ~29s synthesis latency at the start of every sector,
with the GPU idle the entire time. Measured: **66-72s/proof** (worse than 42.8s batch-all
with parallel synth overlap).

---

## Part B: Architecture

### B.1 Core Idea: Synth Worker Pool

Replace the current "synthesize all partitions → send one job to GPU" model with a
**pool of synth workers** that each process one partition at a time and submit to the
engine's GPU channel individually.

The pool has a fixed number of workers (configurable, default 20). Each worker:
1. Takes a `(Arc<ParsedC1Output>, partition_idx, job_id)` work item from a shared queue
2. Calls `synthesize_partition()` (~29s, mostly single-threaded)
3. Wraps the result in a `SynthesizedJob` with `partition_index = Some(k)`
4. Sends it to the engine's `synth_tx` channel
5. **Blocks** if the channel is full (backpressure limits settled partitions in memory)
6. Loops back to step 1 for the next work item

The engine's existing GPU worker receives individual partition jobs, proves each
(num_circuits=1, ~3s), and routes results to a per-request `ProofAssembler`.

### B.2 Pipeline Topology

```
                          ┌─────────────┐
Proof Request ──parse──→  │ Work Queue   │
(C1 JSON)       once      │ (partition   │
                          │  items)      │
                          └──┬──┬──┬──┬──┘
                             │  │  │  │
                   ┌─────────┘  │  │  └─────────┐
                   ▼            ▼  ▼             ▼
              ┌─────────┐ ┌─────────┐  ... ┌─────────┐
              │ Worker 1 │ │ Worker 2 │      │Worker 20│  ← synth worker pool
              │ synth_   │ │ synth_   │      │ synth_  │     (tokio::spawn_blocking)
              │ partition │ │ partition │      │partition│
              └────┬─────┘ └────┬─────┘      └────┬────┘
                   │            │                  │
                   ▼            ▼                  ▼
              ┌────────────────────────────────────────┐
              │        synth_tx / synth_rx              │  ← engine GPU channel
              │    (capacity: 2-3 partition jobs)       │     (tokio::sync::mpsc)
              └───────────────────┬────────────────────┘
                                  │
                                  ▼
                          ┌───────────────┐
                          │  GPU Worker    │  ← existing engine GPU worker
                          │  gpu_prove()   │     (num_circuits=1, ~3s/partition)
                          │  b_g2_msm=0.4s │
                          └───────┬───────┘
                                  │
                          ┌───────▼───────┐
                          │ ProofAssembler │  ← per job_id, in JobTracker
                          │ insert(k, pf) │
                          │ is_complete()? │
                          └───────┬───────┘
                                  │ all 10 done
                                  ▼
                          ┌───────────────┐
                          │ Final Proof    │  → oneshot::send → gRPC caller
                          │ (1920 bytes)   │
                          └───────────────┘
```

### B.3 Steady-State Timeline

With 20 synth workers and a continuous queue of sectors:

```
Sector A (10 partitions):
  Workers 1-10:  [═══ A.P0 synth 29s ═══][═══ C.P0 synth 29s ═══]...
                 [═══ A.P1 synth 29s ═══][═══ C.P1 synth 29s ═══]...
                 ...
                 [═══ A.P9 synth 29s ═══][═══ C.P9 synth 29s ═══]...

Sector B (starts immediately, workers 11-20):
  Workers 11-20: [═══ B.P0 synth 29s ═══][═══ D.P0 synth 29s ═══]...
                 [═══ B.P1 synth 29s ═══][═══ D.P1 synth 29s ═══]...
                 ...
                 [═══ B.P9 synth 29s ═══][═══ D.P9 synth 29s ═══]...

GPU (single):
  t=0                    t=29                   t=59                   t=89
  |← idle (warming) ──→ |[A.P0][A.P1]...[A.P9]|[B.P0][B.P1]...[B.P9]|[C.P0]...
                         ← 30s (10 × 3s) ─────→← 30s ───────────────→
```

**Steady-state**: One sector completes every **30s** (10 partitions × 3s GPU each).

The 20 workers collectively produce partitions at ~0.69/s (20 workers / 29s each). The
GPU consumes at ~0.33/s (1 partition / 3s). So the GPU is the bottleneck and workers
often block on the channel. This is the desired behavior — the GPU is **100% utilized**
and workers absorb the slack.

**Cross-sector gap**: When Sector A's last GPU partition (A.P9) completes at ~t=59s,
Sector B's first partition (B.P0) is already waiting in the channel (it was synthesized
at ~t=29s by Worker 11). **Zero GPU idle between sectors.**

### B.4 Data Structures

**Extended `SynthesizedJob`** (engine.rs):

```rust
pub(crate) struct SynthesizedJob {
    // ── Existing fields (unchanged) ──
    pub request: ProofRequest,
    pub synth: SynthesizedProof,        // num_circuits=1 for partitions
    #[cfg(feature = "cuda-supraseal")]
    pub params: Arc<SuprasealParameters<Bls12>>,
    pub circuit_id: CircuitId,
    pub batch_requests: Vec<ProofRequest>,
    pub sector_boundaries: Vec<usize>,

    // ── New fields for partitioned dispatch ──
    /// Partition index within the sector (0-9 for PoRep 32G).
    /// None = monolithic job (legacy, non-partitioned).
    pub partition_index: Option<usize>,
    /// Total partitions for this proof (10 for PoRep 32G).
    pub total_partitions: Option<usize>,
    /// The original job_id that this partition belongs to.
    /// Used to route GPU results to the correct ProofAssembler.
    pub parent_job_id: Option<JobId>,
}
```

**`PartitionedJobState`** (new, in engine.rs):

```rust
struct PartitionedJobState {
    assembler: ProofAssembler,         // from pipeline.rs (already exists)
    request: ProofRequest,             // original request for metadata
    proof_kind: ProofKind,
    total_synth_duration: Duration,    // accumulated across partitions
    total_gpu_duration: Duration,
    start_time: Instant,               // when dispatch began
}
```

**Extended `JobTracker`** (engine.rs):

```rust
struct JobTracker {
    // ── Existing fields (unchanged) ──
    pending: HashMap<JobId, Vec<oneshot::Sender<JobStatus>>>,
    completed: HashMap<JobId, JobStatus>,
    // ...

    // ── New field ──
    /// Per-job partition assemblers for in-progress partitioned proofs.
    assemblers: HashMap<JobId, PartitionedJobState>,
}
```

**Shared work queue item** (new):

```rust
struct PartitionWorkItem {
    parsed: Arc<ParsedC1Output>,
    partition_idx: usize,
    job_id: JobId,
    request: ProofRequest,              // lightweight clone for metadata
    params: Arc<SuprasealParameters<Bls12>>,
    circuit_id: CircuitId,
}
```

### B.5 Dispatch: process_batch()

When a PoRep C2 single-sector request arrives, `process_batch()` replaces the current
`slot_size > 0` block (engine.rs:591-693) with partition-level dispatch:

```rust
// In process_batch(), for PoRepSealCommit single-sector:

// 1. Parse C1 once (blocking)
let parsed = Arc::new(parse_c1_output(&request.vanilla_proof)?);
let num_partitions = parsed.num_partitions;  // 10

// 2. Load SRS once
let params = srs_manager.ensure_loaded(&CircuitId::Porep32G)?;

// 3. Register ProofAssembler in JobTracker
{
    let mut tracker = tracker.lock().await;
    tracker.assemblers.insert(job_id.clone(), PartitionedJobState {
        assembler: ProofAssembler::new(num_partitions),
        request: request.clone(),
        proof_kind: ProofKind::PoRepSealCommit,
        total_synth_duration: Duration::ZERO,
        total_gpu_duration: Duration::ZERO,
        start_time: Instant::now(),
    });
}

// 4. Dispatch each partition to the synth worker pool
for partition_idx in 0..num_partitions {
    let permit = synth_semaphore.clone().acquire_owned().await?;
    let item = PartitionWorkItem { parsed, partition_idx, job_id, ... };
    let synth_tx = synth_tx.clone();

    tokio::task::spawn_blocking(move || {
        let _permit = permit;  // held until synth + send complete

        // Synthesize one partition (~29s, mostly single-threaded)
        let synth = synthesize_partition(&item.parsed, item.partition_idx, &job_id)?;
        let synth_duration = synth.synthesis_duration;

        // Submit to engine GPU channel (blocks if full → backpressure)
        let job = SynthesizedJob {
            synth,
            partition_index: Some(item.partition_idx),
            total_partitions: Some(num_partitions),
            parent_job_id: Some(item.job_id.clone()),
            params: item.params,
            circuit_id: item.circuit_id,
            ..
        };

        synth_tx.blocking_send(job)?;
        Ok(())
    });
}

// process_batch() returns immediately. Completion is signaled asynchronously
// by the GPU worker when the ProofAssembler collects all 10 partitions.
```

**Key**: `process_batch()` becomes non-blocking for partitioned proofs. It dispatches
10 spawn_blocking tasks (each gated by the semaphore) and returns. The caller's
`oneshot::Receiver` is already registered in `tracker.pending[job_id]` and will fire
when the last partition's GPU proof completes.

### B.6 GPU Worker: Partition-Aware Routing

The existing GPU worker loop (engine.rs: `run_gpu_worker`) needs a branch after
`gpu_prove()` returns:

```rust
// After gpu_prove() returns Ok(gpu_result):

if let Some(parent_id) = &synth_job.parent_job_id {
    // ── Partitioned proof: route to assembler ──
    let partition_idx = synth_job.partition_index.unwrap();
    let mut tracker = tracker.lock().await;

    if let Some(state) = tracker.assemblers.get_mut(parent_id) {
        state.assembler.insert(partition_idx, gpu_result.proof_bytes);
        state.total_gpu_duration += gpu_result.gpu_duration;
        state.total_synth_duration += synth_job.synth.synthesis_duration;

        // Release memory after each partition
        #[cfg(target_os = "linux")]
        unsafe { libc::malloc_trim(0); }

        if state.assembler.is_complete() {
            // All partitions done — assemble and deliver
            let state = tracker.assemblers.remove(parent_id).unwrap();
            let final_proof = state.assembler.assemble();

            let timings = ProofTimings {
                synthesis: state.total_synth_duration,
                gpu_compute: state.total_gpu_duration,
                total: state.start_time.elapsed(),
                ..
            };

            let status = JobStatus::Completed(ProofResult {
                job_id: parent_id.clone(),
                proof_bytes: final_proof,
                timings,
                ..
            });

            // Notify all waiting callers
            if let Some(senders) = tracker.pending.remove(parent_id) {
                for sender in senders {
                    let _ = sender.send(status.clone());
                }
            }
            tracker.completed.insert(parent_id.clone(), status);
        }
    }
} else {
    // ── Monolithic proof (existing path, unchanged) ──
    // WinningPoSt, WindowPoSt, SnapDeals, batch-all PoRep fallback
    deliver_result_to_callers(tracker, job_id, gpu_result);
}
```

**Partition ordering**: The GPU worker processes whatever job is next in the channel.
Partitions from different sectors can interleave freely:

```
GPU: [A.P0][B.P0][A.P1][B.P1][A.P2]...
```

The `ProofAssembler` accepts partitions in any order (indexed by `partition_idx`). The
final proof is assembled in partition-index order when all partitions are present.

### B.7 Error Handling

If a partition synthesis fails:
1. The `spawn_blocking` task returns an error
2. The engine marks the assembler as failed: `tracker.assemblers[job_id].failed = true`
3. All waiting callers are notified with `JobStatus::Failed`
4. Subsequent partition synthesis tasks for the same `job_id` check `assembler.failed`
   before starting synthesis and skip if true (wasting no CPU time)
5. GPU worker checks `assembler.failed` before inserting a proof; drops the result if
   the job is already failed

A `failed: bool` field is added to `PartitionedJobState`.

If a GPU prove fails for a partition:
1. Same as above — mark assembler failed, notify callers
2. Other partitions' GPU results for this job are discarded

### B.8 Memory Model

**Per-partition memory** (from source analysis):

| State | Per-Partition GiB | Notes |
|---|---|---|
| During synthesis (peak) | **~19.4** | WitnessCS vectors + SpMV result + witness concat |
| Settled (SynthesizedProof) | **~13.6** | a/b/c evaluations + density + aux_assignment |
| On GPU (being proved) | **~13.6** | Same as settled, consumed/freed after prove |

**Static memory** (shared, does not scale with concurrency):

| Component | GiB |
|---|---|
| PCE (PoRep 32G) | 25.7 |
| SRS (PoRep 32G, CUDA pinned) | 44.0 |
| OS / kernel / other | ~20 |
| **Total fixed** | **~90** |

**Live memory formula**:

```
peak_memory = fixed
            + (num_synth_workers) × 19.4 GiB    (worst case: all in peak synthesis)
            + (channel_capacity + 1) × 13.6 GiB  (settled in channel + on GPU)
```

**Budget for 754 GiB machine** (664 GiB available after fixed):

| Workers | Channel | Peak GiB | Fits? |
|---|---|---|---|
| 10 | 2 | 194 + 41 = 235 | Yes (429 GiB free) |
| 15 | 2 | 291 + 41 = 332 | Yes (332 GiB free) |
| 20 | 2 | 388 + 41 = 429 | Yes (235 GiB free) |
| 25 | 2 | 485 + 41 = 526 | Yes (138 GiB free) |
| 30 | 2 | 582 + 41 = 623 | Tight (41 GiB free) |

**Recommended default**: 20 workers with channel capacity 2. Peak ~429 GiB against 664
GiB available, leaving 235 GiB headroom for malloc fragmentation, page cache, and other
processes.

**Note**: The 19.4 GiB peak is transient (~4s during SpMV). Most of the ~29s synthesis
time is witness-gen where peak memory is only ~4 GiB per worker. The realistic steady-state
memory is significantly lower than the worst-case formula.

**Backpressure**: When the channel is full (2 settled partitions buffered + 1 on GPU),
workers block on `synth_tx.blocking_send()`. This naturally limits how many settled
SynthesizedProofs exist in memory, regardless of the worker pool size. Workers that are
blocked on send still hold their synthesis output in memory (~13.6 GiB each), so the
effective peak depends on how many workers reach the send point before the GPU drains
the channel. With 29s synthesis and 3s GPU, at most ~1-2 workers will be blocked at any
time (the GPU is fast enough to drain faster than new partitions arrive).

### B.9 Thread Pool Interactions

Three thread pools are active during proving:

| Pool | Type | Threads | Used By |
|---|---|---|---|
| Rayon global | Rust (rayon) | 192 (default) | SpMV `par_chunks_mut`, bellperson `par_iter` |
| groth16_pool | C++ (sppark) | 192 (or `CUZK_GPU_THREADS`) | b_g2_msm, preprocessing |
| Synth worker pool | tokio spawn_blocking | 20 (configurable) | One `synthesize_partition` per worker |

**Contention analysis**:

- **Witness gen** (25-27s per partition): Runs on the spawn_blocking thread. Single-threaded
  per partition. Zero rayon usage. With 20 workers, 20 OS threads run independently — the
  96-core machine can handle this with no contention (each thread gets its own core).

- **SpMV** (4-10s per partition): Uses rayon `par_chunks_mut` + `rayon::join`. At any
  instant, with 20 workers, statistically ~3-4 will be in the SpMV phase (4s out of 29s
  = ~14% of time). Each SpMV submits ~48K rayon tasks for 3 matrices. With 3-4 concurrent
  SpMV evaluations sharing 192 rayon threads, each gets ~50-60 threads — more than the
  ~19 threads each gets when 10 partitions compete simultaneously in the current batch
  model. **SpMV per partition should be faster, not slower.**

- **b_g2_msm** (0.4s per partition): Runs on the C++ groth16_pool during GPU prove. With
  `num_circuits=1`, the full pool is used. This happens on the GPU worker's
  `spawn_blocking` thread and does NOT interfere with synthesis workers.

- **Thread isolation config** (Phase 6b): `synthesis.threads` and `gpus.gpu_threads`
  remain useful. If `gpu_threads=32`, the groth16_pool gets 32 threads for the 0.4s
  b_g2_msm, leaving 160 threads for synthesis workers. But since b_g2_msm is only 0.4s
  per 3s GPU call (13% of GPU time), the contention window is small even without isolation.

### B.10 Configuration

New and modified config parameters:

```toml
[synthesis]
# CPU threads for circuit synthesis (rayon global pool). 0 = auto (all CPUs).
threads = 0

# Number of concurrent partition synthesis workers.
# Each worker processes one partition at a time (~29s each).
# With 20 workers and 10 partitions per sector, 2 sectors can be
# synthesized simultaneously, keeping the GPU continuously fed.
#
# Recommended: 20 (fits in 754 GiB with ~235 GiB headroom).
# Minimum for full GPU utilization: ceil(synth_time / gpu_time) × partitions
#   = ceil(29/3) × 1 = 10 (one sector in flight)
#   = 20 (two sectors in flight, zero cross-sector GPU idle)
#
# Set lower on machines with less RAM:
#   512 GiB → 15 workers (~332 GiB peak)
#   256 GiB → 5 workers (~100 GiB peak, GPU will idle)
partition_workers = 20

[pipeline]
enabled = true

# Channel capacity for synthesized partitions waiting for GPU.
# Higher = more buffering tolerance for synthesis timing variance.
# Lower = less memory for settled partitions.
# Recommended: 2-3.
synthesis_lookahead = 2
```

The `slot_size` parameter (Phase 6) is **repurposed**: `slot_size = 0` now means
"use engine-level per-partition dispatch" (the new default). The old Phase 6
self-contained pipeline is retained as a fallback at `slot_size > 0` for testing or
machines where the engine-integrated path has issues.

### B.11 Non-PoRep Circuit Types

| Circuit Type | Partitions | Per-Partition Dispatch? | Notes |
|---|---|---|---|
| PoRep 32G | 10 | **Yes** | Primary target |
| PoRep 64G | 10 | **Yes** | Same structure |
| SnapDeals 32G | 16 | **Yes** | Even more benefit (16 partitions) |
| WinningPoSt | 1 | No | Single partition, use existing monolithic path |
| WindowPoSt | 1 | No | Single partition, use existing monolithic path |

SnapDeals benefits more than PoRep because it has 16 partitions. The per-partition
dispatch reduces SnapDeals GPU time from batch-all (with b_g2_msm contention) to
16 × ~3s = 48s GPU, fully pipelined with synthesis.

---

## Part C: Implementation Plan

### C.1 Phase Ordering

1. **Data structure changes** — 0.5 session
   - Add `partition_index`, `total_partitions`, `parent_job_id` to `SynthesizedJob`
   - Add `PartitionedJobState` struct with `ProofAssembler`
   - Add `assemblers` map to `JobTracker`
   - Make `parse_c1_output()` and `ParsedC1Output` public and `Arc`-safe
   - Add `PartitionWorkItem` type
   - Add `partition_workers` to `SynthesisConfig`

2. **Dispatch refactor** — 1 session
   - Add synth worker semaphore (`Arc<Semaphore>` with `partition_workers` permits)
   - Refactor `process_batch()` PoRep C2 path: parse once, register assembler,
     dispatch 10 `spawn_blocking` tasks gated by semaphore
   - Each task: `synthesize_partition()` → pack `SynthesizedJob` → `synth_tx.blocking_send()`
   - Handle `process_batch()` returning before proof completes (non-blocking dispatch)

3. **GPU worker routing** — 1 session
   - Add partition-aware branch after `gpu_prove()`
   - Route partition proofs to `ProofAssembler` via `tracker.assemblers`
   - Deliver final proof when `is_complete()`, clean up assembler
   - Add `malloc_trim(0)` after each partition GPU prove

4. **Error handling** — 0.5 session
   - Add `failed: bool` to `PartitionedJobState`
   - Synthesis failure → mark assembler failed → notify callers
   - GPU failure → mark assembler failed → notify callers
   - Skip work for already-failed jobs

5. **Benchmarking** — 1 session
   - Single-sector latency: partition dispatch vs batch-all vs Phase 6 partitioned
   - Multi-sector throughput: queue 5 proofs, measure steady-state
   - Memory monitoring: RSS at each phase with 20 workers
   - Compare with waterfall timeline instrumentation

6. **SnapDeals support** — 0.5 session
   - Apply same dispatch pattern to 16-partition SnapDeals
   - Verify with SnapDeals test data

### C.2 Files Changed

| File | Changes | ~Lines |
|---|---|---|
| `engine.rs` — `SynthesizedJob` | Add 3 new fields | +10 |
| `engine.rs` — `PartitionedJobState` | New struct definition | +15 |
| `engine.rs` — `JobTracker` | Add `assemblers` field | +5 |
| `engine.rs` — `process_batch()` | Replace slot_size>0 block with partition dispatch | -100, +80 |
| `engine.rs` — GPU worker result routing | Add partition-aware branch | +60 |
| `engine.rs` — Error handling | Partition failure propagation | +30 |
| `pipeline.rs` — `parse_c1_output` | Make `pub`, ensure `ParsedC1Output` is `Send + Sync` | +5 |
| `pipeline.rs` — `synthesize_partition` | Already exists, no change | 0 |
| `pipeline.rs` — `ProofAssembler` | Already exists, already `pub` | 0 |
| `config.rs` — `SynthesisConfig` | Add `partition_workers: u32` | +8 |
| `cuzk.example.toml` | Document `partition_workers` | +15 |
| **Total** | | **~110 net new** |

### C.3 Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Per-partition GPU overhead is higher than measured | Low | Throughput regression | Data shows ~3s/partition consistently; fallback to batch-all |
| SpMV contention with 20 workers degrades synthesis time | Low-Medium | Slower synthesis | Monitor SpMV timing; adjust `partition_workers` down if needed |
| `ParsedC1Output` lifetime issues with `Arc` | Low | Build failure | Types already store owned `Vec`s; `Arc` wrapping is straightforward |
| `ProofAssembler` race condition with multi-GPU | Low | Incorrect proof | Assembler is behind `Mutex` in `JobTracker`; insert is atomic |
| Worker starvation: all workers on same sector, next sector delayed | Medium | Sub-optimal pipelining | Workers take from global queue; sectors interleave naturally |
| Memory fragmentation from many small allocs over time | Medium | RSS creep | `malloc_trim(0)` after each partition; monitor RSS long-term |
| `tokio::task::spawn_blocking` pool exhaustion with 20+ tasks | Low | Deadlock | tokio default blocking pool is 512 threads; 20 is well within limits |

### C.4 Compatibility

The per-partition dispatch is **backward compatible**:

- Non-partitioned proof types (WinningPoSt, WindowPoSt) continue using the existing
  monolithic path — the `partition_index: None` field signals the GPU worker to use the
  legacy result delivery.
- Setting `partition_workers = 0` could disable per-partition dispatch and fall back to
  the batch-all behavior (or slot_size pipeline).
- The `synthesis_concurrency` parameter becomes less relevant — it controlled sector-level
  overlap, which is now superseded by the worker pool. It can be kept at 1 (default) for
  non-PoRep types.
- All existing gRPC API contracts are unchanged. The caller submits a proof request and
  gets back the final assembled proof — the partitioned dispatch is invisible.
- All existing bench subcommands continue to work without modification.

### C.5 Testing Strategy

1. **Correctness**: Run per-partition dispatch for 3 sectors, verify each proof passes
   `groth16::verify`. Compare proof output format (1920 bytes = 10 × 192) against
   batch-all output (proofs are randomized by r/s, so verify rather than byte-compare).

2. **Single-sector latency**: Measure first-proof latency (includes 29s synth startup).
   Expected: ~33-35s (29s first partition synth + 10 × 3s GPU, but GPU starts before
   last partition is synthesized because all 10 workers run simultaneously for the
   first sector).

3. **Steady-state throughput**: Queue 5 sectors, measure average s/proof for proofs 2-5.
   Expected: ~30s/proof. Compare against batch-all (46.1s) and parallel-synth (42.8s).

4. **Memory**: Run `cuzk-memmon.sh` during 5-proof run with `partition_workers=20`.
   Verify peak RSS matches prediction (~430 GiB + fixed ~90 GiB = ~520 GiB).

5. **Waterfall timeline**: Use the TIMELINE instrumentation (Phase 6a) to visualize
   per-partition GPU scheduling. Verify zero GPU idle gaps between sectors.

6. **Error injection**: Force one partition synthesis to fail (e.g., corrupt vanilla proof
   for partition 3). Verify the job fails cleanly, other partitions' work is discarded,
   and the gRPC caller gets an error response.

7. **Multi-GPU** (if available): With 2 GPUs, verify partitions from different sectors
   can be proved on different GPUs simultaneously. The `ProofAssembler` should handle
   out-of-order partition completion from different workers.

---

## Appendix: Timing Derivations

All numbers from RTX 5070 Ti + Threadripper 7995WX measurements.

### Single-sector latency (per-partition dispatch)

All 10 partition syntheses start simultaneously (20 workers, 10 used for one sector).
Witness gen is sequential per partition: all 10 finish at ~t=29s.

GPU starts as soon as first partition arrives at t=29s. GPU processes 10 partitions
sequentially at 3s each: t=29s to t=59s.

```
First-sector latency = synth_first_partition + 10 × gpu_per_partition
                     = 29s + 30s = 59s
```

This is worse than batch-all (39 + 27 = 66s) only for the very first sector with no
overlap. In practice, the per-partition path is competitive for single sectors because
the batch-all path has 25s of b_g2_msm overhead baked into the 27s GPU time.

Actual batch-all: 39 + 27 = **66s** (measured 64-67s).
Per-partition: 29 + 30 = **59s** (estimated, needs benchmark).

### Steady-state throughput (continuous queue)

With N workers ≥ 10 (enough for at least one sector at a time):

```
steady_state_period = max(synth_wall_per_sector, gpu_total_per_sector)
                    = max(29s, 30s)
                    = 30s/sector
```

With N workers ≥ 20 (two sectors overlapping):
- Sector A completes synth at t=29s, GPU runs t=29s-59s
- Sector B starts synth at t=0s (on workers 11-20), completes at t=29s
- Sector B GPU starts at t=59s (immediately after A), runs to t=89s
- **Period = 30s/sector**

The steady-state throughput is **GPU-limited at 30s/proof**, regardless of the number
of workers (as long as ≥10 to keep the GPU fed). More workers provide smoother pipeline
fill and cross-sector overlap, not higher peak throughput.

### GPU utilization

```
Single sector:  gpu_active / total = 30s / 59s = 50.8%
Steady state:   gpu_active / period = 30s / 30s = 100%
```

Compare to current best (batch-all, parallel synth):
```
Measured: 42.8s/proof, GPU utilization ~77-82%
```

### Comparison table

| Metric | Batch-all (current) | Parallel synth (c=2,j=3) | Phase 6 partitioned | **Phase 7 (this proposal)** |
|---|---|---|---|---|
| s/proof (single) | 66s | 64s | 66-72s | **~59s** |
| s/proof (steady) | 46.1s | 42.8s | 66-72s | **~30s** |
| GPU utilization (steady) | 70.9% | 77-82% | ~50% | **~100%** |
| b_g2_msm / partition | 25s total | 25s total | 0.4s | **0.4s** |
| Peak memory (1 sector) | 136 GiB | 272 GiB | 55 GiB | **~430 GiB** (20 workers) |
| Cross-sector GPU idle | 12-14s | 8-10s | 29s | **~0s** |
