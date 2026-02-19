# Phase 9 — PCIe Transfer Optimization

## Problem

Phase 8 achieves 100% GPU utilization at the scheduling level — the GPU is never idle
between partitions. However, within each partition's CUDA kernel region (~3.3s), GPU SM
utilization and power draw show periodic dips correlating with large PCIe traffic bursts
(50 GB/s RX, sometimes large TX). These dips represent time the SMs are stalled waiting
for data to arrive over the PCIe bus, reducing effective throughput below the theoretical
compute-only floor.

### Root Cause

Inside the GPU mutex, `generate_groth16_proofs_c` issues ~23.6 GiB of host-to-device
transfers per partition for a 32 GiB PoRep sector:

| Phase | Transfer | Size | Pinned? | Issue |
|---|---|---|---|---|
| NTT | a polynomial | ~2 GiB | **No** | Non-pinned: staged through 32 MB bounce buffer |
| NTT | b polynomial | ~2 GiB | **No** | Same — blocks CPU thread, half effective bandwidth |
| NTT | c polynomial | ~2 GiB | **No** | Same |
| H MSM | H SRS points (8 batches) | ~6 GiB | Yes | Per-batch `sync()` creates GPU idle gaps |
| Batch add | L SRS points | ~2.2 GiB | Yes | OK — double-buffered, single sync at end |
| Batch add | A SRS points | ~2.3 GiB | Yes | OK |
| Batch add | B_G1 SRS points | ~1.07 GiB | Yes | OK |
| Batch add | B_G2 SRS points | ~2.14 GiB | Yes | OK |
| Tail MSMs | L/A/B bases + scalars | ~3.9 GiB | **No** | Non-pinned `std::vector` |
| Tail MSMs | Bucket results DtoH | ~30 MiB | N/A | 3× destructor `sync()` at scope exit |
| **Total** | | **~23.6 GiB** | | |

Two distinct problems:

1. **Non-pinned HtoD transfers (a/b/c polynomials, ~6 GiB):** These originate from Rust
   `Vec<Fr>` allocations in the bellperson `Assignment` struct. When `cudaMemcpyAsync` is
   called with non-pinned source memory, CUDA internally copies through a small (~32 MB)
   pinned staging buffer in chunks. This serializes the transfer (each chunk: memcpy to
   staging → DMA to device → wait → next chunk), halving effective bandwidth and blocking
   the calling CPU thread. The NTTs cannot start until their input data arrives.

2. **Per-batch hard sync in Pippenger MSM:** The `msm_t::invoke()` loop (in
   `sppark/msm/pippenger.cuh`) processes H SRS points in ~8 batches. Each batch ends with:
   ```
   gpu[i&1].DtoH(ones, ...)   // small bucket results (~150 KiB)
   gpu[i&1].DtoH(res, ...)    // small bucket results (~1.5 MiB)
   gpu[i&1].sync()            // HARD SYNC — GPU fully idle
   ```
   The `sync()` ensures bucket results are on host before the CPU `collect()` in the next
   iteration. But this creates a gap: GPU finishes → DtoH → CPU collect → CPU issues next
   batch uploads + kernels → GPU starts. The gap is small per-batch (~1-5ms) but occurs 8×
   per H MSM plus 3-6× per tail MSM, totaling 50-200ms per partition.

### Why This Matters

Phase 8 achieved 37.4s/proof (10 partitions × ~3.75s CUDA time). If 5-10% of that CUDA
time is PCIe stall rather than compute, eliminating stalls could save 2-4s/proof —
breaking below the current "GPU-bound" plateau.

## Design

### Change 1: Pre-Stage a/b/c Before Mutex (Tier 1)

**Goal:** Move the 6 GiB of non-pinned a/b/c polynomial uploads out of the mutex region
entirely. The uploads overlap with the other worker's CUDA kernels via the GPU's
independent copy engine.

**Approach:** Before acquiring the GPU mutex:

1. **Pin host memory** via `cudaHostRegister(ptr, size, cudaHostRegisterDefault)`. This
   page-locks existing Rust-allocated pages in place, enabling DMA without bounce-buffer
   staging. Cost: ~1-5ms for 2 GiB. No data copy.

2. **Allocate device buffers** for a, b, c (total ~6 GiB: d_a = domain_size × 32 bytes,
   d_bc = domain_size × (1 + lot_of_memory) × 32 bytes for b and c combined).

3. **Issue async HtoD** on a dedicated upload CUDA stream. Record events after each
   upload + zero-pad completes.

4. **Acquire the mutex.** The uploads may still be in flight — that's fine, they use the
   copy engine which is independent of the compute engine.

5. **Inside the mutex**, the NTT functions wait on the upload events before starting
   compute. Since the uploads began potentially seconds earlier (while waiting for
   the mutex, or while the other worker was running CUDA), the data is usually already
   resident by the time the NTT starts.

6. **After mutex release**, unregister host pages.

**Memory budget (RTX 5070 Ti, 16 GiB VRAM):**

| Allocation | Size | When |
|---|---|---|
| d_a (domain_size scalars) | 2 GiB | Pre-mutex |
| d_bc (b + c, shared allocation) | 4 GiB (lot_of_memory=true) or 2 GiB | Pre-mutex |
| MSM buckets + d_temp | ~0.5 GiB | Inside mutex (H MSM) |
| batch_add d_temp | ~0.6 GiB | Inside mutex (batch add) |
| tail MSM buckets | ~0.5 GiB | Inside mutex (tail MSMs) |
| **Peak** | **~7.6 GiB** | Fits in 16 GiB |

Note: d_bc is freed (goes out of scope) after the NTT phase, before the H MSM. So the
actual peak during H MSM is only d_a (2 GiB) + MSM working memory (~0.5 GiB).

**Code changes:**

`groth16_ntt_h.cu`:
- Add `execute_ntts_prestaged()`: like `execute_ntts_single()` but skips HtoD, waits on
  a CUDA event instead.
- Add `execute_ntt_msm_h_prestaged()`: like `execute_ntt_msm_h()` but takes pre-allocated
  d_b/d_c device pointers and upload events. Skips `dev_ptr_t<fr_t> d_b(...)` allocation.

`groth16_cuda.cu`:
- Before `gpu_lock` acquisition (after `prep_msm_thread` launch):
  - `cudaHostRegister` for provers[c].a, .b, .c
  - Allocate d_a, d_bc on device
  - Create upload stream + events
  - Issue `cudaMemcpyAsync` + `cudaMemsetAsync` (zero-pad) + `cudaEventRecord` for each
- Inside per_gpu thread: call `execute_ntt_msm_h_prestaged()` instead of
  `execute_ntt_msm_h()`, passing pre-allocated buffers and events
- After mutex release + `prep_msm_thread.join()`:
  - `cudaHostUnregister` for a, b, c
  - Destroy upload stream and events
  - d_a freed via `gpu_ptr_t` destructor, d_bc via `dev_ptr_t` destructor
  
**Scope limitation:** Phase 7 always calls with `num_circuits = 1`. The pre-staging path
only needs to handle the single-circuit case initially. Multi-circuit (Phase 3 batching)
can loop: after each circuit's NTT completes, re-upload the next circuit's a/b/c into the
same device buffers. This is a future extension if batch mode is re-enabled.

**Backward compatibility:** The original `execute_ntt_msm_h()` is preserved. If the
pre-staging setup fails (e.g. `cudaHostRegister` returns an error on some systems), fall
back to the original code path with the upload inside the mutex.

### Change 2: Deferred Batch Sync in Pippenger MSM (Tier 3)

**Goal:** Eliminate per-batch GPU idle gaps in the Pippenger MSM by deferring the
`sync()` call so the GPU never stalls waiting for CPU to process previous batch results.

**Current pattern** (`sppark/msm/pippenger.cuh`, `msm_t::invoke()`):

```
for (i = 0; i < batch; i++) {
    gpu[i&1].wait(ev)
    batch_addition kernel           // GPU compute
    accumulate kernel               // GPU compute
    gpu[i&1].record(ev)
    integrate kernel                // GPU compute

    if (i < batch-1) {
        // Pipeline next batch: upload scalars + points on other streams
        gpu[2].HtoD(d_scalars, ...)
        gpu[2].wait(ev); digits kernel; gpu[2].record(ev)
        gpu[j].HtoD(d_points[j], ...)
    }

    if (i > 0)
        collect(p, res, ones)       // CPU: reduce previous batch's buckets
        out.add(p)

    gpu[i&1].DtoH(ones, ...)       // ~150 KiB
    gpu[i&1].DtoH(res, ...)        // ~1.5 MiB
    gpu[i&1].sync()                // *** HARD SYNC — GPU idle ***
}
collect(p, res, ones)               // reduce last batch
out.add(p)
```

The `sync()` at the end of each iteration creates a bubble: GPU must fully drain, DtoH
must complete, then CPU can proceed to the next iteration's kernel launches.

**Proposed pattern — deferred collect with double-buffered host results:**

```
std::vector<result_t> res_buf[2] = { vector(nwins), vector(nwins) };
std::vector<bucket_t> ones_buf[2] = { vector(ones_sz), vector(ones_sz) };

for (i = 0; i < batch; i++) {
    gpu[i&1].wait(ev)
    batch_addition, accumulate, integrate kernels   // GPU compute

    if (i < batch-1) {
        // Pipeline next batch uploads (unchanged)
        ...
    }

    if (i > 0) {
        gpu[(i-1)&1].sync()                         // sync PREVIOUS batch
        collect(p, res_buf[(i-1)&1], ones_buf[(i-1)&1])
        out.add(p)
    }

    gpu[i&1].DtoH(ones_buf[i&1], d_buckets + ...)  // DtoH to current buffer
    gpu[i&1].DtoH(res_buf[i&1], d_buckets, ...)    // DtoH to current buffer
    // *** NO sync here — deferred to next iteration ***
}
// Final batch
gpu[(batch-1)&1].sync()
collect(p, res_buf[(batch-1)&1], ones_buf[(batch-1)&1])
out.add(p)
```

**How this helps:** The sync is now for batch `i-1`, which already finished its GPU
compute. By the time we call `sync((i-1)&1)`, the DtoH has long completed (it was issued
at the end of the previous iteration, and the current iteration's GPU compute ran in
between). The sync completes instantly. Meanwhile, the current batch's DtoH is queued
but we don't wait for it — the next iteration will.

**The key insight:** By splitting the DtoH target into two host-side buffers (`res_buf[0]`
and `res_buf[1]`), we avoid the data race where the GPU is writing to the same `ones`/
`res` vectors that the CPU is reading. Each batch writes to its own buffer, and the CPU
reads from the other buffer after sync.

**Extra host memory:** 2 × `nwins` × `sizeof(result_t)` + 2 × `ones_size` × `sizeof(bucket_t)`.
For wbits=17, nwins=16: result_t = 512 buckets × 192 bytes = ~1.5 MiB per buffer;
ones_buf = sm_count × 8 × 192 bytes = ~150 KiB per buffer. Total: ~3.3 MiB extra. Negligible.

**Risk:** This modifies `sppark/msm/pippenger.cuh`, a third-party file (sppark is a
Supranational dependency vendored in `extern/supraseal/deps/sppark/`). The change is:
- Localized to the `invoke()` method
- Backward compatible (same API, same results, different scheduling)
- Numerically identical (same `collect()` calls, same accumulation order)
- Safe: double-buffered host targets prevent any CPU/GPU data race

## Verification

### Build
```bash
rm -rf extern/cuzk/target/release/build/supraseal-c2-*
cargo build --release -p cuzk-daemon
cargo build --release -p cuzk-bench --no-default-features
```

### Benchmark (compare to Phase 8 baseline)

```bash
# Daemon (terminal 1)
FIL_PROOFS_PARAMETER_CACHE=/data/zk/params \
  extern/cuzk/target/release/cuzk-daemon --config /dev/stdin <<'EOF'
[daemon]
listen = "0.0.0.0:9820"
[srs]
param_cache = "/data/zk/params"
preload = ["porep-32g"]
[synthesis]
partition_workers = 10
[gpus]
gpu_workers_per_device = 2
EOF

# Bench (terminal 2)
extern/cuzk/target/release/cuzk-bench batch \
  -t porep --c1 /data/32gbench/c1.json -c 5 -j 3

# Capture daemon log for TIMELINE + CUZK_TIMING analysis
```

### Expected Improvements

| Metric | Phase 8 Baseline | Phase 9 Expected | Delta |
|---|---|---|---|
| `gpu_total_ms` per partition | ~3300ms | ~3000-3150ms | -5-10% |
| `ntt_msm_h_ms` per partition | ~2430ms | ~2200-2300ms | Biggest win (a/b/c pre-staged) |
| Throughput (s/proof, pw=10) | 37.4s | ~34-36s | 4-9% improvement |

### Metrics to Compare

From `CUZK_TIMING` lines in daemon logs:

| Metric | Description | Expected Change |
|---|---|---|
| `ntt_msm_h_ms` | NTT + H MSM time | Decrease (no non-pinned HtoD stalls) |
| `batch_add_ms` | Batch additions time | Unchanged (already double-buffered) |
| `tail_msm_ms` | Tail MSM time | Slight decrease (less sync overhead) |
| `gpu_total_ms` | Total per-partition GPU time | Decrease (sum of above) |

From GPU monitoring:

| Metric | Description | Expected Change |
|---|---|---|
| GPU power draw dips | Periodic drops during kernel region | Fewer and smaller dips |
| PCIe RX bandwidth | Bursts during kernel region | Smoother (pre-staged) or unchanged |
| SM utilization | Average during kernel region | Higher (fewer stalls) |

## Files Modified

| File | Description |
|---|---|
| `extern/supraseal-c2/cuda/groth16_ntt_h.cu` | Add `execute_ntts_prestaged()` and `execute_ntt_msm_h_prestaged()` |
| `extern/supraseal-c2/cuda/groth16_cuda.cu` | Pre-stage a/b/c before mutex, cleanup after |
| `extern/supraseal/deps/sppark/msm/pippenger.cuh` | Deferred batch sync with double-buffered host results |

## PCIe Transfer Inventory (Reference)

Detailed breakdown of all host↔device transfers inside `generate_groth16_proofs_c` for
num_circuits=1, 32 GiB PoRep (domain_size=2^26, abc_size≈67M):

### Phase 1: NTT + H MSM (inside per_gpu thread)

**NTT (execute_ntt_msm_h → execute_ntts_single × 3):**

Each call does `stream.HtoD(d_inout, input.{a,b,c}, actual_size)` — a
`cudaMemcpyAsync(..., cudaMemcpyHostToDevice, stream)` of ~2 GiB from Rust `Vec<Fr>`
(non-pinned pageable memory). CUDA stages this through an internal ~32 MB pinned bounce
buffer, issuing many small DMA transfers. This blocks the calling CPU thread and the
GPU stream until complete.

- `input.a` → `d_a`: 67,108,863 × 32 bytes = **2.00 GiB HtoD** (non-pinned)
- `input.b` → `d_b`: 67,108,863 × 32 bytes = **2.00 GiB HtoD** (non-pinned)
- `input.c` → `d_c`: 67,108,863 × 32 bytes = **2.00 GiB HtoD** (non-pinned)
- Zero-pad: `cudaMemsetAsync` for (domain_size - abc_size) × 32 bytes ≈ 32 bytes (negligible)

Between NTTs, streams overlap via events (gpu[0] for a, gpu[1] for b, gpu[1+lom] for c),
but each individual HtoD is serialized due to non-pinned source.

**H MSM (msm_t::invoke with d_a as scalars, points_h as host SRS):**

The H SRS contains ~67M affine G1 points in `cudaHostAlloc`-pinned memory. The MSM
processes them in ~8 batches via batched Pippenger:

Per batch:
- `gpu[0].HtoD(d_points[d_off], &points_h[h_off], stride)`:
  stride ≈ 8,388,608 points × 96 bytes = **~768 MiB HtoD** (pinned, full bandwidth)
- `gpu[2].HtoD(d_scalars, ...)`: scalars already on device (d_a), so this path is
  skipped when using the `gpu_ptr_t` overload.
- `gpu[i&1].DtoH(ones, ...)`: sm_count × 8 × 192 bytes ≈ **150 KiB DtoH**
- `gpu[i&1].DtoH(res, ...)`: nwins × result_t ≈ **1.5 MiB DtoH**
- `gpu[i&1].sync()`: **hard synchronization — GPU idle until DtoH completes**

Totals for H MSM: **~6 GiB HtoD** (pinned), **~13 MiB DtoH**, 8 hard syncs.

### Phase 2: barrier.wait()

No PCIe. GPU idle waiting for CPU preprocessing.

### Phase 3: Batch Additions (execute_batch_addition × 4)

Each call allocates `dev_ptr_t<uint8_t>` device memory (synchronous `cudaMalloc`),
then streams SRS points with double-buffering and a single final sync:

**Bit vectors:** Per circuit per MSM type: ~2.8 MiB HtoD (non-pinned `std::vector<mask_t>`).

**L batch_addition:**
- L SRS: 23,887,871 × 96 bytes = **~2.19 GiB HtoD** (pinned, `slice_t`)
- Double-buffered batches, single final `gpu[sid].sync()` at end
- DtoH: nbuckets × num_circuits × sizeof(bucket_h) ≈ **150 KiB**

**A batch_addition:**
- A SRS: 25,165,731 × 96 bytes = **~2.30 GiB HtoD** (pinned)
- Same pattern, **~150 KiB DtoH**

**B_G1 batch_addition:**
- B_G1 SRS: 11,671,521 × 96 bytes = **~1.07 GiB HtoD** (pinned)
- **~150 KiB DtoH**

**B_G2 batch_addition:**
- B_G2 SRS: 11,671,521 × 192 bytes = **~2.14 GiB HtoD** (pinned)
- **~150 KiB DtoH**

Totals for batch additions: **~7.7 GiB HtoD** (pinned), **~600 KiB DtoH**, 4 syncs.

### Phase 4: Tail MSMs (msm_t::invoke × 3, L/A/B_G1)

Three separate `msm_t` objects, each constructed with `nullptr` (no pre-loaded points).
Bases and scalars streamed from host in batched Pippenger:

**L tail MSM:**
- Bases: `tail_msm_l_bases` (~12M × 96 bytes) = **~1.15 GiB HtoD** (non-pinned `std::vector`)
- Scalars: `split_vectors_l.tail_msm_scalars[c]` (~12M × 32 bytes) = **~384 MiB HtoD** (non-pinned)
- Bucket results: **~12 MiB DtoH**
- Multiple per-batch `sync()` calls

**A tail MSM:**
- Bases: **~1.2 GiB HtoD** (non-pinned), Scalars: **~400 MiB HtoD** (non-pinned)
- **~12 MiB DtoH**

**B_G1 tail MSM:**
- Bases: **~576 MiB HtoD** (non-pinned), Scalars: **~192 MiB HtoD** (non-pinned)
- **~6 MiB DtoH**

**Destructor overhead:** Each `msm_t` destructor calls `gpu.sync()` (syncs ALL streams)
+ `gpu.Dfree()` (synchronous device free). 3 destructors = 3 full GPU stalls.

Totals for tail MSMs: **~3.9 GiB HtoD** (non-pinned), **~30 MiB DtoH**, 9+ syncs.

### Grand Total per Partition

| | HtoD | DtoH | Syncs |
|---|---|---|---|
| NTT (a/b/c) | 6.0 GiB (non-pinned) | — | 0 (async on streams) |
| H MSM | 6.0 GiB (pinned) | 13 MiB | 8 |
| Batch additions | 7.7 GiB (pinned) | 600 KiB | 4 |
| Tail MSMs | 3.9 GiB (non-pinned) | 30 MiB | 9+ |
| **Total** | **23.6 GiB** | **44 MiB** | **21+** |

Phase 9 targets the 6 GiB non-pinned NTT transfers (Change 1) and the 8 H MSM syncs +
9+ tail MSM syncs (Change 2).
