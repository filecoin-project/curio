# Phase 11: Memory-Bandwidth-Aware Pipeline Scheduling

## Problem Statement

Phase 9 achieved 32.1s/proof in isolation but degrades to 38.0s/proof at high
concurrency (c=20 j=15). Waterfall analysis shows GPU per-partition time inflates
from 4.9s to 7.5s under load, prep_msm from 1.7s to 2.7s+, and synthesis from 35s
to 54s. The system shifted from GPU-bound (Phase 8) to **CPU memory subsystem-bound**
(Phase 9).

### Phase 10 Post-Mortem

Phase 10 attempted to split the GPU mutex into `compute_mtx` + `mem_mtx` so 3 workers
could overlap CPU work with GPU kernels. It was **abandoned** because:

1. **VRAM too small**: The RTX 5070 Ti has 16 GB. Pre-staging requires ~12 GB per worker.
   When Worker A pre-stages under `mem_mtx` and Worker B enters `compute_mtx` first,
   Worker A's 12 GB of pre-staged buffers cause OOM for Worker B's kernel allocations.
2. **CUDA memory APIs are device-global**: `cudaDeviceSynchronize`, `cudaMemPoolTrimTo`,
   and `cudaMemGetInfo` all block until ALL streams complete, serializing with any
   worker's running kernels. This defeats the purpose of separate locks.
3. **Phase 9 already hides b_g2_msm**: The single lock is released before
   `prep_msm_thread.join()`, so b_g2_msm (~0.5s) already overlaps with the next
   worker's GPU kernels. There is no additional CPU time to hide.

### Root Cause Analysis

The throughput gap (32.1s isolation → 38.0s at c=20 j=15) comes from three sources,
identified through waterfall timing and system profiling:

**1. TLB shootdowns from unbounded async_dealloc threads**

After each partition, two detached background threads free large memory regions:
- C++ side (`groth16_cuda.cu:1044-1068`): ~37 GiB (split_vectors + tail_msm bases)
- Rust side (`supraseal.rs:398-404`): ~130 GiB (10 ProvingAssignments)

These threads call `munmap()` / `drop()` on hundreds of GiB, triggering IPI-based
TLB invalidation broadcasts across all 192 hardware threads. With gw=2 and fast
partition completion (~2s/partition), 2-3 dealloc threads can run concurrently, each
causing thousands of TLB shootdowns that stall all synthesis and prep_msm threads.

The dealloc threads are spawned with `std::thread(...).detach()` and
`std::thread::spawn(move || ...)` — **completely unbounded**, no semaphore, no limit.

**2. b_g2_msm thread pool thrashing**

With `num_circuits=1` (per-partition mode), b_g2_msm at `groth16_cuda.cu:570-575`
calls `mult_pippenger` with the full `groth16_pool` — **all available CPUs** (192
threads by default). Each Pippenger worker allocates a bucket array (~6 MB per worker
for window=15 on G2 points) totaling ~1.1 GiB of bucket RAM across 192 workers.

This runs concurrently with 10 synthesis rayon threads (also 192 threads) doing SpMV.
The combined 384 active threads thrash across 12 CCD L3 domains (32 MB each),
evicting each other's working sets.

Key finding: **prep_msm itself is single-threaded** with `num_circuits=1` because
`par_map(1, ...)` only dispatches to 1 pool thread regardless of pool size. The
prep_msm inflation (1.7s → 2.7s) comes from L3 eviction by concurrent synthesis,
not from pool size.

**3. Aggregate TLB pressure from enormous working set**

At steady state, the actively-accessed virtual memory includes:
- PCE CSR matrices: 25.7 GiB (shared, read-only)
- 10 concurrent partition witnesses: 10 × ~8 GiB = 80 GiB (unique per partition)
- SRS pinned memory: ~10 GiB (read by prep_msm)
- prep_msm/b_g2_msm working data: ~17 GiB (tail_msm vectors, bucket arrays)
- **Total: ~133 GiB actively accessed**

With 4 KB pages, the L2 DTLB covers only 12 MB (3072 entries). Even with THP
(currently `always` mode on this system), L1 DTLB covers only 144 MB (72 × 2 MB).
Every access to the CSR matrices (25.7 GiB) is an L1 TLB miss, requiring an L2 TLB
lookup and potentially a page table walk.

## System Configuration

```
CPU:    AMD Threadripper PRO 7995WX, 96 cores / 192 threads, 1 socket
        12 CCDs (Dies), each with 8 cores + 32 MB L3 = 384 MB L3 total
NUMA:   1 node (NPS1), all memory equidistant
RAM:    755 GiB DDR5 (~333 GB/s theoretical aggregate bandwidth)
THP:    always (kernel auto-promotes to 2 MB pages)
HugeP:  325 × 2 MB reserved (trivial, 324 free). No 1 GB hugepages.
GPU:    RTX 5070 Ti (16 GB VRAM, Blackwell sm_120, PCIe gen5)
```

## Proposed Changes

Three independent interventions, ordered by implementation complexity:

### Intervention 1: Bound async_dealloc to 1 concurrent thread

**Goal**: Eliminate TLB shootdown storms from unbounded `munmap()` threads.

**Mechanism**: Serialize deallocation so at most 1 C++ dealloc thread and 1 Rust
dealloc thread run at any time.

**C++ side** (`extern/supraseal-c2/cuda/groth16_cuda.cu`):

Add a static mutex before the detached dealloc thread at line ~1044:

```cpp
// Phase 11: Serialize async dealloc to prevent concurrent munmap() TLB
// shootdown storms. At most 1 dealloc thread active at a time.
static std::mutex dealloc_mtx;

std::thread([
    sv_l = std::move(split_vectors_l),
    // ... existing captures ...
]() mutable {
    std::lock_guard<std::mutex> lk(dealloc_mtx);
    // ... existing dealloc timing + drops ...
}).detach();
```

**Rust side** (`extern/bellperson/src/groth16/prover/supraseal.rs`):

Add a static mutex for the Rust dealloc thread at line ~398:

```rust
use std::sync::Mutex;
static DEALLOC_MTX: Mutex<()> = Mutex::new(());

std::thread::spawn(move || {
    let _guard = DEALLOC_MTX.lock().unwrap();
    drop(provers);
    drop(input_assignments);
    drop(aux_assignments);
    drop(r_s);
    drop(s_s);
});
```

**Impact**: Reduces concurrent `munmap()` from 2-3 threads to 1+1 (one C++, one Rust).
Expected **2-5% throughput improvement** at high concurrency.

**Risk to parallelism**: None. Dealloc is pure cleanup with zero impact on proof
latency. If a dealloc thread must wait 2s for the previous one, it just delays memory
reclamation — the caller already has its proof result and has moved on.

### Intervention 2: Reduce groth16_pool size to 32 threads

**Goal**: Reduce b_g2_msm's L3 cache pressure and bucket memory footprint.

**Mechanism**: Set `gpu_threads = 32` in the TOML config (2 CCDs worth of threads).

**Analysis**:

With `num_circuits=1`, the only consumer of the groth16_pool is:
- `prep_msm` pass 1 and 2: `par_map(1, ...)` → always 1 thread, unaffected by pool size
- `b_g2_msm`: `mult_pippenger(..., &get_groth16_pool())` → uses full pool

With `gpu_threads=32`:
- b_g2_msm bucket RAM: 32 workers × ~6 MB = ~192 MB (vs ~1.1 GiB at 192 workers)
- b_g2_msm duration: ~0.5-0.7s (vs ~0.4s at 192 workers) — 50-75% slower
- L3 interference with synthesis: much reduced (32 vs 192 threads competing)

**Impact**: b_g2_msm slows by ~0.2s but reduces L3 eviction pressure on synthesis.
Since b_g2_msm runs after the GPU lock is released (concurrent with the next worker's
GPU kernels), the 0.2s slowdown does NOT affect GPU throughput — it just extends the
time before `prep_msm_thread.join()` completes. Expected **3-5% throughput improvement**
at high concurrency from reduced synthesis interference.

**Risk to parallelism**: b_g2_msm gets 0.2s slower. This is acceptable because:
1. It runs outside the GPU lock — doesn't block GPU work
2. The GPU kernel takes 1.8s per partition, b_g2_msm only 0.5-0.7s — ample time
3. `prep_msm_thread.join()` is called after GPU kernels complete, so b_g2_msm
   must finish within 1.8s (it does, with margin)

### Intervention 3: Memory-bandwidth throttle during b_g2_msm

**Goal**: Briefly reduce synthesis SpMV activity during b_g2_msm's 0.4-0.7s window,
giving Pippenger uncontested memory bandwidth.

**Mechanism**: A shared atomic flag, set by C++ before b_g2_msm and cleared after.
Rust synthesis code checks the flag periodically and yields when set.

**Design**:

Extend the existing `gpu_mtx` opaque pointer from a `std::mutex*` to a
`gpu_resources*` struct:

```cpp
// Phase 11: GPU resources struct replacing raw std::mutex.
// ABI-compatible: Rust side still passes *mut c_void.
struct gpu_resources {
    std::mutex gpu_mtx;
    std::atomic<int> membw_throttle{0};  // >0 = synthesis should yield
};
```

Update `create_gpu_mutex()` / `destroy_gpu_mutex()` to allocate `gpu_resources`.
The `gpu_mtx` field replaces the old `std::mutex`. Existing code casts back and
dereferences `->gpu_mtx`.

**C++ changes** (`groth16_cuda.cu`):

Before b_g2_msm:
```cpp
resources->membw_throttle.store(1, std::memory_order_release);
```

After b_g2_msm:
```cpp
resources->membw_throttle.store(0, std::memory_order_release);
```

**Rust changes** (`cuzk-pce/src/eval.rs`):

Add a throttle check in `spmv_parallel()`. Rather than checking every row, check
every N chunks (e.g., every 64 chunks = 524K rows):

```rust
fn spmv_parallel<Scalar: PrimeField>(
    matrix: &CsrMatrix<Scalar>,
    witness: &[Scalar],
    throttle: Option<&AtomicI32>,  // None = no throttling
) -> Vec<Scalar> {
    // ... existing code ...
    result
        .par_chunks_mut(chunk_size)
        .enumerate()
        .for_each(|(chunk_idx, out_chunk)| {
            // Phase 11: Yield briefly if b_g2_msm is active
            if chunk_idx % 64 == 0 {
                if let Some(flag) = throttle {
                    if flag.load(Ordering::Acquire) > 0 {
                        std::thread::yield_now();
                    }
                }
            }
            // ... existing row evaluation ...
        });
    result
}
```

**Threading the pointer through the call chain**:

```
engine.rs: alloc_gpu_resources() → *mut c_void (contains gpu_mtx + throttle)
         ↓
engine.rs: extract throttle pointer → Arc<AtomicI32> (unsafe alias to the C++ atomic)
         ↓
pipeline.rs: synthesize_partition(..., throttle: Option<&AtomicI32>)
         ↓
eval.rs: evaluate_pce(..., throttle: Option<&AtomicI32>)
         ↓
eval.rs: spmv_parallel(..., throttle)
```

The `AtomicI32` in Rust has the same memory layout as `std::atomic<int>` in C++
on x86-64 Linux. We alias the same memory — both C++ and Rust atomics operate on
the same 4-byte location.

**FFI helpers** (extern "C" in `groth16_cuda.cu`):

```cpp
extern "C" void* get_membw_throttle(void* res) {
    return &(static_cast<gpu_resources*>(res)->membw_throttle);
}
```

Rust calls this once at engine init to get the throttle pointer, then passes it
through the synthesis pipeline.

**Impact**: During b_g2_msm (~0.5s with Intervention 2), rayon synthesis threads
yield on ~1/64 of their chunk iterations. With 192 rayon threads, each checking
every 64 chunks and yielding for one timeslice (~100µs), the effective synthesis
slowdown is ~50ms total. b_g2_msm gets less contention and may speed up 10-20%.

**Risk to parallelism**: Minimal. The `yield_now()` call is a hint — it doesn't
sleep or block. It just lets the OS scheduler run another thread. If no other thread
is waiting, it returns immediately. The check is every 64 chunks (524K rows ≈ 16M
constraint evaluations), so the overhead of the atomic load is negligible (one load
per ~16M multiply-add operations).

When `membw_throttle == 0` (99%+ of the time), the only cost is one atomic load every
64 chunks — roughly 1 ns overhead per 500µs of SpMV work.

## Expected Results

| Configuration | Phase 9 Baseline | After All 3 Interventions | Improvement |
|---|---|---|---|
| c=1 j=1 (isolation) | 32.1s | ~31.5s | ~2% |
| c=5 j=5 | 41.4s | ~38.0s | ~8% |
| c=10 j=10 | 38.7s | ~35.5s | ~8% |
| c=15 j=15 | 38.2s | ~34.5s | ~10% |
| c=20 j=15 | 38.0s | ~34.0s | ~11% |

The isolation case barely improves because there's no contention to reduce. The
high-concurrency cases improve because all three sources of interference are
mitigated.

**Theoretical floor**: 30.0s/proof (10 partitions × 1.5s avg with 2 workers and
zero contention). The remaining gap (~4s) comes from synthesis lead time (first
partition must complete before GPU can start), Rust overhead, and irreducible
queue wait.

## Implementation Order

1. Intervention 1 (dealloc mutex): ~30 lines C++ + ~10 lines Rust
2. Benchmark c=20 j=15, compare to baseline
3. Intervention 2 (gpu_threads=32): config-only change
4. Benchmark c=20 j=15, compare
5. Intervention 3 (membw_throttle): ~40 lines C++ + ~30 lines Rust + ~20 lines threading
6. Full benchmark sweep: c=5/10/15/20, compare all

## Files Modified

| File | Intervention | Description |
|---|---|---|
| `extern/supraseal-c2/cuda/groth16_cuda.cu` | 1 | `static std::mutex dealloc_mtx` around detached dealloc thread |
| `extern/supraseal-c2/cuda/groth16_cuda.cu` | 3 | `gpu_resources` struct, `membw_throttle` set/clear around b_g2_msm |
| `extern/supraseal-c2/cuda/groth16_cuda.cu` | 3 | `get_membw_throttle()` FFI helper |
| `extern/supraseal-c2/src/lib.rs` | 3 | FFI declaration for `get_membw_throttle` |
| `extern/bellperson/src/groth16/prover/supraseal.rs` | 1 | `static DEALLOC_MTX` for Rust-side dealloc serialization |
| `extern/cuzk/cuzk-pce/src/eval.rs` | 3 | Throttle check in `spmv_parallel()` |
| `extern/cuzk/cuzk-core/src/pipeline.rs` | 3 | Pass throttle pointer through synthesis |
| `extern/cuzk/cuzk-core/src/engine.rs` | 3 | Extract throttle pointer from `gpu_resources`, pass to synthesis |
| TOML config | 2 | `gpu_threads = 32` |

## Appendix: Phase 9 Throughput Sweep (Baseline)

Benchmark data collected with Phase 9, gw=2, pw=10:

| Config | Throughput (s/proof) | Avg Prove Time | Avg GPU/Part | GPU Util |
|---|---|---|---|---|
| c=5 j=5 | 41.4 | 50.3s | 5.0s | 84.9% |
| c=10 j=10 | 38.7 | 56.3s | 5.6s | 89.7% |
| c=15 j=15 | 38.2 | 54.3s | 5.4s | 88.0% |
| c=20 j=15 | 38.0 | 60.0s | 6.0s | 90.8% |

GPU per-partition time inflation under load:
- Isolation: 4.9s avg → c=20 j=15: 6.0-7.5s avg (22-53% inflation)
- prep_msm inflation: 1.7s → 2.7s (59% under worst contention)
- Synthesis inflation: 35s → 54s (54% at c=20 j=15)

GPU idle gaps (between proofs): 3-11s, caused by synthesis not producing partitions
fast enough. 12 gaps totaling 66.5s at c=20 j=15.

## Appendix: Phase 10 Post-Mortem

Phase 10 (two-lock GPU interlock with 3 workers) was implemented, tested, and
abandoned. Key results:

- **Correctness**: Passed (proof verified with gw=3, single proof: 72.5s)
- **OOM crash**: At c=3 j=3, Worker B's kernel allocation failed with "out of memory"
  because Worker A's pre-staged 12 GiB buffers were still on GPU when Worker B
  entered compute_mtx
- **Design flaw**: Pre-staged VRAM allocated under `mem_mtx` persists until consumed
  under `compute_mtx`, but another worker may enter `compute_mtx` first
- **CUDA device-global APIs**: `cudaDeviceSynchronize`, `cudaMemPoolTrimTo` block
  all streams — cannot isolate memory ops from compute ops on the same device
- **No benefit over Phase 9**: Phase 9 already releases the lock before b_g2_msm,
  hiding the only CPU work that the two-lock design aimed to overlap

Code was reverted to Phase 9 single-lock state.
