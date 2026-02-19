# Proposal 8: Dual-Worker GPU Interlock

**Goal**: Eliminate per-partition GPU idle gaps by overlapping one worker's
CPU preamble/epilogue with another worker's CUDA kernel execution on the
same physical GPU. Two GPU worker tasks per GPU share a fine-grained
semaphore that brackets only the actual CUDA kernel region, allowing CPU
work from the "next" partition to proceed in parallel with GPU execution
of the "current" partition.

**Impact** (estimated):
- Per-partition GPU gap: 10-200ms → ~0ms (CPU work hidden behind CUDA)
- Steady-state GPU idle: ~5-8% → ~1-2%
- Throughput improvement: ~3-5% on top of Phase 7

**Scope**: Engine spawns 2 GPU workers per physical GPU. A new
`Arc<Semaphore(1)>` per GPU is passed into `gpu_prove` → `prove_from_assignments`
→ the C++ `generate_groth16_proofs_c` call, which acquires the semaphore just
before launching CUDA kernels and releases it as soon as GPU-side work completes
(before b_g2_msm, epilogue, and deallocation).

---

## Part A: Problem Analysis

### A.1 Current Per-Partition Overhead

Phase 7 timeline data from a 5-proof throughput run (RTX 5070 Ti, 192T Zen4):

| Metric | Value |
|---|---|
| GPU calls measured | 110 |
| Gaps < 50ms | 87 (80%) |
| Gaps 50-500ms | 14 (13%) |
| Gaps > 500ms | 8 (7%) — cross-sector stalls |
| Average gap | ~15ms (excluding cross-sector) |
| Total gap time | 251.9s |
| Total GPU wall time | 453.1s |
| **GPU efficiency** | **64.3%** |

The 64.3% figure includes the large cross-sector stalls (where no partitions are
available from the next sector yet). Excluding those 8 stalls (which are a
scheduling issue, not a per-partition overhead issue), the intra-sector efficiency
is much higher. But there is still consistent 10-200ms overhead per partition
transition that could be hidden.

### A.2 What Happens Between Partitions

When one partition's GPU work completes and the next begins, the single GPU
worker executes this sequence **serially**:

```
Partition N finishes:
  1. [inside C++] Proof assembly epilogue (CPU, ~1-2ms)
  2. [inside C++] Async dealloc thread spawn (~0ms)
  3. [inside C++] Mutex release at function return (line 821)
  4. [Rust FFI]   Return from generate_groth16_proofs_c
  5. [Rust]       prove_from_assignments: spawn background dealloc thread (~0ms)
  6. [Rust]       gpu_prove: serialize proof bytes (192B, ~0ms)
  7. [Rust]       gpu_prove: emit GPU_END timeline event
  8. [tokio]      spawn_blocking future resolves → async task resumes
  9. [engine]     tracker.lock().await — acquire job tracker mutex
  10. [engine]    assembler.insert() + duration bookkeeping
  11. [engine]    malloc_trim(0) — return pages to OS (variable, 0-200ms)
  12. [engine]    check is_complete(), maybe assemble final proof
  13. [engine]    drop tracker lock
  14. [engine]    loop back, synth_rx.lock().await — acquire channel mutex
  15. [engine]    rx.recv() — dequeue next partition (instant if backlog)
  16. [engine]    extract metadata, build tracing span
  17. [engine]    emit GPU_START timeline event

Partition N+1 starts:
  18. [tokio]     spawn_blocking dispatches to thread pool
  19. [Rust]      gpu_prove entry, build span
  20. [Rust]      prove_from_assignments: extract dimensions, validate
  21. [Rust]      prove_from_assignments: build raw pointer arrays
  22. [Rust]      prove_from_assignments: allocate output vec
  23. [Rust]      prove_from_assignments: get SRS handle
  24. [Rust FFI]  Call generate_groth16_proofs_c
  25. [C++]       Acquire static mutex (line 134)
  26. [C++]       SRS slice extraction (lines 145-151)
  27. [C++]       Split-vector allocation (lines 155-181)
  28. [C++]       Launch prep_msm_thread (line 194)
  29. [C++]       Launch per-GPU thread (line 571)
  30. [C++ GPU]   NTT + H-MSM ← actual CUDA kernels start here
```

Steps 1-30 are all sequential on a single worker. The actual CUDA hardware
sits idle during steps 1-29 (~10-200ms depending on malloc_trim).

### A.3 The Static Mutex

The C++ function `generate_groth16_proofs_c` in `groth16_cuda.cu:132-134` holds
a `static std::mutex` for its entire duration:

```cpp
132:    // Mutex to serialize execution of this subroutine
133:    static std::mutex mtx;
134:    std::lock_guard<std::mutex> lock(mtx);
    // ... ~690 lines of work ...
820:    return ret;
821: }  // lock released here
```

This mutex covers:
- CPU preprocessing (build bit vectors, popcount, populate tail-MSM bases)
- **All CUDA kernels** (NTT, H-MSM, batch additions, tail MSMs)
- **CPU b_g2_msm** (Pippenger on CPU thread pool, 0.4s for num_circuits=1)
- CPU proof assembly epilogue
- Async dealloc thread spawn

The mutex prevents two calls from overlapping at all, even if one is doing
pure CPU work while the other could be using the GPU.

### A.4 Internal Structure of generate_groth16_proofs_c

```
Timeline within one call (num_circuits=1, ~3.5s total):

[prep_msm_thread]  ═══CPU═══(~0.3s)═══▶ barrier.notify() ══b_g2_msm(0.4s)══▶ join
                                               │
[per_gpu_thread]   ═NTT+H_MSM(~0.8s)═▶ barrier.wait() ═batch_add(~0.3s)═tail_MSM(~1.0s)═▶ join
                                                                                                │
[main_thread]      ══════════════════════════════wait for joins════════════════════════▶ epilogue(~0.1s)

Actual GPU kernel region: NTT+H_MSM → batch_add → tail_MSM  (~2.1s)
CPU-only region inside the lock: prep(0.3s) + b_g2_msm(0.4s) + epilogue(0.1s) = ~0.8s
```

The per-GPU thread calls `barrier.wait()` at line 603, waiting for the
prep_msm_thread to finish Phase 1-3 preprocessing. After the barrier,
both threads run concurrently:
- GPU thread: batch additions + tail MSMs (CUDA kernels)
- CPU thread: b_g2_msm (pure CPU Pippenger)

After both threads join (~line 717-719), the main thread does the proof
assembly epilogue (lines 727-775).

---

## Part B: Architecture

### B.1 Core Idea: GPU Semaphore Inside C++

Replace the coarse `static std::mutex` with a finer-grained approach:

1. Pass a **GPU semaphore** (or callback/mutex pointer) from Rust into the
   C++ function.
2. Inside `generate_groth16_proofs_c`, replace the static mutex with:
   - Acquire the semaphore **just before** launching the per-GPU thread
     (line ~571, after preprocessing is done).
   - Release the semaphore **just after** the per-GPU thread joins
     (line ~719, before b_g2_msm completes and before epilogue).
3. CPU preprocessing (Phase 1-3) and b_g2_msm (Phase 4) run **outside**
   the semaphore, overlapping with the other worker's GPU kernels.

### B.2 Dual Worker Topology

```
                    synth_rx channel
                    (partition jobs)
                         │
                    ┌────┴────┐
                    ▼         ▼
              ┌──────────┐ ┌──────────┐
              │ Worker A │ │ Worker B │   ← 2 tokio tasks per GPU
              └────┬─────┘ └────┬─────┘
                   │            │
              Both call gpu_prove() on spawn_blocking threads.
              Inside C++, they acquire gpu_sem before CUDA kernels:
                   │            │
    Time ─────────────────────────────────────────────────────▶

    Worker A: [CPU prep][██ CUDA kernels ██][b_g2+epilogue][CPU prep][██ CUDA ██]...
    Worker B:           [CPU prep ─────────][██ CUDA kernels ██][b_g2+epi][CPU prep]...
    GPU sem:  ──────────[A holds ──────────][B holds ──────────][A holds ─────────]...
    GPU HW:             [████ A ████████████][████ B ████████████][████ A ████████]...
```

Worker B starts its CPU preprocessing while Worker A is on the GPU. When A
releases the semaphore after its CUDA kernels, B acquires it immediately
and launches its kernels — zero GPU idle. A then does b_g2_msm + epilogue +
Rust-side bookkeeping while B is on the GPU.

### B.3 What Overlaps With What

| Worker A phase | Worker B phase | GPU owner |
|---|---|---|
| CUDA kernels (NTT+MSM) | CPU prep (pointer setup, FFI, C++ alloc, preprocessing) | A |
| b_g2_msm + epilogue + Rust bookkeeping | CUDA kernels (NTT+MSM) | B |
| CPU prep for next partition | b_g2_msm + epilogue + Rust bookkeeping | neither (but B just finished) |

The key constraint: only ONE worker can be inside the CUDA kernel region at
a time (they share the same physical GPU). The semaphore enforces this.

### B.4 Semaphore Placement Options

#### Option 1: Rust-level semaphore with split prove function

Split `gpu_prove` into three phases:
1. `gpu_prove_prepare()` — CPU preprocessing (no GPU, no semaphore)
2. `gpu_prove_execute()` — acquire semaphore, run CUDA kernels, release semaphore
3. `gpu_prove_finalize()` — b_g2_msm, epilogue, proof serialization

**Pro**: Clean Rust API, no C++ changes.
**Con**: Requires splitting `generate_groth16_proofs_c` into 3 C++ functions
(or introducing callbacks). Major refactor of the C++ code. The C++ function
uses many local variables across phases that would need to be moved to a
context struct.

#### Option 2: Pass semaphore FD / callback into C++

Pass a callback pair `(acquire_fn, release_fn)` via FFI into the C++ function.
The C++ code calls `acquire_fn()` before launching GPU threads and
`release_fn()` after GPU threads join.

```cpp
// In generate_groth16_proofs_c:
//   After preprocessing, before GPU thread launch:
    acquire_gpu_fn(gpu_context);   // blocks until semaphore available
//   ... launch per-GPU threads, run CUDA kernels ...
//   After per-GPU threads join:
    release_gpu_fn(gpu_context);   // allow other worker to proceed
//   ... b_g2_msm, epilogue continue without holding GPU ...
```

**Pro**: Minimal C++ refactor — just two function call insertions.
**Con**: FFI callback complexity. Need to ensure callbacks are safe
(no Rust async across FFI boundary — use a blocking semaphore or condvar).

#### Option 3: C++ internal dual-buffer (no Rust changes)

Restructure the C++ function internally to use a double-buffered approach
with two internal contexts. The static mutex is replaced with a semaphore
and the function manages its own pipelining.

**Pro**: Completely transparent to Rust.
**Con**: Most complex C++ change. The function's local-variable-heavy style
makes this difficult. Buffer management for GPU memory is tricky.

#### Option 4: Replace static mutex with passed-in mutex/semaphore

Pass a `std::mutex*` from Rust into the C++ function. The C++ code acquires
it at the narrowest possible scope (around CUDA kernel region only) instead
of the full function duration.

```cpp
// Replace lines 132-134 with:
    // mutex_ptr is passed from Rust
    // ... CPU preprocessing (no lock held) ...
    
    std::unique_lock<std::mutex> gpu_lock(*mutex_ptr);
    // ... launch GPU threads, CUDA kernels ...
    // ... wait for GPU threads to join ...
    gpu_lock.unlock();
    
    // ... b_g2_msm, epilogue (no lock held) ...
```

**Pro**: Simplest C++ change. Just move the lock scope.
**Con**: Need to pass the mutex through FFI. Need to handle the case
where prep_msm_thread and per-GPU threads share data — the prep thread
must finish preprocessing before the GPU thread can start (the existing
`barrier` handles this). The tricky part: prep_msm_thread starts BEFORE
the GPU lock is acquired (it does CPU preprocessing), but also does
b_g2_msm AFTER the GPU work — so it must NOT be inside the GPU lock scope.

### B.5 Recommended Approach: Option 4 (Passed Mutex)

Option 4 is the simplest and most robust. The changes are:

**C++ side (`groth16_cuda.cu`):**

1. Add a `std::mutex*` parameter to `generate_groth16_proofs_c`.
2. Remove the static mutex at lines 132-134.
3. Add `std::unique_lock<std::mutex> gpu_lock(*mutex_ptr)` just before
   launching the per-GPU thread (after prep_msm_thread is launched and
   has started preprocessing).
4. Call `gpu_lock.unlock()` after the per-GPU threads join (line 719)
   but before `prep_msm_thread.join()` (line 717), so b_g2_msm runs
   without the lock.

Wait — there's a subtlety. The execution order is:

```
main thread:  launch prep_msm_thread → launch per_gpu_threads → join all → epilogue
prep_msm:     [preprocess] → barrier.notify() → [b_g2_msm] → done
per_gpu:      [NTT+H_MSM] → barrier.wait() → [batch_add+tail_MSM] → done
```

The per-GPU threads **start NTT+H_MSM immediately** (line 591) without
waiting for the preprocessing barrier. The barrier is only for batch-add
and tail-MSM which need the preprocessing results.

So the GPU lock must be acquired BEFORE the per-GPU threads are launched
(to protect NTT+H_MSM from concurrent GPU access). But preprocessing can
run outside the lock.

Revised sequence:

```
Worker A:
  1. CPU preprocessing (prep_msm_thread Phase 1-3, no GPU lock)
  2. ACQUIRE GPU LOCK
  3. Launch per-GPU thread (NTT+H_MSM → barrier.wait → batch_add+tail_MSM)
  4. prep_msm_thread: barrier.notify() → b_g2_msm starts (CPU, OK under GPU lock)
  5. per-GPU thread joins (CUDA work done)
  6. RELEASE GPU LOCK
  7. prep_msm_thread: b_g2_msm finishes, thread joins
  8. Epilogue (proof assembly, CPU only)
```

Hmm, but b_g2_msm runs on the prep_msm_thread which was launched BEFORE
the GPU lock was acquired. The prep_msm_thread does barrier.notify() then
immediately starts b_g2_msm. If we release the GPU lock after per-GPU
thread joins (step 6), b_g2_msm may still be running when the other worker
acquires the lock and starts its GPU work. That's fine — b_g2_msm is pure
CPU and doesn't conflict with GPU kernels.

But the **epilogue** (lines 727-775) runs AFTER `prep_msm_thread.join()`
at line 717. The join waits for b_g2_msm to finish. If we release the GPU
lock after per-GPU thread joins but before prep_msm_thread.join(), the
epilogue runs without the GPU lock — which is correct since it's pure CPU.

Actually, let me re-read the join order more carefully.

```cpp
717:    prep_msm_thread.join();    // waits for b_g2_msm to finish
718:    for (auto& tid : per_gpu)
719:        tid.join();            // waits for GPU kernels to finish
```

The joins are: prep_msm first, then per-GPU threads. Both run concurrently.
If b_g2_msm finishes before GPU kernels (likely — 0.4s vs 2.1s), the order
doesn't matter. If not, the main thread waits.

For the GPU lock, we want:
- Acquire BEFORE per-GPU threads are launched
- Release AFTER per-GPU threads join (GPU kernels done)
- b_g2_msm and epilogue run outside the lock

Revised C++ structure:

```cpp
    // Launch prep_msm_thread (starts CPU preprocessing immediately)
    auto prep_msm_thread = std::thread([&]() {
        // Phase 1-3: CPU preprocessing (no GPU needed)
        ...
        barrier.notify();
        // Phase 4: b_g2_msm (CPU only, no GPU needed)
        ...
    });

    // ═══ ACQUIRE GPU LOCK ═══
    std::unique_lock<std::mutex> gpu_lock(*mutex_ptr);

    // Launch per-GPU threads (CUDA kernels)
    for (size_t i = 0; i < n_gpus; i++) {
        per_gpu.emplace_back(std::thread([&]() {
            // NTT + H-MSM (CUDA)
            ...
            barrier.wait();
            // Batch additions (CUDA)
            // Tail MSMs (CUDA)
            ...
        }));
    }

    // Wait for GPU work to complete
    for (auto& tid : per_gpu)
        tid.join();

    // ═══ RELEASE GPU LOCK ═══
    gpu_lock.unlock();

    // Wait for b_g2_msm to complete (CPU only, lock not held)
    prep_msm_thread.join();

    // Epilogue: proof assembly (CPU only, lock not held)
    ...
```

**Problem**: The per-GPU thread calls `barrier.wait()` which blocks until
prep_msm_thread calls `barrier.notify()`. If another worker is currently
holding the GPU lock (running CUDA), our prep_msm_thread is already running
CPU preprocessing concurrently — good. But our per-GPU thread can't start
until we acquire the GPU lock. Meanwhile, another worker's per-GPU thread
is using the GPU. The barrier between our prep_msm and per_gpu will only
fire after we acquire the lock and launch our per_gpu thread.

This means: our CPU preprocessing runs concurrently with the other worker's
GPU work (good!), but our per-GPU thread only starts after we get the lock.
The barrier is internal to our call, so it's fine — our prep_msm_thread
continues with b_g2_msm after notify(), regardless of whether our per_gpu
thread has started yet.

Wait — there IS a problem. The barrier is between our prep_msm and our
per_gpu. If our per_gpu hasn't launched yet (waiting for GPU lock), our
prep_msm does `barrier.notify()` and immediately proceeds to b_g2_msm.
When our per_gpu finally launches (after acquiring lock), it does
`barrier.wait()` — but the barrier was already notified, so it proceeds
immediately. This should work correctly with a counting barrier or
semaphore-style barrier.

Let me check the barrier type... it's likely a custom semaphore or
condition variable. The `barrier.notify()` / `barrier.wait()` pattern
suggests it stores a count. As long as notify() before wait() works
(i.e., the notification is latched), this is fine.

### B.6 Thread Safety Considerations

The prep_msm_thread writes into shared data structures (split_vectors,
tail_msm bases) that the per-GPU thread reads after the barrier. As long
as the barrier provides the memory ordering guarantee (which it does —
it's a synchronization point), the data is safely visible.

The dual-worker approach means two calls to `generate_groth16_proofs_c`
can overlap, but never in the CUDA-kernel region. The CPU preprocessing
uses local variables (each call has its own stack frame and heap allocs),
so there's no data race. The only shared state is the SRS (read-only) and
the GPU hardware (serialized by the lock).

### B.7 Memory Impact

Each call allocates ~2-3 GB of working buffers (split vectors, tail MSM
bases) on the CPU side. With two overlapping calls, peak memory increases
by ~3 GB — negligible compared to the 13.6 GB per synthesized partition.

GPU memory (VRAM) is only used during the CUDA kernel region, which is
serialized by the lock. No VRAM duplication.

### B.8 Handling the Existing Mutex Users

The static mutex in `groth16_cuda.cu:133` also prevents concurrent calls
from **different** GPUs in a multi-GPU setup. If we're passing a per-GPU
mutex from Rust, multi-GPU is naturally handled — each GPU gets its own
mutex, so GPU 0 and GPU 1 can run kernels concurrently.

However, the current cuzk engine only supports a single GPU (the RTX 5070
Ti), so multi-GPU is not tested. The per-GPU mutex approach is correct for
both single and multi-GPU.

---

## Part C: Implementation Plan

### C.1 Changes Required

| File | Change | Lines |
|---|---|---|
| `groth16_cuda.cu` | Remove static mutex. Add `std::mutex*` param. Acquire before per-GPU launch, release after per-GPU join. Move `prep_msm_thread.join()` after lock release. | ~20 |
| `supraseal-c2/src/lib.rs` | Add `*mut std::ffi::c_void` (mutex ptr) param to `generate_groth16_proofs_c` FFI decl and `generate_groth16_proof` wrapper. | ~10 |
| `bellperson/.../supraseal.rs` | Add `gpu_mutex: *mut c_void` param to `prove_from_assignments`. Pass through to FFI. | ~5 |
| `pipeline.rs` | `gpu_prove`: accept and pass `gpu_mutex` to `prove_from_assignments`. | ~5 |
| `engine.rs` | Create one `Box<std::sync::Mutex<()>>` per GPU. Spawn 2 GPU workers per GPU. Pass raw mutex pointer to `gpu_prove`. | ~30 |
| `config.rs` | Add `gpu_workers_per_device: u32` (default 2). | ~5 |
| **Total** | | **~75 lines** |

### C.2 Phase Ordering

1. **C++ mutex refactor** (0.5 session)
   - Remove static mutex
   - Add mutex pointer parameter
   - Restructure lock scope: acquire before per-GPU launch, release after per-GPU join
   - Move `prep_msm_thread.join()` after lock release
   - Verify barrier semantics work with delayed per-GPU launch

2. **FFI + Rust plumbing** (0.5 session)
   - Thread mutex pointer through FFI boundary
   - Update `prove_from_assignments`, `gpu_prove`
   - Create per-GPU mutex in engine, pass to workers

3. **Dual worker spawn** (0.5 session)
   - Engine spawns 2 workers per GPU, sharing synth_rx and gpu_mutex
   - Both workers pull from the same channel
   - Config: `gpu_workers_per_device`

4. **Benchmarking** (0.5 session)
   - Single-proof latency (should be ~same or slightly worse)
   - Multi-proof throughput (should show 3-5% improvement)
   - Timeline analysis: verify GPU gaps < 5ms in steady state

### C.3 Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Barrier notify/wait ordering issue with delayed per-GPU launch | Medium | GPU hangs or incorrect results | Test barrier semantics independently; add timeout |
| CPU preprocessing of worker B contends with worker A's CUDA (memory bandwidth) | Low | Minor throughput loss | Monitor — preprocessing is mostly pointer arithmetic, not memory-heavy |
| Two overlapping b_g2_msm calls contend for CPU | Medium | b_g2_msm takes 0.4s→0.6s | Acceptable — still hidden behind other worker's CUDA |
| VRAM fragmentation with interleaved allocations | Low | CUDA OOM | Per-partition (num_circuits=1) uses minimal VRAM; serialized by lock |
| Background dealloc threads from both workers contend | Low-Medium | malloc_trim stalls | Already using background dealloc; malloc_trim runs outside GPU lock |

### C.4 Questions to Resolve

1. **Barrier type**: ✅ RESOLVED. The barrier is `semaphore_t` from
   `sppark/util/thread_pool_t.hpp:25-47`. It's a counting semaphore:
   `notify()` increments counter, `wait()` blocks until counter > 0
   then decrements. **notify() before wait() latches correctly** — the
   notification is preserved. This confirms that prep_msm_thread can
   call `barrier.notify()` before the per-GPU thread calls `barrier.wait()`,
   and the wait will return immediately.

2. **prep_msm_thread launch timing**: Currently the prep_msm_thread is
   launched at line 194, before the per-GPU threads at line 571. In the
   new scheme, prep_msm starts BEFORE acquiring the GPU lock (good — its
   preprocessing overlaps with the other worker's CUDA). But we need to
   verify that launching it this early doesn't cause issues when the
   per-GPU thread starts later (after lock acquisition).

3. **Per-GPU thread allocation data**: The per-GPU threads at lines 571-715
   allocate GPU-side buffers (d_a, etc.) at the start. With the lock held,
   only one call allocates at a time. If we move lock acquisition to just
   before per-GPU launch, this is still serialized — correct.

4. **Split vectors lifetime**: `split_vectors_l`, `split_vectors_b` etc.
   are allocated by the main thread and used by both prep_msm_thread and
   per-GPU threads. They're local to the function call, so two overlapping
   calls have independent copies — no sharing issue.

5. **SRS access**: Both workers read from the same SRS. The SRS is read-only
   after loading, so concurrent reads are safe (no mutex needed).

---

## Appendix: Timeline Derivation

### Current (single worker per GPU)

```
Partition N:
  [CPU prep 0.3s][══ CUDA 2.1s ══][b_g2 0.4s][epilogue 0.1s][Rust overhead ~0.1s]
                                                                    ↓
                                                              ~15ms gap (avg)
                                                                    ↓
Partition N+1:
  [CPU prep 0.3s][══ CUDA 2.1s ══][b_g2 0.4s][epilogue 0.1s][Rust overhead ~0.1s]

GPU active: 2.1s / 3.5s = 60% (per-partition, excluding cross-sector stalls)
```

### Proposed (dual interleaved workers)

```
Worker A: [prep][══ CUDA A ══][b_g2+epi+Rust][prep][══ CUDA A ══]...
Worker B:       [prep────────][══ CUDA B ══][b_g2+epi+Rust][prep]...
GPU sem:        [A ═══════════][B ═══════════][A ═══════════]...
GPU HW:         [████ A ██████][████ B ██████][████ A ██████]...

GPU active: ~2.1s / 2.1s ≈ 100% (CPU work fully hidden)
```

The CPU work per partition (~1.3s of prep + b_g2_msm + epilogue + Rust
overhead) is fully hidden behind the other worker's CUDA execution (~2.1s).
As long as CPU work < CUDA time (1.3s < 2.1s), the GPU never idles.

### Throughput Impact

Current steady-state: 10 partitions × 3.5s/partition = 35s GPU wall per sector.
Proposed: 10 partitions × 2.1s CUDA / partition = 21s GPU wall per sector,
plus ~1.3s of unhidden CPU for the last partition = 22.3s per sector.

However, the actual CUDA time might be closer to 2.5-3.0s (the 3.5s wall
includes some GPU idle within the C++ function itself). The improvement
depends on the exact split between CUDA kernels and CPU work inside
`generate_groth16_proofs_c`.

Conservative estimate: **30-35s/proof → 27-30s/proof** (~10% improvement).
