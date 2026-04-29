# Proposal 6: Slotted Partition Pipeline & PCE Disk Persistence

**Goal**: Replace the batch-all-then-prove model with a fine-grained slotted pipeline
that overlaps partition synthesis with GPU proving, dramatically reducing both single-proof
latency and per-proof memory footprint. Also persist PCE matrices to disk for instant
daemon startup.

**Impact**:
- Single-proof latency: 69.5s → ~38-41s (1.7-1.8x faster)
- Per-proof working memory: 136 GiB → 14-27 GiB (5-10x less)
- Daemon startup: 47s PCE extraction → ~5s disk load
- GPU utilization: near-100% in steady state with ≥1 proof queued

**Scope**: Engine architecture change (slotted pipeline), PCE serialization, new config
knobs, proof assembly from incremental partitions.

---

## Table of Contents

- [Part A: PCE Disk Persistence](#part-a-pce-disk-persistence)
- [Part B: Slotted Partition Pipeline](#part-b-slotted-partition-pipeline)
  - [B.1 The Problem: Batch-All Memory Bloat](#b1-the-problem-batch-all-memory-bloat)
  - [B.2 Key Insight: Matched Per-Circuit Timing](#b2-key-insight-matched-per-circuit-timing)
  - [B.3 Pipeline Architecture](#b3-pipeline-architecture)
  - [B.4 Slot Size Analysis](#b4-slot-size-analysis)
  - [B.5 Multi-Proof Steady State](#b5-multi-proof-steady-state)
  - [B.6 Proof Assembly from Incremental Partitions](#b6-proof-assembly-from-incremental-partitions)
  - [B.7 Engine Changes](#b7-engine-changes)
  - [B.8 Multi-Sector Batching Interaction](#b8-multi-sector-batching-interaction)
  - [B.9 Memory Model](#b9-memory-model)
  - [B.10 Configuration](#b10-configuration)
- [Part C: Implementation Plan](#part-c-implementation-plan)

---

## Part A: PCE Disk Persistence

### A.1 Motivation

The Pre-Compiled Circuit (PCE) contains the R1CS constraint matrices (A, B, C) in CSR
format plus density bitmaps. This data is **deterministic** — every 32 GiB PoRep circuit
produces identical R1CS structure regardless of the witness values. The same is true for
WinningPoSt, WindowPoSt, and SnapDeals circuit types.

Current state:
- **First proof**: Must extract PCE by running `RecordingCS` synthesis (~47s for PoRep 32G)
- **Subsequent proofs**: PCE cached in `OnceLock` statics, zero cost
- **Daemon restart**: Must re-extract on first proof (47s penalty again)

### A.2 Design

Serialize `PreCompiledCircuit<Fr>` to disk using `bincode`. The type already derives
`serde::Serialize` and `serde::Deserialize`.

**File layout** (under `$FIL_PROOFS_PARAMETER_CACHE`):
```
/data/zk/params/
  pce-porep-32g.bin       # 25.7 GiB, PoRep 32 GiB
  pce-winning-post.bin    # ~small, WinningPoSt
  pce-window-post.bin     # ~small, WindowPoSt
  pce-snap-deals-32g.bin  # ~medium, SnapDeals
```

**Lifecycle**:
1. Daemon startup: attempt `load_pce_from_disk(circuit_id)` for each configured proof type
2. If file exists + valid header: `bincode::deserialize` → populate `OnceLock`
3. If file missing: leave `OnceLock` empty, first proof triggers extraction
4. After extraction: `bincode::serialize` → write to disk atomically (write tmp + rename)

**Header format** (prepended to bincode payload):
```rust
struct PceFileHeader {
    magic: [u8; 4],      // b"PCE\x01"
    version: u32,         // 1
    circuit_id: u32,      // CircuitId discriminant
    num_inputs: u32,
    num_aux: u32,
    num_constraints: u32,
    total_nnz: u64,
    blake3_hash: [u8; 32], // hash of the bincode payload
}
```

The header allows quick validation before loading 25+ GiB:
- Check magic + version (reject incompatible files from older builds)
- Check circuit dimensions match expected values
- Verify blake3 integrity hash

**Load time estimate**: 25.7 GiB / 5 GB/s (NVMe sequential) ≈ **5.1s**. With `mmap +
MAP_POPULATE`, the kernel can prefault pages while the daemon initializes other state.

**Alternative: mmap without deserialization**: If bincode layout matches in-memory layout
(it doesn't for Vec — bincode prefixes with length), we'd need a flat-file format. Not
worth the complexity for v1; regular deserialize into heap is fine at 5s.

### A.3 API

```rust
// In cuzk-pce crate:
pub fn save_to_disk(pce: &PreCompiledCircuit<Fr>, path: &Path) -> Result<()>;
pub fn load_from_disk(path: &Path) -> Result<PreCompiledCircuit<Fr>>;

// In cuzk-core pipeline.rs:
pub fn load_or_extract_pce(circuit_id: &CircuitId, param_cache: &Path) -> Result<()>;
```

`load_or_extract_pce` is called at daemon startup for each proof type in the config's
`preload` list. It checks the `OnceLock`, then tries disk, then falls back to lazy
extraction on first proof.

---

## Part B: Slotted Partition Pipeline

### B.1 The Problem: Batch-All Memory Bloat

The current PoRep C2 proving model synthesizes ALL 10 partitions at once, then sends them
to the GPU as a single batch:

```
CPU:  |====== synthesize 10 circuits (35.5s PCE, 50.4s old) ======|
GPU:                                                                |==== prove 10 circuits (34.0s) ====|
                                                                                                        ^ done
```

**Memory**: All 10 circuits' synthesis output (~136 GiB) must be held simultaneously in RAM
while the GPU processes them. For multi-sector batching (Phase 3), 20 circuits need ~272 GiB.

This creates two problems:
1. **Memory floor**: A machine needs ≥200 GiB RAM just for a single PoRep proof
2. **Latency**: Single proof takes 69.5s (35.5 + 34.0, sequential)
3. **GPU idle time**: GPU sits idle during the entire synthesis phase

### B.2 Key Insight: Matched Per-Circuit Timing

Measured data on RTX 5070 Ti (Phase 5 Wave 1):

| Metric | Per-circuit time | Source |
|---|---|---|
| GPU prove | **3.4s** | 34.0s / 10 circuits (near-zero fixed overhead) |
| PCE synthesis | **3.55s** | 35.5s / 10 circuits |
| Old synthesis | **5.0s** | 50.4s / 10 circuits |

GPU time scales linearly with circuit count (measured: 10 circuits = 34.0s, 20 circuits =
69.4s → 3.47s/circuit). **There is near-zero fixed GPU overhead per invocation.** This
means calling `gpu_prove()` with 1-2 circuits is not wasteful.

With PCE, synthesis and GPU are nearly **matched per-circuit** (3.55s vs 3.4s). This is
the ideal condition for fine-grained overlap.

### B.3 Pipeline Architecture

Instead of batch-all-then-prove, we use a **slotted pipeline** where synthesis and GPU
proving overlap at the partition granularity:

```
slot_size = 2 (2 partitions per GPU call):

CPU synth:  [0,1]─────[2,3]─────[4,5]─────[6,7]─────[8,9]
GPU prove:        [0,1]─────[2,3]─────[4,5]─────[6,7]─────[8,9]
            ├─7.1s─┤
                    ├─6.8s─┤
```

The pipeline has two stages connected by a bounded channel:
1. **Synth stage**: Synthesizes `slot_size` partitions, pushes `SynthesizedSlot` to channel
2. **GPU stage**: Pulls slot from channel, runs `gpu_prove()`, stores partial proof bytes

The channel capacity is 1 (single-buffered): the synth stage can be at most one slot ahead
of the GPU. This bounds the memory to `2 × slot_size × 13.6 GiB` (one slot being proved,
one pre-synthesized).

#### Data Flow

```
                     bounded channel (cap=1)
                     ┌──────────────┐
[synth thread] ────→ │ SynthSlot    │ ────→ [GPU thread]
  partition k..k+S   │ (S circuits) │         gpu_prove()
                     └──────────────┘         → partial_proofs[k..k+S]

                            ↓ when all slots done

                     [assemble_proof]
                     concatenate partial_proofs → final proof bytes
```

#### `SynthesizedSlot` type

```rust
/// A slot of synthesized partitions, ready for GPU proving.
struct SynthesizedSlot {
    /// The synthesized circuits for this slot.
    synth: SynthesizedProof,
    /// Which partition indices this slot covers.
    partition_range: Range<usize>,
}
```

This reuses the existing `SynthesizedProof` struct. The GPU thread calls `gpu_prove()`
on each slot independently.

### B.4 Slot Size Analysis

All timings for 32 GiB PoRep on RTX 5070 Ti with PCE:

| slot_size | # GPU calls | Synth/slot | GPU/slot | Pipeline total | Memory (2 slots) | GPU util |
|---|---|---|---|---|---|---|
| 1 | 10 | 3.55s | 3.4s | ~38.5s* | 27 GiB | 88% |
| 2 | 5 | 7.1s | 6.8s | ~41.0s | 54 GiB | 93% |
| 5 | 2 | 17.8s | 17.0s | ~52.8s | 136 GiB | 95% |
| 10 | 1 | 35.5s | 34.0s | 69.5s | 272 GiB** | 49% |

*Pipeline total = first_slot_synth + max(synth, gpu) × (N-1) + last_slot_gpu
**Current batch model: all 10 held + next proof's 10 pre-synth'd

Note: per-circuit synthesis time may increase slightly at slot_size=1 because rayon has
fewer circuits to parallelize across. The 10-circuit parallel PCE synthesis achieves good
L3 utilization because different circuits access different witness data. With slot_size=1,
all 96 cores work on a single circuit's 130M constraints — this should still be efficient
since the MatVec is row-parallel and the witness generation is already single-circuit.

**slot_size=2 is the sweet spot**: good GPU utilization (93%), 54 GiB working set (down
from 272 GiB), and 41s latency (down from 69.5s).

**slot_size=1 is viable** if memory is the primary constraint: 27 GiB working set, 38.5s
latency, with slightly lower GPU utilization.

### B.5 Multi-Proof Steady State

When multiple proofs are queued (common in production), the pipeline runs continuously:

```
slot_size=2, 3 proofs queued:

CPU: [P1.01][P1.23][P1.45][P1.67][P1.89][P2.01][P2.23]...
GPU:        [P1.01][P1.23][P1.45][P1.67][P1.89][P2.01]...
     ├7.1s─┤
```

Steady-state throughput: one slot every max(7.1s, 6.8s) = 7.1s.
Per-proof: 5 slots × 7.1s = **35.5s/proof** (synthesis-bound, matches PCE time).

Compare to current batch model steady-state:
- With pipeline overlap: max(35.5s synth, 34.0s GPU) = **35.5s/proof**
- Without overlap: 35.5 + 34.0 = **69.5s/proof**

The slotted pipeline achieves the **same throughput** as the current proof-level pipeline
(both synthesis-bound at 35.5s/proof) but with:
- **5x less memory** (54 GiB vs 272 GiB working set for slot_size=2)
- **1.7x lower single-proof latency** (41s vs 69.5s)
- **Earlier GPU start**: GPU begins after 7.1s instead of waiting 35.5s

### B.6 Proof Assembly from Incremental Partitions

Each GPU call produces `slot_size × 192` bytes of proof data. These must be concatenated
in partition order to form the final proof.

```rust
/// Accumulates partition proofs as they complete from GPU slots.
struct ProofAssembler {
    /// Total partitions expected (e.g., 10 for PoRep 32G).
    total_partitions: usize,
    /// Accumulated proof bytes, indexed by partition.
    /// None = not yet computed.
    partition_proofs: Vec<Option<Vec<u8>>>,
}

impl ProofAssembler {
    fn new(total_partitions: usize) -> Self { ... }

    /// Insert proof bytes for a range of partitions.
    fn insert(&mut self, partition_range: Range<usize>, proof_bytes: Vec<u8>) { ... }

    /// Check if all partitions are complete.
    fn is_complete(&self) -> bool { ... }

    /// Assemble final proof bytes (concatenate in order).
    fn assemble(self) -> Vec<u8> { ... }
}
```

For the simple case (slot_size divides num_partitions evenly, sequential processing),
the assembler just concatenates bytes as they arrive. The struct supports out-of-order
completion (needed for future multi-GPU partition parallelism).

### B.7 Engine Changes

The current engine architecture in `engine.rs`:

```
Scheduler → BatchCollector → process_batch() → synth_tx channel → GPU worker
```

The slotted pipeline changes `process_batch()` for PoRep C2:

**Current flow** (in `process_batch`):
1. `synthesize_porep_c2_batch()` → all 10 circuits at once
2. Send one `SynthesizedJob` to `synth_tx`
3. GPU worker calls `gpu_prove()` once

**New flow** (for slotted mode):
1. Deserialize C1 output, build setup params (same as current)
2. Spawn a **slotted pipeline task** that:
   a. Synthesis thread: builds + synthesizes `slot_size` circuits at a time, sends each
      `SynthesizedSlot` through a bounded(1) channel
   b. GPU thread: receives slots, calls `gpu_prove()` per slot, feeds proof bytes to
      `ProofAssembler`
   c. When all slots complete, assemble final proof and complete the job

This means `process_batch` for PoRep in slotted mode does NOT use the engine-level
`synth_tx` → GPU worker pipeline. Instead, it runs its own internal mini-pipeline and
sends the final result directly. This avoids architectural conflicts with the existing
proof-level pipeline.

**Alternative**: Integrate slots into the engine-level channel by sending individual
`SynthesizedSlot`s through `synth_tx` and having GPU workers understand partial proofs.
This is more invasive but enables multi-GPU partition parallelism within a single proof.
Deferred to a future phase.

#### Proposed new function

```rust
/// Slotted pipeline for a single PoRep C2 proof.
///
/// Overlaps partition synthesis with GPU proving at `slot_size` granularity.
/// Returns complete proof bytes when all partitions are proved.
pub fn prove_porep_c2_slotted(
    vanilla_proof_json: &[u8],
    sector_number: u64,
    miner_id: u64,
    params: &SuprasealParameters<Bls12>,
    slot_size: usize,
    job_id: &str,
) -> Result<(Vec<u8>, Duration, Duration)>  // (proof_bytes, synth_total, gpu_total)
```

This function encapsulates the entire slotted pipeline for a single sector. For
multi-sector batching, each sector runs its own slotted pipeline (potentially in
parallel across GPUs).

### B.8 Multi-Sector Batching Interaction

Phase 3 cross-sector batching currently synthesizes N×10 circuits at once and sends
them to the GPU in a single call. With the slotted pipeline, there are two options:

**Option A: Per-sector slotted pipelines (recommended for single GPU)**

Each sector gets its own slotted pipeline. With 2 sectors:
```
Sector 1: [S1.01][S1.23][S1.45][S1.67][S1.89]
Sector 2:                                      [S2.01][S2.23]...
GPU:             [S1.01][S1.23][S1.45][S1.67][S1.89][S2.01]...
```

Sequential sectors, but each sector benefits from CPU/GPU overlap. Memory: same as
single sector (54 GiB for slot_size=2). Total time for 2 sectors: ~82s (vs 125.4s
current batch model).

**Option B: Interleaved sector slots (for multi-GPU)**

With 2 GPUs and 2 sectors:
```
CPU: [S1.01][S2.01][S1.23][S2.23][S1.45][S2.45]...
GPU0:       [S1.01]       [S1.23]       [S1.45]...
GPU1:              [S2.01]       [S2.23]       ...
```

Each GPU processes one sector's slots. Synthesis alternates between sectors.
Memory: 2 × slot_size × 13.6 GiB per GPU = 108 GiB for slot_size=2, 2 GPUs.
Throughput: ~35.5s per sector (synthesis-bound, 2 sectors in parallel).

Option B is deferred — it requires engine-level multi-GPU slot routing.

### B.9 Memory Model

Comparison of memory models for PoRep C2 (single sector, single GPU):

| Model | Working set | PCE static | Total RAM needed |
|---|---|---|---|
| Current batch (10 circuits) | 136 GiB | 25.7 GiB | ~162 GiB |
| Current pipeline (2 proofs overlapped) | 272 GiB | 25.7 GiB | ~298 GiB |
| Slotted (slot=2, pipeline) | 54 GiB | 25.7 GiB | ~80 GiB |
| Slotted (slot=1, pipeline) | 27 GiB | 25.7 GiB | ~53 GiB |

With PCE from disk (Part A), the 25.7 GiB static memory can be mmap'd — it only needs
physical RAM for pages actually accessed during MatVec. Under memory pressure, the OS can
evict and re-fault PCE pages from disk, making it effectively "free" from the OOM
perspective.

For a production 8-GPU machine with slot_size=2:
- PCE static: 25.7 GiB (×1, mmap'd)
- Per-GPU working set: 54 GiB (×8) = 432 GiB
- SRS pinned: 47 GiB (×8) = 376 GiB
- **Total: ~834 GiB** (vs ~738 GiB current — slightly more due to per-GPU pipeline
  overhead, but each GPU is now independently productive)

For a **budget machine** (single GPU, 64 GiB RAM):
- PCE static: 25.7 GiB (mmap'd, ~5 GiB resident)
- slot_size=1 working: 13.6 GiB (1 slot) + 13.6 GiB (pre-synth'd) = 27 GiB
- SRS pinned: cannot fit 47 GiB — would need SRS streaming (separate proposal)
- This is tight but the slotted pipeline makes 64 GiB machines **possible** for PoRep,
  whereas the current batch model requires 200+ GiB minimum.

### B.10 Configuration

New config parameters in `PipelineConfig`:

```toml
[pipeline]
enabled = true
synthesis_lookahead = 1

# Slotted partition pipeline (Phase 6)
# Number of partitions to synthesize per GPU call.
# Lower = less memory, higher = slightly better GPU utilization.
# 0 = auto (batch all partitions, current behavior)
# 1 = minimal memory (~13.6 GiB per slot)
# 2 = recommended (good balance of memory and GPU util)
# 10 = equivalent to current batch-all behavior
slot_size = 2
```

The `slot_size` config interacts with `max_batch_size`:
- `slot_size = 0` (auto): Use batch-all for backward compatibility
- `slot_size > 0`: Enable slotted pipeline for PoRep C2 and SnapDeals
- WinningPoSt and WindowPoSt are single-partition and bypass slotting

### B.11 Non-PoRep Circuit Types

| Circuit Type | Partitions | Slotted? | Notes |
|---|---|---|---|
| PoRep 32G | 10 | Yes | Primary target |
| PoRep 64G | 10 | Yes | Same structure |
| SnapDeals 32G | 16 | Yes | Even more benefit (16 partitions) |
| WinningPoSt | 1 | No | Single partition, no benefit |
| WindowPoSt | 1 | No | Single partition, no benefit |

---

## Part C: Implementation Plan

### C.1 Phase Ordering

1. **PCE disk persistence** (Part A) — 1 session
   - Add `save_to_disk()` / `load_from_disk()` to cuzk-pce
   - Wire into daemon startup via `load_or_extract_pce()`
   - Background save after first extraction
   - Test: daemon restart loads PCE in ~5s instead of extracting in 47s

2. **Slotted pipeline core** — 1-2 sessions
   - `ProofAssembler` struct
   - `prove_porep_c2_slotted()` function
   - Per-partition synthesis using existing `synthesize_porep_c2_partition()`
   - Internal bounded channel between synth and GPU threads
   - Benchmark: slot_size=1,2,5 vs batch-all

3. **Engine integration** — 1 session
   - New config parameter `slot_size`
   - Route PoRep C2 through `prove_porep_c2_slotted()` when `slot_size > 0`
   - PCE auto-extraction on first proof (background thread)
   - Update job tracking for slotted proofs

4. **SnapDeals support** — 0.5 session
   - Apply same pattern to 16-partition SnapDeals
   - Test with SnapDeals vanilla proofs

### C.2 Risk Assessment

| Risk | Likelihood | Mitigation |
|---|---|---|
| Per-circuit synth time increases at slot_size=1 (less rayon parallelism) | Medium | Benchmark; fallback to slot_size=2 |
| GPU fixed overhead higher than measured | Low | Data shows near-zero; benchmark slot_size=1 explicitly |
| Bincode format changes break PCE disk files | Medium | Version header + hash; delete stale files on version mismatch |
| `prove_from_assignments` overhead for small batches | Low | Already tested with num_circuits=1 (WinningPoSt uses it) |
| Memory fragmentation from many small allocs | Medium | malloc_trim after each slot; monitor RSS delta |

### C.3 Compatibility

The slotted pipeline is **backward compatible**:
- `slot_size = 0` preserves current batch-all behavior
- `slot_size = 10` is equivalent to batch-all (single GPU call with all partitions)
- PCE disk loading is opportunistic (missing file = extract on first proof)
- All existing bench subcommands continue to work

### C.4 Testing Strategy

1. **Correctness**: Run slotted pipeline, verify proof bytes match batch-all output
   (proofs are randomized by r/s values, so compare using `groth16::verify`)
2. **Memory**: Track RSS at each slot transition, verify working set matches prediction
3. **Latency**: Measure single-proof E2E time for slot_size=1,2,5 vs batch-all
4. **Throughput**: Queue 3+ proofs, measure steady-state throughput
5. **PCE persistence**: Verify load-from-disk produces identical proofs to extract-fresh

---

## Appendix: Timing Derivations

All numbers from RTX 5070 Ti measurements (cuzk-project.md §14).

### Single-proof latency formula

For N partitions and slot_size S:
- Number of GPU calls: ceil(N/S)
- Synth time per slot: S × 3.55s (PCE)
- GPU time per slot: S × 3.4s
- Pipeline total: synth_first_slot + max(synth_slot, gpu_slot) × (ceil(N/S) - 1) + gpu_last_slot
- = S×3.55 + max(S×3.55, S×3.4) × (ceil(N/S) - 1) + S×3.4
- = S×3.55 + S×3.55 × (ceil(N/S) - 1) + S×3.4
- = S×3.55 × ceil(N/S) + S×3.4

For N=10, S=2: 2×3.55×5 + 2×3.4 = 35.5 + 6.8 = **42.3s**
For N=10, S=1: 1×3.55×10 + 1×3.4 = 35.5 + 3.4 = **38.9s**
For N=10, S=5: 5×3.55×2 + 5×3.4 = 35.5 + 17.0 = **52.5s**
For N=10, S=10: 10×3.55×1 + 10×3.4 = 35.5 + 34.0 = **69.5s** (matches current)

### Steady-state throughput formula

With continuous proof queue:
- Throughput = one proof per N/S × max(synth_slot, gpu_slot)
- = N × max(3.55, 3.4)s
- = N × 3.55s
- = **35.5s/proof** regardless of slot_size

Slot size only affects **latency** and **memory**, not steady-state throughput.

### GPU utilization

Single proof: GPU_total / pipeline_total
- slot=1: 34.0 / 38.9 = 87%
- slot=2: 34.0 / 42.3 = 80%
- slot=10: 34.0 / 69.5 = 49%

Steady state (continuous queue):
- All slot sizes: GPU_total / max(synth_total, GPU_total) = 34.0/35.5 = **96%**
