//! Engine — the central coordinator of the cuzk proving daemon.
//!
//! Owns the scheduler, GPU workers, and SRS manager. Provides the public API
//! for submitting proofs, querying status, and managing SRS.
//!
//! Phase 1: Multi-GPU worker pool. Each worker is bound to a specific GPU
//! via `CUDA_VISIBLE_DEVICES`. The scheduler dispatches jobs to workers,
//! preferring workers whose GPU already has the required SRS loaded (affinity).

use anyhow::Result;
use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::sync::{Mutex, oneshot, watch, RwLock};
use tracing::{error, info, info_span, warn, Instrument};

use crate::config::Config;
use crate::prover;
use crate::scheduler::Scheduler;
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
    preloaded_srs: Arc<RwLock<HashMap<String, u64>>>,
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
        Self {
            config,
            scheduler: Arc::new(Scheduler::new()),
            tracker: Arc::new(Mutex::new(JobTracker::new(vec![]))),
            started_at: Instant::now(),
            shutdown_tx,
            shutdown_rx,
            preloaded_srs: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    /// Start the engine: preload SRS, spawn GPU worker(s).
    pub async fn start(&self) -> Result<()> {
        info!("starting cuzk engine");

        // Preload configured SRS entries
        let param_cache = self.config.srs.param_cache.clone();
        for circuit_id in &self.config.srs.preload {
            match prover::preload_srs(circuit_id, &param_cache) {
                Ok(elapsed) => {
                    let mut srs = self.preloaded_srs.write().await;
                    srs.insert(circuit_id.clone(), elapsed.as_millis() as u64);
                    info!(
                        circuit_id = circuit_id.as_str(),
                        elapsed_ms = elapsed.as_millis(),
                        "SRS preloaded"
                    );
                }
                Err(e) => {
                    warn!(circuit_id = circuit_id.as_str(), error = %e, "SRS preload failed");
                }
            }
        }

        // Detect GPUs and create worker states
        let gpu_ordinals = detect_gpu_ordinals(&self.config);
        let num_workers = gpu_ordinals.len();
        info!(num_workers = num_workers, gpus = ?gpu_ordinals, "initializing GPU workers");

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

        // Store worker states in tracker
        {
            let mut tracker = self.tracker.lock().await;
            tracker.workers = worker_states.clone();
        }

        // Spawn one worker task per GPU
        for state in &worker_states {
            let worker_id = state.worker_id;
            let gpu_ordinal = state.gpu_ordinal;
            let scheduler = self.scheduler.clone();
            let tracker = self.tracker.clone();
            let mut shutdown_rx = self.shutdown_rx.clone();
            let param_cache = self.config.srs.param_cache.clone();

            tokio::spawn(async move {
                info!(worker_id = worker_id, gpu = gpu_ordinal, "GPU worker started");
                loop {
                    let request = tokio::select! {
                        biased;
                        _ = shutdown_rx.changed() => {
                            if *shutdown_rx.borrow() {
                                info!(worker_id = worker_id, "GPU worker received shutdown signal, draining");
                                break;
                            }
                            continue;
                        }
                        request = scheduler.next() => request,
                    };

                    let job_id = request.job_id.clone();
                    let proof_kind = request.proof_kind;
                    let span = info_span!("gpu_worker", worker_id = worker_id, gpu = gpu_ordinal, job_id = %job_id, proof_kind = %proof_kind);

                    async {
                        info!("processing job");

                        // Mark as running on this worker
                        {
                            let mut t = tracker.lock().await;
                            if let Some(w) = t.workers.get_mut(worker_id as usize) {
                                w.current_job = Some((job_id.clone(), proof_kind));
                            }
                        }

                        // Set param cache and GPU device for this worker
                        let param_cache_str = param_cache.to_string_lossy().to_string();
                        let gpu_str = gpu_ordinal.to_string();

                        // Capture fields for the blocking task
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

                        // Execute proof on a blocking thread (proving is CPU/GPU-bound)
                        let result = tokio::task::spawn_blocking(move || -> Result<(Vec<u8>, ProofTimings)> {
                            // Set environment variables for this worker thread.
                            // CUDA_VISIBLE_DEVICES constrains which GPU is visible.
                            // FIL_PROOFS_PARAMETER_CACHE tells filecoin-proofs where to find params.
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

                        // Process result
                        let queue_wait = submitted_at.elapsed().saturating_sub(
                            match &result {
                                Ok(Ok((_, t))) => t.total,
                                _ => Duration::ZERO,
                            }
                        );

                        // Compute the circuit_id for SRS affinity tracking
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
                                    "proof completed successfully"
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

                        // Update tracker
                        let mut t = tracker.lock().await;
                        if let Some(w) = t.workers.get_mut(worker_id as usize) {
                            w.current_job = None;
                            // Track last SRS for affinity
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

                        // Send result to all awaiting clients
                        if let Some(senders) = t.pending.remove(&job_id) {
                            for sender in senders {
                                let _ = sender.send(status.clone());
                            }
                        }
                        // Also store for late AwaitProof calls
                        t.completed.insert(job_id, status);
                    }.instrument(span).await;
                }
                info!(worker_id = worker_id, "GPU worker stopped");
            });
        }

        info!(num_workers = num_workers, "cuzk engine started");
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
