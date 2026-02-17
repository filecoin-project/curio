//! Engine — the central coordinator of the cuzk proving daemon.
//!
//! Owns the scheduler, GPU workers, and SRS manager. Provides the public API
//! for submitting proofs, querying status, and managing SRS.

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

/// Shared state for tracking in-flight and completed jobs.
struct JobTracker {
    /// Map from job_id → completion channel sender.
    pending: HashMap<JobId, Vec<oneshot::Sender<JobStatus>>>,
    /// Map from job_id → result (for completed jobs, retained for AwaitProof).
    completed: HashMap<JobId, JobStatus>,
    /// Currently running job (Phase 0: single worker).
    running: Option<(JobId, ProofKind)>,
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
    fn new() -> Self {
        Self {
            pending: HashMap::new(),
            completed: HashMap::new(),
            running: None,
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

impl Engine {
    /// Create a new engine with the given configuration.
    pub fn new(config: Config) -> Self {
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        Self {
            config,
            scheduler: Arc::new(Scheduler::new()),
            tracker: Arc::new(Mutex::new(JobTracker::new())),
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

        // Spawn the GPU worker loop (Phase 0: single worker)
        let scheduler = self.scheduler.clone();
        let tracker = self.tracker.clone();
        let mut shutdown_rx = self.shutdown_rx.clone();
        let param_cache = self.config.srs.param_cache.clone();

        tokio::spawn(async move {
            info!("GPU worker started (worker_id=0)");
            loop {
                let request = tokio::select! {
                    biased;
                    _ = shutdown_rx.changed() => {
                        if *shutdown_rx.borrow() {
                            // Drain: finish current job but don't pick up new ones
                            info!("GPU worker received shutdown signal, draining");
                            break;
                        }
                        continue;
                    }
                    request = scheduler.next() => request,
                };

                let job_id = request.job_id.clone();
                let proof_kind = request.proof_kind;
                let span = info_span!("gpu_worker", job_id = %job_id, proof_kind = %proof_kind);

                async {
                    info!("processing job");

                    // Mark as running
                    {
                        let mut t = tracker.lock().await;
                        t.running = Some((job_id.clone(), proof_kind));
                    }

                    // Set param cache for this worker
                    std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", &param_cache);

                    // Capture fields for the blocking task
                    let vanilla = request.vanilla_proof.clone();
                    let sector_number = request.sector_number;
                    let miner_id = request.miner_id;
                    let randomness = request.randomness.clone();
                    let partition_index = request.partition_index;
                    let sector_key_cid = request.sector_key_cid.clone();
                    let new_sealed_cid = request.new_sealed_cid.clone();
                    let new_unsealed_cid = request.new_unsealed_cid.clone();
                    let submitted_at = request.submitted_at;
                    let jid = job_id.0.clone();

                    // Execute proof on a blocking thread (proving is CPU/GPU-bound)
                    let result = tokio::task::spawn_blocking(move || -> Result<(Vec<u8>, ProofTimings)> {
                        match proof_kind {
                            ProofKind::PoRepSealCommit => {
                                prover::prove_porep_c2(&vanilla, sector_number, miner_id, &jid)
                            }
                            ProofKind::WinningPost => {
                                prover::prove_winning_post(&vanilla, miner_id, &randomness, &jid)
                            }
                            ProofKind::WindowPostPartition => {
                                prover::prove_window_post(&vanilla, miner_id, &randomness, partition_index, &jid)
                            }
                            ProofKind::SnapDealsUpdate => {
                                prover::prove_snap_deals(&vanilla, &sector_key_cid, &new_sealed_cid, &new_unsealed_cid, &jid)
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
                    t.running = None;
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
            info!("GPU worker stopped");
        });

        info!("cuzk engine started");
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
            running_job: tracker.running.clone(),
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
    /// Signals the GPU worker to stop picking up new jobs. The currently
    /// running proof (if any) will be allowed to finish.
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
    pub running_job: Option<(JobId, ProofKind)>,
    pub queue_stats: Vec<(ProofKind, u32)>,
    pub preloaded_srs: Vec<String>,
    pub completed_by_kind: HashMap<ProofKind, u64>,
    pub failed_by_kind: HashMap<ProofKind, u64>,
    pub recent_durations: Vec<(ProofKind, Duration)>,
}
