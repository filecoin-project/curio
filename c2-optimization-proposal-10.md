# Phase 10: Two-Lock GPU Interlock with 3 Workers

## Problem Statement

Phase 9 cut GPU kernel time per partition from 3.75s to 1.82s (51% reduction) via
pinned DMA pre-staging and deferred Pippenger sync. However, the **CPU critical path**
(prep_msm + b_g2_msm = 2.39s) now exceeds GPU kernel time, making the system
CPU memory-bandwidth-bound rather than GPU-bound.

The current single-mutex design forces the entire proving function — pre-staging
allocation, GPU kernels, and VRAM cleanup — into one serialized region. With gw=1,
the b_g2_msm (0.48s) and Rust epilogue run sequentially after GPU work. With gw=2,
the Phase 9 pre-staging overhead (cudaDeviceSynchronize + pool trim + 12 GiB alloc)
serializes under the same lock as kernels, causing a regression (41.0s vs 37.4s).

## Root Cause Analysis (Phase 9 Timing)

Per-partition breakdown (steady-state, gw=1, c=15 j=15):

| Component | Time (ms) | Lock held | Notes |
|---|---|---|---|
| cudaHostRegister | ~5 | none | CPU mlock, before lock |
| Pre-stage setup (pool trim + alloc + upload) | 18 | gpu_mtx | Fast on PCIe gen5 |
| NTT + coeff_wise_mult + sub_mult | 690 | gpu_mtx | GPU kernels |
| Batch additions (L, A, B_G1, B_G2) | 650 | gpu_mtx | GPU kernels |
| Tail MSMs (L, A, B_G1) | 83 | gpu_mtx | GPU kernels |
| d_bc/event/stream cleanup | ~2 | gpu_mtx | Post-kernel, pre-unlock |
| **GPU kernel total** | **1824** | **gpu_mtx** | |
| cudaHostUnregister | ~3 | none | After unlock |
| prep_msm (split vectors + CPU preprocessing) | 1909 | none | CPU thread, overlaps |
| b_g2_msm (G2 Pippenger on CPU) | 484 | none | After prep, blocks return |
| Epilogue (point arithmetic, proof assembly) | ~5 | none | |
| **CPU critical path** | **2398** | | prep_msm + b_g2_msm |

Key observations:
- GPU kernels: 1.82s, CPU critical path: 2.40s → **CPU is 580ms slower**
- b_g2_msm (484ms) runs AFTER the GPU lock is released but BEFORE the function returns
- TIMELINE gpu_ms ≈ 3.7s because it includes the Rust-side `gpu_prove()` call which
  blocks on `prep_msm_thread.join()` after GPU work completes
- Actual GPU silicon utilization: 1824ms / 3760ms = **48.5%**
- Pre-staging setup is only 18ms — negligible, PCIe gen5 is not the bottleneck

## Proposed Change: Two-Lock Interlock

Split the single `std::mutex* gpu_mtx` into a struct with two mutexes:

```cpp
struct gpu_locks {
    std::mutex compute_mtx;  // Held during GPU kernel execution
    std::mutex mem_mtx;      // Held during VRAM alloc/free + upload
};
```

### Lock Protocol

Each worker follows this sequence:

```
Phase 0: [NO LOCK]
  - cudaHostRegister(a, b, c)           ~5ms, CPU-only mlock
  - prep_msm_thread launched            CPU preprocessing starts

Phase 1: lock(mem_mtx)                  ~18ms hold time
  - cudaMemPoolTrimTo(pool, 0)          Reclaim cached pool memory
  - cudaMemGetInfo                      Check free VRAM
  - cudaMalloc(d_a, 4 GiB)             Synchronous
  - cudaMalloc(d_bc, 8 GiB)            Synchronous
  - cudaStreamCreate + cudaEventCreate  Upload infrastructure
  - cudaMemcpyAsync(d_a ← host_a)      Async DMA on upload_stream
  - cudaMemcpyAsync(d_b ← host_b)      Async DMA on upload_stream
  - cudaMemcpyAsync(d_c ← host_c)      Async DMA on upload_stream
  - cudaEventRecord(ev_a/b/c)          Signal upload completion
  unlock(mem_mtx)

Phase 2: lock(compute_mtx)             ~1.8s hold time
  - stream.wait(ev_a/b/c)              GPU-side wait for uploads
  - NTT inverse + coset forward (a,b,c) on GPU streams
  - coeff_wise_mult(a, b)
  - sub_mult_with_constant(a, c, z_inv)
  - coset_iNTT(a)
  - cudaFree(d_bc)                      8 GiB freed immediately (synchronous)
  - H MSM: msm_t(npoints).invoke()      Uses d_a as scalars
  - gpu_ptr_t(d_a) destructor           4 GiB freed (synchronous cudaFree)
  - barrier.wait()                      Wait for prep_msm split vectors
  - batch_add(L, A, B_G1, B_G2)        Sequential, each ~1.3 GiB temp
  - tail_msm(L, A, B_G1)               Sequential, each ~1.1 GiB temp
  unlock(compute_mtx)

Phase 3: [NO LOCK]
  - cudaEventDestroy(ev_a/b/c)         Tiny objects
  - cudaStreamDestroy(upload_stream)
  - cudaHostUnregister(a, b, c)         CPU munlock
  - prep_msm_thread.join()              Wait for b_g2_msm to finish
  - Epilogue: point arithmetic          ~5ms
```

### Lock Ordering

**mem_mtx is always acquired before compute_mtx. They are never held simultaneously.**

This eliminates deadlock by construction:
- Worker never holds mem_mtx while waiting for compute_mtx
- Worker never holds compute_mtx while waiting for mem_mtx
- No nested locking occurs

### VRAM Safety

Peak VRAM during Phase 2 (compute):
- d_a: 4 GiB + d_bc: 8 GiB = 12 GiB (during NTT phase)
- After d_bc free: d_a: 4 GiB + H-MSM temp: ~1.8 GiB = ~5.8 GiB
- After d_a free: batch_add temp: ~1.3 GiB, tail_msm temp: ~1.35 GiB

Another worker's Phase 1 (mem_mtx) can run while this worker is in Phase 2 (compute_mtx).
However, VRAM is shared — if the current worker's d_a+d_bc (12 GiB) is still live,
the next worker's cudaMalloc for 12 GiB will fail on 16 GiB GPU.

**This is safe because:**
1. Phase 1 checks `cudaMemGetInfo` before allocating
2. If insufficient VRAM, Phase 1 sets `prestage_ok = false` and falls back to the
   non-prestaged path inside Phase 2 (alloc + sync HtoD under compute_mtx)
3. The next worker's Phase 1 runs ~18ms; the current worker's Phase 2 frees d_bc
   after ~690ms (NTT time). So the next worker typically sees insufficient VRAM during
   its Phase 1, falls back, then allocates inside its Phase 2 after the previous
   worker's compute_mtx is released.

In practice with 3 workers:
- Worker A in Phase 2 (1.8s, VRAM occupied)
- Worker B in Phase 1 (18ms, VRAM check fails → fallback)
- Worker C in Phase 0 (CPU prep, no VRAM)

Worker B will hold mem_mtx briefly (18ms for the failed check), fall back, then wait
for compute_mtx. When A releases compute_mtx, B enters and does in-mutex alloc+upload
(original path). Meanwhile C can enter mem_mtx, also fail the VRAM check, and queue
behind B for compute_mtx.

**Net effect:** The two-lock split doesn't help with VRAM pre-staging overlap (16 GiB
GPU is too small for two concurrent 12 GiB allocations). The primary benefit is
**decoupling the mem_mtx hold time from compute_mtx**, so the event/stream cleanup
(Phase 3) and b_g2_msm + epilogue run fully outside any lock.

### Where the Real Win Comes From

With the single lock (Phase 9), the call sequence is:
```
lock(gpu_mtx)     ← Worker B waits here while A does pre-stage + kernels + cleanup
  pre-stage: 18ms
  kernels: 1.8s
  cleanup: ~5ms
unlock(gpu_mtx)
  b_g2_msm: 484ms  ← GPU is idle
  epilogue: 5ms     ← GPU is idle
```

Worker B's `gpu_prove()` doesn't return until b_g2_msm finishes. The engine's GPU
worker loop sees `gpu_ms = 3.7s` (TIMELINE gpu_ms) and can't pick up the next job
until then.

With two locks + 3 workers:
```
Worker A: [compute:1.8s][b_g2+epilog:0.5s]
Worker B:          [compute:1.8s][b_g2+epilog:0.5s]
Worker C:                   [compute:1.8s][b_g2+epilog:0.5s]
```

Three workers rotate through compute_mtx. While Worker A does b_g2_msm (0.5s, no lock),
Worker B is already running kernels. Worker C's prep_msm (1.9s, no lock) overlaps with
B's compute. The GPU is never idle waiting for b_g2_msm.

**Theoretical throughput:** 1.82s/partition × 10 partitions = **18.2s/proof**

**Expected practical throughput** (accounting for prep_msm slightly exceeding compute,
synthesis queue gaps, and DDR5 contention): **28-34s/proof** at high concurrency.

## Files Modified

| File | Change |
|---|---|
| `extern/supraseal-c2/cuda/groth16_cuda.cu` | `gpu_locks` struct, two-lock protocol in `generate_groth16_proofs_c` |
| `extern/supraseal-c2/cuda/groth16_cuda.cu` | `create_gpu_mutex` / `destroy_gpu_mutex` updated for `gpu_locks` |

No changes needed in:
- `extern/supraseal-c2/src/lib.rs` (FFI stays `*mut c_void`)
- `extern/bellperson/src/groth16/prover/supraseal.rs` (opaque pointer unchanged)
- `extern/cuzk/cuzk-core/src/engine.rs` (same `alloc_gpu_mutex` call)

Config change:
- `gpu_workers_per_device = 3` in benchmark config files

## VRAM Allocation Inventory

All allocations happen inside compute_mtx (Phase 2). The mem_mtx (Phase 1) pre-allocates
d_a + d_bc only when VRAM permits; otherwise allocation moves inside compute_mtx.

| # | Buffer | Size | Alloc API | Lifetime within compute_mtx |
|---|---|---|---|---|
| 1 | d_a (NTT polynomial a) | 4 GiB | cudaMalloc (Phase 1) or gpu.Dmalloc (fallback) | Start → H-MSM end |
| 2 | d_bc (NTT polynomials b,c) | 8 GiB | cudaMalloc (Phase 1) or dev_ptr_t (fallback) | Start → post-NTT (freed early) |
| 3 | H-MSM d_buckets | ~184 MiB | gpu.Dmalloc | H-MSM only |
| 4 | H-MSM d_temp | ~1.6 GiB | cudaMallocAsync | H-MSM invoke() only |
| 5 | batch_add d_temp (×4) | ~0.65-1.3 GiB each | cudaMalloc | One at a time, sequential |
| 6 | tail_msm d_buckets (×3) | ~250 MiB total | gpu.Dmalloc | All coexist during tail phase |
| 7 | tail_msm d_temp (×3) | ~1.1 GiB each | cudaMallocAsync | One at a time, sequential |

Peak VRAM: 12 GiB during NTT phase (items 1+2), dropping to ~5.8 GiB after d_bc free.

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Pool trim insufficient without cudaDeviceSynchronize | Medium | Fallback to non-prestaged path | Already implemented; fallback works correctly |
| 3 workers' prep_msm saturating DDR5 bandwidth | Medium | Slower prep_msm cancels throughput gain | Monitor timing; can fall back to gw=2 |
| cudaMalloc failing in mem_mtx due to stale pool cache | Low | Fallback path handles gracefully | Pool trim + cudaMemGetInfo check |
| Correctness: race on shared state | None | N/A | Each worker has its own prep_msm_thread, provers[], results |

## Benchmark Plan

1. Correctness: `c=1 j=1` — verify proof passes
2. Isolation: `gw=3, c=3, j=1` — measure per-partition overhead
3. Pipeline: `gw=3, c=10, j=5` — measure steady-state throughput
4. High concurrency: `gw=3, c=15, j=10` — measure DDR5 contention impact
5. Compare against Phase 9 baseline (gw=1): 32.1s isolation, 41.3s high-concurrency
