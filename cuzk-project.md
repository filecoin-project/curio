# cuzk — Pipelined SNARK Proving Engine

**Location:** `extern/cuzk/`
**Language:** Rust (tokio async runtime, tonic gRPC)
**Deployment:** Library crate with exec-into-daemon mode, embeddable in Curio

---

## 1. What Is cuzk

cuzk is a persistent GPU-resident SNARK proving engine — a "proving server" analogous to how
vLLM/TensorRT serve inference. It accepts a pipeline of Filecoin proof requests (PoRep C2,
SnapDeals, WindowPoSt, WinningPoSt) over gRPC, manages Groth16 SRS parameter residency in a
tiered memory hierarchy, schedules work across GPUs with priority awareness, and returns proof
results.

### Why a Daemon

The current architecture (`lib/ffiselect/`) spawns a fresh child process per proof, each of
which:
1. Initializes a CUDA context
2. Loads and deserializes the SRS (~47 GiB for 32 GiB PoRep, 30-90 seconds)
3. Runs one proof
4. Exits (discarding all state)

This wastes 30-90 seconds per proof on SRS loading alone. A persistent daemon loads SRS once
and keeps it resident in CUDA-pinned host memory across proofs.

### Analogy to Inference Engines

| Inference Concept | cuzk Equivalent |
|---|---|
| Model weights | SRS / Groth16 parameters (~47 GiB PoRep, ~2 GiB WdPoSt, ~600 MB SnapDeals, ~11 MB WinPost) |
| Model loading/swapping | SRS loading/eviction from pinned memory |
| Inference request | Proof request (vanilla proof + proof type + sector ID) |
| KV cache / activations | Witness vectors, a/b/c evaluations, NTT intermediates |
| Continuous batching | Cross-sector proof batching (same circuit topology) |
| Prefill vs decode | Witness synthesis (CPU) vs GPU compute (NTT/MSM) |
| Multi-model serving | Multi-circuit-type serving (PoRep, WdPoSt, WinPost, SnapDeals) |

---

## 2. Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Curio (Go)                                 │
│   tasks/seal, tasks/window, tasks/snap, tasks/proofshare        │
│                          │                                       │
│                     gRPC client                                  │
│                  (lib/cuzk/client.go)                            │
└──────────────┬──────────────────────────────────────────────────┘
               │  gRPC (unix socket or TCP)
               │  Vanilla proof streamed inline (~50 MB for PoRep)
               ▼
┌─────────────────────────────────────────────────────────────────┐
│                      cuzk daemon                                 │
│                    (Rust, tokio + tonic)                         │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                 gRPC Server (tonic)                        │  │
│  │   SubmitProof / AwaitProof / Prove / GetStatus / Cancel   │  │
│  │   PreloadSRS / EvictSRS / GetMetrics                      │  │
│  └────────────────────────┬──────────────────────────────────┘  │
│                           │                                      │
│  ┌────────────────────────▼──────────────────────────────────┐  │
│  │                    Scheduler                               │  │
│  │                                                            │  │
│  │  • Priority queues: CRITICAL > HIGH > NORMAL > LOW         │  │
│  │  • Batch collector (same-circuit accumulation)             │  │
│  │  • GPU affinity tracking (prefer GPU with SRS loaded)      │  │
│  │  • Memory budget enforcement                               │  │
│  └────────┬──────────────────┬──────────────────┬────────────┘  │
│           │                  │                  │                │
│  ┌────────▼────────┐ ┌──────▼───────┐ ┌────────▼────────┐      │
│  │   GPU Worker 0  │ │ GPU Worker 1 │ │  GPU Worker N   │      │
│  │   (CUDA:0)      │ │ (CUDA:1)     │ │  (CUDA:N)       │      │
│  │                 │ │              │ │                 │      │
│  │ synthesis(CPU)  │ │              │ │                 │      │
│  │ NTT+MSM(GPU)   │ │              │ │                 │      │
│  └─────────────────┘ └──────────────┘ └─────────────────┘      │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │            SRS Memory Manager (global)                     │  │
│  │                                                            │  │
│  │  Hot:  CUDA pinned host RAM (ready for GPU DMA)            │  │
│  │  Warm: mmap'd file (OS page cache, fast re-pin)            │  │
│  │  Cold: Disk (/data/zk/params)                              │  │
│  │                                                            │  │
│  │  Budget: configurable pinned memory ceiling                │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Library / Binary Structure

```
extern/cuzk/
├── Cargo.toml                 # workspace root
├── cuzk-core/                 # core engine library
│   ├── Cargo.toml
│   └── src/
│       ├── lib.rs             # public API: Engine, Config, ProofRequest, ProofResult
│       ├── engine.rs          # Engine lifecycle: start / submit / await / shutdown
│       ├── scheduler.rs       # priority queues, batch collector, GPU affinity
│       ├── srs_manager.rs     # tiered SRS residency (hot/warm/cold)
│       ├── gpu_worker.rs      # per-GPU worker loop
│       ├── circuit.rs         # circuit type registry, proof-type → SRS mapping
│       ├── prover.rs          # calls into filecoin-proofs-api (Phase 0) or split API (Phase 2+)
│       └── metrics.rs         # counters, histograms
├── cuzk-proto/                # protobuf definitions
│   ├── Cargo.toml
│   ├── build.rs
│   └── proto/
│       └── cuzk/v1/
│           └── proving.proto
├── cuzk-server/               # gRPC server (thin wrapper over core)
│   ├── Cargo.toml
│   └── src/
│       ├── lib.rs
│       └── service.rs         # tonic service implementation
├── cuzk-daemon/               # standalone binary
│   ├── Cargo.toml
│   └── src/
│       └── main.rs            # CLI, config loading, signal handling
├── cuzk-bench/                # testing/benchmarking utility
│   ├── Cargo.toml
│   └── src/
│       └── main.rs            # CLI: single-prove, batch-prove, stress-test, gen-vanilla
└── cuzk-ffi/                  # C ABI for Go embedding (future)
    ├── Cargo.toml
    └── src/
        └── lib.rs
```

### Deployment Modes

**Mode A — Exec into daemon:** Curio binary includes `cuzk-ffi` linked in. On startup with
`curio prove-daemon`, it exec's into the cuzk daemon. Same binary, different entry point
(like current `curio ffi` subcommand).

**Mode B — Spawn as child:** Curio runs `cuzk-daemon` as a persistent subprocess. Parent
connects over gRPC unix socket. If daemon crashes, Curio restarts it (SRS reload penalty).

**Mode C — External:** Operator runs `cuzk-daemon` independently. Curio configured with
socket path. Suitable for dedicated proving machines in a proofshare marketplace.

All modes use identical gRPC protocol — only process lifecycle differs.

---

## 3. Proof Types & Circuit Profiles

Each proof type has a fixed **circuit profile** that determines resource requirements:

| Proof Type | Constraints | FFT Domain | SRS File Size | Partitions | Priority |
|---|---|---|---|---|---|
| **PoRep C2 (32 GiB)** | ~130M | 2^27 | ~47 GiB | 10 | NORMAL |
| **PoRep C2 (64 GiB)** | ~130M | 2^27 | ~47 GiB | 10 | NORMAL |
| **SnapDeals (32 GiB)** | ~81M | 2^27 | ~626 MB | 16 | NORMAL |
| **WindowPoSt (32 GiB)** | ~125M | 2^27 | ~2.4 MB (VK only)* | 1/partition | HIGH |
| **WinningPoSt (32 GiB)** | ~3.5M | 2^22 | ~11 MB | 1 | CRITICAL |

*WindowPoSt 32 GiB `.params` file is not on disk in test env. Must be fetched — see setup.
The VK is 2.4 MB; the full params are multi-GiB.

**Key asymmetry:** PoRep SRS is enormous (~47 GiB pinned). Everything else is 1-3 orders of
magnitude smaller. This means:
- PoRep SRS dominates the memory budget
- PoSt SRS can always stay hot alongside PoRep
- SRS "swapping" only matters between PoRep ↔ SnapDeals

**Current supraseal status:** When built with `cuda-supraseal`, ALL proof types go through
the supraseal CUDA prover (bellperson compile-time switch). Without that feature, all use
bellperson's native prover (GPU via `ec-gpu-gen` or CPU fallback). cuzk Phase 0 uses whichever
backend the linked `filecoin-proofs-api` was built with.

---

## 4. gRPC API

```protobuf
syntax = "proto3";
package cuzk.v1;

service ProvingEngine {
  // Submit a proof request. Returns immediately with a job ID.
  rpc SubmitProof(SubmitProofRequest) returns (SubmitProofResponse);

  // Block until a submitted proof completes or is cancelled.
  rpc AwaitProof(AwaitProofRequest) returns (AwaitProofResponse);

  // Submit + Await in one call (convenience for synchronous clients).
  rpc Prove(ProveRequest) returns (ProveResponse);

  // Cancel a pending or in-progress proof.
  rpc CancelProof(CancelProofRequest) returns (CancelProofResponse);

  // Query daemon capabilities, loaded SRS, GPU status.
  rpc GetStatus(GetStatusRequest) returns (GetStatusResponse);

  // Prometheus-compatible metrics snapshot.
  rpc GetMetrics(GetMetricsRequest) returns (GetMetricsResponse);

  // Explicitly load an SRS into hot tier (pre-warm before proofs arrive).
  rpc PreloadSRS(PreloadSRSRequest) returns (PreloadSRSResponse);

  // Explicitly evict an SRS from hot tier.
  rpc EvictSRS(EvictSRSRequest) returns (EvictSRSResponse);
}

// --- Proof Type ---

enum ProofKind {
  PROOF_KIND_UNSPECIFIED = 0;
  POREP_SEAL_COMMIT = 1;
  SNAP_DEALS_UPDATE = 2;
  WINDOW_POST_PARTITION = 3;
  WINNING_POST = 4;
}

enum Priority {
  PRIORITY_UNSPECIFIED = 0;
  LOW = 1;
  NORMAL = 2;
  HIGH = 3;
  CRITICAL = 4;  // WinningPoSt: must complete within epoch
}

// --- Submit ---

message SubmitProofRequest {
  string request_id = 1;         // caller-provided idempotency key
  ProofKind proof_kind = 2;
  uint64 sector_size = 3;        // e.g. 34359738368 for 32 GiB
  uint64 registered_proof = 4;   // numeric RegisteredSealProof / RegisteredPoStProof
  Priority priority = 5;

  bytes vanilla_proof = 10;      // C1 output / vanilla proof (serialized JSON, ~50 MB for PoRep)

  uint64 sector_number = 20;
  uint64 miner_id = 21;
  bytes randomness = 22;
  uint32 partition_index = 23;   // for WindowPoSt per-partition

  // For SnapDeals: old sealed CID, new sealed CID, new unsealed CID
  bytes sector_key_cid = 30;
  bytes new_sealed_cid = 31;
  bytes new_unsealed_cid = 32;
}

message SubmitProofResponse {
  string job_id = 1;
  uint32 queue_position = 2;
  string assigned_gpu = 3;       // empty if not yet assigned
}

// --- Await ---

message AwaitProofRequest {
  string job_id = 1;
  uint64 timeout_ms = 2;         // 0 = wait forever
}

message AwaitProofResponse {
  string job_id = 1;

  enum Status {
    UNKNOWN = 0;
    COMPLETED = 1;
    FAILED = 2;
    CANCELLED = 3;
    TIMEOUT = 4;
  }
  Status status = 2;

  bytes proof = 3;               // serialized Groth16 proof bytes
  string error_message = 4;

  // Timing
  uint64 queue_wait_ms = 10;
  uint64 srs_load_ms = 11;
  uint64 synthesis_ms = 12;
  uint64 gpu_compute_ms = 13;
  uint64 total_ms = 14;
}

// --- Prove (combined Submit + Await) ---

message ProveRequest {
  SubmitProofRequest submit = 1;
}

message ProveResponse {
  AwaitProofResponse result = 1;
}

// --- Cancel ---

message CancelProofRequest {
  string job_id = 1;
}

message CancelProofResponse {
  bool was_running = 1;
}

// --- Status ---

message GetStatusRequest {}

message GetStatusResponse {
  repeated GPUStatus gpus = 1;
  repeated SRSStatus loaded_srs = 2;
  repeated QueueStatus queues = 3;
  uint64 total_proofs_completed = 4;
  uint64 total_proofs_failed = 5;
  uint64 uptime_seconds = 6;
  uint64 pinned_memory_bytes = 7;
  uint64 pinned_memory_limit_bytes = 8;
}

message GPUStatus {
  uint32 ordinal = 1;
  string name = 2;
  uint64 vram_total_bytes = 3;
  uint64 vram_free_bytes = 4;
  string current_job_id = 5;
  string current_proof_kind = 6;
}

message SRSStatus {
  string circuit_id = 1;        // e.g. "porep-32g", "wpost-32g", "winning-32g", "snap-32g"

  enum Tier {
    HOT = 0;    // CUDA pinned, ready for proving
    WARM = 1;   // mmap'd / page cache, needs re-pin
    COLD = 2;   // on disk only
  }
  Tier tier = 2;
  uint64 size_bytes = 3;
  uint32 ref_count = 4;         // in-flight proofs using this SRS
}

message QueueStatus {
  string proof_kind = 1;
  uint32 pending = 2;
  uint32 in_progress = 3;
}

// --- SRS Management ---

message PreloadSRSRequest {
  string circuit_id = 1;        // e.g. "porep-32g"
}

message PreloadSRSResponse {
  bool already_loaded = 1;
  uint64 load_time_ms = 2;
}

message EvictSRSRequest {
  string circuit_id = 1;
}

message EvictSRSResponse {
  bool was_loaded = 1;
  uint64 freed_bytes = 2;
}

// --- Metrics ---

message GetMetricsRequest {}

message GetMetricsResponse {
  string prometheus_text = 1;   // Prometheus exposition format
}
```

---

## 5. SRS Memory Manager

### Tiered Residency

| Tier | Storage | Promote Time | Description |
|---|---|---|---|
| **Hot** | `cudaHostAlloc` pinned RAM | Immediate | Deserialized BLS12-381 points, page-locked, ready for GPU DMA |
| **Warm** | `mmap` of `.params` file | ~2-10s (re-deserialize into pinned) | File is mmap'd; OS page cache may have it hot. Faster than cold because no disk seek. |
| **Cold** | Disk only | 30-90s | Full deserialization from `.params` file |

### Budget Management

```rust
pub struct SRSManager {
    /// Max bytes of CUDA pinned memory for SRS storage
    pinned_budget: u64,
    /// Currently pinned (hot) SRS entries
    hot: HashMap<CircuitId, Arc<SRSHandle>>,
    /// mmap'd but not pinned (warm) entries
    warm: HashMap<CircuitId, MmapHandle>,
    /// Reference counts: in-flight proofs using each SRS
    ref_counts: HashMap<CircuitId, AtomicU32>,
    /// LRU order for eviction decisions
    lru: VecDeque<CircuitId>,
}
```

**Eviction rules:**
1. Never evict SRS with `ref_count > 0`
2. Evict LRU with `ref_count == 0` from hot → warm (unpin, keep mmap)
3. If warm exceeds budget, evict LRU warm → cold (munmap)
4. Before proving: ensure required SRS is hot; promote from warm/cold if needed

### Implementation Strategy (Phase 0)

Phase 0 doesn't build a custom SRS manager. Instead, it leverages the existing
`GROTH_PARAM_MEMORY_CACHE` (`lazy_static HashMap<String, Arc<Bls12GrothParams>>`) inside
`filecoin-proofs`:

```rust
// At daemon startup, pre-populate the in-process cache:
fn preload_srs(porep_config: &PoRepConfig) -> Result<()> {
    // This internally calls read_cached_params() which:
    //   - with cuda-supraseal: creates SuprasealParameters → SRS{path, cache=false}
    //     → cudaHostAlloc ~47 GiB pinned, deserialize points
    //   - without cuda-supraseal: creates MappedParameters → mmap the .params file
    // Either way, the result is stored in GROTH_PARAM_MEMORY_CACHE forever.
    let _params = get_stacked_params::<SectorShape32GiB>(porep_config)?;
    Ok(())
}
```

Because the daemon process is long-lived, this cache persists across all proofs. Each
`seal_commit_phase2()` call hits the cache and skips disk loading.

**Limitation:** The `GROTH_PARAM_MEMORY_CACHE` is unbounded and never evicts. On a small
machine, loading both PoRep and SnapDeals SRS simultaneously would consume ~48 GiB of pinned
memory. Phase 1 adds explicit budget management.

### Small vs Large Machine Strategies

**Small machine (96-128 GiB RAM, 1 GPU):**
- Pinned budget: ~50 GiB
- Hot: one large SRS (PoRep OR SnapDeals) + all PoSt SRS (tiny)
- Switching PoRep ↔ SnapDeals costs 30-60s
- Scheduler groups same-type proofs to minimize SRS swaps
- Config: `scheduler.sort_by_type = true`

**Large machine (256+ GiB RAM, 2-8 GPUs):**
- Pinned budget: ~140 GiB
- Hot: PoRep + SnapDeals + all PoSt SRS simultaneously
- Zero swap overhead for any proof type
- GPUs can be dedicated to specific proof types via affinity config

---

## 6. Scheduler

### Priority Levels

| Priority | Proof Types | Behavior |
|---|---|---|
| CRITICAL | WinningPoSt | Jumps to front of queue. Preempts NORMAL synthesis (not GPU kernels). Must complete within ~30s. |
| HIGH | WindowPoSt | Ahead of NORMAL. Deadline-sensitive but more tolerant (~30 min window). |
| NORMAL | PoRep C2, SnapDeals | Default. Eligible for batching. |
| LOW | Background re-proofs, testing | Only runs when no other work pending. |

### Batch Collector

Same-circuit-type jobs accumulate in a batch collector. Flushed when:
- Batch reaches `max_batch_size` (default: 1 in Phase 0, configurable later)
- Timer exceeds `max_batch_wait_ms` (default: 10000)
- Higher-priority job arrives (flush current batch to make room)

Batching is only meaningful for PoRep (same 130M-constraint circuit, same SRS). WindowPoSt
partitions are already per-partition from Curio. WinningPoSt is always single.

### GPU Affinity

The scheduler tracks which SRS is currently loaded on each GPU worker. When dispatching:
1. Prefer a GPU that already has the required SRS hot (zero swap)
2. If no GPU has it, prefer the GPU with the smallest loaded SRS (fastest to evict+reload)
3. If all GPUs are busy, queue the job

On large multi-GPU machines, the operator can pin GPU affinity:
```toml
[gpus.affinity]
0 = "porep-32g"   # GPU 0 always proves PoRep
1 = "porep-32g"   # GPU 1 always proves PoRep
2 = "wpost-32g"   # GPU 2 for WindowPoSt
3 = "any"          # GPU 3 floats
```

---

## 7. GPU Worker Pipeline

### Phase 0: Sequential (no pipelining)

```
GPU Worker loop:
  1. Receive job from scheduler
  2. Ensure SRS is hot (may block on load — 0s if cached, 30-90s if cold)
  3. Call filecoin-proofs-api:
     - seal_commit_phase2()        for PoRep
     - generate_window_post()      for WindowPoSt
     - generate_winning_post()     for WinningPoSt
     - prove_replica_update2()     for SnapDeals
  4. Return proof bytes via channel
  5. Loop
```

This is the simplest possible implementation. The filecoin-proofs-api functions handle
circuit synthesis + GPU proving internally. The daemon's value is SRS residency + scheduling.

### Phase 2+: Pipelined (requires bellperson split API)

```
GPU Worker pipeline (2-stage):

  CPU Thread Pool          GPU (CUDA)
  ──────────────           ──────────
  synth(job N+1)    ||     NTT+MSM(job N)
  synth(job N+2)    ||     NTT+MSM(job N+1)
  ...
```

This requires exposing `synthesize_circuits_batch()` and `prove_from_assignments()` as
separate public APIs in bellperson. See Phase 2 in the roadmap.

---

## 8. cuzk-bench: Testing & Benchmarking Utility

A standalone CLI tool for testing and benchmarking the proving engine, with easy setup
for different proof types.

### Commands

```
cuzk-bench single          Run a single proof through the daemon
cuzk-bench batch           Submit N proofs and measure throughput
cuzk-bench stress          Continuous proving with mixed proof types
cuzk-bench gen-vanilla     Generate vanilla proofs for test data (wraps lotus-bench)
cuzk-bench status          Query daemon status
cuzk-bench preload         Pre-warm SRS
```

### Usage Examples

```bash
# Single PoRep C2 proof using golden c1.json
cuzk-bench single --type porep --c1 /data/32gbench/c1.json

# Batch of 5 PoRep proofs (reuses same c1 input — different random r,s each time)
cuzk-bench batch --type porep --c1 /data/32gbench/c1.json --count 5

# Mixed workload stress test
cuzk-bench stress \
  --porep-c1 /data/32gbench/c1.json \
  --porep-rate 1/120s \
  --wpost-vanilla /data/zk/testdata/wpost-vanilla.json \
  --wpost-rate 1/1800s \
  --winning-vanilla /data/zk/testdata/winning-vanilla.json \
  --winning-rate 1/30s \
  --duration 1h

# Generate vanilla proofs for WindowPoSt using lotus-bench
cuzk-bench gen-vanilla window-post \
  --sealed /data/32gbench/sealed \
  --cache /data/32gbench/cache \
  --comm-r bagboea4b5abcbx3jccdohrttzfneleehcpkmle4oltwuh4q3rcf5tpdodaoj6mtl \
  --sector-num 1 \
  --sector-size 32GiB \
  --out /data/zk/testdata/wpost-vanilla.json

# Generate vanilla proofs for WinningPoSt
cuzk-bench gen-vanilla winning-post \
  --sealed /data/32gbench/sealed \
  --cache /data/32gbench/cache \
  --comm-r bagboea4b5abcbx3jccdohrttzfneleehcpkmle4oltwuh4q3rcf5tpdodaoj6mtl \
  --sector-num 1 \
  --sector-size 32GiB \
  --out /data/zk/testdata/winning-vanilla.json

# Generate vanilla proofs for SnapDeals
cuzk-bench gen-vanilla snap-prove \
  --sealed /data/32gbench/sealed \
  --cache /data/32gbench/cache \
  --update /data/32gbench/update \
  --update-cache /data/32gbench/updatecache \
  --sector-key bagboea4b5abcbx3jccdohrttzfneleehcpkmle4oltwuh4q3rcf5tpdodaoj6mtl \
  --new-sealed bagboea4b5abcatbhenhvjwofejgf6tnb2rrcc75laoisrswubzey42d33npqn4ku \
  --new-unsealed baga6ea4seaqojszbta3apke462f63birspvjgdhifhedksycyns3fqpynrm6gki \
  --sector-size 32GiB \
  --out /data/zk/testdata/snap-vanilla.json

# Query daemon status
cuzk-bench status --addr unix:///run/curio/cuzk.sock
```

### gen-vanilla Implementation

The `gen-vanilla` subcommands wrap calls to `lotus-bench simple` commands:

```
gen-vanilla window-post  → lotus-bench simple window-post --sector-size 32GiB {sealed} {cache} {commR} {sectorNum}
                           (capture vanilla proof output, save to file)

gen-vanilla winning-post → lotus-bench simple winning-post --sector-size 32GiB --show-inputs {sealed} {cache} {commR} {sectorNum}
                           (capture vanilla proof output, save to file)

gen-vanilla snap-prove   → lotus-bench simple provereplicaupdate1 --sector-size 32GiB {sealed} {cache} {update} {updatecache} {sectorKey} {newSealed} {newUnsealed} {output.json}
                           (generates vanilla proof file directly)
```

Alternatively, `cuzk-bench gen-vanilla` can call directly into `filecoin-proofs-api` Rust
functions (since it's a Rust binary linking the same crates), bypassing lotus-bench entirely.
This is the preferred approach for tighter integration.

For PoRep C2, the golden `c1.json` (in `/data/32gbench/c1.json`) is already a
`SealCommitPhase1Output` and can be used directly — no generation needed.

---

## 9. Environment & Test Data Setup

### Params

Fetch Groth16 parameters using Curio:

```bash
# Fetch all params for 32 GiB sector size
FIL_PROOFS_PARAMETER_CACHE=/data/zk/params curio fetch-params 32GiB
```

This downloads all required `.params` and `.vk` files for 32 GiB sectors (PoRep, WindowPoSt,
WinningPoSt, SnapDeals) into `/data/zk/params/`. Approximate sizes:

| File | Size |
|---|---|
| PoRep 32 GiB `.params` | ~47 GiB |
| WindowPoSt 32 GiB `.params` | ~5 GiB (estimated) |
| WinningPoSt 32 GiB `.params` | ~11 MB |
| SnapDeals 32 GiB `.params` | ~626 MB |
| Inner product SRS (SnarkPack) | ~302 MB |
| Various `.vk` files | ~few MB total |

**Total: ~53 GiB for all 32 GiB proof types.**

### Data Directories

```
/data/zk/
├── params/                    # Groth16 parameter files (FIL_PROOFS_PARAMETER_CACHE)
│   ├── v28-stacked-proof-of-replication-*.params
│   ├── v28-proof-of-spacetime-fallback-*.params
│   ├── v28-empty-sector-update-*.params
│   └── v28-fil-inner-product-v1.srs
├── testdata/                  # Generated vanilla proofs for testing
│   ├── wpost-vanilla.json
│   ├── winning-vanilla.json
│   └── snap-vanilla.json
└── scratch/                   # Daemon working directory, logs, socket

/data/32gbench/                # Pre-existing golden test data
├── c1.json                    # PoRep C1 output (32 GiB, SectorNum=1, ~50 MB)
├── c1-single.json             # PoRep C1 output (single partition variant)
├── c1-8p.json                 # PoRep C1 output (8 partition variant)
├── sealed                     # Sealed sector (32 GiB)
├── unsealed                   # Unsealed sector (32 GiB)
├── cache/                     # Sealing cache (layers, trees, p_aux, t_aux)
├── update                     # SnapDeals updated sealed file
├── update-unsealed            # SnapDeals new unsealed data
├── updatecache/               # SnapDeals update cache (trees)
├── updatecache-curio/         # SnapDeals update cache (curio variant)
├── commdr.txt                 # Original sealed: commD and commR
├── update-commdr.txt          # Update: commD and commR
├── update-unsealed-pi.txt     # Update: new unsealed PieceInfo
├── pc1out.txt                 # PreCommit1 output (base64 JSON)
└── supra_seal.cfg             # Supraseal configuration reference
```

### Golden Data Details

**commdr.txt** (original sector):
```
d:baga6ea4seaqao7s73y24kcutaosvacpdjgfe5pw76ooefnyqw4ynr3d2y6x2mpq
r:bagboea4b5abcbx3jccdohrttzfneleehcpkmle4oltwuh4q3rcf5tpdodaoj6mtl
```

**update-commdr.txt** (SnapDeals sector):
```
d:baga6ea4seaqojszbta3apke462f63birspvjgdhifhedksycyns3fqpynrm6gki
r:bagboea4b5abcatbhenhvjwofejgf6tnb2rrcc75laoisrswubzey42d33npqn4ku
```

**c1.json structure:**
```json
{
  "SectorNum": 1,
  "Phase1Out": "<base64-encoded SealCommitPhase1Output, ~51MB>",
  "SectorSize": 34359738368
}
```
The `Phase1Out` decodes to JSON with `registered_proof: "StackedDrg32GiBV1_1"` containing
vanilla proofs for all 10 partitions.

### Vanilla Proof Generation for PoSt/SnapDeals

Use `lotus-bench simple` from `/data/32gbench/lotus-bench` (or `~/lotus/lotus-bench`):

```bash
LOTUS=/data/32gbench/lotus-bench
# or
LOTUS=~/lotus/lotus-bench

# WindowPoSt vanilla proof
$LOTUS simple window-post \
  --sector-size 32GiB \
  /data/32gbench/sealed \
  /data/32gbench/cache \
  bagboea4b5abcbx3jccdohrttzfneleehcpkmle4oltwuh4q3rcf5tpdodaoj6mtl \
  1

# WinningPoSt vanilla proof
$LOTUS simple winning-post \
  --sector-size 32GiB \
  /data/32gbench/sealed \
  /data/32gbench/cache \
  bagboea4b5abcbx3jccdohrttzfneleehcpkmle4oltwuh4q3rcf5tpdodaoj6mtl \
  1

# SnapDeals vanilla proof (writes to file)
$LOTUS simple provereplicaupdate1 \
  --sector-size 32GiB \
  /data/32gbench/sealed \
  /data/32gbench/cache \
  /data/32gbench/update \
  /data/32gbench/updatecache \
  bagboea4b5abcbx3jccdohrttzfneleehcpkmle4oltwuh4q3rcf5tpdodaoj6mtl \
  bagboea4b5abcatbhenhvjwofejgf6tnb2rrcc75laoisrswubzey42d33npqn4ku \
  baga6ea4seaqojszbta3apke462f63birspvjgdhifhedksycyns3fqpynrm6gki \
  /data/zk/testdata/snap-vanilla.json
```

Note: `window-post` and `winning-post` print the vanilla proof to stdout. `cuzk-bench
gen-vanilla` will capture this output and save to a file. `provereplicaupdate1` writes
directly to the output path argument.

---

## 10. Configuration

```toml
# /data/zk/cuzk.toml (or passed via --config)

[daemon]
listen = "unix:///run/curio/cuzk.sock"
# listen = "0.0.0.0:9820"          # TCP for remote proving
# listen = "unix:///data/zk/cuzk.sock"  # Alternative socket path

[memory]
# Maximum CUDA pinned memory for SRS residency.
# Small machine (96 GiB): "50GiB" — fits one PoRep SRS + all PoSt SRS
# Large machine (256 GiB): "140GiB" — fits PoRep + SnapDeals + all PoSt
pinned_budget = "50GiB"

# Maximum total memory (pinned + heap) for proof working set.
working_memory_budget = "80GiB"

[gpus]
# GPU ordinals to use. Empty = auto-detect all.
devices = []

# Optional GPU-to-circuit-type affinity. Omit for dynamic assignment.
# [gpus.affinity]
# 0 = "porep-32g"
# 1 = "any"

[scheduler]
# Max proofs to batch into a single GPU invocation (same circuit type).
# Phase 0: always 1. Phase 3+: 2-3 for PoRep.
max_batch_size = 1

# Max time (ms) to wait for batch to fill before flushing.
max_batch_wait_ms = 10000

# Reorder NORMAL-priority queue to group by circuit type.
# Reduces SRS swaps on machines with limited pinned memory.
sort_by_type = true

[synthesis]
# CPU threads for circuit synthesis. 0 = auto (num_cpus / 2).
threads = 0

[srs]
# Directory containing .params and .vk files.
param_cache = "/data/zk/params"

# SRS entries to preload at startup. Saves 30-90s on first proof.
preload = ["porep-32g"]
# preload = ["porep-32g", "wpost-32g", "winning-32g", "snap-32g"]

[logging]
level = "info"
# format = "json"
```

---

## 11. Phased Implementation Roadmap

### Phase 0: Scaffold (Weeks 1-3)

**"It proves a PoRep C2 with SRS residency, measurable via cuzk-bench."**

**Goal:** Working daemon + bench tool. Accepts PoRep C2 proof requests over gRPC, delegates to
existing `filecoin-proofs-api::seal_commit_phase2()` (zero upstream modifications), keeps SRS
resident via `GROTH_PARAM_MEMORY_CACHE` pre-population.

**Deliverables:**

| Crate | Contents |
|---|---|
| `cuzk-proto` | Protobuf definitions, tonic codegen. Only `Prove` and `GetStatus` RPCs needed initially. |
| `cuzk-core` | `Engine` with single-GPU sequential proving, trivial scheduler (FIFO), SRS preload at startup via filecoin-proofs cache. |
| `cuzk-server` | tonic gRPC server wrapping Engine. Unix socket listener. |
| `cuzk-daemon` | CLI binary: `cuzk-daemon --config cuzk.toml`. Loads config, starts server, handles SIGTERM. |
| `cuzk-bench` | CLI: `single`, `status` commands. Submit proof, measure time, print result. |

**How SRS residency works (no upstream changes):**

At startup, the daemon calls `get_stacked_params()` for each configured SRS entry. This
triggers `filecoin-proofs`' internal `GROTH_PARAM_MEMORY_CACHE` to load and retain the
parameters. All subsequent `seal_commit_phase2()` calls within the same process hit the cache.

```rust
// cuzk-core/src/prover.rs (Phase 0)
pub fn prove_porep_c2(
    vanilla_proof_json: &[u8],
    sector_num: u64,
    miner_id: u64,
) -> Result<Vec<u8>> {
    // Deserialize vanilla proof (SealCommitPhase1Output)
    let c1_output: SealCommitPhase1Output = serde_json::from_slice(vanilla_proof_json)?;

    // This hits GROTH_PARAM_MEMORY_CACHE — SRS already loaded at daemon startup
    let proof = seal_commit_phase2(c1_output, sector_id, registered_proof)?;

    Ok(proof.as_bytes())
}
```

**GPU device management:** Each GPU worker thread sets `CUDA_VISIBLE_DEVICES=<ordinal>` via
`std::env::set_var` before the first proof call (or uses the cuda runtime API to set device).
With a single worker per GPU, this is sufficient.

**What to test:**
```bash
# Start daemon (terminal 1)
FIL_PROOFS_PARAMETER_CACHE=/data/zk/params \
  cuzk-daemon --config /data/zk/cuzk.toml

# Run single proof (terminal 2)
cuzk-bench single \
  --addr unix:///run/curio/cuzk.sock \
  --type porep \
  --c1 /data/32gbench/c1.json

# Expected output:
# Proof completed in 185.3s (queue: 0.1s, srs: 0.0s, synthesis: 95.2s, gpu: 90.0s)
# proof: <hex bytes>

# Run second proof immediately (SRS cached — srs_load should be ~0)
cuzk-bench single --addr unix:///run/curio/cuzk.sock --type porep --c1 /data/32gbench/c1.json
# Expected: srs: 0.0s (vs 30-90s for baseline ffiselect)
```

**Estimated impact:** Eliminates 30-90s SRS load per proof ≈ **+25% throughput** on repeated proofs.

**Build dependencies:**
- `filecoin-proofs-api` (from Cargo registry or git)
- `supraseal-c2` (from `extern/supra_seal/c2/`)
- `tonic`, `prost` (gRPC)
- `tokio` (async runtime)
- `serde`, `serde_json` (vanilla proof serialization)
- `clap` (CLI parsing)

### Phase 1: Multi-Type + Scheduling (Weeks 3-5)

**"It handles all four proof types with priority scheduling."**

**Deliverables:**
1. Support all proof types: PoRep C2, SnapDeals, WindowPoSt, WinningPoSt
2. Priority queue scheduler (CRITICAL > HIGH > NORMAL)
3. Multi-GPU worker pool (one worker thread per GPU)
4. SRS warm tier (track which SRS is loaded per-worker, swap on demand)
5. GPU affinity tracking
6. `cuzk-bench gen-vanilla` command for generating test inputs
7. `cuzk-bench batch` command for throughput measurement

**Prover backends for each type:**
```rust
match proof_kind {
    PoRepSealCommit => seal_commit_phase2(c1_output, sector_id, proof_type),
    SnapDealsUpdate => generate_update_proof_with_vanilla(proof_type, old_sealed, new_sealed, new_unsealed, vanilla_proofs),
    WindowPostPartition => generate_single_partition_window_post_with_vanilla(proof_type, miner_id, randomness, vanilla, partition_idx),
    WinningPost => generate_winning_post_with_vanilla(proof_type, miner_id, randomness, vanilla),
}
```

**SRS swapping on small machines:**
- When a proof requires a different SRS than what's currently loaded:
  1. If pinned budget allows, load the new SRS alongside the old
  2. If budget exceeded, drop the old SRS reference → `Arc` refcount hits 0 → `Drop` calls
     `cudaFreeHost` → load new SRS
  3. Track which circuit_id each GPU worker has loaded
  4. Scheduler prefers routing to a worker with matching SRS

**Test scenarios:**
```bash
# Mixed workload
cuzk-bench batch --type porep --c1 /data/32gbench/c1.json --count 3
cuzk-bench single --type winning-post --vanilla /data/zk/testdata/winning-vanilla.json
cuzk-bench single --type window-post --vanilla /data/zk/testdata/wpost-vanilla.json --partition 0
cuzk-bench single --type snap --vanilla /data/zk/testdata/snap-vanilla.json
```

### Phase 2: Pipelining (Weeks 5-8)

**"GPU never sits idle waiting for synthesis."**

**Requires bellperson modification** — fork or `[patch]` to expose split API:

```rust
// New public API in bellperson (forked):
pub fn synthesize_circuits_batch<E, C>(
    circuits: Vec<C>,
) -> Result<(Vec<ProvingAssignment<E::Fr>>, Vec<Arc<Vec<E::Fr>>>, Vec<Arc<Vec<E::Fr>>>)>;

pub fn prove_from_assignments<E, P>(
    provers: Vec<ProvingAssignment<E::Fr>>,
    input_assignments: Vec<Arc<Vec<E::Fr>>>,
    aux_assignments: Vec<Arc<Vec<E::Fr>>>,
    params: P,
) -> Result<Vec<Proof<E>>>;
```

**Pipeline in GPU worker:**
```
Thread A (CPU):  [synth job N+1] ────────────────── [synth job N+2]
Thread B (GPU):  ── [prove job N] ────────────────── [prove job N+1]
                     ↑ overlap ↑
```

**Deliverables:**
1. Forked bellperson with split synthesis/prove API
2. GPU worker with 2-stage pipeline (synthesis || GPU compute)
3. CPU thread pool for synthesis (configurable concurrency)
4. Pipeline backpressure: if GPU is still busy, synthesis waits before starting next

**Estimated impact:** GPU utilization ~40% → ~70%. Throughput ~1.5x over Phase 0.

### Phase 3: Cross-Sector Batching (Weeks 8-11)

**"Multiple sectors proved in one GPU pass."**

**Requires:**
- Phase 2 (split API)
- Bump `max_num_circuits = 10` → 30+ in `groth16_srs.cuh:62`
- Batch collector in scheduler

**Deliverables:**
1. Batch collector: accumulate same-circuit-type proofs, flush on size/timeout
2. Batched proving: concatenate circuits from N sectors into one `generate_groth16_proofs_c` call
3. Parallelize B_G2 CPU MSMs (`groth16_cuda.cu:494-507`: sequential → `par_map`)
4. Adaptive batch sizing based on available RAM
5. `cuzk-bench batch` with configurable batch size for throughput comparison

**Estimated impact:** 2-3x throughput per GPU (batch=3).

### Phase 4: Compute Quick Wins (Weeks 11-14)

**"Faster per-proof via targeted optimizations."**

Cherry-pick high-impact items from c2-optimization-proposal-4:

| Item | Change | Impact | Effort |
|---|---|---|---|
| SmallVec for LC Indexer (P4-A1) | `bellpepper-core/src/lc.rs` | 15-30% synthesis speedup | ~5 LOC |
| Pre-size vectors (P4-A2) | `bellperson/prover/mod.rs` | 5-10% synthesis speedup | ~20 LOC |
| Pin a,b,c memory (P4-B1) | `groth16_cuda.cu:105` | +50% transfer bandwidth | ~20 LOC |
| Parallelize B_G2 MSMs (P4-A4) | `groth16_cuda.cu:494` | 50s → 5s | 1 LOC |
| batch_addition occupancy (P4-D2) | `batch_addition.cuh:119` | 5-12% MSM speedup | 1 LOC |
| Reuse GPU allocs (P4-B3) | `groth16_ntt_h.cu:92` | 10-50ms/proof | ~10 LOC |

**Estimated impact:** 30-40% faster per-proof on top of batching.

### Phase 5: PCE — Pre-Compiled Constraint Evaluator (Weeks 14-18)

**"Replace circuit synthesis with sparse matrix-vector multiply."**

The biggest single optimization. See `c2-optimization-proposal-5.md` for full design.

**Deliverables:**
1. `RecordingCS`: extract fixed R1CS matrices into CSR format (run once per circuit topology)
2. `WitnessCS`-based witness generation: only run `alloc()` closures, skip `enforce()`
3. Sparse MatVec evaluator: `a = A·w`, `b = B·w`, `c = C·w`
4. Coefficient-specialized MatVec: ±1 coefficients skip multiply, boolean witness fast-path
5. Pre-sorted SRS topology for split MSM

**Estimated impact:** 3-5x faster synthesis → ~10x total throughput over baseline.

### Summary Timeline

```
Week  1-3:  Phase 0 — Scaffold + cuzk-bench (SRS residency, PoRep C2 only)
Week  3-5:  Phase 1 — All proof types, priority scheduling, SRS swapping
Week  5-8:  Phase 2 — Pipelined synthesis || GPU (bellperson fork)
Week  8-11: Phase 3 — Cross-sector batching
Week 11-14: Phase 4 — Compute quick wins
Week 14-18: Phase 5 — Pre-compiled constraint evaluator
```

### Stopping Points & Cumulative Impact

| After Phase | Throughput vs Baseline | Peak RAM | Key Win |
|---|---|---|---|
| **Phase 0** | **1.3x** (measured) | 203 GiB | SRS residency, daemon scaffold |
| **Phase 1** | **1.3x** (+ scheduling) | 203 GiB | All proof types, priority |
| **Phase 2** | **1.27x** pipeline (measured) | 203 GiB | GPU pipelining (synth∥GPU overlap) |
| **Phase 3** | **1.42x** batch=2 (measured) | 360 GiB | Cross-sector batching (62.3s/proof) |
| **Phase 4** | **2-3x** (estimated) | ~200 GiB | Per-proof speedups |
| **Phase 5** | **5-10x** (estimated) | ~100 GiB | PCE eliminates synthesis |

*Note: Phase 2/3 measured on RTX 5070 Ti. Pipeline overlap is modest (1.27x) because
synthesis (55s) dominates GPU (34s) on this hardware. Phase 3 batch=2 amortizes synthesis
across sectors, giving 1.42x. Larger batch sizes and Phase 4/5 synthesis reduction will
compound significantly.*

---

## 12. Curio Integration Path

### Phase 0-1: Opt-in, Parallel to ffiselect

New Go client package `lib/cuzk/client.go` wraps gRPC calls. Task code gains a feature flag:

```go
// tasks/seal/task_porep.go (modified)
if p.cuzkClient != nil {
    proof, err = p.cuzkClient.PoRepC2(ctx, c1Output, sectorID, minerID)
} else {
    proof, err = ffiselect.FFISelect.SealCommitPhase2(ctx, c1Output, sectorID.Number, sectorID.Miner)
}
```

Both paths coexist. Operator chooses via config. ffiselect remains the default.

### Future: Replace ffiselect

Once cuzk is proven stable, it replaces ffiselect for all GPU proving. The
`lib/ffiselect/` child-process model is retired. Curio either embeds the cuzk engine
(Mode A) or manages the daemon lifecycle (Mode B).

### Curio Config Addition

```toml
# config.toml (Curio)
[Proving]
  # Use cuzk proving daemon instead of ffiselect child processes.
  # CuzkEnabled = false
  
  # Path to cuzk daemon socket. If empty, Curio spawns its own.
  # CuzkSocket = "unix:///run/curio/cuzk.sock"
  
  # If true, Curio starts and manages the cuzk daemon as a child process.
  # CuzkManaged = true
```

---

## 13. Key Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Language | Rust (tokio) | Direct access to filecoin-proofs/bellperson/supraseal. No CGO overhead on hot path. |
| RPC | gRPC (tonic + prost) | Strongly typed, streaming for ~50 MB vanilla proofs, mature Rust+Go ecosystem. |
| Vanilla proof transfer | Inline over gRPC | ~50 MB fits in a single gRPC message. No need for shared memory complexity. |
| SRS management (Phase 0) | Pre-populate `GROTH_PARAM_MEMORY_CACHE` | Zero upstream changes. The existing `lazy_static HashMap` keeps SRS alive in-process. |
| SRS management (Phase 1+) | Custom tiered manager with explicit budget | Inference engine pattern. Works for 96 GiB through 512 GiB machines. |
| Batching granularity | Per-circuit-type only | Circuits in a batch must share R1CS structure and SRS. |
| Preemption | Queue-level only | CUDA kernels can't be safely interrupted. GPU phase is bounded (~85s max). |
| Library vs binary | Library with exec mode | Max flexibility: embed, spawn, or run standalone. |
| Upstream modifications | Phase 0: zero. Phase 2+: bellperson fork. | Scaffold is immediately useful. Deeper opts require controlled fork. |
| Error handling | Retry at daemon level | Detect GPU OOM/fault, retry on same or different GPU. |
| Test data | Golden files in `/data/32gbench/` | Real 32 GiB sector data for authentic benchmarks. |
| Params location | `/data/zk/params` via `FIL_PROOFS_PARAMETER_CACHE` | Separate from default `/var/tmp/...` to avoid interference. |

---

## 14. E2E Test Results (RTX 5070 Ti, 32 GiB PoRep C2)

### Hardware
- **GPU**: NVIDIA RTX 5070 Ti (Blackwell sm_120, 16 GB VRAM, CUDA 13.1)
- **RAM**: 512 GiB DDR5
- **CPU**: ~142 cores used during synthesis

### Phase 2 Baseline (Single Proof, Pipeline, batch_size=1)

| Metric | Value |
|---|---|
| **Total** | 88.9s |
| Synthesis | 54.7s (10 partitions, ~130M constraints) |
| GPU | 34.0s |
| Queue | 0.2s |
| Proof size | 1920 bytes |
| Peak RSS | **202.9 GiB** |
| Idle RSS (SRS resident) | 45.0 GiB |
| SRS load (cold) | ~15s (from disk) |

### Phase 3 Test 1: Timeout Flush (batch_size=2, single proof submitted)

Verifies that the BatchCollector correctly flushes after `max_batch_wait_ms` when insufficient
proofs arrive to fill the batch.

| Metric | Value |
|---|---|
| **Total** | 120.2s |
| Queue (batch wait) | 30.3s (matches 30s timeout + 0.3s overhead) |
| Synthesis | 55.6s |
| GPU | 34.4s |
| Proof size | 1920 bytes |
| **Result** | **PASS** — batch timeout works correctly |

### Phase 3 Test 2: Batched Proofs (batch_size=2, 2 concurrent proofs)

Verifies cross-sector batching: 2 PoRep C2 proofs batched into a single 20-circuit
synthesis + GPU call, then split back into 2 individual proof results.

| Metric | Value |
|---|---|
| **Total (wall)** | 125.4s for 2 proofs |
| Queue | 0.5s (batch filled immediately) |
| Synthesis | 55.3s for **20 circuits** (2×10 partitions) |
| GPU | 69.4s for **20 circuits** |
| Proof sizes | 2 × 1920 bytes (3840 total, correctly split) |
| **Throughput** | **0.96 proofs/min (62.7s/proof)** |
| **Peak RSS** | **~360 GiB** |
| **Result** | **PASS** — cross-sector batching works correctly |

**Key insight**: Synthesis time is nearly identical for 10 vs 20 circuits (55.3s vs 54.7s)
because rayon saturates all CPU cores either way. The synthesis cost is fully amortized
across sectors. GPU time doubles linearly (69.4s vs 34.0s).

### Phase 3 Test 3: Overflow (batch_size=2, 3 concurrent proofs)

Verifies batch overflow: 3 proofs submitted, batch fills at 2, 3rd proof overflows to
next batch (flushed by timeout or next batch fill).

| Metric | Value |
|---|---|
| **Total (wall)** | 186.8s for 3 proofs |
| Proofs 1-2 (batched) | 133.9s each (synth=56.7s, GPU=76.6s) |
| Proof 3 (overflow, timeout flush) | 186.7s (queue=87.4s waiting, synth=58.1s, GPU=41.1s) |
| **Throughput** | **0.96 proofs/min (62.3s/proof)** |
| **Peak RSS** | **420.3 GiB** (batch-of-2 synth + 3rd proof synth overlap) |
| **Result** | **PASS** — overflow handling + pipeline overlap work correctly |

**Pipeline overlap observed**: The 3rd proof's synthesis started while the batch-of-2 GPU
phase was still running, demonstrating Phase 2 pipeline + Phase 3 batching working together.

### Phase 3 Test 4: Non-Batchable Type (WinningPoSt with batch_size=2)

Verifies that non-batchable proof types bypass the BatchCollector entirely.

| Metric | Value |
|---|---|
| **Total** | 0.8s |
| Queue | 88ms (no batch wait!) |
| Synthesis | 52ms (370K constraints vs 130M for PoRep) |
| GPU | 666ms |
| Proof size | 192 bytes |
| SRS load | 87ms (184 MiB WinningPoSt params, lazy-loaded) |
| **Result** | **PASS** — WinningPoSt bypasses BatchCollector correctly |

### Throughput Comparison

| Configuration | Throughput | Per-proof | Notes |
|---|---|---|---|
| **Phase 2 baseline** (batch=1) | ~0.67 proofs/min | 89s | Single proof at a time |
| **Phase 2 pipeline** (batch=1, 3 proofs, j=3) | ~0.84 proofs/min | ~71s | Synth∥GPU overlap |
| **Phase 3 batch=2** (2 proofs, j=2) | **0.96 proofs/min** | **62.7s** | Cross-sector batching |
| **Phase 3 batch=2** (3 proofs, j=3) | **0.96 proofs/min** | **62.3s** | Batch + pipeline |
| Speedup (batch=2 vs baseline) | | | **1.42x throughput** |

### Memory Comparison

| Configuration | Peak RSS | Idle RSS | Notes |
|---|---|---|---|
| Phase 2 baseline (batch=1) | 202.9 GiB | 45.0 GiB | 10 partitions |
| Phase 3 batch=2 | ~360 GiB | 45.0 GiB | 20 circuits |
| Phase 3 batch=2 + overlap | 420.3 GiB | 45.0 GiB | Batch + next proof synth |

---

## 15. Open Questions

1. **SnapDeals 16 partitions:** SnapDeals has 16 partitions vs PoRep's 10. With batching
   (Phase 3), this means 16+ circuits in one GPU call. The supraseal code has
   `max_num_circuits = 10` — needs bump. Is 16 safe? Verify GPU memory.

2. **Default Curio build vs cuda-supraseal:** Default Curio builds with `cuda` not
   `cuda-supraseal`. cuzk should support both backends. Phase 0 uses whichever
   `filecoin-proofs-api` was compiled with. The daemon build should use `cuda-supraseal`
   for best performance.

3. **SnarkPack aggregation:** Should cuzk handle proof aggregation? It's CPU-only, no GPU.
   Could be a separate proof_kind with NORMAL priority. Deferred — Curio handles this today.

4. **Remote proving over TCP:** The gRPC API supports TCP. Useful for proofshare marketplace.
   Deferred to after Phase 1 (need auth, TLS, proof routing).

5. **Multiple sector sizes:** 64 GiB sectors use the same SRS as 32 GiB (same circuit shape).
   2 KiB/8 MiB sectors are for testing only. Should cuzk dynamically handle all sizes?
   Phase 0: only 32 GiB. Phase 1: all sizes via `registered_proof` field.

---

## 16. Dependency Versions

cuzk links against the same Filecoin proving stack as Curio:

| Crate | Version | Source |
|---|---|---|
| `filecoin-proofs-api` | 19.0.0 | crates.io |
| `filecoin-proofs` | 19.0.1 | crates.io |
| `bellperson` | 0.26.0 | crates.io (Phase 0) / fork (Phase 2+) |
| `supraseal-c2` | 0.1.0 | `extern/supra_seal/c2/` (local path) |
| `storage-proofs-core` | 19.0.1 | crates.io |
| `storage-proofs-porep` | 19.0.1 | crates.io |
| `storage-proofs-post` | 19.0.1 | crates.io |
| `storage-proofs-update` | 19.0.1 | crates.io |
| `blst` | 0.3.x | crates.io |
| `sppark` | 0.1.x | crates.io |

**Build requirements:**
- CUDA toolkit (nvcc)
- Rust nightly or stable 1.70+ (for supraseal CUDA compilation)
- `protoc` (protobuf compiler, for tonic codegen)
- C++ compiler with C++17 support

---

## 17. File Reference

### Curio (Go) — Current Architecture

| File | Purpose |
|---|---|
| `lib/ffiselect/ffiselect.go:131-205` | Child process spawning per proof (to be replaced) |
| `lib/ffiselect/ffiselect.go:41-114` | GPU ordinal manager |
| `lib/ffiselect/ffidirect/ffi-direct.go` | FFI method delegation (1:1 wrappers) |
| `lib/ffi/sdr_funcs.go:367-397` | `PoRepSnark()`: C1 + C2 + verify orchestration |
| `lib/ffi/snap_funcs.go:426-439` | SnapDeals proving via ffiselect |
| `tasks/seal/task_porep.go:177-198` | PoRep task resource declaration (GPU:1, RAM:128 GiB) |
| `tasks/window/compute_do.go:553-568` | WindowPoSt per-partition proving |
| `tasks/winning/winning_task.go:529` | WinningPoSt proving |
| `tasks/snap/task_prove.go:138-156` | SnapDeals proving |
| `tasks/proofshare/task_prove.go:203-221` | Remote proof service (PoRep + Snap) |
| `cmd/curio/main.go:208-229` | `fetch-params` CLI command |

### FFI Layer (Rust/CGO)

| File | Purpose |
|---|---|
| `extern/filecoin-ffi/proofs.go:403-456` | Go: `SealCommitPhase2` + `AggregateSealProofs` |
| `extern/filecoin-ffi/rust/src/proofs/api.rs:283-438` | Rust FFI: commit + aggregate exports |
| `extern/filecoin-ffi/rust/Cargo.toml:77-98` | `cuda-supraseal` feature definition |

### Supraseal C2 (CUDA)

| File | Purpose |
|---|---|
| `extern/supra_seal/c2/src/lib.rs` | Rust FFI: `SRS`, `Assignment`, `generate_groth16_proof[s]` |
| `extern/supra_seal/c2/cuda/groth16_cuda.cu:104-108` | C++ entry: `generate_groth16_proofs_c()` |
| `extern/supra_seal/c2/cuda/groth16_cuda.cu:111-112` | Static mutex (serializes all proving) |
| `extern/supra_seal/c2/cuda/groth16_srs.cuh:62` | `max_num_circuits = 10` (must bump for batching) |
| `extern/supra_seal/c2/cuda/groth16_srs.cuh:450-462` | `create_SRS` C FFI with LRU cache |

### Bellperson (in ~/.cargo/registry/src/)

| File | Purpose |
|---|---|
| `bellperson-0.26.0/src/groth16/prover/mod.rs:1-4` | Conditional: native vs supraseal prover |
| `bellperson-0.26.0/src/groth16/prover/supraseal.rs` | Supraseal prover: synthesize + prove |
| `bellperson-0.26.0/src/groth16/supraseal_params.rs` | `SuprasealParameters` wrapping `SRS` |
| `bellperson-0.26.0/src/groth16/prover/mod.rs:130` | `ProvingAssignment::enforce()` |

### filecoin-proofs (in ~/.cargo/registry/src/)

| File | Purpose |
|---|---|
| `filecoin-proofs-19.0.1/src/caches.rs` | `GROTH_PARAM_MEMORY_CACHE` — the key to Phase 0 SRS residency |
| `storage-proofs-core-19.0.1/src/compound_proof.rs` | `MAX_GROTH16_BATCH_SIZE = 10` |
| `storage-proofs-core-19.0.1/src/parameter_cache.rs` | `parameter_cache_params_path()`, `FIL_PROOFS_PARAMETER_CACHE` |

---

## 18. Related Documents

| Document | Contents |
|---|---|
| `c2-improvement-background.md` | Full call chain trace, memory budget, circuit analysis |
| `c2-optimization-proposal-1.md` | Sequential partition synthesis (memory reduction) |
| `c2-optimization-proposal-2.md` | Persistent prover daemon (SRS residency) — original inspiration |
| `c2-optimization-proposal-3.md` | Cross-sector batching |
| `c2-optimization-proposal-4.md` | 18 compute-level optimizations |
| `c2-optimization-proposal-5.md` | PCE + SnarkPack transpositions |
| `c2-total-impact-assessment.md` | Combined 10x assessment, 13-week plan |
