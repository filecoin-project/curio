# cuzk Phase 2: Pipelined Synthesis ∥ GPU

**Goal**: GPU never sits idle waiting for synthesis. Overlap CPU circuit synthesis
with GPU NTT+MSM proving to achieve ~1.5-1.8x throughput over Phase 1.

**Prerequisite**: Minimal bellperson fork exposing the synthesis/GPU split point.

**Estimated effort**: 2-3 weeks.

---

## 1. The Problem

In Phase 0-1, each proof call is monolithic:

```
  seal_commit_phase2()
    ├── synthesis (CPU) ─── ~55-65s for 32G PoRep ───┐
    │                                                  │  Sequential
    └── GPU (NTT+MSM)  ─── ~30-40s for 32G PoRep ───┘
                                                       Total: ~90-100s
```

During synthesis the GPU is idle. During GPU compute the CPU is idle. With a
stream of N proofs, the GPU utilization is only `gpu_time / (synth_time + gpu_time)` ≈ 35-40%.

## 2. The Target

```
Time ──────────────────────────────────────────────────────────────────►
CPU:  [synth job 0] [synth job 1] [synth job 2] [synth job 3] ...
GPU:         idle   [prove job 0] [prove job 1] [prove job 2] ...
                     ↑────────────overlap───────────────↑
```

After the initial synthesis latency, one proof completes every
`max(synth_time, gpu_time)` seconds instead of `synth_time + gpu_time`.

For 32G PoRep: ~65s per proof vs ~100s = **1.5x throughput**.

When synthesis is faster than GPU (true for PoSt circuits with ~3M constraints):
the pipeline is GPU-bound, which is optimal.

---

## 3. Key Discovery: The Split Already Exists

Inside `bellperson-0.26.0/src/groth16/prover/supraseal.rs`, the prover is
**already structured** as two separate phases:

### Phase A: `synthesize_circuits_batch()` (private fn, CPU-only)

```rust
fn synthesize_circuits_batch<Scalar, C>(
    circuits: Vec<C>,
) -> Result<(
    Instant,
    Vec<ProvingAssignment<Scalar>>,    // a,b,c evaluations + density trackers
    Vec<Arc<Vec<Scalar>>>,              // input_assignments (moved out)
    Vec<Arc<Vec<Scalar>>>,              // aux_assignments (moved out)
), SynthesisError>
```

This function:
1. Runs `circuit.synthesize(&mut prover)` for each circuit in parallel (rayon)
2. Extracts `input_assignment` and `aux_assignment` via `std::mem::take()`
3. Returns the intermediate state

### Phase B: GPU proving (inline in `create_proof_batch_priority_inner`)

After `synthesize_circuits_batch` returns, the function:
1. Validates uniformity (all circuits same constraint count and density)
2. Packs raw pointers to a/b/c/input/aux into arrays
3. Calls `supraseal_c2::generate_groth16_proof()` — the FFI into C++ CUDA code

**The boundary is clean.** The intermediate state is fully captured in
`ProvingAssignment<Scalar>` + the extracted assignment vectors.

### What's Not Public

The types and functions we need are all crate-private or module-private:

| Item | Current Visibility | Needed |
|---|---|---|
| `ProvingAssignment<Scalar>` | `struct` (no pub) in `prover/mod.rs` | `pub struct` |
| `synthesize_circuits_batch()` | `fn` (private) in `prover/supraseal.rs` | `pub fn` |
| GPU phase code | Inline in `create_proof_batch_priority_inner` | Extract to `pub fn prove_from_assignments()` |
| `DensityTracker` | `pub` in `ec-gpu-gen` (already accessible) | No change needed |

---

## 4. Bellperson Fork: Minimal Changes

### 4.1 Fork Setup

Create a local fork of bellperson 0.26.0 in the curio workspace:

```
extern/bellperson/          # forked bellperson, tracked in git
├── Cargo.toml              # version "0.26.0-cuzk.1"
├── src/
│   └── groth16/
│       └── prover/
│           ├── mod.rs       # make ProvingAssignment pub
│           └── supraseal.rs # expose split API
```

The fork ONLY touches 3 files with visibility changes + 1 new function.
No logic changes to synthesis or proving.

### 4.2 Changes to `prover/mod.rs`

```diff
-struct ProvingAssignment<Scalar: PrimeField> {
+pub struct ProvingAssignment<Scalar: PrimeField> {
     // Density of queries
-    a_aux_density: DensityTracker,
-    b_input_density: DensityTracker,
-    b_aux_density: DensityTracker,
+    pub a_aux_density: DensityTracker,
+    pub b_input_density: DensityTracker,
+    pub b_aux_density: DensityTracker,
 
     // Evaluations of A, B, C polynomials
-    a: Vec<Scalar>,
-    b: Vec<Scalar>,
-    c: Vec<Scalar>,
+    pub a: Vec<Scalar>,
+    pub b: Vec<Scalar>,
+    pub c: Vec<Scalar>,
 
     // Assignments of variables
-    input_assignment: Vec<Scalar>,
-    aux_assignment: Vec<Scalar>,
+    pub input_assignment: Vec<Scalar>,
+    pub aux_assignment: Vec<Scalar>,
 }
```

Also re-export:
```diff
 // In groth16/mod.rs:
+pub use self::prover::ProvingAssignment;
```

### 4.3 Changes to `prover/supraseal.rs`

Make `synthesize_circuits_batch` public:
```diff
-fn synthesize_circuits_batch<Scalar, C>(
+pub fn synthesize_circuits_batch<Scalar, C>(
```

Add new `prove_from_assignments` function (extracted from `create_proof_batch_priority_inner`):

```rust
/// Prove from pre-synthesized assignments using SupraSeal GPU.
///
/// This is the "Phase B" of proving: takes the output of
/// `synthesize_circuits_batch()` and runs NTT+MSM on GPU.
///
/// # Arguments
/// * `provers` — ProvingAssignment instances (a/b/c + densities)
/// * `input_assignments` — Input witness vectors (moved out of provers)
/// * `aux_assignments` — Aux witness vectors (moved out of provers)
/// * `params` — SupraSeal SRS parameters
/// * `r_s`, `s_s` — Randomization vectors (one per circuit)
pub fn prove_from_assignments<E, P: ParameterSource<E>>(
    provers: Vec<ProvingAssignment<E::Fr>>,
    input_assignments: Vec<Arc<Vec<E::Fr>>>,
    aux_assignments: Vec<Arc<Vec<E::Fr>>>,
    params: P,
    r_s: Vec<E::Fr>,
    s_s: Vec<E::Fr>,
) -> Result<Vec<Proof<E>>, SynthesisError>
where
    E: MultiMillerLoop,
    E::Fr: GpuName,
    E::G1Affine: GpuName,
    E::G2Affine: GpuName,
{
    // ... the ~70 lines extracted from create_proof_batch_priority_inner,
    //     starting after the synthesize_circuits_batch() call.
    //     This is a pure extraction, no logic changes.
}
```

### 4.4 Re-exports in `groth16/mod.rs`

```diff
 #[cfg(feature = "cuda-supraseal")]
 pub use self::supraseal_params::SuprasealParameters;
+#[cfg(feature = "cuda-supraseal")]
+pub use self::prover::ProvingAssignment;
+#[cfg(feature = "cuda-supraseal")]
+pub use self::prover::supraseal::{synthesize_circuits_batch, prove_from_assignments};
```

### 4.5 Total Diff Size

~30 lines of visibility changes + ~80 lines for `prove_from_assignments` (extracted,
not new logic) = ~110 lines total. Zero behavioral changes.

### 4.6 Workspace Integration

In `extern/cuzk/Cargo.toml`:

```toml
[workspace.dependencies]
# Replace crates.io bellperson with local fork
bellperson = { path = "../../extern/bellperson", default-features = false }
```

`filecoin-proofs-api` and its transitive deps will pick up the patched bellperson
via `[patch.crates-io]` in the workspace:

```toml
[patch.crates-io]
bellperson = { path = "../bellperson" }
```

---

## 5. cuzk-core: Pipelined Prover Architecture

### 5.1 New Types

```rust
/// Intermediate state between synthesis and GPU proving.
/// This is the "synthesized witness" — all CPU work is done,
/// ready to be shipped to a GPU worker.
pub struct SynthesizedProof {
    /// Job metadata (ID, priority, proof kind, timings)
    pub job: ProofRequest,

    /// Which circuit type was synthesized (determines SRS)
    pub circuit_id: CircuitId,

    /// Bellperson proving assignments (a, b, c + density trackers)
    pub provers: Vec<ProvingAssignment<blstrs::Scalar>>,

    /// Input witness vectors
    pub input_assignments: Vec<Arc<Vec<blstrs::Scalar>>>,

    /// Aux witness vectors
    pub aux_assignments: Vec<Arc<Vec<blstrs::Scalar>>>,

    /// Randomization vectors
    pub r_s: Vec<blstrs::Scalar>,
    pub s_s: Vec<blstrs::Scalar>,

    /// Timing: how long synthesis took
    pub synthesis_duration: Duration,
}

/// Circuit identifier for SRS routing.
#[derive(Clone, Debug, Hash, Eq, PartialEq)]
pub enum CircuitId {
    Porep32G,
    Porep64G,
    WindowPost32G,
    WinningPost32G,
    SnapDeals32G,
    // ... other sizes as needed
}
```

### 5.2 Synthesis Phase (CPU Thread Pool)

Instead of calling `filecoin-proofs-api::seal_commit_phase2()` (which does synthesis+GPU
monolithically), cuzk-core will:

1. **Deserialize** the vanilla proof / C1 output (same as Phase 1)
2. **Build circuit instances** using `CompoundProof::circuit()` — the public trait method
3. **Call `bellperson::synthesize_circuits_batch()`** — the newly-public function
4. **Return `SynthesizedProof`** to the engine's pipeline queue

```rust
/// Synthesize a PoRep C2 proof (CPU-only).
///
/// Builds the circuit from the C1 output, runs witness generation,
/// returns the intermediate state ready for GPU proving.
pub fn synthesize_porep_c2(
    vanilla_proof_json: &[u8],
    sector_number: u64,
    miner_id: u64,
) -> Result<SynthesizedProof> {
    // 1. Parse C1 wrapper, decode base64, deserialize SealCommitPhase1Output
    //    (same as current prove_porep_c2 deserialization)

    // 2. Build circuit instances using CompoundProof::circuit()
    //    This replicates ~20 lines from filecoin-proofs seal_commit_phase2_circuit_proofs:
    //    - Construct stacked::PublicInputs
    //    - Construct compound_proof::SetupParams
    //    - Call StackedCompound::setup() to get PublicParams
    //    - For each partition, call StackedCompound::circuit() to build circuit instances

    // 3. Call bellperson::synthesize_circuits_batch(circuits)
    //    This is the CPU-bound step that was previously hidden inside
    //    create_random_proof_batch.

    // 4. Package result as SynthesizedProof
}
```

**For PoSt and SnapDeals**, the same pattern applies with their respective
`CompoundProof::circuit()` implementations.

### 5.3 GPU Phase (Worker Thread)

Each GPU worker pulls `SynthesizedProof` from the pipeline queue and:

```rust
/// Prove a pre-synthesized witness on GPU.
///
/// Calls bellperson::prove_from_assignments() with the SRS for this circuit type.
pub fn gpu_prove(
    synth: SynthesizedProof,
    srs: &SuprasealParameters<Bls12>,
) -> Result<Vec<u8>> {
    let proofs = bellperson::groth16::prove_from_assignments::<Bls12, _>(
        synth.provers,
        synth.input_assignments,
        synth.aux_assignments,
        srs,
        synth.r_s,
        synth.s_s,
    )?;

    // Serialize proof bytes (same as current Phase 1)
    // For PoRep with 10 partitions: 10 proofs, each 192 bytes
    // Verify and aggregate as needed
}
```

### 5.4 Pipeline Engine

The engine runs two async tasks connected by a bounded channel:

```rust
pub struct PipelinedEngine {
    /// Receives ProofRequests from the scheduler
    request_rx: mpsc::Receiver<ProofRequest>,

    /// Synthesis results waiting for GPU
    synth_queue: mpsc::Sender<SynthesizedProof>,
    synth_rx: mpsc::Receiver<SynthesizedProof>,

    /// CPU thread pool for synthesis (configurable concurrency)
    synth_pool: rayon::ThreadPool,

    /// Per-GPU workers
    gpu_workers: Vec<GpuWorker>,
}

impl PipelinedEngine {
    async fn run(&mut self) {
        // Synthesis task: pulls from request_rx, synthesizes, pushes to synth_queue
        let synth_handle = tokio::spawn(async move {
            while let Some(request) = request_rx.recv().await {
                // Spawn synthesis on the CPU thread pool
                let synth_result = synth_pool.spawn(move || {
                    match request.proof_kind {
                        ProofKind::PorepSealCommit => synthesize_porep_c2(...),
                        ProofKind::WindowPostPartition => synthesize_window_post(...),
                        ProofKind::WinningPost => synthesize_winning_post(...),
                        ProofKind::SnapDealsUpdate => synthesize_snap_deals(...),
                    }
                });
                synth_queue.send(synth_result.await).await;
            }
        });

        // GPU workers: each pulls from synth_rx, proves on its GPU
        for worker in &mut self.gpu_workers {
            let gpu_handle = tokio::spawn(async move {
                while let Some(synth) = synth_rx.recv().await {
                    // Set CUDA_VISIBLE_DEVICES for this worker
                    let srs = get_srs_for_circuit(&synth.circuit_id);
                    let result = gpu_prove(synth, srs);
                    // Send result back via job completion channel
                }
            });
        }
    }
}
```

### 5.5 Backpressure

The `synth_queue` channel is bounded (capacity = number of GPU workers + 1).
This prevents synthesis from racing too far ahead and consuming unbounded memory.

Each `SynthesizedProof` for a 32G PoRep contains:
- 10 × `ProvingAssignment` with ~130M-entry a/b/c vectors (32 bytes each) = ~12 GiB
- 10 × input_assignment + aux_assignment = ~4 GiB

Total: ~16 GiB per in-flight PoRep proof. With a bound of 2 (one being GPU-proved,
one pre-synthesized), that's ~32 GiB of intermediate state. Manageable on a 256 GiB
machine. On 128 GiB machines, the bound should be 1 (no look-ahead).

Configuration:
```toml
[pipeline]
# Max pre-synthesized proofs waiting for GPU. 0 = auto (num_gpus).
synthesis_lookahead = 1
```

---

## 6. SRS Management (Phase 2 Enhancement)

### 6.1 Direct SRS Loading

Phase 1 relies on `GROTH_PARAM_MEMORY_CACHE` (filecoin-proofs internal).
Phase 2 manages SRS directly using `SuprasealParameters`:

```rust
use bellperson::groth16::SuprasealParameters;
use blstrs::Bls12;

pub struct SrsManager {
    /// Loaded SRS entries, keyed by circuit ID
    loaded: HashMap<CircuitId, Arc<SuprasealParameters<Bls12>>>,

    /// Parameter file paths
    param_paths: HashMap<CircuitId, PathBuf>,

    /// Memory budget tracking
    pinned_bytes: u64,
    pinned_budget: u64,
}

impl SrsManager {
    /// Load SRS for a circuit type. Returns immediately if already loaded.
    pub fn ensure_loaded(&mut self, circuit_id: &CircuitId) -> Result<Arc<SuprasealParameters<Bls12>>> {
        if let Some(srs) = self.loaded.get(circuit_id) {
            return Ok(Arc::clone(srs));
        }

        let path = self.param_paths.get(circuit_id)
            .ok_or_else(|| anyhow!("no param path configured for {:?}", circuit_id))?;

        // SuprasealParameters::new() calls SRS::try_new() which:
        // - Opens the .params file
        // - cudaHostAlloc's pinned memory
        // - Deserializes BLS12-381 points into pinned memory
        let srs = Arc::new(SuprasealParameters::<Bls12>::new(path.clone()));
        self.loaded.insert(circuit_id.clone(), Arc::clone(&srs));

        Ok(srs)
    }
}
```

This gives cuzk explicit control over SRS lifetime, rather than relying on
the global `GROTH_PARAM_MEMORY_CACHE`.

### 6.2 Circuit ID to Parameter File Mapping

```rust
fn circuit_id_to_param_filename(id: &CircuitId) -> &'static str {
    match id {
        CircuitId::Porep32G => "v28-stacked-proof-of-replication-merkletree-poseidon_hasher-8-8-0-sha256_hasher-82a357d2f2ca81dc61bb45f4a762807aedee1b0a53fd6c4e77b46a01bfef7820.params",
        CircuitId::WinningPost32G => "v28-stacked-proof-of-replication-merkletree-poseidon_hasher-8-0-0-sha256_hasher-d739d2d29da tried89b2...params",  // exact name TBD
        CircuitId::WindowPost32G => "v28-proof-of-spacetime-fallback-merkletree-poseidon_hasher-8-8-0-sha256_hasher-...params",
        CircuitId::SnapDeals32G => "v28-empty-sector-update-merkletree-poseidon_hasher-8-8-0-sha256_hasher-...params",
    }
}
```

The exact filenames are determined by looking at `/data/zk/params/` contents.

---

## 7. Dependency Graph Changes

### 7.1 New Direct Dependencies for cuzk-core

To call `CompoundProof::circuit()` directly, cuzk-core needs:

```toml
[dependencies]
# Existing
filecoin-proofs-api = { workspace = true }

# New: for direct circuit construction
filecoin-proofs = { version = "19.0.1", default-features = false }
storage-proofs-core = { version = "19.0.1", default-features = false }
storage-proofs-porep = { version = "19.0.1", default-features = false }
storage-proofs-post = { version = "19.0.1", default-features = false }
storage-proofs-update = { version = "19.0.1", default-features = false }

# Forked bellperson (via workspace patch)
bellperson = { workspace = true }

# For BLS12-381 types
blstrs = "0.7"
```

These are already transitive dependencies (pulled in by `filecoin-proofs-api`).
Adding them as direct deps doesn't increase compile time or binary size.

### 7.2 Workspace Cargo.toml Patch

```toml
[patch.crates-io]
bellperson = { path = "../bellperson" }
```

This ensures ALL crates in the workspace (and their transitive deps) use
the forked bellperson with the split API.

---

## 8. Call Chain Comparison

### Phase 1 (monolithic)

```
cuzk-core::prover::prove_porep_c2()
  → filecoin_proofs_api::seal::seal_commit_phase2()
    → filecoin_proofs::seal_commit_phase2_circuit_proofs()
      → CompoundProof::circuit_proofs()              // builds circuits + proves
        → bellperson::create_random_proof_batch()
          → synthesize_circuits_batch() + GPU prove   // monolithic
```

### Phase 2 (pipelined)

```
SYNTHESIS (CPU worker):
  cuzk-core::pipeline::synthesize_porep_c2()
    → deserialize C1 output
    → StackedCompound::circuit() × 10 partitions     // build circuit instances
    → bellperson::synthesize_circuits_batch(circuits)  // CPU synthesis only
    → return SynthesizedProof

GPU (GPU worker):
  cuzk-core::pipeline::gpu_prove()
    → srs_manager.ensure_loaded(CircuitId::Porep32G)
    → bellperson::prove_from_assignments(...)          // GPU NTT+MSM only
    → return proof bytes
```

---

## 9. Alternative Considered: Patching filecoin-proofs Instead

We considered forking `filecoin-proofs` and `storage-proofs-core` to add
`CompoundProof::synthesize_circuits_only()`. This was rejected because:

1. `CompoundProof::circuit()` is already a public trait method — we can call it directly
2. The circuit construction code in `CompoundProof::circuit_proofs()` is only ~30 lines
   of boilerplate (build circuits, batch them, call bellperson)
3. Replicating those 30 lines in cuzk-core is simpler than maintaining 4 additional forks
4. `CompoundProof::circuit_proofs()` also handles verification, which cuzk may want to
   control separately

**We fork only bellperson** (1 crate, ~110 LOC changes) and write ~100 lines of
glue in cuzk-core to replicate the circuit-building logic.

---

## 10. Detailed Implementation Plan

### Step 1: Create bellperson fork (Week 1, Day 1-2)

1. Copy `bellperson-0.26.0` source from cargo registry to `extern/bellperson/`
2. Update `Cargo.toml` version to `"0.26.0-cuzk.1"`
3. Apply the 3 visibility changes to `prover/mod.rs`
4. Make `synthesize_circuits_batch()` pub in `prover/supraseal.rs`
5. Extract `prove_from_assignments()` from `create_proof_batch_priority_inner()`
6. Add re-exports in `groth16/mod.rs`
7. Verify: `cargo check` with `cuda-supraseal` feature
8. Verify: existing `create_proof_batch_priority` still works (calls both phases internally)

### Step 2: Wire up patched bellperson in cuzk workspace (Week 1, Day 2)

1. Add `[patch.crates-io]` for bellperson in workspace `Cargo.toml`
2. Add new deps to cuzk-core `Cargo.toml`
3. Verify: `cargo check --workspace --no-default-features` still passes
4. Verify: `cargo check --workspace --features cuda-supraseal` still passes

### Step 3: Implement SRS manager (Week 1, Day 3-4)

1. `cuzk-core/src/srs_manager.rs`:
   - `SrsManager` struct with `HashMap<CircuitId, Arc<SuprasealParameters<Bls12>>>`
   - `ensure_loaded()`, `evict()`, `preload()`
   - Circuit ID → param filename mapping
   - Memory budget tracking (track sizes based on file size, not actual pinned bytes)
2. Wire `SrsManager` into `Engine`
3. Tests: load/evict/preload lifecycle

### Step 4: Implement synthesis functions (Week 2, Day 1-3)

1. `cuzk-core/src/pipeline.rs`:
   - `SynthesizedProof` struct
   - `synthesize_porep_c2()` — builds circuits via `StackedCompound::circuit()`,
     calls `bellperson::synthesize_circuits_batch()`
   - `synthesize_winning_post()` — via `FallbackPoStCompound::circuit()`
   - `synthesize_window_post()` — via `FallbackPoStCompound::circuit()`
   - `synthesize_snap_deals()` — via `EmptySectorUpdateCompound::circuit()`
   - `gpu_prove()` — calls `bellperson::prove_from_assignments()`
2. Unit tests for each synthesis function (verify intermediate state is valid)

### Step 5: Implement pipelined engine (Week 2, Day 3-5)

1. Refactor `Engine` to support both monolithic (Phase 1 fallback) and pipelined modes
2. Add synthesis thread pool (rayon or dedicated tokio blocking pool)
3. Add `synth_queue` bounded channel between synthesis and GPU workers
4. Backpressure: configurable `synthesis_lookahead`
5. Timing breakdown: separate synthesis_ms and gpu_compute_ms in ProofTimings

### Step 6: Integration testing (Week 3, Day 1-3)

1. Build daemon with `--features cuda-supraseal`
2. Run single PoRep C2 proof through pipeline — verify proof bytes match Phase 1
3. Run batch of 5 PoRep C2 proofs — measure pipeline throughput vs Phase 1
4. Run mixed workload (PoRep + WinningPoSt) — verify priority preemption
5. Verify SRS manager loads correct params for each circuit type
6. Benchmark: pipeline throughput at various synthesis_lookahead values

### Step 7: Documentation and commit (Week 3, Day 3-5)

1. Update `cuzk-project.md` with Phase 2 completion notes
2. Update `AGENTS.md` discoveries section
3. Commit all changes

---

## 11. Memory Budget Analysis

### Per-Proof Intermediate State (32G PoRep)

Each PoRep partition has ~106M constraints (18 challenges × 11 layers × ~532K
constraints per label creation + inclusion proofs). Interactive PoRep 32G has
10 partitions, all processed in one batch (`MAX_GROTH16_BATCH_SIZE = 10`).

The `SynthesizedProof` for a 32G PoRep with 10 partitions contains:

| Data | Size per partition | × 10 partitions |
|---|---|---|
| `a` vector (~106M entries × 32B) | ~3.4 GiB | ~34 GiB |
| `b` vector (~106M entries × 32B) | ~3.4 GiB | ~34 GiB |
| `c` vector (~106M entries × 32B) | ~3.4 GiB | ~34 GiB |
| `input_assignment` (~small) | ~1 KB | ~10 KB |
| `aux_assignment` (~106M × 32B) | ~3.4 GiB | ~34 GiB |
| Density trackers (~106M bits) | ~13 MB | ~130 MB |
| **Total per partition** | **~13.6 GiB** | |
| **Total for 10 partitions** | | **~136 GiB** |

**This is large but matches current memory usage.** The existing monolithic
`seal_commit_phase2()` already allocates this same intermediate state — we're
just holding it across an async boundary instead of using it immediately.

**Critical implication for pipeline depth:**

With `synthesis_lookahead = 1`:
- 1 proof being GPU-proved: data consumed by supraseal_c2 (a/b/c read, then freed)
- 1 proof pre-synthesized waiting: ~136 GiB in heap
- Plus SRS: ~47 GiB pinned

Total: ~183 GiB. **Requires a 256 GiB machine for PoRep pipelining.**

With `synthesis_lookahead = 0` (no pipelining):
- Falls back to Phase 1 monolithic behavior
- Only ~136 GiB during proving (same as current)

**The pipeline lookahead should default to 0 for PoRep on machines with < 256 GiB RAM,
and 1 on larger machines.** The scheduler can be smarter: pipeline PoSt proofs
(which have small intermediate state) even on small machines, and only pipeline
PoRep on large machines.

### PoSt Circuits (Much Smaller)

WinningPoSt: ~3.5M constraints × 1 partition = ~0.45 GiB intermediate state
WindowPoSt: ~125M constraints × 1 partition = ~16 GiB intermediate state

PoSt proofs can always be pipelined, even on 128 GiB machines.

### Per-Circuit-Type Pipeline Strategy

| Circuit | Partitions | Intermediate Size | Pipeline on 128G | Pipeline on 256G |
|---|---|---|---|---|
| PoRep 32G | 10 | ~136 GiB | No | Yes (lookahead=1) |
| WinningPoSt | 1 | ~0.45 GiB | Yes | Yes |
| WindowPoSt | 1 | ~16 GiB | Yes | Yes |
| SnapDeals 32G | 16 | TBD (~10 GiB est.) | Likely | Yes |

### 11.1 Optimization: Per-Partition Pipeline (Reduces Memory 10x)

The current `CompoundProof::circuit_proofs()` batches all 10 PoRep partitions into
one `create_random_proof_batch` call. But each partition is an independent proof with
its own independent circuit. The final PoRep proof is just the concatenation of 10
individual partition proofs.

**Key insight:** We can pipeline at the **partition level** instead of the proof level:

```
CPU:  [synth P0] [synth P1] [synth P2] ... [synth P9]  [synth P0'] ...
GPU:       idle  [prove P0] [prove P1] ... [prove P9]   [prove P0'] ...
```

Each partition: ~13.6 GiB intermediate state.
Two in flight (1 synthesizing + 1 proving): ~27 GiB.

This means **PoRep pipelining works on 128 GiB machines** (27 GiB intermediate + 47 GiB SRS + working set < 128 GiB).

The tradeoff: supraseal's `generate_groth16_proof()` can process 1-10 circuits
in a single call. Processing them individually loses any batching benefit inside
the C++ code. However, in practice, supraseal processes circuits sequentially within
a batch (the GPU does NTT+MSM for each circuit one at a time), so there's minimal
benefit to batching at the bellperson level.

**Phase 2 implementation**: Start with per-partition pipelining. If profiling shows
that supraseal has significant per-call overhead, we can batch 2-3 partitions together
as a middle ground.

To implement per-partition pipelining:
1. Call `CompoundProof::circuit()` with `k = partition_index` to build one circuit
2. Call `bellperson::synthesize_circuits_batch(vec![circuit])` for that one partition
3. Queue the `SynthesizedProof` (single partition, ~13.6 GiB)
4. GPU worker calls `prove_from_assignments()` for that one partition
5. Collect 10 partition proofs, concatenate into final PoRep proof

This is the recommended approach for Phase 2.

---

## 12. Compatibility & Fallback

### Phase 1 Compatibility

The Phase 2 engine MUST support falling back to Phase 1 monolithic mode:
- When the bellperson fork is not available (building without patch)
- When configured: `pipeline.enabled = false`
- When running proof types not yet supported by the pipeline

The Phase 1 prover functions (`prove_porep_c2`, etc.) are retained unchanged.

### Proof Equivalence

Phase 2 produces identical proofs to Phase 1 (same bellperson code, same
randomization). The only change is execution order:
- Phase 1: `synthesize + prove` per batch
- Phase 2: `synthesize` then later `prove` per batch

The `r_s` and `s_s` randomization vectors are generated before synthesis
and carried through in `SynthesizedProof`.

---

## 13. Risks & Mitigations

| Risk | Mitigation |
|---|---|
| bellperson fork diverges from upstream | Pin to exact 0.26.0, minimal changes (visibility only + 1 extracted function) |
| Intermediate state too large for small machines | Configurable `synthesis_lookahead`, can be 0 (no pipelining) |
| SRS manager conflicts with `GROTH_PARAM_MEMORY_CACHE` | Phase 2 SRS manager loads via `SuprasealParameters::new()` directly; does not use filecoin-proofs cache at all |
| CompoundProof::circuit() requires types from storage-proofs-* | Already transitive deps; add as direct deps |
| Priority preemption during synthesis | Synthesis is cancellable (check flag between partitions); GPU phase is atomic (~30-40s, acceptable) |

---

## 14. Success Metrics

| Metric | Phase 1 Baseline | Phase 2 Target |
|---|---|---|
| PoRep C2 single proof | ~93s (warm SRS) | ~93s (no change for single) |
| PoRep C2 steady-state pipeline | ~93s/proof | ~65s/proof (1.4x) |
| GPU utilization (continuous load) | ~40% | ~70% |
| Synthesis timing breakdown | Not measured | Measured separately |
| Time to first proof (cold SRS) | ~117s | ~117s (no change) |

---

## 15. Files Created / Modified

### New files
- `extern/bellperson/` — forked bellperson (tracked in git)
- `extern/cuzk/cuzk-core/src/srs_manager.rs` — SRS lifecycle manager
- `extern/cuzk/cuzk-core/src/pipeline.rs` — pipelined synthesis/prove functions

### Modified files
- `extern/cuzk/Cargo.toml` — add `[patch.crates-io]` for bellperson, new workspace deps
- `extern/cuzk/cuzk-core/Cargo.toml` — add direct deps on storage-proofs-*, bellperson, blstrs
- `extern/cuzk/cuzk-core/src/engine.rs` — refactor for pipeline mode
- `extern/cuzk/cuzk-core/src/lib.rs` — export new modules
- `extern/cuzk/cuzk-core/src/types.rs` — add `SynthesizedProof`, `CircuitId`
- `extern/cuzk/cuzk-core/src/config.rs` — add pipeline config section
