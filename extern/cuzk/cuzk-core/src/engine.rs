//! Engine — the central coordinator of the cuzk proving daemon.
//!
//! Owns the scheduler, GPU workers, and SRS manager. Provides the public API
//! for submitting proofs, querying status, and managing SRS.
//!
//! Phase 2: Supports both monolithic (Phase 1) and pipelined proving modes.
//! In pipeline mode, a dedicated synthesis task pre-synthesizes proofs on CPU
//! and feeds them to GPU workers via a bounded channel. This allows CPU
//! synthesis of proof N+1 to overlap with GPU proving of proof N, yielding
//! ~1.6x throughput for PoRep C2 under continuous load (synthesis-bound at
//! ~55s/proof instead of ~91s sequential).
//!
//! Architecture (pipeline mode, per GPU):
//! ```text
//! Scheduler → [synthesis task] → bounded channel → [GPU worker] → completion
//!              (CPU-bound)       (cap=lookahead)    (GPU-bound)
//! ```

use anyhow::Result;
use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::sync::{Mutex, oneshot, watch, RwLock};
use tracing::{error, info, info_span, warn, Instrument};

// ─── Timeline Instrumentation ───────────────────────────────────────────────
//
// Records wall-clock events for waterfall visualization. All times are
// millisecond offsets from a shared epoch (engine start time). Events are
// emitted as structured log lines with target "timeline" so they can be
// easily grepped from the daemon log:
//
//   grep "^TIMELINE" /tmp/cuzk-daemon.log | sort -t, -k2 -n
//
// Event format: TIMELINE,<offset_ms>,<event>,<job_id>,<detail>
//
// Events: SYNTH_START, SYNTH_END, CHAN_SEND, GPU_PICKUP, GPU_START, GPU_END

/// Shared timeline epoch — set once at engine start.
static TIMELINE_EPOCH: std::sync::OnceLock<Instant> = std::sync::OnceLock::new();

/// Emit a timeline event. Uses eprintln for guaranteed ordering (bypasses
/// tracing buffering). The output goes to stderr which is captured in daemon logs.
fn timeline_event(event: &str, job_id: &str, detail: &str) {
    let epoch = TIMELINE_EPOCH.get().copied().unwrap_or_else(Instant::now);
    let offset_ms = epoch.elapsed().as_millis();
    eprintln!("TIMELINE,{},{},{},{}", offset_ms, event, job_id, detail);
}

use crate::config::Config;
use crate::prover;
use crate::scheduler::Scheduler;
use crate::srs_manager::{CircuitId, SrsManager};
use crate::types::*;

/// Per-worker state visible to the engine.
#[derive(Debug, Clone)]
pub struct WorkerState {
    pub worker_id: u32,
    pub gpu_ordinal: u32,
    /// The circuit ID (SRS) last used by this worker.
    pub last_circuit_id: Option<String>,
    /// Currently running job, if any.
    pub current_job: Option<(JobId, ProofKind)>,
}

/// Shared state for tracking in-flight and completed jobs.
struct JobTracker {
    /// Map from job_id → completion channel sender.
    pending: HashMap<JobId, Vec<oneshot::Sender<JobStatus>>>,
    /// Map from job_id → result (for completed jobs, retained for AwaitProof).
    completed: HashMap<JobId, JobStatus>,
    /// Per-worker state.
    workers: Vec<WorkerState>,
    /// Total proofs completed (global).
    total_completed: u64,
    /// Total proofs failed (global).
    total_failed: u64,
    /// Per proof-kind completed count.
    completed_by_kind: HashMap<ProofKind, u64>,
    /// Per proof-kind failed count.
    failed_by_kind: HashMap<ProofKind, u64>,
    /// Recent proof durations (ring buffer for histogram approximation).
    /// Stores (proof_kind, total_duration). Max 1000 entries.
    recent_durations: Vec<(ProofKind, Duration)>,
    /// Phase 7: Per-job partition assemblers for in-progress partitioned proofs.
    assemblers: HashMap<JobId, PartitionedJobState>,
}

impl JobTracker {
    fn new(workers: Vec<WorkerState>) -> Self {
        Self {
            pending: HashMap::new(),
            completed: HashMap::new(),
            workers,
            total_completed: 0,
            total_failed: 0,
            completed_by_kind: HashMap::new(),
            failed_by_kind: HashMap::new(),
            recent_durations: Vec::with_capacity(1000),
            assemblers: HashMap::new(),
        }
    }

    fn record_completion(&mut self, kind: ProofKind, duration: Duration) {
        self.total_completed += 1;
        *self.completed_by_kind.entry(kind).or_insert(0) += 1;
        if self.recent_durations.len() >= 1000 {
            self.recent_durations.remove(0);
        }
        self.recent_durations.push((kind, duration));
    }

    fn record_failure(&mut self, kind: ProofKind) {
        self.total_failed += 1;
        *self.failed_by_kind.entry(kind).or_insert(0) += 1;
    }
}

// ─── Phase 12: Result-processing helpers ─────────────────────────────────────
//
// Extracted from the GPU worker loop so they can be called from both the
// inline fallback path (non-supraseal) and the spawned finalizer task
// (Phase 12 split API).

/// Process a partition GPU result: route proof bytes to the assembler,
/// deliver the final assembled proof when all partitions are complete.
pub(crate) fn process_partition_result(
    t: &mut JobTracker,
    result: Result<Result<(Vec<u8>, Duration)>, tokio::task::JoinError>,
    parent_id: &JobId,
    p_idx: usize,
    synth_duration: Duration,
    proof_kind: ProofKind,
    worker_id: u32,
    circuit_id_str: &str,
    _submitted_at: Instant,
) {
    match result {
        Ok(Ok((proof_bytes, gpu_duration))) => {
            // Check if assembler still exists and isn't failed
            if let Some(state) = t.assemblers.get_mut(parent_id) {
                if state.failed {
                    info!(
                        job_id = %parent_id,
                        partition = p_idx,
                        "discarding GPU result for failed job"
                    );
                    #[cfg(target_os = "linux")]
                    unsafe { libc::malloc_trim(0); }
                    return;
                }

                state.assembler.insert(p_idx, proof_bytes);
                state.total_gpu_duration += gpu_duration;
                state.total_synth_duration += synth_duration;

                info!(
                    job_id = %parent_id,
                    partition = p_idx,
                    gpu_ms = gpu_duration.as_millis(),
                    filled = state.assembler.is_complete() as u8,
                    "partition GPU prove complete"
                );

                #[cfg(target_os = "linux")]
                unsafe { libc::malloc_trim(0); }

                if state.assembler.is_complete() {
                    let state = t.assemblers.remove(parent_id).unwrap();
                    let final_proof = state.assembler.assemble();
                    let total_elapsed = state.start_time.elapsed();

                    let mut timings = ProofTimings::default();
                    timings.synthesis = state.total_synth_duration;
                    timings.gpu_compute = state.total_gpu_duration;
                    timings.proving = total_elapsed;
                    timings.queue_wait = state.request.submitted_at.elapsed().saturating_sub(total_elapsed);
                    timings.total = state.request.submitted_at.elapsed();

                    info!(
                        job_id = %parent_id,
                        proof_len = final_proof.len(),
                        total_ms = timings.total.as_millis(),
                        synth_ms = timings.synthesis.as_millis(),
                        gpu_ms = timings.gpu_compute.as_millis(),
                        "Phase 7: all partitions complete, proof assembled"
                    );

                    if let Some(w) = t.workers.get_mut(worker_id as usize) {
                        w.last_circuit_id = Some(circuit_id_str.to_string());
                    }
                    t.record_completion(state.proof_kind, timings.total);

                    let status = JobStatus::Completed(ProofResult {
                        job_id: parent_id.clone(),
                        proof_kind: state.proof_kind,
                        proof_bytes: final_proof,
                        timings,
                    });
                    if let Some(senders) = t.pending.remove(parent_id) {
                        for sender in senders {
                            let _ = sender.send(status.clone());
                        }
                    }
                    t.completed.insert(parent_id.clone(), status);
                }
            } else {
                warn!(
                    job_id = %parent_id,
                    partition = p_idx,
                    "assembler not found, discarding partition result"
                );
            }
        }
        Ok(Err(e)) => {
            error!(
                error = %e,
                job_id = %parent_id,
                partition = p_idx,
                "partition GPU proving failed"
            );
            if let Some(state) = t.assemblers.get_mut(parent_id) {
                if !state.failed {
                    state.failed = true;
                    t.record_failure(proof_kind);
                    let status = JobStatus::Failed(
                        format!("partition {} GPU prove failed: {}", p_idx, e)
                    );
                    if let Some(senders) = t.pending.remove(parent_id) {
                        for sender in senders {
                            let _ = sender.send(status.clone());
                        }
                    }
                    t.completed.insert(parent_id.clone(), status);
                }
            }
        }
        Err(e) => {
            error!(
                error = %e,
                job_id = %parent_id,
                partition = p_idx,
                "partition GPU proving task panicked"
            );
            if let Some(state) = t.assemblers.get_mut(parent_id) {
                if !state.failed {
                    state.failed = true;
                    t.record_failure(proof_kind);
                    let status = JobStatus::Failed(
                        format!("partition {} GPU task panicked: {}", p_idx, e)
                    );
                    if let Some(senders) = t.pending.remove(parent_id) {
                        for sender in senders {
                            let _ = sender.send(status.clone());
                        }
                    }
                    t.completed.insert(parent_id.clone(), status);
                }
            }
        }
    }
}

/// Process a monolithic (non-partitioned) GPU result: deliver single or
/// batched proof results to callers.
pub(crate) fn process_monolithic_result(
    t: &mut JobTracker,
    result: Result<Result<(Vec<u8>, Duration)>, tokio::task::JoinError>,
    job_id: &JobId,
    proof_kind: ProofKind,
    worker_id: u32,
    circuit_id_str: &str,
    synth_duration: Duration,
    submitted_at: Instant,
    is_batched: bool,
    batch_requests: &[ProofRequest],
    sector_boundaries: &[usize],
) {
    match result {
        Ok(Ok((proof_bytes, gpu_duration))) if is_batched => {
            // Phase 3: Split batched proof output back into per-sector groups
            match crate::pipeline::split_batched_proofs(&proof_bytes, sector_boundaries) {
                Ok(per_sector_proofs) => {
                    info!(
                        total_proof_bytes = proof_bytes.len(),
                        num_sectors = per_sector_proofs.len(),
                        synth_ms = synth_duration.as_millis(),
                        gpu_ms = gpu_duration.as_millis(),
                        "batched proof completed — splitting results"
                    );

                    if let Some(w) = t.workers.get_mut(worker_id as usize) {
                        w.last_circuit_id = Some(circuit_id_str.to_string());
                    }

                    for (i, (sector_proof, req)) in per_sector_proofs.into_iter()
                        .zip(batch_requests.iter())
                        .enumerate()
                    {
                        let mut timings = ProofTimings::default();
                        timings.synthesis = synth_duration;
                        timings.gpu_compute = gpu_duration;
                        timings.proving = synth_duration + gpu_duration;
                        timings.queue_wait = req.submitted_at.elapsed().saturating_sub(timings.proving);
                        timings.total = req.submitted_at.elapsed();

                        info!(
                            sector_idx = i,
                            job_id = %req.job_id,
                            proof_len = sector_proof.len(),
                            total_ms = timings.total.as_millis(),
                            "sector proof delivered from batch"
                        );

                        t.record_completion(proof_kind, timings.total);

                        let status = JobStatus::Completed(ProofResult {
                            job_id: req.job_id.clone(),
                            proof_kind,
                            proof_bytes: sector_proof,
                            timings,
                        });

                        if let Some(senders) = t.pending.remove(&req.job_id) {
                            for sender in senders {
                                let _ = sender.send(status.clone());
                            }
                        }
                        t.completed.insert(req.job_id.clone(), status);
                    }
                }
                Err(e) => {
                    error!(error = %e, "failed to split batched proofs");
                    for req in batch_requests {
                        t.record_failure(proof_kind);
                        let status = JobStatus::Failed(format!("proof split failed: {}", e));
                        if let Some(senders) = t.pending.remove(&req.job_id) {
                            for sender in senders {
                                let _ = sender.send(status.clone());
                            }
                        }
                        t.completed.insert(req.job_id.clone(), status);
                    }
                }
            }
        }
        Ok(Ok((proof_bytes, gpu_duration))) => {
            // Single-sector proof (Phase 2 path)
            let mut timings = ProofTimings::default();
            timings.synthesis = synth_duration;
            timings.gpu_compute = gpu_duration;
            timings.proving = synth_duration + gpu_duration;
            timings.queue_wait = submitted_at.elapsed().saturating_sub(timings.proving);
            timings.total = submitted_at.elapsed();
            info!(
                proof_len = proof_bytes.len(),
                total_ms = timings.total.as_millis(),
                queue_ms = timings.queue_wait.as_millis(),
                synth_ms = timings.synthesis.as_millis(),
                gpu_ms = timings.gpu_compute.as_millis(),
                "proof completed successfully (pipeline)"
            );

            if let Some(w) = t.workers.get_mut(worker_id as usize) {
                w.last_circuit_id = Some(circuit_id_str.to_string());
            }
            t.record_completion(proof_kind, timings.total);

            let status = JobStatus::Completed(ProofResult {
                job_id: job_id.clone(),
                proof_kind,
                proof_bytes,
                timings,
            });
            if let Some(senders) = t.pending.remove(job_id) {
                for sender in senders {
                    let _ = sender.send(status.clone());
                }
            }
            t.completed.insert(job_id.clone(), status);
        }
        Ok(Err(e)) => {
            error!(error = %e, "GPU proving failed");
            let all_requests = if is_batched {
                batch_requests.to_vec()
            } else {
                vec![ProofRequest {
                    job_id: job_id.clone(),
                    ..Default::default()
                }]
            };
            for req in &all_requests {
                t.record_failure(proof_kind);
                let status = JobStatus::Failed(e.to_string());
                if let Some(senders) = t.pending.remove(&req.job_id) {
                    for sender in senders {
                        let _ = sender.send(status.clone());
                    }
                }
                t.completed.insert(req.job_id.clone(), status);
            }
        }
        Err(e) => {
            error!(error = %e, "GPU proving task panicked");
            let all_requests = if is_batched {
                batch_requests.to_vec()
            } else {
                vec![ProofRequest {
                    job_id: job_id.clone(),
                    ..Default::default()
                }]
            };
            for req in &all_requests {
                t.record_failure(proof_kind);
                let status = JobStatus::Failed(format!("GPU task panicked: {}", e));
                if let Some(senders) = t.pending.remove(&req.job_id) {
                    for sender in senders {
                        let _ = sender.send(status.clone());
                    }
                }
                t.completed.insert(req.job_id.clone(), status);
            }
        }
    }
}

/// A synthesized proof bundled with its job metadata, ready for GPU proving.
///
/// This is the message type sent through the pipeline channel from the
/// synthesis task to GPU workers.
///
/// Phase 3: Supports both single-sector and batched multi-sector proofs.
/// For batched proofs, `batch_requests` contains the individual sector
/// requests (with their job IDs for result routing), and `sector_boundaries`
/// holds the partition count per sector for splitting GPU output.
pub(crate) struct SynthesizedJob {
    /// The proof request for single-sector proofs.
    /// For batched proofs, this is the first request in the batch (used for proof_kind etc).
    pub request: ProofRequest,
    /// The synthesized proof (intermediate CPU state for GPU consumption).
    pub synth: crate::pipeline::SynthesizedProof,
    /// The SRS parameters to use for GPU proving.
    #[cfg(feature = "cuda-supraseal")]
    pub params: Arc<bellperson::groth16::SuprasealParameters<blstrs::Bls12>>,
    /// Circuit ID (for SRS affinity tracking).
    pub circuit_id: CircuitId,
    /// For Phase 3 batched proofs: the individual requests in this batch.
    /// Empty for single-sector proofs.
    pub batch_requests: Vec<ProofRequest>,
    /// For Phase 3 batched proofs: number of partitions per sector.
    /// Used by GPU worker to split proof output back into per-sector groups.
    /// Empty for single-sector proofs.
    pub sector_boundaries: Vec<usize>,

    // ── Phase 7: Per-partition dispatch fields ──

    /// Partition index within the sector (0-9 for PoRep 32G).
    /// None = monolithic job (legacy, non-partitioned).
    pub partition_index: Option<usize>,
    /// Total partitions for this proof (10 for PoRep 32G).
    pub total_partitions: Option<usize>,
    /// The original job_id that this partition belongs to.
    /// Used to route GPU results to the correct ProofAssembler.
    pub parent_job_id: Option<JobId>,
}

/// Phase 7: Tracks in-progress partitioned proof assembly.
///
/// Created when a partitioned PoRep dispatch begins, removed when all
/// partitions have been GPU-proved and assembled into the final proof.
struct PartitionedJobState {
    /// Collects per-partition proof bytes in order.
    assembler: crate::pipeline::ProofAssembler,
    /// Original request (for metadata in final proof result).
    request: ProofRequest,
    /// The proof kind (for stats recording).
    proof_kind: ProofKind,
    /// Accumulated synthesis duration across all partitions.
    total_synth_duration: Duration,
    /// Accumulated GPU duration across all partitions.
    total_gpu_duration: Duration,
    /// When dispatch began (for total elapsed time).
    start_time: Instant,
    /// Set to true if any partition synthesis or GPU prove fails.
    /// Subsequent partitions check this and skip work.
    failed: bool,
}

/// Phase 7: Work item for the partition synthesis worker pool.
#[cfg(feature = "cuda-supraseal")]
struct PartitionWorkItem {
    /// Shared parsed C1 output (Arc for zero-copy sharing across workers).
    parsed: Arc<crate::pipeline::ParsedC1Output>,
    /// Which partition to synthesize (0..num_partitions).
    partition_idx: usize,
    /// The parent job ID this partition belongs to.
    job_id: JobId,
    /// Lightweight request clone for metadata.
    request: ProofRequest,
    /// SRS parameters (shared across all partitions).
    params: Arc<bellperson::groth16::SuprasealParameters<blstrs::Bls12>>,
    /// Circuit ID for SRS affinity.
    circuit_id: CircuitId,
    /// Total partitions in this proof.
    num_partitions: usize,
}

/// The cuzk proving engine.
pub struct Engine {
    config: Config,
    scheduler: Arc<Scheduler>,
    tracker: Arc<Mutex<JobTracker>>,
    /// Startup time for uptime reporting.
    started_at: Instant,
    /// Shutdown signal (true = shutdown requested).
    shutdown_tx: watch::Sender<bool>,
    shutdown_rx: watch::Receiver<bool>,
    /// SRS entries that have been preloaded (circuit_id → load time ms).
    /// Used by Phase 1 (monolithic) mode.
    preloaded_srs: Arc<RwLock<HashMap<String, u64>>>,
    /// Phase 2 SRS manager — loads params directly via SuprasealParameters.
    /// Shared across all GPU workers.
    srs_manager: Arc<Mutex<SrsManager>>,
    /// Whether pipeline mode is enabled.
    pipeline_enabled: bool,
}

/// Detect available GPU ordinals.
/// If the config specifies explicit devices, use those.
/// Otherwise, detect via nvidia-smi.
fn detect_gpu_ordinals(config: &Config) -> Vec<u32> {
    if !config.gpus.devices.is_empty() {
        return config.gpus.devices.clone();
    }

    // Auto-detect via nvidia-smi
    let output = std::process::Command::new("nvidia-smi")
        .args(["--query-gpu=index", "--format=csv,noheader,nounits"])
        .output();

    match output {
        Ok(out) if out.status.success() => {
            let text = String::from_utf8_lossy(&out.stdout);
            text.lines()
                .filter_map(|line| line.trim().parse::<u32>().ok())
                .collect()
        }
        _ => {
            warn!("nvidia-smi not available, defaulting to single GPU (ordinal 0)");
            vec![0]
        }
    }
}

impl Engine {
    /// Create a new engine with the given configuration.
    pub fn new(config: Config) -> Self {
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        let pipeline_enabled = config.pipeline.enabled;
        let srs_manager = SrsManager::new(
            config.srs.param_cache.clone(),
            config.memory.pinned_budget_bytes(),
        );
        Self {
            config,
            scheduler: Arc::new(Scheduler::new()),
            tracker: Arc::new(Mutex::new(JobTracker::new(vec![]))),
            started_at: Instant::now(),
            shutdown_tx,
            shutdown_rx,
            preloaded_srs: Arc::new(RwLock::new(HashMap::new())),
            srs_manager: Arc::new(Mutex::new(srs_manager)),
            pipeline_enabled,
        }
    }

    /// Start the engine: preload SRS, spawn GPU worker(s).
    pub async fn start(&self) -> Result<()> {
        // Initialize timeline epoch for waterfall instrumentation
        let _ = TIMELINE_EPOCH.set(Instant::now());

        info!(
            pipeline_enabled = self.pipeline_enabled,
            "starting cuzk engine"
        );

        // Preload configured SRS entries
        if self.pipeline_enabled {
            // Phase 2: Preload via SrsManager (direct SuprasealParameters loading)
            let srs_mgr = self.srs_manager.clone();
            let preload_ids: Vec<CircuitId> = self
                .config
                .srs
                .preload
                .iter()
                .filter_map(|s| CircuitId::from_str_id(s))
                .collect();
            if !preload_ids.is_empty() {
                info!(
                    circuit_ids = ?preload_ids,
                    "preloading SRS via SrsManager (Phase 2)"
                );
                let srs_mgr_clone = srs_mgr.clone();
                let preloaded_srs = self.preloaded_srs.clone();
                // SRS loading is blocking I/O, run on blocking thread
                tokio::task::spawn_blocking(move || {
                    let mut mgr = srs_mgr_clone.blocking_lock();
                    for cid in &preload_ids {
                        let start = Instant::now();
                        match mgr.ensure_loaded(cid) {
                            Ok(_) => {
                                let elapsed = start.elapsed();
                                let mut srs = preloaded_srs.blocking_write();
                                srs.insert(cid.to_string(), elapsed.as_millis() as u64);
                                info!(
                                    circuit_id = %cid,
                                    elapsed_ms = elapsed.as_millis(),
                                    "SRS preloaded (Phase 2)"
                                );
                            }
                            Err(e) => {
                                warn!(circuit_id = %cid, error = %e, "SRS preload failed");
                            }
                        }
                    }
                })
                .await?;
            }
        } else {
            // Phase 1: Preload via environment variable (lazy GROTH_PARAM_MEMORY_CACHE)
            let param_cache = self.config.srs.param_cache.clone();
            for circuit_id in &self.config.srs.preload {
                match prover::preload_srs(circuit_id, &param_cache) {
                    Ok(elapsed) => {
                        let mut srs = self.preloaded_srs.write().await;
                        srs.insert(circuit_id.clone(), elapsed.as_millis() as u64);
                        info!(
                            circuit_id = circuit_id.as_str(),
                            elapsed_ms = elapsed.as_millis(),
                            "SRS preloaded (Phase 1)"
                        );
                    }
                    Err(e) => {
                        warn!(circuit_id = circuit_id.as_str(), error = %e, "SRS preload failed");
                    }
                }
            }
        }

        // Preload PCE caches from disk (Phase 5/6)
        #[cfg(feature = "cuda-supraseal")]
        {
            let param_cache = self.config.srs.param_cache.clone();
            let loaded = tokio::task::spawn_blocking(move || {
                crate::pipeline::preload_pce_from_disk(&param_cache)
            })
            .await?;
            info!(loaded = loaded, "PCE disk preload complete");
        }

        // Detect GPUs and create worker states
        let gpu_ordinals = detect_gpu_ordinals(&self.config);
        let gpu_workers_per_device = if self.pipeline_enabled {
            self.config.gpus.gpu_workers_per_device.max(1) as usize
        } else {
            1 // monolithic mode: one worker per GPU
        };
        let num_workers = gpu_ordinals.len() * gpu_workers_per_device;
        info!(
            num_gpus = gpu_ordinals.len(),
            gpu_workers_per_device = gpu_workers_per_device,
            total_workers = num_workers,
            gpus = ?gpu_ordinals,
            "initializing GPU workers"
        );

        // For pipeline mode, worker_states tracks per-GPU (not per-worker) for
        // internal iteration. The tracker gets the expanded per-worker list.
        let worker_states: Vec<WorkerState> = gpu_ordinals
            .iter()
            .enumerate()
            .map(|(i, &ordinal)| WorkerState {
                worker_id: i as u32,
                gpu_ordinal: ordinal,
                last_circuit_id: None,
                current_job: None,
            })
            .collect();

        // Phase 8: Expanded tracker worker list (one entry per actual worker)
        let tracker_worker_states: Vec<WorkerState> = gpu_ordinals
            .iter()
            .flat_map(|&ordinal| {
                (0..gpu_workers_per_device).map(move |_| WorkerState {
                    worker_id: 0, // will be re-assigned below
                    gpu_ordinal: ordinal,
                    last_circuit_id: None,
                    current_job: None,
                })
            })
            .enumerate()
            .map(|(i, mut ws)| { ws.worker_id = i as u32; ws })
            .collect();

        // Store worker states in tracker
        {
            let mut tracker = self.tracker.lock().await;
            tracker.workers = tracker_worker_states;
        }

        if self.pipeline_enabled {
            // ─── Phase 2: Two-stage pipeline ───────────────────────────────
            //
            // Stage 1 (synthesis task): pulls from scheduler, runs CPU synthesis
            //   on a blocking thread, pushes SynthesizedJob to bounded channel.
            // Stage 2 (GPU workers): pull SynthesizedJob from channel, run
            //   gpu_prove on a blocking thread, complete the job.
            //
            // The bounded channel provides backpressure: if all GPU workers are
            // busy and the channel is full, the synthesis task blocks — preventing
            // unbounded memory growth from pre-synthesized proofs.
            //
            // For single-GPU setups (most common), this achieves:
            //   synth(N) → GPU(N) | synth(N+1) → GPU(N+1) | ...
            //   steady-state = max(synth_time, gpu_time) per proof
            //   For PoRep 32G: ~55s/proof (synthesis-bound) vs ~91s sequential

            // Channel capacity sizing:
            //
            // In partition mode (partition_workers > 0), up to `pw` partitions
            // complete synthesis concurrently. If the channel is too small,
            // completed syntheses block on send() while still holding their
            // full memory allocation (~16 GiB each pre-a/b/c-free, ~4 GiB after).
            // This caused OOM at pw=12 with channel capacity=1 (28 provers piled up).
            //
            // We auto-scale the channel to max(synthesis_lookahead, partition_workers)
            // so completed partitions can drain into the channel buffer without
            // blocking the synthesis tasks. The GPU worker pulls one at a time,
            // and once the channel is full, backpressure kicks in naturally.
            //
            // In non-partition mode, synthesis_lookahead (default 1) still applies.
            let configured_lookahead = self.config.pipeline.synthesis_lookahead.max(1) as usize;
            let pw = self.config.synthesis.partition_workers as usize;
            let lookahead = if pw > 0 {
                configured_lookahead.max(pw)
            } else {
                configured_lookahead
            };
            let (synth_tx, synth_rx) = tokio::sync::mpsc::channel::<SynthesizedJob>(lookahead);
            let synth_rx = Arc::new(Mutex::new(synth_rx));

            info!(
                configured_lookahead = configured_lookahead,
                partition_workers = pw,
                effective_lookahead = lookahead,
                num_gpus = num_workers,
                "starting pipeline: synthesis task + GPU workers"
            );

            // Spawn the synthesis dispatcher task.
            //
            // When synthesis_concurrency > 1, multiple proofs can be synthesized
            // in parallel. The dispatcher pulls from the scheduler, acquires a
            // semaphore permit, and spawns each process_batch as an independent
            // tokio task. The semaphore limits how many syntheses run concurrently.
            // The bounded channel (synthesis_lookahead) still provides backpressure
            // on the GPU side — completed syntheses block on channel send if the
            // GPU hasn't consumed yet.
            //
            // This eliminates the GPU idle gap caused by sequential synthesis:
            //   Before (synthesis_concurrency=1):
            //     Synth: [====P1====]            [====P2====]
            //     GPU:              [==P1==]idle             [==P2==]
            //
            //   After (synthesis_concurrency=2):
            //     Synth A: [====P1====]  [====P3====]
            //     Synth B:   [====P2====]  [====P4====]
            //     GPU:              [P1][P2][P3][P4]  ← no idle gaps
            {
                let scheduler = self.scheduler.clone();
                let tracker = self.tracker.clone();
                let srs_manager = self.srs_manager.clone();
                let param_cache = self.config.srs.param_cache.clone();
                let mut shutdown_rx = self.shutdown_rx.clone();
                let max_batch_size = self.config.scheduler.max_batch_size;
                let max_batch_wait_ms = self.config.scheduler.max_batch_wait_ms;
                let slot_size = self.config.pipeline.slot_size;
                let synthesis_concurrency = self.config.pipeline.synthesis_concurrency.max(1) as usize;
                let partition_workers = self.config.synthesis.partition_workers;

                // Semaphore limits concurrent synthesis tasks.
                // With concurrency=1, behavior is identical to the old sequential loop.
                let synth_semaphore = Arc::new(tokio::sync::Semaphore::new(synthesis_concurrency));

                // Phase 7: Semaphore limits concurrent partition synthesis workers.
                // Each worker processes one partition (~29s, mostly single-threaded).
                let partition_semaphore = Arc::new(tokio::sync::Semaphore::new(
                    if partition_workers > 0 { partition_workers as usize } else { 1 },
                ));

                tokio::spawn(async move {
                    info!(
                        max_batch_size = max_batch_size,
                        max_batch_wait_ms = max_batch_wait_ms,
                        slot_size = slot_size,
                        synthesis_concurrency = synthesis_concurrency,
                        partition_workers = partition_workers,
                        "synthesis dispatcher started"
                    );

                    let mut batch_collector = crate::batch_collector::BatchCollector::new(
                        crate::batch_collector::BatchConfig {
                            max_batch_size,
                            max_batch_wait_ms,
                        },
                    );

                    /// Helper: dispatch a batch for processing. If synthesis_concurrency > 1,
                    /// spawns as a background task (non-blocking). If 1, awaits inline (old behavior).
                    async fn dispatch_batch(
                        batch: crate::batch_collector::ProofBatch,
                        tracker: &Arc<Mutex<JobTracker>>,
                        srs_manager: &Arc<Mutex<SrsManager>>,
                        param_cache: &std::path::Path,
                        synth_tx: &tokio::sync::mpsc::Sender<SynthesizedJob>,
                        slot_size: u32,
                        partition_workers: u32,
                        partition_semaphore: &Arc<tokio::sync::Semaphore>,
                        semaphore: &Arc<tokio::sync::Semaphore>,
                        concurrency: usize,
                        span: tracing::Span,
                    ) -> bool {
                        if concurrency <= 1 {
                            // Sequential mode: await inline (old behavior)
                            process_batch(
                                batch, tracker, srs_manager, param_cache, synth_tx, slot_size,
                                partition_workers, partition_semaphore,
                            ).instrument(span).await
                        } else {
                            // Parallel mode: acquire semaphore, spawn task
                            let permit = match semaphore.clone().acquire_owned().await {
                                Ok(p) => p,
                                Err(_) => {
                                    error!("synthesis semaphore closed");
                                    return false;
                                }
                            };
                            let tracker = tracker.clone();
                            let srs_manager = srs_manager.clone();
                            let param_cache = param_cache.to_owned();
                            let synth_tx = synth_tx.clone();
                            let partition_semaphore = partition_semaphore.clone();
                            tokio::spawn(async move {
                                let _permit = permit; // held until task completes
                                process_batch(
                                    batch, &tracker, &srs_manager, &param_cache, &synth_tx, slot_size,
                                    partition_workers, &partition_semaphore,
                                ).instrument(span).await;
                            });
                            true // always continue — errors handled inside the spawned task
                        }
                    }

                    loop {
                        // Determine if we should wait for scheduler or flush a timeout.
                        // If the batch collector has pending items, use a timeout.
                        let request = if batch_collector.has_pending() {
                            // There's a pending batch. Race between:
                            //   1. Scheduler delivers another same-type request
                            //   2. Batch timeout expires
                            //   3. Shutdown signal
                            let timeout_dur = batch_collector.time_until_flush()
                                .unwrap_or(Duration::from_millis(100));
                            tokio::select! {
                                biased;
                                _ = shutdown_rx.changed() => {
                                    if *shutdown_rx.borrow() {
                                        info!("synthesis dispatcher received shutdown signal");
                                        // Flush any pending batch before exiting
                                        if let Some(batch) = batch_collector.force_flush() {
                                            let span = info_span!("synth_shutdown_flush", batch_size = batch.len());
                                            let _ = dispatch_batch(
                                                batch, &tracker, &srs_manager, &param_cache, &synth_tx, slot_size,
                                                partition_workers, &partition_semaphore,
                                                &synth_semaphore, synthesis_concurrency, span,
                                            ).await;
                                        }
                                        break;
                                    }
                                    continue;
                                }
                                _ = tokio::time::sleep(timeout_dur) => {
                                    // Timer expired — check timeout and flush
                                    if let Some(batch) = batch_collector.check_timeout() {
                                        let span = info_span!("synth_timeout_flush", batch_size = batch.len());
                                        let ok = dispatch_batch(
                                            batch, &tracker, &srs_manager, &param_cache, &synth_tx, slot_size,
                                            partition_workers, &partition_semaphore,
                                            &synth_semaphore, synthesis_concurrency, span,
                                        ).await;
                                        if !ok { break; }
                                    }
                                    continue;
                                }
                                request = scheduler.next() => Some(request),
                            }
                        } else {
                            // No pending batch — block on scheduler
                            tokio::select! {
                                biased;
                                _ = shutdown_rx.changed() => {
                                    if *shutdown_rx.borrow() {
                                        info!("synthesis dispatcher received shutdown signal");
                                        break;
                                    }
                                    continue;
                                }
                                request = scheduler.next() => Some(request),
                            }
                        };

                        let request = match request {
                            Some(r) => r,
                            None => continue,
                        };

                        let proof_kind = request.proof_kind;

                        // Phase 3: Route through batch collector for batchable types,
                        // or process immediately for non-batchable types.
                        if crate::batch_collector::is_batchable(proof_kind) && max_batch_size > 1 {
                            // Try to add to batch. May return a flushed batch.
                            if let Some(batch) = batch_collector.add(request) {
                                let span = info_span!("synth_batch_full",
                                    batch_size = batch.len(),
                                    proof_kind = %batch.proof_kind,
                                );
                                let ok = dispatch_batch(
                                    batch, &tracker, &srs_manager, &param_cache, &synth_tx, slot_size,
                                    partition_workers, &partition_semaphore,
                                    &synth_semaphore, synthesis_concurrency, span,
                                ).await;
                                if !ok { break; }
                            }
                        } else {
                            // Non-batchable (PoSt) or batching disabled — process immediately.
                            // First, flush any pending batch of a different type.
                            if let Some(pending_batch) = batch_collector.force_flush() {
                                let span = info_span!("synth_preempt_flush",
                                    batch_size = pending_batch.len(),
                                    proof_kind = %pending_batch.proof_kind,
                                );
                                let ok = dispatch_batch(
                                    pending_batch, &tracker, &srs_manager, &param_cache, &synth_tx, slot_size,
                                    partition_workers, &partition_semaphore,
                                    &synth_semaphore, synthesis_concurrency, span,
                                ).await;
                                if !ok { break; }
                            }

                            // Process the single non-batchable request
                            let single_batch = crate::batch_collector::ProofBatch {
                                proof_kind,
                                requests: vec![request],
                                first_received_at: Instant::now(),
                            };
                            let span = info_span!("synth_single",
                                proof_kind = %proof_kind,
                            );
                            let ok = dispatch_batch(
                                single_batch, &tracker, &srs_manager, &param_cache, &synth_tx, slot_size,
                                partition_workers, &partition_semaphore,
                                &synth_semaphore, synthesis_concurrency, span,
                            ).await;
                            if !ok { break; }
                        }
                    }
                    info!("synthesis dispatcher stopped");
                });
            }

            /// Process a batch of proof requests: synthesize all circuits, load SRS,
            /// and send the synthesized job to the GPU channel.
            ///
            /// Phase 7: When `partition_workers > 0` and the request is single-sector
            /// PoRep C2, dispatches individual partitions as independent work units
            /// through the engine's synthesis→GPU pipeline. Each partition is synthesized
            /// by a `spawn_blocking` worker gated by `partition_semaphore`, then sent to
            /// `synth_tx` as a `SynthesizedJob` with `partition_index` set.
            ///
            /// Falls back to Phase 6 slotted pipeline when `slot_size > 0` and
            /// `partition_workers == 0`, or to batch-all when both are 0.
            ///
            /// Returns `true` to continue, `false` to stop the synthesis task.
            async fn process_batch(
                batch: crate::batch_collector::ProofBatch,
                tracker: &Arc<Mutex<JobTracker>>,
                srs_manager: &Arc<Mutex<SrsManager>>,
                param_cache: &std::path::Path,
                synth_tx: &tokio::sync::mpsc::Sender<SynthesizedJob>,
                slot_size: u32,
                partition_workers: u32,
                partition_semaphore: &Arc<tokio::sync::Semaphore>,
            ) -> bool {
                let batch_size = batch.len();
                let proof_kind = batch.proof_kind;
                let requests = batch.requests;

                info!(
                    batch_size = batch_size,
                    proof_kind = %proof_kind,
                    slot_size = slot_size,
                    "processing batch"
                );

                let param_cache_str = param_cache.to_string_lossy().to_string();
                let srs_mgr = srs_manager.clone();
                let tracker_clone = tracker.clone();

                #[cfg(feature = "cuda-supraseal")]
                {
                    use crate::pipeline;

                    // ── Phase 7: Engine-level per-partition dispatch ─────────────────
                    //
                    // When partition_workers > 0, single-sector PoRep C2 is dispatched
                    // as 10 independent partition work items through the engine's
                    // synthesis→GPU pipeline. Each partition is synthesized by a
                    // spawn_blocking worker (gated by partition_semaphore), then sent
                    // to synth_tx as a SynthesizedJob with partition_index set.
                    //
                    // The GPU worker routes partition results to a ProofAssembler
                    // (registered in tracker.assemblers) and delivers the final proof
                    // when all partitions are complete.
                    //
                    // This eliminates the thundering-herd pattern: partitions from
                    // different sectors interleave on the GPU, achieving zero idle gaps.
                    if proof_kind == ProofKind::PoRepSealCommit
                        && requests.len() == 1
                        && partition_workers > 0
                    {
                        let req = requests[0].clone();
                        let job_id = req.job_id.clone();
                        let srs_mgr_clone = srs_mgr.clone();
                        let param_cache_owned = param_cache_str.clone();

                        // 1. Parse C1 once (blocking) and load SRS
                        let parse_result = tokio::task::spawn_blocking({
                            let vanilla_proof = req.vanilla_proof.clone();
                            let srs_mgr = srs_mgr_clone.clone();
                            move || -> Result<(Arc<pipeline::ParsedC1Output>, Arc<bellperson::groth16::SuprasealParameters<blstrs::Bls12>>)> {
                                std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", &param_cache_owned);
                                let parsed = pipeline::parse_c1_output(&vanilla_proof)?;
                                let srs = {
                                    let mut mgr = srs_mgr.blocking_lock();
                                    mgr.ensure_loaded(&CircuitId::Porep32G)?
                                };
                                Ok((Arc::new(parsed), srs))
                            }
                        }).await;

                        let (parsed, srs) = match parse_result {
                            Ok(Ok(v)) => v,
                            Ok(Err(e)) => {
                                error!(error = %e, "Phase 7: C1 parse/SRS load failed");
                                let mut t = tracker_clone.lock().await;
                                t.record_failure(proof_kind);
                                let status = JobStatus::Failed(format!("C1 parse failed: {}", e));
                                if let Some(senders) = t.pending.remove(&job_id) {
                                    for sender in senders {
                                        let _ = sender.send(status.clone());
                                    }
                                }
                                t.completed.insert(job_id, status);
                                return true;
                            }
                            Err(e) => {
                                error!(error = %e, "Phase 7: C1 parse task panicked");
                                let mut t = tracker_clone.lock().await;
                                t.record_failure(proof_kind);
                                let status = JobStatus::Failed(format!("C1 parse panicked: {}", e));
                                if let Some(senders) = t.pending.remove(&job_id) {
                                    for sender in senders {
                                        let _ = sender.send(status.clone());
                                    }
                                }
                                t.completed.insert(job_id, status);
                                return true;
                            }
                        };

                        let num_partitions = parsed.num_partitions;

                        // 2. Register ProofAssembler in JobTracker
                        {
                            let mut t = tracker_clone.lock().await;
                            t.assemblers.insert(job_id.clone(), PartitionedJobState {
                                assembler: pipeline::ProofAssembler::new(num_partitions),
                                request: req.clone(),
                                proof_kind,
                                total_synth_duration: Duration::ZERO,
                                total_gpu_duration: Duration::ZERO,
                                start_time: Instant::now(),
                                failed: false,
                            });
                        }

                        info!(
                            job_id = %job_id,
                            num_partitions = num_partitions,
                            partition_workers = partition_workers,
                            "Phase 7: dispatching per-partition synthesis"
                        );

                        // 3. Trigger background PCE extraction if not yet cached
                        if pipeline::get_pce(&CircuitId::Porep32G).is_none() {
                            let c1_data = req.vanilla_proof.clone();
                            let sn = req.sector_number;
                            let mid = req.miner_id;
                            std::thread::spawn(move || {
                                info!("background PCE extraction starting for PoRep 32G");
                                match pipeline::extract_and_cache_pce_from_c1(&c1_data, sn, mid) {
                                    Ok(()) => info!("background PCE extraction complete"),
                                    Err(e) => warn!(error = %e, "background PCE extraction failed"),
                                }
                            });
                        }

                        // 4. Dispatch each partition to spawn_blocking workers
                        for partition_idx in 0..num_partitions {
                            let item = PartitionWorkItem {
                                parsed: parsed.clone(),
                                partition_idx,
                                job_id: job_id.clone(),
                                request: req.clone(),
                                params: srs.clone(),
                                circuit_id: CircuitId::Porep32G,
                                num_partitions,
                            };
                            let synth_tx = synth_tx.clone();
                            let tracker_for_err = tracker_clone.clone();
                            let partition_sem = partition_semaphore.clone();

                            tokio::spawn(async move {
                                // Acquire partition worker permit (backpressure)
                                let permit = match partition_sem.acquire_owned().await {
                                    Ok(p) => p,
                                    Err(_) => {
                                        error!("partition semaphore closed");
                                        return;
                                    }
                                };

                                let p_idx = item.partition_idx;
                                let p_job_id = item.job_id.clone();
                                let _p_num_partitions = item.num_partitions;

                                // Check if job is already failed before starting synthesis
                                {
                                    let t = tracker_for_err.lock().await;
                                    if let Some(state) = t.assemblers.get(&p_job_id) {
                                        if state.failed {
                                            info!(
                                                job_id = %p_job_id,
                                                partition = p_idx,
                                                "skipping synthesis for failed job"
                                            );
                                            drop(permit);
                                            return;
                                        }
                                    }
                                }

                                crate::pipeline::buf_synth_start();
                                crate::pipeline::log_buffers("synth_start");
                                timeline_event(
                                    "SYNTH_START",
                                    &p_job_id.0,
                                    &format!("partition={}", p_idx),
                                );

                                // Run synthesis on blocking thread.
                                // The partition permit is held across both synthesis AND
                                // the channel send. This bounds total in-flight synthesis
                                // outputs to `partition_workers` — preventing unbounded
                                // memory growth when synthesis is faster than GPU consumption.
                                //
                                // With channel capacity = partition_workers, the send()
                                // is non-blocking (channel always has room), so holding the
                                // permit through the send adds no latency.
                                let synth_result = tokio::task::spawn_blocking(move || {
                                    let result = pipeline::synthesize_partition(
                                        &item.parsed,
                                        item.partition_idx,
                                        &item.job_id.0,
                                    );

                                    result.map(|synth| SynthesizedJob {
                                        request: item.request,
                                        synth,
                                        params: item.params,
                                        circuit_id: item.circuit_id,
                                        batch_requests: vec![],
                                        sector_boundaries: vec![],
                                        partition_index: Some(item.partition_idx),
                                        total_partitions: Some(item.num_partitions),
                                        parent_job_id: Some(item.job_id),
                                    })
                                }).await;

                                match synth_result {
                                    Ok(Ok(job)) => {
                                        timeline_event(
                                            "SYNTH_END",
                                            &p_job_id.0,
                                            &format!("partition={},synth_ms={}", p_idx, job.synth.synthesis_duration.as_millis()),
                                        );
                                        crate::pipeline::buf_synth_done();
                                        crate::pipeline::log_buffers("synth_done");
                                        info!(
                                            job_id = %p_job_id,
                                            partition = p_idx,
                                            synth_ms = job.synth.synthesis_duration.as_millis(),
                                            "partition synthesis complete, sending to GPU"
                                        );
                                        timeline_event("CHAN_SEND", &p_job_id.0, &format!("partition={}", p_idx));
                                        if synth_tx.send(job).await.is_err() {
                                            error!("GPU channel closed during partition dispatch");
                                        }
                                        // Drop permit AFTER send succeeds — bounds in-flight outputs
                                        drop(permit);
                                    }
                                    Ok(Err(e)) => {
                                        error!(
                                            error = %e,
                                            job_id = %p_job_id,
                                            partition = p_idx,
                                            "partition synthesis failed"
                                        );
                                        // Mark assembler as failed and notify callers
                                        let mut t = tracker_for_err.lock().await;
                                        if let Some(state) = t.assemblers.get_mut(&p_job_id) {
                                            if !state.failed {
                                                state.failed = true;
                                                t.record_failure(proof_kind);
                                                let status = JobStatus::Failed(
                                                    format!("partition {} synthesis failed: {}", p_idx, e)
                                                );
                                                if let Some(senders) = t.pending.remove(&p_job_id) {
                                                    for sender in senders {
                                                        let _ = sender.send(status.clone());
                                                    }
                                                }
                                                t.completed.insert(p_job_id, status);
                                            }
                                        }
                                    }
                                    Err(e) => {
                                        error!(
                                            error = %e,
                                            job_id = %p_job_id,
                                            partition = p_idx,
                                            "partition synthesis task panicked"
                                        );
                                        let mut t = tracker_for_err.lock().await;
                                        if let Some(state) = t.assemblers.get_mut(&p_job_id) {
                                            if !state.failed {
                                                state.failed = true;
                                                t.record_failure(proof_kind);
                                                let status = JobStatus::Failed(
                                                    format!("partition {} synthesis panicked: {}", p_idx, e)
                                                );
                                                if let Some(senders) = t.pending.remove(&p_job_id) {
                                                    for sender in senders {
                                                        let _ = sender.send(status.clone());
                                                    }
                                                }
                                                t.completed.insert(p_job_id, status);
                                            }
                                        }
                                    }
                                }
                            });
                        }

                        // process_batch() returns immediately — completion is signaled
                        // asynchronously by the GPU worker when ProofAssembler collects
                        // all partitions.
                        return true;
                    }

                    // Phase 6 fallback: Slotted partition pipeline (self-contained)
                    // Used when partition_workers=0 and slot_size > 0.
                    if proof_kind == ProofKind::PoRepSealCommit
                        && requests.len() == 1
                        && slot_size > 0
                    {
                        let req = requests[0].clone();
                        let srs_mgr_clone = srs_mgr.clone();
                        let _tracker_clone2 = tracker_clone.clone();
                        let param_cache_owned = param_cache_str.clone();

                        let slotted_result = tokio::task::spawn_blocking(move || -> Result<(Vec<u8>, crate::pipeline::PipelinedTimings)> {
                            std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", &param_cache_owned);

                            // Load SRS
                            let srs = {
                                let mut mgr = srs_mgr_clone.blocking_lock();
                                mgr.ensure_loaded(&CircuitId::Porep32G)?
                            };

                            pipeline::prove_porep_c2_partitioned(
                                &req.vanilla_proof,
                                req.sector_number,
                                req.miner_id,
                                &srs,
                                slot_size as usize,
                                &req.job_id.0,
                            )
                        }).await;

                        // Trigger background PCE extraction if not yet cached
                        if pipeline::get_pce(&CircuitId::Porep32G).is_none() {
                            let c1_data = requests[0].vanilla_proof.clone();
                            let sn = requests[0].sector_number;
                            let mid = requests[0].miner_id;
                            std::thread::spawn(move || {
                                info!("background PCE extraction starting for PoRep 32G");
                                match pipeline::extract_and_cache_pce_from_c1(&c1_data, sn, mid) {
                                    Ok(()) => info!("background PCE extraction complete"),
                                    Err(e) => warn!(error = %e, "background PCE extraction failed"),
                                }
                            });
                        }

                        // Process result
                        let mut t = tracker_clone.lock().await;
                        match slotted_result {
                            Ok(Ok((proof_bytes, timings))) => {
                                let mut pt = ProofTimings::default();
                                pt.synthesis = timings.synthesis;
                                pt.gpu_compute = timings.gpu_compute;
                                pt.proving = timings.total;
                                pt.queue_wait = requests[0].submitted_at.elapsed().saturating_sub(pt.proving);
                                pt.total = requests[0].submitted_at.elapsed();

                                info!(
                                    proof_len = proof_bytes.len(),
                                    total_ms = pt.total.as_millis(),
                                    synth_ms = pt.synthesis.as_millis(),
                                    gpu_ms = pt.gpu_compute.as_millis(),
                                    slot_size = slot_size,
                                    "slotted PoRep C2 proof completed"
                                );

                                t.record_completion(proof_kind, pt.total);

                                let status = JobStatus::Completed(ProofResult {
                                    job_id: requests[0].job_id.clone(),
                                    proof_kind,
                                    proof_bytes,
                                    timings: pt,
                                });
                                if let Some(senders) = t.pending.remove(&requests[0].job_id) {
                                    for sender in senders {
                                        let _ = sender.send(status.clone());
                                    }
                                }
                                t.completed.insert(requests[0].job_id.clone(), status);
                            }
                            Ok(Err(e)) => {
                                error!(error = %e, "slotted PoRep C2 proof failed");
                                t.record_failure(proof_kind);
                                let status = JobStatus::Failed(format!("slotted proof failed: {}", e));
                                if let Some(senders) = t.pending.remove(&requests[0].job_id) {
                                    for sender in senders {
                                        let _ = sender.send(status.clone());
                                    }
                                }
                                t.completed.insert(requests[0].job_id.clone(), status);
                            }
                            Err(e) => {
                                error!(error = %e, "slotted PoRep C2 task panicked");
                                t.record_failure(proof_kind);
                                let status = JobStatus::Failed(format!("slotted proof panicked: {}", e));
                                if let Some(senders) = t.pending.remove(&requests[0].job_id) {
                                    for sender in senders {
                                        let _ = sender.send(status.clone());
                                    }
                                }
                                t.completed.insert(requests[0].job_id.clone(), status);
                            }
                        }

                        return true;
                    }

                    // Standard path: synthesize, then send to GPU channel
                    let reqs = requests.clone();
                    let synth_job_id = reqs[0].job_id.0.clone();
                    timeline_event("SYNTH_START", &synth_job_id, &format!("kind={}", proof_kind));
                    let synth_result = tokio::task::spawn_blocking(move || -> Result<SynthesizedJob> {
                        std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", &param_cache_str);

                        let batch_job_id = reqs[0].job_id.0.clone();

                        let (circuit_id, synth, sector_boundaries) = match proof_kind {
                            ProofKind::PoRepSealCommit if reqs.len() > 1 => {
                                // Phase 3: Multi-sector batch synthesis
                                let sector_inputs: Vec<(&[u8], u64, u64)> = reqs.iter()
                                    .map(|r| (r.vanilla_proof.as_slice(), r.sector_number, r.miner_id))
                                    .collect();
                                let (s, boundaries) = pipeline::synthesize_porep_c2_multi(
                                    &sector_inputs, &batch_job_id,
                                )?;
                                (CircuitId::Porep32G, s, boundaries)
                            }
                            ProofKind::PoRepSealCommit => {
                                // Single sector — use existing batch synthesis
                                let req = &reqs[0];
                                let s = pipeline::synthesize_porep_c2_batch(
                                    &req.vanilla_proof, req.sector_number, req.miner_id, &batch_job_id,
                                )?;
                                (CircuitId::Porep32G, s, vec![])
                            }
                            ProofKind::WinningPost => {
                                let req = &reqs[0];
                                let s = pipeline::synthesize_winning_post(
                                    &req.vanilla_proofs, req.registered_proof, req.miner_id,
                                    &req.randomness, &batch_job_id,
                                )?;
                                (CircuitId::WinningPost32G, s, vec![])
                            }
                            ProofKind::WindowPostPartition => {
                                let req = &reqs[0];
                                let s = pipeline::synthesize_window_post(
                                    &req.vanilla_proofs, req.registered_proof, req.miner_id,
                                    &req.randomness, req.partition_index, &batch_job_id,
                                )?;
                                (CircuitId::WindowPost32G, s, vec![])
                            }
                            ProofKind::SnapDealsUpdate => {
                                let req = &reqs[0];
                                let s = pipeline::synthesize_snap_deals(
                                    req.vanilla_proofs.clone(), req.registered_proof,
                                    &req.comm_r_old, &req.comm_r_new, &req.comm_d_new,
                                    &batch_job_id,
                                )?;
                                (CircuitId::SnapDeals32G, s, vec![])
                            }
                        };

                        // Load SRS
                        let srs = {
                            let mut mgr = srs_mgr.blocking_lock();
                            mgr.ensure_loaded(&circuit_id)?
                        };

                        Ok(SynthesizedJob {
                            request: reqs[0].clone(),
                            synth,
                            params: srs,
                            circuit_id,
                            batch_requests: if reqs.len() > 1 { reqs } else { vec![] },
                            sector_boundaries,
                            partition_index: None,
                            total_partitions: None,
                            parent_job_id: None,
                        })
                    }).await;

                    match synth_result {
                        Ok(Ok(job)) => {
                            let is_batched = !job.batch_requests.is_empty();
                            let synth_job_id_str = job.request.job_id.0.clone();
                            timeline_event("SYNTH_END", &synth_job_id_str, &format!("synth_ms={}", job.synth.synthesis_duration.as_millis()));
                            info!(
                                synth_ms = job.synth.synthesis_duration.as_millis(),
                                circuit_id = %job.circuit_id,
                                batch_size = if is_batched { job.batch_requests.len() } else { 1 },
                                sectors = if is_batched { job.sector_boundaries.len() } else { 1 },
                                "synthesis complete, sending to GPU"
                            );

                            // Phase 5/6: Trigger background PCE extraction if not yet cached.
                            // Uses the first request's C1 data to build an extraction circuit.
                            // The extraction runs in a background thread so it doesn't block
                            // the GPU from processing this proof.
                            if pipeline::get_pce(&job.circuit_id).is_none() {
                                if let ProofKind::PoRepSealCommit = proof_kind {
                                    let c1_data = requests[0].vanilla_proof.clone();
                                    let sn = requests[0].sector_number;
                                    let mid = requests[0].miner_id;
                                    std::thread::spawn(move || {
                                        info!("background PCE extraction starting for PoRep 32G");
                                        match pipeline::extract_and_cache_pce_from_c1(&c1_data, sn, mid) {
                                            Ok(()) => info!("background PCE extraction complete — next proof will use fast path"),
                                            Err(e) => warn!(error = %e, "background PCE extraction failed (non-fatal)"),
                                        }
                                    });
                                }
                            }

                            crate::pipeline::buf_synth_done();
                            crate::pipeline::log_buffers("synth_done_mono");
                            timeline_event("CHAN_SEND", &synth_job_id_str, "");
                            if synth_tx.send(job).await.is_err() {
                                error!("GPU channel closed, stopping synthesis task");
                                return false;
                            }
                        }
                        Ok(Err(e)) => {
                            error!(error = %e, "synthesis failed");
                            let mut t = tracker_clone.lock().await;
                            for req in &requests {
                                t.record_failure(proof_kind);
                                let status = JobStatus::Failed(format!("synthesis failed: {}", e));
                                if let Some(senders) = t.pending.remove(&req.job_id) {
                                    for sender in senders {
                                        let _ = sender.send(status.clone());
                                    }
                                }
                                t.completed.insert(req.job_id.clone(), status);
                            }
                        }
                        Err(e) => {
                            error!(error = %e, "synthesis task panicked");
                            let mut t = tracker_clone.lock().await;
                            for req in &requests {
                                t.record_failure(proof_kind);
                                let status = JobStatus::Failed(format!("synthesis panicked: {}", e));
                                if let Some(senders) = t.pending.remove(&req.job_id) {
                                    for sender in senders {
                                        let _ = sender.send(status.clone());
                                    }
                                }
                                t.completed.insert(req.job_id.clone(), status);
                            }
                        }
                    }
                }

                #[cfg(not(feature = "cuda-supraseal"))]
                {
                    let _ = (param_cache_str, srs_mgr, synth_tx);
                    error!("pipeline mode requires cuda-supraseal feature");
                    let mut t = tracker_clone.lock().await;
                    for req in &requests {
                        t.record_failure(proof_kind);
                        let status = JobStatus::Failed("pipeline mode requires cuda-supraseal".into());
                        if let Some(senders) = t.pending.remove(&req.job_id) {
                            for sender in senders {
                                let _ = sender.send(status.clone());
                            }
                        }
                        t.completed.insert(req.job_id.clone(), status);
                    }
                }

                true
            }

            // Phase 8: Spawn multiple GPU worker tasks per GPU for dual-worker
            // interlock. Each GPU gets its own C++ std::mutex, shared by all
            // workers on that GPU. Workers acquire the mutex only during CUDA
            // kernel execution, allowing CPU preprocessing to overlap.
            let gpu_workers_per_device = self.config.gpus.gpu_workers_per_device.max(1) as usize;
            info!(
                gpu_workers_per_device = gpu_workers_per_device,
                "spawning GPU workers with Phase 8 dual-worker interlock"
            );

            // Create one C++ mutex per GPU
            #[cfg(feature = "cuda-supraseal")]
            let gpu_mutexes: Vec<bellperson::groth16::SendableGpuMutex> = gpu_ordinals
                .iter()
                .map(|_| {
                    let ptr = bellperson::groth16::alloc_gpu_mutex();
                    bellperson::groth16::SendableGpuMutex(ptr)
                })
                .collect();

            let mut global_worker_id = 0u32;
            for (gpu_idx, state) in worker_states.iter().enumerate() {
                for worker_sub_id in 0..gpu_workers_per_device {
                let worker_id = global_worker_id;
                global_worker_id += 1;
                let gpu_ordinal = state.gpu_ordinal;
                let tracker = self.tracker.clone();
                let synth_rx = synth_rx.clone();
                let mut shutdown_rx = self.shutdown_rx.clone();
                // Phase 8: Convert GPU mutex pointer to usize for Send safety.
                // Raw pointers aren't Send, but the underlying C++ mutex lives
                // for the engine's lifetime, so the address is always valid.
                #[cfg(feature = "cuda-supraseal")]
                let gpu_mutex_addr: usize = gpu_mutexes[gpu_idx].0 as usize;

                tokio::spawn(async move {
                    info!(worker_id = worker_id, gpu = gpu_ordinal, sub_id = worker_sub_id, "pipeline GPU worker started");
                    loop {
                        // Pull next synthesized job from channel
                        let synth_job = {
                            let mut rx = synth_rx.lock().await;
                            tokio::select! {
                                biased;
                                _ = shutdown_rx.changed() => {
                                    if *shutdown_rx.borrow() {
                                        info!(worker_id = worker_id, "pipeline GPU worker received shutdown signal");
                                        break;
                                    }
                                    continue;
                                }
                                job = rx.recv() => {
                                    match job {
                                        Some(j) => j,
                                        None => {
                                            info!(worker_id = worker_id, "synthesis channel closed, GPU worker stopping");
                                            break;
                                        }
                                    }
                                }
                            }
                        };

                        let job_id = synth_job.request.job_id.clone();
                        let proof_kind = synth_job.request.proof_kind;
                        let submitted_at = synth_job.request.submitted_at;
                        let synth_duration = synth_job.synth.synthesis_duration;
                        let circuit_id_str = synth_job.circuit_id.to_string();
                        let is_batched = !synth_job.batch_requests.is_empty();
                        let batch_requests = synth_job.batch_requests.clone();
                        let sector_boundaries = synth_job.sector_boundaries.clone();
                        // Phase 7: Extract partition metadata before moving synth_job
                        let partition_index = synth_job.partition_index;
                        let _total_partitions = synth_job.total_partitions;
                        let parent_job_id = synth_job.parent_job_id.clone();
                        let is_partitioned = parent_job_id.is_some();
                        let span = info_span!("gpu_worker",
                            worker_id = worker_id,
                            gpu = gpu_ordinal,
                            job_id = %job_id,
                            proof_kind = %proof_kind,
                            batch_size = if is_batched { batch_requests.len() } else { 1 },
                            partition = ?partition_index,
                        );

                        async {
                            timeline_event("GPU_PICKUP", &job_id.0, &format!(
                                "worker={}{}", worker_id,
                                if let Some(pi) = partition_index { format!(",partition={}", pi) } else { String::new() }
                            ));
                            info!(
                                batched = is_batched,
                                partitioned = is_partitioned,
                                partition = ?partition_index,
                                "GPU worker picked up synthesized proof"
                            );

                            // Phase 7: Check if partitioned job is already failed — skip GPU proving
                            if let Some(ref pid) = parent_job_id {
                                let t = tracker.lock().await;
                                if let Some(state) = t.assemblers.get(pid) {
                                    if state.failed {
                                        info!(
                                            job_id = %pid,
                                            partition = ?partition_index,
                                            "skipping GPU prove for failed partitioned job"
                                        );
                                        return; // skip this partition entirely
                                    }
                                }
                            }

                            // Mark as running on this GPU worker
                            {
                                let mut t = tracker.lock().await;
                                if let Some(w) = t.workers.get_mut(worker_id as usize) {
                                    w.current_job = Some((
                                        parent_job_id.as_ref().unwrap_or(&job_id).clone(),
                                        proof_kind,
                                    ));
                                }
                            }

                            let gpu_str = gpu_ordinal.to_string();
                            let gpu_job_id = job_id.0.clone();

                            // Phase 12: Split GPU proving — start returns quickly
                            // after GPU lock release, with b_g2_msm still running.
                            // Finalization runs in a separate tokio task.
                            #[cfg(feature = "cuda-supraseal")]
                            let start_result = {
                                let partition_detail = if let Some(pi) = partition_index {
                                    format!("worker={},partition={}", worker_id, pi)
                                } else {
                                    format!("worker={}", worker_id)
                                };
                                timeline_event("GPU_START", &gpu_job_id, &partition_detail);
                                let gpu_str2 = gpu_str.clone();
                                tokio::task::spawn_blocking(move || -> Result<(crate::pipeline::PendingGpuProof, String)> {
                                    std::env::set_var("CUDA_VISIBLE_DEVICES", &gpu_str2);
                                    let gpu_mtx_ptr = gpu_mutex_addr as *mut std::ffi::c_void;
                                    let pending = crate::pipeline::gpu_prove_start(synth_job.synth, &synth_job.params, gpu_mtx_ptr)?;
                                    Ok((pending, gpu_str2))
                                }).await
                            };

                            #[cfg(feature = "cuda-supraseal")]
                            match start_result {
                                Ok(Ok(((pending_handle, pending_pi, gpu_start), _gpu_str_ret))) => {
                                    // Spawn finalizer task — GPU worker loops immediately
                                    let fin_tracker = tracker.clone();
                                    let fin_job_id = job_id.clone();
                                    let fin_parent_job_id = parent_job_id.clone();
                                    let fin_circuit_id_str = circuit_id_str.clone();
                                    let fin_batch_requests = batch_requests.clone();
                                    let fin_sector_boundaries = sector_boundaries.clone();
                                    let fin_gpu_job_id = gpu_job_id.clone();
                                    tokio::spawn(async move {
                                        // Finalize on a blocking thread (joins b_g2_msm)
                                        let result = tokio::task::spawn_blocking(move || -> Result<(Vec<u8>, Duration)> {
                                            let gpu_result = crate::pipeline::gpu_prove_finish(pending_handle, pending_pi, gpu_start)?;
                                            let detail = if let Some(pi) = gpu_result.partition_index {
                                                format!("worker={},partition={},gpu_ms={}", worker_id, pi, gpu_result.gpu_duration.as_millis())
                                            } else {
                                                format!("worker={},gpu_ms={}", worker_id, gpu_result.gpu_duration.as_millis())
                                            };
                                            timeline_event("GPU_END", &fin_gpu_job_id, &detail);
                                            Ok((gpu_result.proof_bytes, gpu_result.gpu_duration))
                                        }).await;

                                        // Process result (same logic as before)
                                        let mut t = fin_tracker.lock().await;

                                        // Clear current job from worker
                                        if let Some(w) = t.workers.get_mut(worker_id as usize) {
                                            w.current_job = None;
                                        }

                                        // Alias variables for the result-processing block
                                        let job_id = fin_job_id;
                                        let parent_job_id = fin_parent_job_id;
                                        let circuit_id_str = fin_circuit_id_str;
                                        let batch_requests = fin_batch_requests;
                                        let sector_boundaries = fin_sector_boundaries;

                                        // --- Begin result processing (same as monolithic path) ---
                                        if let Some(ref parent_id) = parent_job_id {
                                            let p_idx = partition_index.unwrap();
                                            crate::engine::process_partition_result(
                                                &mut t, result, parent_id, p_idx,
                                                synth_duration, proof_kind, worker_id,
                                                &circuit_id_str, submitted_at,
                                            );
                                            return;
                                        }

                                        crate::engine::process_monolithic_result(
                                            &mut t, result, &job_id, proof_kind, worker_id,
                                            &circuit_id_str, synth_duration, submitted_at,
                                            is_batched, &batch_requests, &sector_boundaries,
                                        );
                                    });

                                    // GPU worker continues — don't process result here
                                    return;
                                }
                                Ok(Err(e)) => {
                                    // gpu_prove_start itself failed (before GPU kernels)
                                    error!(error = %e, "GPU prove start failed");
                                    let mut t = tracker.lock().await;
                                    if let Some(w) = t.workers.get_mut(worker_id as usize) {
                                        w.current_job = None;
                                    }
                                    t.record_failure(proof_kind);
                                    let status = JobStatus::Failed(e.to_string());
                                    let target_id = parent_job_id.as_ref().unwrap_or(&job_id);
                                    if let Some(senders) = t.pending.remove(target_id) {
                                        for sender in senders {
                                            let _ = sender.send(status.clone());
                                        }
                                    }
                                    t.completed.insert(target_id.clone(), status);
                                    return;
                                }
                                Err(e) => {
                                    // spawn_blocking panicked
                                    error!(error = %e, "GPU prove start task panicked");
                                    let mut t = tracker.lock().await;
                                    if let Some(w) = t.workers.get_mut(worker_id as usize) {
                                        w.current_job = None;
                                    }
                                    t.record_failure(proof_kind);
                                    let status = JobStatus::Failed(format!("GPU start panicked: {}", e));
                                    let target_id = parent_job_id.as_ref().unwrap_or(&job_id);
                                    if let Some(senders) = t.pending.remove(target_id) {
                                        for sender in senders {
                                            let _ = sender.send(status.clone());
                                        }
                                    }
                                    t.completed.insert(target_id.clone(), status);
                                    return;
                                }
                            }

                            #[cfg(not(feature = "cuda-supraseal"))]
                            {
                            let result: Result<Result<(Vec<u8>, Duration)>, tokio::task::JoinError> = {
                                let _ = (gpu_str, synth_job);
                                Ok(Err(anyhow::anyhow!("GPU proving requires cuda-supraseal feature")))
                            };

                            // Process result and notify callers (non-supraseal fallback)
                            let mut t = tracker.lock().await;

                            // Clear current job from worker
                            if let Some(w) = t.workers.get_mut(worker_id as usize) {
                                w.current_job = None;
                            }

                            // ── Phase 7: Partition-aware result routing ──────────────
                            if let Some(ref parent_id) = parent_job_id {
                                let p_idx = partition_index.unwrap();
                                process_partition_result(
                                    &mut t, result, parent_id, p_idx,
                                    synth_duration, proof_kind, worker_id,
                                    &circuit_id_str, submitted_at,
                                );
                                return;
                            }

                            // ── Monolithic result delivery ─────────
                            process_monolithic_result(
                                &mut t, result, &job_id, proof_kind, worker_id,
                                &circuit_id_str, synth_duration, submitted_at,
                                is_batched, &batch_requests, &sector_boundaries,
                            );
                            } // #[cfg(not(feature = "cuda-supraseal"))]
                        }.instrument(span).await;
                    }
                    info!(worker_id = worker_id, "pipeline GPU worker stopped");
                });
                } // for worker_sub_id
            } // for gpu_idx
        } else {
            // ─── Phase 1: Monolithic workers ───────────────────────────────
            //
            // Each GPU worker does the full cycle: pull from scheduler →
            // synthesize + GPU prove on a blocking thread → complete.
            // No overlap between synthesis and GPU.

            for state in &worker_states {
                let worker_id = state.worker_id;
                let gpu_ordinal = state.gpu_ordinal;
                let scheduler = self.scheduler.clone();
                let tracker = self.tracker.clone();
                let mut shutdown_rx = self.shutdown_rx.clone();
                let param_cache = self.config.srs.param_cache.clone();

                tokio::spawn(async move {
                    info!(worker_id = worker_id, gpu = gpu_ordinal, "monolithic GPU worker started");
                    loop {
                        let request = tokio::select! {
                            biased;
                            _ = shutdown_rx.changed() => {
                                if *shutdown_rx.borrow() {
                                    info!(worker_id = worker_id, "monolithic GPU worker received shutdown signal");
                                    break;
                                }
                                continue;
                            }
                            request = scheduler.next() => request,
                        };

                        let job_id = request.job_id.clone();
                        let proof_kind = request.proof_kind;
                        let span = info_span!("monolithic_worker", worker_id = worker_id, gpu = gpu_ordinal, job_id = %job_id, proof_kind = %proof_kind);

                        async {
                            info!("processing job (monolithic)");

                            // Mark as running on this worker
                            {
                                let mut t = tracker.lock().await;
                                if let Some(w) = t.workers.get_mut(worker_id as usize) {
                                    w.current_job = Some((job_id.clone(), proof_kind));
                                }
                            }

                            let param_cache_str = param_cache.to_string_lossy().to_string();
                            let gpu_str = gpu_ordinal.to_string();
                            let vanilla = request.vanilla_proof.clone();
                            let vanilla_proofs = request.vanilla_proofs.clone();
                            let registered_proof = request.registered_proof;
                            let sector_number = request.sector_number;
                            let miner_id = request.miner_id;
                            let randomness = request.randomness.clone();
                            let partition_index = request.partition_index;
                            let comm_r_old = request.comm_r_old.clone();
                            let comm_r_new = request.comm_r_new.clone();
                            let comm_d_new = request.comm_d_new.clone();
                            let submitted_at = request.submitted_at;
                            let jid = job_id.0.clone();

                            let result = tokio::task::spawn_blocking(move || -> Result<(Vec<u8>, ProofTimings)> {
                                std::env::set_var("CUDA_VISIBLE_DEVICES", &gpu_str);
                                std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", &param_cache_str);

                                match proof_kind {
                                    ProofKind::PoRepSealCommit => {
                                        prover::prove_porep_c2(&vanilla, sector_number, miner_id, &jid)
                                    }
                                    ProofKind::WinningPost => {
                                        prover::prove_winning_post(&vanilla_proofs, registered_proof, miner_id, &randomness, &jid)
                                    }
                                    ProofKind::WindowPostPartition => {
                                        prover::prove_window_post(&vanilla_proofs, registered_proof, miner_id, &randomness, partition_index, &jid)
                                    }
                                    ProofKind::SnapDealsUpdate => {
                                        prover::prove_snap_deals(vanilla_proofs, registered_proof, &comm_r_old, &comm_r_new, &comm_d_new, &jid)
                                    }
                                }
                            }).await;

                            let queue_wait = submitted_at.elapsed().saturating_sub(
                                match &result {
                                    Ok(Ok((_, t))) => t.total,
                                    _ => Duration::ZERO,
                                }
                            );
                            let sector_size_gib = (request.sector_size / (1024 * 1024 * 1024)) as u32;
                            let circuit_id = proof_kind.circuit_id(if sector_size_gib > 0 { sector_size_gib } else { 32 });

                            let status = match result {
                                Ok(Ok((proof_bytes, mut timings))) => {
                                    timings.queue_wait = queue_wait;
                                    timings.total = submitted_at.elapsed();
                                    info!(
                                        proof_len = proof_bytes.len(),
                                        total_ms = timings.total.as_millis(),
                                        queue_ms = timings.queue_wait.as_millis(),
                                        deser_ms = timings.deserialize.as_millis(),
                                        prove_ms = timings.proving.as_millis(),
                                        "proof completed successfully (monolithic)"
                                    );
                                    JobStatus::Completed(ProofResult {
                                        job_id: job_id.clone(),
                                        proof_kind,
                                        proof_bytes,
                                        timings,
                                    })
                                }
                                Ok(Err(e)) => {
                                    error!(error = %e, "proof failed");
                                    JobStatus::Failed(e.to_string())
                                }
                                Err(e) => {
                                    error!(error = %e, "proof task panicked");
                                    JobStatus::Failed(format!("task panicked: {}", e))
                                }
                            };

                            let mut t = tracker.lock().await;
                            if let Some(w) = t.workers.get_mut(worker_id as usize) {
                                w.current_job = None;
                                if matches!(&status, JobStatus::Completed(_)) {
                                    w.last_circuit_id = Some(circuit_id);
                                }
                            }
                            match &status {
                                JobStatus::Completed(result) => {
                                    t.record_completion(proof_kind, result.timings.total);
                                }
                                JobStatus::Failed(_) => {
                                    t.record_failure(proof_kind);
                                }
                                _ => {}
                            }

                            if let Some(senders) = t.pending.remove(&job_id) {
                                for sender in senders {
                                    let _ = sender.send(status.clone());
                                }
                            }
                            t.completed.insert(job_id, status);
                        }.instrument(span).await;
                    }
                    info!(worker_id = worker_id, "monolithic GPU worker stopped");
                });
            }
        }

        info!(
            num_workers = num_workers,
            pipeline = self.pipeline_enabled,
            "cuzk engine started"
        );
        Ok(())
    }

    /// Submit a proof request. Returns (job_id, queue_position).
    pub async fn submit(&self, mut request: ProofRequest) -> Result<(JobId, u32)> {
        // Generate job ID if not provided
        if request.job_id.0.is_empty() {
            request.job_id = JobId(uuid::Uuid::new_v4().to_string());
        }
        let job_id = request.job_id.clone();

        // Create completion channel
        let (tx, _rx) = oneshot::channel();
        {
            let mut t = self.tracker.lock().await;
            t.pending.entry(job_id.clone()).or_default().push(tx);
        }

        let position = self.scheduler.submit(request).await;
        Ok((job_id, position))
    }

    /// Submit a proof and wait for completion (Prove RPC).
    pub async fn prove(&self, request: ProofRequest) -> Result<JobStatus> {
        let job_id = if request.job_id.0.is_empty() {
            JobId(uuid::Uuid::new_v4().to_string())
        } else {
            request.job_id.clone()
        };

        // Create completion channel
        let (tx, rx) = oneshot::channel();
        {
            let mut t = self.tracker.lock().await;
            t.pending.entry(job_id.clone()).or_default().push(tx);
        }

        let mut request = request;
        request.job_id = job_id.clone();
        self.scheduler.submit(request).await;

        // Wait for completion
        match rx.await {
            Ok(status) => Ok(status),
            Err(_) => Err(EngineError::Internal("completion channel dropped".into()).into()),
        }
    }

    /// Await a previously submitted proof.
    ///
    /// If the proof is already complete, returns immediately.
    /// Otherwise, registers a listener and blocks until completion.
    pub async fn await_proof(&self, job_id: &JobId) -> Result<JobStatus> {
        let rx = {
            let mut t = self.tracker.lock().await;
            // Check if already completed
            if let Some(status) = t.completed.get(job_id) {
                return Ok(status.clone());
            }
            // Check if it's pending — register a new listener
            if t.pending.contains_key(job_id) {
                let (tx, rx) = oneshot::channel();
                t.pending.entry(job_id.clone()).or_default().push(tx);
                Some(rx)
            } else {
                None
            }
        };

        match rx {
            Some(rx) => match rx.await {
                Ok(status) => Ok(status),
                Err(_) => Err(EngineError::Internal("completion channel dropped".into()).into()),
            },
            None => Err(EngineError::JobNotFound(job_id.clone()).into()),
        }
    }

    /// Get engine status.
    pub async fn get_status(&self) -> EngineStatus {
        let tracker = self.tracker.lock().await;
        let queue_stats = self.scheduler.stats().await;
        let preloaded = self.preloaded_srs.read().await;

        EngineStatus {
            uptime_seconds: self.started_at.elapsed().as_secs(),
            total_completed: tracker.total_completed,
            total_failed: tracker.total_failed,
            queue_depth: self.scheduler.len().await as u32,
            workers: tracker.workers.clone(),
            queue_stats,
            preloaded_srs: preloaded.keys().cloned().collect(),
            completed_by_kind: tracker.completed_by_kind.clone(),
            failed_by_kind: tracker.failed_by_kind.clone(),
            recent_durations: tracker.recent_durations.clone(),
        }
    }

    /// Get proof stats for Prometheus metrics.
    pub async fn get_proof_stats(&self) -> ProofStats {
        let tracker = self.tracker.lock().await;
        ProofStats {
            completed_by_kind: tracker.completed_by_kind.iter().map(|(k, v)| (*k, *v)).collect(),
            failed_by_kind: tracker.failed_by_kind.iter().map(|(k, v)| (*k, *v)).collect(),
            recent_durations: tracker.recent_durations.clone(),
        }
    }

    /// Preload an SRS entry.
    pub async fn preload_srs(&self, circuit_id: &str) -> Result<(bool, u64)> {
        let srs = self.preloaded_srs.read().await;
        if srs.contains_key(circuit_id) {
            return Ok((true, 0));
        }
        drop(srs);

        let param_cache = self.config.srs.param_cache.clone();
        let cid = circuit_id.to_string();
        let elapsed = tokio::task::spawn_blocking(move || {
            prover::preload_srs(&cid, &param_cache)
        })
        .await??;

        let mut srs = self.preloaded_srs.write().await;
        let ms = elapsed.as_millis() as u64;
        srs.insert(circuit_id.to_string(), ms);
        Ok((false, ms))
    }

    /// Shutdown the engine gracefully.
    ///
    /// Signals all GPU workers to stop picking up new jobs. Currently
    /// running proofs will be allowed to finish.
    pub async fn shutdown(&self) {
        info!("shutting down cuzk engine");
        let _ = self.shutdown_tx.send(true);
    }
}

/// Engine status snapshot.
#[derive(Debug)]
pub struct EngineStatus {
    pub uptime_seconds: u64,
    pub total_completed: u64,
    pub total_failed: u64,
    pub queue_depth: u32,
    /// Per-worker state (replaces single `running_job`).
    pub workers: Vec<WorkerState>,
    pub queue_stats: Vec<(ProofKind, u32)>,
    pub preloaded_srs: Vec<String>,
    pub completed_by_kind: HashMap<ProofKind, u64>,
    pub failed_by_kind: HashMap<ProofKind, u64>,
    pub recent_durations: Vec<(ProofKind, Duration)>,
}
