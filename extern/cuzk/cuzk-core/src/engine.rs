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
use std::collections::{BTreeMap, HashMap};
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering as AtomicOrdering};
use std::time::{Duration, Instant};
use tokio::sync::{Mutex, Notify, oneshot, watch, RwLock};
use tracing::{debug, error, info, info_span, warn, Instrument};

// ─── Priority Work Queue ────────────────────────────────────────────────────
//
// A priority queue that pops items in order of (job_seq ASC, partition_idx ASC).
// This ensures the GPU and synthesis workers always pick the lowest partition
// in the oldest pipeline — completing jobs sequentially rather than interleaving
// partitions randomly across pipelines.
//
// Uses BTreeMap for O(log n) insert and O(log n) pop-min. The key is
// (job_seq, partition_idx) where job_seq is a monotonically increasing counter
// assigned when each pipeline is dispatched. Lower key = higher priority.

struct PriorityWorkQueue<T> {
    inner: std::sync::Mutex<BTreeMap<(u64, usize), T>>,
    notify: Notify,
}

impl<T> PriorityWorkQueue<T> {
    fn new() -> Self {
        Self {
            inner: std::sync::Mutex::new(BTreeMap::new()),
            notify: Notify::new(),
        }
    }

    /// Insert an item with the given priority key.
    /// Wakes one blocked `pop()` caller (or stores a permit for the next one).
    fn push(&self, job_seq: u64, partition_idx: usize, item: T) {
        let mut map = self.inner.lock().unwrap();
        map.insert((job_seq, partition_idx), item);
        drop(map);
        self.notify.notify_one();
    }

    /// Try to remove the highest-priority item (smallest key).
    /// If successful and more items remain, wakes the next waiter.
    fn try_pop(&self) -> Option<T> {
        let mut map = self.inner.lock().unwrap();
        let item = map.first_entry().map(|e| e.remove());
        if item.is_some() && !map.is_empty() {
            drop(map);
            self.notify.notify_one();
        }
        item
    }

    /// Number of items currently in the queue.
    fn len(&self) -> usize {
        self.inner.lock().unwrap().len()
    }

    /// Wait until an item might be available.
    /// Callers should call `try_pop()` after this returns.
    async fn notified(&self) {
        self.notify.notified().await;
    }
}

// ─── Dispatch Pacer (PI controller with GPU rate feed-forward) ─────────────
//
// Regulates synthesis dispatch rate to maintain `target` synthesized partitions
// waiting in the GPU queue.
//
//   Feed-forward: actual GPU processing time / num_workers (from worker reports)
//   Feedback:     PI correction on error = (target - EMA_waiting)
//   Backpressure: memory budget naturally limits synthesis concurrency
//
// The GPU rate is measured from actual worker processing durations (not
// inter-completion intervals), making it immune to idle gaps and pipeline
// fill delays. With N interleaved GPU workers, the effective dispatch
// interval is processing_time / N.
//
// No synthesis throughput cap — that creates a self-reinforcing collapse:
// slow dispatch → fewer concurrent synths → slower throughput → tighter
// cap → even slower dispatch. Instead, the memory budget provides natural
// backpressure: budget.acquire() blocks when too many partitions are
// in-flight, which is the correct concurrency limit.
//
// Bootstrap (initial + re-entry): dispatches `target` items at moderate
// spacing (3s initial, max(2s, gpu_eff) for re-bootstrap). Initial bootstrap
// is slow to avoid flooding the pinned memory pool with cudaHostAlloc calls.
// Re-bootstrap triggers when the pipeline drains (between proof batches)
// and new work arrives.

struct DispatchPacer {
    target: f64,

    // Smoothed waiting count (gpu_work_queue.len())
    ema_waiting: f64,
    alpha_waiting: f64,

    // GPU processing rate tracking (from actual worker durations)
    //
    // GPU workers atomically accumulate their processing time into a shared
    // counter. The pacer computes avg processing time per partition from
    // deltas. Effective dispatch interval = processing_time / num_workers.
    //
    // Immune to pipeline fill delays, idle gaps, and correctly handles
    // multiple interleaved GPU workers.
    ema_gpu_processing_s: f64, // EMA of per-partition GPU processing time
    gpu_rate_known: bool,      // false until first measurement
    prev_gpu_count: u64,
    prev_gpu_total_ns: u64,    // previous reading of gpu_processing_total_ns
    alpha_gpu: f64,
    num_gpu_workers: usize,

    // Synthesis throughput tracking (monitoring only, not used for control)
    ema_synth_interval_s: f64,
    synth_rate_known: bool,
    synth_completions_seen: u64,
    last_synth_event: Instant,
    prev_synth_count: u64,
    alpha_synth: f64,
    synth_warmup: u64,

    // PI state
    //
    // Error is normalized: (target - ema_waiting) / target, giving a
    // range of roughly [-2, +1] in normal operation. This makes the
    // gains independent of the target value.
    //
    // The integral is asymmetrically clamped: the negative limit is
    // much smaller than the positive limit. Going negative means
    // "dispatch slower than GPU rate" which is rarely correct — it
    // drains the pipeline. The positive limit allows sustained fast
    // dispatch to fill the pipe.
    integral_error: f64,
    last_pi_update: Instant,
    kp: f64,
    ki: f64,
    max_integral_pos: f64,  // positive limit (dispatch faster)
    max_integral_neg: f64,  // negative limit (dispatch slower — small!)

    // Bootstrap: dispatch `target` items at moderate spacing
    bootstrap_remaining: usize,
    rebootstrap_count: u64, // how many times we re-entered bootstrap

    // Logging
    total_dispatched: u64,
}

impl DispatchPacer {
    fn new(target: usize, num_gpu_workers: usize) -> Self {
        let now = Instant::now();
        Self {
            target: target as f64,
            ema_waiting: 0.0,
            alpha_waiting: 0.15,
            ema_gpu_processing_s: 1.0,
            gpu_rate_known: false,
            prev_gpu_count: 0,
            prev_gpu_total_ns: 0,
            alpha_gpu: 0.2,
            num_gpu_workers: num_gpu_workers.max(1),
            ema_synth_interval_s: 1.0,
            synth_rate_known: false,
            synth_completions_seen: 0,
            last_synth_event: now,
            prev_synth_count: 0,
            alpha_synth: 0.25,
            synth_warmup: 8,
            integral_error: 0.0,
            last_pi_update: now,
            // Gains tuned for normalized error (error/target ∈ [-2, +1]).
            //
            // P (kp=0.5): immediate response to queue depth error.
            //   half-empty → +25% rate, empty → +50%, overfull → -50%
            //
            // I (ki=0.001): very slow trim that takes minutes to saturate.
            //   Integral accumulates norm_error*dt (≈ ±0.5/s typical).
            //   At error=0.5: reaches max (100) in ~200s (3.3 min).
            //   Max correction: ki*100 = ±0.10 — gentle persistent nudge.
            //   Asymmetric: negative cap (-20) limits slowdown authority.
            kp: 0.5,
            ki: 0.001,
            max_integral_pos: 100.0,  // slow to saturate, max correction +0.10
            max_integral_neg: -20.0,  // max correction -0.02
            bootstrap_remaining: target,
            rebootstrap_count: 0,
            total_dispatched: 0,
        }
    }

    /// Update state with current observations.
    fn update(&mut self, waiting: usize, gpu_count: u64, gpu_total_ns: u64, synth_count: u64) {
        let now = Instant::now();

        // --- GPU rate (from actual processing durations) ---
        let new_gpu = gpu_count.saturating_sub(self.prev_gpu_count);
        if new_gpu > 0 {
            let delta_ns = gpu_total_ns.saturating_sub(self.prev_gpu_total_ns);
            let avg_processing_s = (delta_ns as f64 / new_gpu as f64) / 1_000_000_000.0;
            if avg_processing_s > 0.0 {
                if self.gpu_rate_known {
                    self.ema_gpu_processing_s =
                        self.alpha_gpu * avg_processing_s + (1.0 - self.alpha_gpu) * self.ema_gpu_processing_s;
                } else {
                    self.ema_gpu_processing_s = avg_processing_s;
                    self.gpu_rate_known = true;
                }
            }
            self.prev_gpu_count = gpu_count;
            self.prev_gpu_total_ns = gpu_total_ns;
        }

        // --- Synthesis throughput (monitoring only) ---
        let new_synth = synth_count.saturating_sub(self.prev_synth_count);
        if new_synth > 0 {
            self.synth_completions_seen += new_synth;
            let elapsed = now.duration_since(self.last_synth_event).as_secs_f64();
            let avg_interval = elapsed / new_synth as f64;
            if self.synth_completions_seen > self.synth_warmup {
                if self.synth_rate_known {
                    self.ema_synth_interval_s =
                        self.alpha_synth * avg_interval + (1.0 - self.alpha_synth) * self.ema_synth_interval_s;
                } else {
                    self.ema_synth_interval_s = avg_interval;
                    self.synth_rate_known = true;
                }
            }
            self.last_synth_event = now;
            self.prev_synth_count = synth_count;
        }

        // --- Waiting EMA ---
        self.ema_waiting =
            self.alpha_waiting * (waiting as f64) + (1.0 - self.alpha_waiting) * self.ema_waiting;

        // --- PI integral (normalized error, asymmetric clamp) ---
        let dt = now.duration_since(self.last_pi_update).as_secs_f64().max(0.001);
        let norm_error = (self.target - self.ema_waiting) / self.target;
        self.integral_error = (self.integral_error + norm_error * dt)
            .clamp(self.max_integral_neg, self.max_integral_pos);
        self.last_pi_update = now;
    }

    /// Compute the dispatch interval.
    fn interval(&self) -> Duration {
        // Bootstrap: moderate spacing to avoid flooding pinned pool
        if self.bootstrap_remaining > 0 {
            if self.gpu_rate_known {
                // Re-bootstrap: known GPU rate, fill at moderate speed
                let gpu_eff = self.ema_gpu_processing_s / self.num_gpu_workers as f64;
                return Duration::from_secs_f64(gpu_eff.max(2.0));
            } else {
                // Initial bootstrap: slow start (pinned pool cold)
                return Duration::from_secs(3);
            }
        }

        if !self.gpu_rate_known {
            return Duration::from_secs(3);
        }

        // PI control (normalized error)
        let norm_error = (self.target - self.ema_waiting) / self.target;
        let correction = self.kp * norm_error + self.ki * self.integral_error;
        let base_interval_s = self.ema_gpu_processing_s / self.num_gpu_workers as f64;
        let rate_mult = (1.0 + correction).clamp(0.3, 3.0);
        let pi_interval_s = base_interval_s / rate_mult;

        Duration::from_millis((pi_interval_s * 1000.0).clamp(50.0, 60000.0) as u64)
    }

    /// Whether we're in bootstrap (initial or re-bootstrap).
    fn in_bootstrap(&self) -> bool {
        self.bootstrap_remaining > 0
    }

    /// Whether initial bootstrap is exhausted and we need GPU data.
    fn bootstrap_exhausted(&self) -> bool {
        !self.gpu_rate_known && self.bootstrap_remaining == 0
    }

    /// Check if we should re-enter bootstrap (pipeline truly drained).
    ///
    /// Only triggers when ALL dispatched items have completed GPU processing
    /// (nothing in synthesis, nothing in GPU queue). This prevents re-bootstrap
    /// spam when items are still in the synthesis pipeline — they'll arrive
    /// at the GPU queue eventually, so re-bootstrapping would just waste time
    /// and budget.
    fn should_rebootstrap(&self, gpu_count: u64) -> bool {
        self.gpu_rate_known
            && self.bootstrap_remaining == 0
            && self.ema_waiting < 1.0
            && self.total_dispatched <= gpu_count // pipeline truly empty
    }

    /// Re-enter bootstrap to fill the pipeline after it drained.
    /// Resets integral (stale from previous batch) and sets bootstrap count.
    /// GPU rate EMA is preserved (it's a hardware characteristic).
    fn enter_rebootstrap(&mut self) {
        self.bootstrap_remaining = self.target as usize;
        self.integral_error = 0.0;
        self.rebootstrap_count += 1;
    }

    fn on_dispatch(&mut self) {
        if self.bootstrap_remaining > 0 {
            self.bootstrap_remaining -= 1;
        }
        self.total_dispatched += 1;
    }
}

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
    st: &crate::status::StatusTracker,
) {
    st.partition_gpu_end(&parent_id.0, p_idx, worker_id);
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
                    {
                        let trim_t = Instant::now();
                        unsafe { libc::malloc_trim(0); }
                        info!(
                            job_id = %parent_id,
                            partition = p_idx,
                            malloc_trim_ms = trim_t.elapsed().as_millis(),
                            "FIN_TIMING malloc_trim (discard path)"
                        );
                    }
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
                {
                    let trim_t = Instant::now();
                    unsafe { libc::malloc_trim(0); }
                    info!(
                        job_id = %parent_id,
                        partition = p_idx,
                        malloc_trim_ms = trim_t.elapsed().as_millis(),
                        "FIN_TIMING malloc_trim (success path)"
                    );
                }

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

                    // Self-check: verify the assembled PoRep proof before returning.
                    // If the self-check fails, return JobStatus::Failed so the caller
                    // doesn't receive an invalid proof. This catches intermittent GPU
                    // proving errors that produce bad partition proofs.
                    let mut self_check_passed = true;
                    if state.proof_kind == ProofKind::PoRepSealCommit {
                        match crate::prover::verify_porep_proof(
                            &state.request.vanilla_proof,
                            &final_proof,
                            state.request.sector_number,
                            state.request.miner_id,
                            &parent_id.0,
                        ) {
                            Ok(true) => {
                                info!(job_id = %parent_id, "Phase 7: PoRep proof self-check PASSED");
                                // If CUZK_VALIDATE_PARTITIONS=1, also run per-partition verifier
                                // against the known-good proof to confirm the verifier itself works.
                                if std::env::var("CUZK_VALIDATE_PARTITIONS").as_deref() == Ok("1") {
                                    info!(job_id = %parent_id, "CUZK_VALIDATE_PARTITIONS=1: validating per-partition verifier against known-good proof");
                                    match crate::prover::verify_porep_partitions(
                                        &state.request.vanilla_proof,
                                        &final_proof,
                                        state.request.sector_number,
                                        state.request.miner_id,
                                        &parent_id.0,
                                    ) {
                                        Ok(results) => {
                                            let valid: Vec<usize> = results.iter().enumerate()
                                                .filter(|(_, &v)| v).map(|(i, _)| i).collect();
                                            let invalid: Vec<usize> = results.iter().enumerate()
                                                .filter(|(_, &v)| !v).map(|(i, _)| i).collect();
                                            if invalid.is_empty() {
                                                info!(
                                                    job_id = %parent_id,
                                                    "VERIFIER VALIDATION: per-partition verifier confirmed correct (all 10 partitions valid on known-good proof)"
                                                );
                                            } else {
                                                warn!(
                                                    job_id = %parent_id,
                                                    valid_partitions = ?valid,
                                                    invalid_partitions = ?invalid,
                                                    "VERIFIER VALIDATION FAILED: per-partition verifier reports invalid partitions on a known-good proof — verifier is BUGGY"
                                                );
                                            }
                                        }
                                        Err(e) => {
                                            warn!(
                                                job_id = %parent_id,
                                                error = %e,
                                                "VERIFIER VALIDATION: per-partition verifier errored on known-good proof"
                                            );
                                        }
                                    }
                                }
                            }
                            Ok(false) => {
                                self_check_passed = false;
                                error!(
                                    job_id = %parent_id,
                                    proof_len = final_proof.len(),
                                    sector = state.request.sector_number,
                                    miner = state.request.miner_id,
                                    "Phase 7: PoRep proof self-check FAILED — proof will NOT be returned to caller"
                                );
                                // Run per-partition verification to diagnose which partition(s) are bad
                                match crate::prover::verify_porep_partitions(
                                    &state.request.vanilla_proof,
                                    &final_proof,
                                    state.request.sector_number,
                                    state.request.miner_id,
                                    &parent_id.0,
                                ) {
                                    Ok(results) => {
                                        let valid: Vec<usize> = results.iter().enumerate()
                                            .filter(|(_, &v)| v).map(|(i, _)| i).collect();
                                        let invalid: Vec<usize> = results.iter().enumerate()
                                            .filter(|(_, &v)| !v).map(|(i, _)| i).collect();
                                        error!(
                                            job_id = %parent_id,
                                            valid_partitions = ?valid,
                                            invalid_partitions = ?invalid,
                                            "Phase 7: per-partition verification results (invalid partitions produced bad proofs)"
                                        );
                                    }
                                    Err(e) => {
                                        error!(
                                            job_id = %parent_id,
                                            error = %e,
                                            "Phase 7: per-partition verification failed"
                                        );
                                    }
                                }
                            }
                            Err(e) => {
                                self_check_passed = false;
                                error!(
                                    job_id = %parent_id,
                                    error = %e,
                                    "Phase 7: PoRep proof self-check error — proof will NOT be returned to caller"
                                );
                            }
                        }
                    }

                    if let Some(w) = t.workers.get_mut(worker_id as usize) {
                        w.last_circuit_id = Some(circuit_id_str.to_string());
                    }

                    let status = if self_check_passed {
                        t.record_completion(state.proof_kind, timings.total);
                        st.job_completed(&parent_id.0, &format!("{}", state.proof_kind), true);
                        JobStatus::Completed(ProofResult {
                            job_id: parent_id.clone(),
                            proof_kind: state.proof_kind,
                            proof_bytes: final_proof,
                            timings,
                        })
                    } else {
                        t.record_failure(state.proof_kind);
                        st.job_completed(&parent_id.0, &format!("{}", state.proof_kind), false);
                        JobStatus::Failed(format!(
                            "PoRep proof self-check failed: assembled proof did not verify (sector={}, miner={})",
                            state.request.sector_number, state.request.miner_id,
                        ))
                    };
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
    single_request: Option<&ProofRequest>,
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

                        // Self-check: verify each batched PoRep proof before returning.
                        let mut batch_self_check_passed = true;
                        if proof_kind == ProofKind::PoRepSealCommit {
                            match crate::prover::verify_porep_proof(
                                &req.vanilla_proof,
                                &sector_proof,
                                req.sector_number,
                                req.miner_id,
                                &req.job_id.0,
                            ) {
                                Ok(true) => {
                                    info!(job_id = %req.job_id, sector_idx = i, "Batched PoRep proof self-check PASSED");
                                }
                                Ok(false) => {
                                    batch_self_check_passed = false;
                                    error!(
                                        job_id = %req.job_id,
                                        sector_idx = i,
                                        proof_len = sector_proof.len(),
                                        sector = req.sector_number,
                                        miner = req.miner_id,
                                        "Batched PoRep proof self-check FAILED — proof will NOT be returned to caller"
                                    );
                                }
                                Err(e) => {
                                    batch_self_check_passed = false;
                                    error!(
                                        job_id = %req.job_id,
                                        sector_idx = i,
                                        error = %e,
                                        "Batched PoRep proof self-check error — proof will NOT be returned to caller"
                                    );
                                }
                            }
                        }

                        let status = if batch_self_check_passed {
                            t.record_completion(proof_kind, timings.total);
                            JobStatus::Completed(ProofResult {
                                job_id: req.job_id.clone(),
                                proof_kind,
                                proof_bytes: sector_proof,
                                timings,
                            })
                        } else {
                            t.record_failure(proof_kind);
                            JobStatus::Failed(format!(
                                "PoRep proof self-check failed: batched proof {} did not verify (sector={}, miner={})",
                                i, req.sector_number, req.miner_id,
                            ))
                        };

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

            // Self-check: verify single-sector PoRep proof before returning.
            let mut single_self_check_passed = true;
            if proof_kind == ProofKind::PoRepSealCommit {
                if let Some(req) = single_request {
                    match crate::prover::verify_porep_proof(
                        &req.vanilla_proof,
                        &proof_bytes,
                        req.sector_number,
                        req.miner_id,
                        &req.job_id.0,
                    ) {
                        Ok(true) => {
                            info!(job_id = %job_id, "Pipeline single-sector PoRep proof self-check PASSED");
                            if std::env::var("CUZK_VALIDATE_PARTITIONS").as_deref() == Ok("1") {
                                info!(job_id = %job_id, "CUZK_VALIDATE_PARTITIONS=1: validating per-partition verifier against known-good proof");
                                match crate::prover::verify_porep_partitions(
                                    &req.vanilla_proof,
                                    &proof_bytes,
                                    req.sector_number,
                                    req.miner_id,
                                    &req.job_id.0,
                                ) {
                                    Ok(results) => {
                                        let valid: Vec<usize> = results.iter().enumerate()
                                            .filter(|(_, &v)| v).map(|(i, _)| i).collect();
                                        let invalid: Vec<usize> = results.iter().enumerate()
                                            .filter(|(_, &v)| !v).map(|(i, _)| i).collect();
                                        if invalid.is_empty() {
                                            info!(
                                                job_id = %job_id,
                                                "VERIFIER VALIDATION: per-partition verifier confirmed correct (all partitions valid on known-good proof)"
                                            );
                                        } else {
                                            warn!(
                                                job_id = %job_id,
                                                valid_partitions = ?valid,
                                                invalid_partitions = ?invalid,
                                                "VERIFIER VALIDATION FAILED: per-partition verifier reports invalid partitions on a known-good proof — verifier is BUGGY"
                                            );
                                        }
                                    }
                                    Err(e) => {
                                        warn!(
                                            job_id = %job_id,
                                            error = %e,
                                            "VERIFIER VALIDATION: per-partition verifier errored on known-good proof"
                                        );
                                    }
                                }
                            }
                        }
                        Ok(false) => {
                            single_self_check_passed = false;
                            error!(
                                job_id = %job_id,
                                proof_len = proof_bytes.len(),
                                sector = req.sector_number,
                                miner = req.miner_id,
                                "Pipeline single-sector PoRep proof self-check FAILED — proof will NOT be returned to caller"
                            );
                            // Run per-partition verification to diagnose which partition(s) are bad
                            match crate::prover::verify_porep_partitions(
                                &req.vanilla_proof,
                                &proof_bytes,
                                req.sector_number,
                                req.miner_id,
                                &req.job_id.0,
                            ) {
                                Ok(results) => {
                                    let valid: Vec<usize> = results.iter().enumerate()
                                        .filter(|(_, &v)| v).map(|(i, _)| i).collect();
                                    let invalid: Vec<usize> = results.iter().enumerate()
                                        .filter(|(_, &v)| !v).map(|(i, _)| i).collect();
                                    error!(
                                        job_id = %job_id,
                                        valid_partitions = ?valid,
                                        invalid_partitions = ?invalid,
                                        "Pipeline single-sector: per-partition verification results"
                                    );
                                }
                                Err(e) => {
                                    error!(
                                        job_id = %job_id,
                                        error = %e,
                                        "Pipeline single-sector: per-partition verification failed"
                                    );
                                }
                            }
                        }
                        Err(e) => {
                            single_self_check_passed = false;
                            error!(
                                job_id = %job_id,
                                error = %e,
                                "Pipeline single-sector PoRep proof self-check error — proof will NOT be returned to caller"
                            );
                        }
                    }
                } else {
                    debug!(job_id = %job_id, "skipping PoRep self-check: no request available for single-sector path");
                }
            }

            if let Some(w) = t.workers.get_mut(worker_id as usize) {
                w.last_circuit_id = Some(circuit_id_str.to_string());
            }

            let status = if single_self_check_passed {
                t.record_completion(proof_kind, timings.total);
                JobStatus::Completed(ProofResult {
                    job_id: job_id.clone(),
                    proof_kind,
                    proof_bytes,
                    timings,
                })
            } else {
                t.record_failure(proof_kind);
                JobStatus::Failed(
                    "PoRep proof self-check failed: pipeline single-sector proof did not verify".to_string()
                )
            };
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

    /// Memory budget reservation for this job's working set.
    /// Released in two phases: partial after prove_start (a/b/c freed),
    /// remainder after prove_finish (shell + aux freed).
    pub reservation: Option<crate::memory::MemoryReservation>,

    /// Monotonic sequence number for pipeline age ordering.
    /// Used by the GPU priority queue key — not read from the struct
    /// after insertion, but carried through for the push() call.
    #[allow(dead_code)]
    pub job_seq: u64,
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

/// Parsed proof input shared across partition workers (Phase 7).
///
/// Each variant holds the pre-deserialized input for a proof type,
/// wrapped in `Arc` for zero-copy sharing across partition workers.
#[cfg(feature = "cuda-supraseal")]
enum ParsedProofInput {
    PoRep(Arc<crate::pipeline::ParsedC1Output>),
    SnapDeals(Arc<crate::pipeline::ParsedSnapDealsInput>),
}

/// Phase 7: Work item for the partition synthesis worker pool.
///
/// Generalized to support any multi-partition proof type (PoRep, SnapDeals).
#[cfg(feature = "cuda-supraseal")]
struct PartitionWorkItem {
    /// Shared parsed proof input (Arc for zero-copy sharing across workers).
    parsed: ParsedProofInput,
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
    /// Monotonic sequence number for pipeline age ordering.
    /// Lower = older pipeline = higher priority.
    job_seq: u64,
    /// Optional pinned memory pool for fast H2D transfers.
    pinned_pool: Option<Arc<crate::pinned_pool::PinnedPool>>,
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
    /// Unified memory budget for all allocations.
    budget: Arc<crate::memory::MemoryBudget>,
    /// PCE (pre-compiled circuit) cache.
    #[cfg(feature = "cuda-supraseal")]
    pce_cache: Arc<crate::pipeline::PceCache>,
    /// Lightweight status tracker for HTTP debug API.
    status_tracker: Arc<crate::status::StatusTracker>,
    /// CUDA pinned memory pool for fast H2D transfers.
    pinned_pool: Arc<crate::pinned_pool::PinnedPool>,
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

        // Warn about deprecated config fields
        config.warn_deprecated();

        // Create unified memory budget
        let total_budget = config.memory.resolve_total_budget();
        let budget = Arc::new(crate::memory::MemoryBudget::new(total_budget));
        info!(
            total_budget_gib = total_budget / crate::memory::GIB,
            "memory budget initialized"
        );

        let srs_manager = SrsManager::new(
            config.srs.param_cache.clone(),
            budget.clone(),
        );

        #[cfg(feature = "cuda-supraseal")]
        let pce_cache = Arc::new(crate::pipeline::PceCache::new(
            budget.clone(),
            config.srs.param_cache.clone(),
        ));

        let status_tracker = Arc::new(crate::status::StatusTracker::new(
            budget.clone(),
        ));

        let pinned_pool = Arc::new(crate::pinned_pool::PinnedPool::new());
        info!("CUDA pinned memory pool initialized");

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
            budget,
            #[cfg(feature = "cuda-supraseal")]
            pce_cache,
            status_tracker,
            pinned_pool,
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

        // SRS and PCE are loaded on demand — no preload.
        // Wire evictor callback so budget.acquire() can evict idle SRS/PCE.
        {
            let srs_for_evict = self.srs_manager.clone();
            #[cfg(feature = "cuda-supraseal")]
            let pce_for_evict = self.pce_cache.clone();
            let pool_for_evict = self.pinned_pool.clone();
            let min_idle = self.config.memory.eviction_min_idle_duration();
            self.budget.set_evictor(Arc::new(move |needed: u64| -> u64 {
                let mut freed = 0u64;

                // First try: shrink the pinned pool (fast, no lock contention).
                // This frees idle pinned buffers that aren't currently checked out.
                if freed < needed {
                    let pool_freed = pool_for_evict.shrink(needed - freed);
                    freed += pool_freed;
                }

                if freed >= needed {
                    return freed;
                }

                // Second try: evict SRS/PCE caches (slower, requires locks).
                // Collect all candidates from both caches.
                // NOTE: try_lock() because this callback runs from async acquire() —
                // blocking_lock() would panic on a tokio runtime thread.
                // If the SrsManager mutex is held, we skip SRS candidates this iteration;
                // the acquire loop retries so we'll catch them next time.
                let mut candidates: Vec<(String, CircuitId, u64, Instant, bool)> = Vec::new();

                // SRS candidates (skip if mutex is held)
                let srs_locked = srs_for_evict.try_lock();
                if let Ok(ref mgr) = srs_locked {
                    for (cid, size, last_used) in mgr.evictable_entries() {
                        if last_used.elapsed() >= min_idle {
                            candidates.push((format!("srs:{}", cid), cid, size, last_used, true));
                        }
                    }
                }
                drop(srs_locked);

                // PCE candidates
                #[cfg(feature = "cuda-supraseal")]
                {
                    for (cid, size, last_used) in pce_for_evict.evictable_entries() {
                        if last_used.elapsed() >= min_idle {
                            candidates.push((format!("pce:{}", cid), cid, size, last_used, false));
                        }
                    }
                }

                // Sort by last_used ascending (oldest first = best eviction candidate)
                candidates.sort_by_key(|c| c.3);

                for (label, cid, _size, _last_used, is_srs) in candidates {
                    if freed >= needed { break; }
                    let bytes = if is_srs {
                        match srs_for_evict.try_lock() {
                            Ok(mut mgr) => mgr.evict(&cid),
                            Err(_) => 0, // Mutex held — skip, will retry
                        }
                    } else {
                        #[cfg(feature = "cuda-supraseal")]
                        { pce_for_evict.evict(&cid) }
                        #[cfg(not(feature = "cuda-supraseal"))]
                        { 0 }
                    };
                    if bytes > 0 {
                        info!(
                            entry = %label,
                            freed_gib = bytes / crate::memory::GIB,
                            "evicted for memory pressure"
                        );
                    }
                    freed += bytes;
                }

                freed
            })).await;
            info!("evictor callback wired");
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

        // Register workers in status tracker
        {
            let worker_pairs: Vec<(u32, u32)> = gpu_ordinals
                .iter()
                .flat_map(|&ordinal| {
                    (0..gpu_workers_per_device).map(move |_| ordinal)
                })
                .enumerate()
                .map(|(i, ordinal)| (i as u32, ordinal))
                .collect();
            self.status_tracker.register_workers(&worker_pairs);
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

            // Budget-based capacity sizing.
            let configured_lookahead = self.config.pipeline.synthesis_lookahead.max(1) as usize;
            let max_partitions_in_budget = (self.budget.total_bytes() / crate::memory::POREP_PARTITION_FULL_BYTES) as usize;
            let lookahead = configured_lookahead.max(max_partitions_in_budget.min(32));

            // ── Priority work queues ──────────────────────────────────────
            //
            // Both synthesis and GPU stages use priority queues keyed on
            // (job_seq, partition_idx). This ensures workers always pick the
            // lowest partition in the oldest pipeline — completing jobs
            // sequentially rather than interleaving partitions randomly.
            //
            // job_seq is a monotonically increasing counter assigned when
            // each pipeline is dispatched. Lower = older = higher priority.
            let synth_work_queue: Arc<PriorityWorkQueue<PartitionWorkItem>> =
                Arc::new(PriorityWorkQueue::new());
            let gpu_work_queue: Arc<PriorityWorkQueue<SynthesizedJob>> =
                Arc::new(PriorityWorkQueue::new());

            // Monotonic counter for pipeline age ordering.
            let next_job_seq = Arc::new(AtomicU64::new(0));

            let max_parallel = {
                let cfg = self.config.pipeline.max_parallel_synthesis;
                if cfg == 0 { 18 } else { cfg as usize }
            };
            let synth_worker_count = max_partitions_in_budget.min(max_parallel).max(2);

            // ── GPU queue target + dispatch pacer ────────────────────────
            //
            // PI-controlled pacer that maintains `target` synthesized
            // partitions waiting in the GPU queue. Dispatches one item per
            // timer tick; the tick interval is computed from:
            //   Feed-forward: EMA of GPU inter-completion interval
            //   Feedback:     PI correction on (target - EMA_waiting)
            //
            // gpu_done_notify wakes the dispatcher on GPU completions (to
            // update pacer state). gpu_completion_count is incremented by
            // GPU finalizers for accurate rate measurement.
            let gpu_queue_target = self.config.pipeline.max_gpu_queue_depth as usize;
            let gpu_done_notify: Arc<tokio::sync::Notify> = Arc::new(tokio::sync::Notify::new());
            let gpu_completion_count: Arc<AtomicU64> = Arc::new(AtomicU64::new(0));
            let gpu_processing_total_ns: Arc<AtomicU64> = Arc::new(AtomicU64::new(0));
            let synth_completion_count: Arc<AtomicU64> = Arc::new(AtomicU64::new(0));
            if gpu_queue_target > 0 {
                tracing::info!(gpu_queue_target, "GPU queue target dispatch pacer enabled");
            }

            // ── Ordered synthesis dispatch ─────────────────────────────────
            //
            // A single dispatcher task serializes BOTH the priority queue pop
            // AND budget acquisition. This guarantees that partitions enter
            // synthesis in strict (job_seq, partition_idx) order — the lowest
            // partition in the oldest pipeline always gets budget first.
            //
            // Without this, N workers each pop an item then race for budget,
            // and budget goes to whichever worker's acquire() happens to
            // complete first (essentially random).
            //
            // The dispatcher hands (item, reservation) to a bounded channel.
            // A pool of synthesis workers receives from the channel, runs
            // synthesis on blocking threads, and pushes results to the GPU
            // priority queue.
            let (synth_dispatch_tx, synth_dispatch_rx) =
                tokio::sync::mpsc::channel::<(PartitionWorkItem, crate::memory::MemoryReservation)>(synth_worker_count);
            let synth_dispatch_rx = Arc::new(Mutex::new(synth_dispatch_rx));

            // Single dispatcher: PI-controlled pacer for GPU queue depth
            //
            // Dispatches one item per timer tick. The tick interval is:
            //   base  = EMA of GPU inter-completion time (feed-forward)
            //   adjusted by PI correction on (target - EMA_waiting)
            //
            // Bootstrap: dispatch `target` items at 200ms spacing, then wait
            // for the first GPU completion to calibrate the pacer.
            //
            // Steady state: converges to dispatching at GPU consumption rate,
            // maintaining `target` items waiting. Given 20-60s synthesis delay,
            // takes several minutes to fully stabilize.
            {
                let synth_work_queue = synth_work_queue.clone();
                let gpu_work_queue_for_dispatch = gpu_work_queue.clone();
                let budget = self.budget.clone();
                let tracker = self.tracker.clone();
                let mut shutdown_rx = self.shutdown_rx.clone();
                let gpu_done = gpu_done_notify.clone();
                let gpu_count = gpu_completion_count.clone();
                let gpu_proc_ns = gpu_processing_total_ns.clone();
                let synth_count = synth_completion_count.clone();
                let target = gpu_queue_target;
                let total_gpu_workers = num_workers;

                tokio::spawn(async move {
                    tracing::info!("synthesis priority dispatcher started (PI pacer)");
                    let mut pacer = DispatchPacer::new(target, total_gpu_workers);

                    loop {
                        // ── Update pacer state ──
                        if target > 0 {
                            let waiting = gpu_work_queue_for_dispatch.len();
                            let count = gpu_count.load(AtomicOrdering::Acquire);
                            let proc_ns = gpu_proc_ns.load(AtomicOrdering::Acquire);
                            let sc = synth_count.load(AtomicOrdering::Acquire);
                            pacer.update(waiting, count, proc_ns, sc);

                            // Re-enter bootstrap when pipeline drains between batches.
                            // GPU rate EMA is preserved; integral is reset (stale).
                            if pacer.should_rebootstrap(count) {
                                pacer.enter_rebootstrap();
                                tracing::info!(
                                    rebootstrap = pacer.rebootstrap_count,
                                    gpu_proc_ms = format!("{:.0}", pacer.ema_gpu_processing_s * 1000.0),
                                    "pacer: pipeline drained, re-entering bootstrap"
                                );
                            }
                        }

                        // ── Pacing / gating ──
                        if target > 0 {
                            if pacer.bootstrap_exhausted() {
                                // Initial bootstrap exhausted: wait for first GPU
                                // completion to calibrate processing time.
                                tracing::info!("pacer: bootstrap done, waiting for first GPU completion");
                                loop {
                                    tokio::select! {
                                        biased;
                                        _ = shutdown_rx.changed() => {
                                            if *shutdown_rx.borrow() { return; }
                                        }
                                        _ = gpu_done.notified() => {
                                            let w = gpu_work_queue_for_dispatch.len();
                                            let c = gpu_count.load(AtomicOrdering::Acquire);
                                            let pns = gpu_proc_ns.load(AtomicOrdering::Acquire);
                                            let sc = synth_count.load(AtomicOrdering::Acquire);
                                            pacer.update(w, c, pns, sc);
                                            if pacer.gpu_rate_known { break; }
                                        }
                                    }
                                }
                                tracing::info!(
                                    gpu_proc_ms = format!("{:.0}", pacer.ema_gpu_processing_s * 1000.0),
                                    gpu_eff_ms = format!("{:.0}", pacer.ema_gpu_processing_s / pacer.num_gpu_workers as f64 * 1000.0),
                                    ema_waiting = format!("{:.1}", pacer.ema_waiting),
                                    "pacer: GPU rate calibrated, switching to PI control"
                                );
                            } else if pacer.in_bootstrap() {
                                // Bootstrap (initial or re-): moderate spacing
                                let interval = pacer.interval();
                                tokio::select! {
                                    biased;
                                    _ = shutdown_rx.changed() => {
                                        if *shutdown_rx.borrow() { return; }
                                        continue;
                                    }
                                    _ = gpu_done.notified() => {
                                        let w = gpu_work_queue_for_dispatch.len();
                                        let c = gpu_count.load(AtomicOrdering::Acquire);
                                        let pns = gpu_proc_ns.load(AtomicOrdering::Acquire);
                                        let sc = synth_count.load(AtomicOrdering::Acquire);
                                        pacer.update(w, c, pns, sc);
                                        // GPU event during bootstrap — still dispatch
                                    }
                                    _ = tokio::time::sleep(interval) => {}
                                }
                            } else {
                                // Steady state: PI-controlled timer tick
                                let interval = pacer.interval();
                                tokio::select! {
                                    biased;
                                    _ = shutdown_rx.changed() => {
                                        if *shutdown_rx.borrow() { return; }
                                        continue;
                                    }
                                    _ = gpu_done.notified() => {
                                        // GPU event: update state, don't dispatch
                                        continue;
                                    }
                                    _ = tokio::time::sleep(interval) => {
                                        // Timer tick: proceed to dispatch one item
                                    }
                                }
                            }
                        }

                        // ── Pop highest-priority item ──
                        let item = loop {
                            if let Some(item) = synth_work_queue.try_pop() {
                                break item;
                            }
                            // No work — wait for work or GPU event
                            tokio::select! {
                                biased;
                                _ = shutdown_rx.changed() => {
                                    if *shutdown_rx.borrow() {
                                        tracing::info!("dispatcher: shutdown");
                                        return;
                                    }
                                }
                                _ = synth_work_queue.notified() => {}
                                _ = gpu_done.notified() => {
                                    // GPU event while waiting for work — update pacer
                                    if target > 0 {
                                        let w = gpu_work_queue_for_dispatch.len();
                                        let c = gpu_count.load(AtomicOrdering::Acquire);
                                        let pns = gpu_proc_ns.load(AtomicOrdering::Acquire);
                                        let sc = synth_count.load(AtomicOrdering::Acquire);
                                        pacer.update(w, c, pns, sc);
                                    }
                                }
                            }
                        };

                        let mem_bytes = crate::memory::proof_kind_full_bytes(&item.circuit_id);
                        let p_idx = item.partition_idx;
                        let p_job_id = item.job_id.clone();

                        // Acquire budget (blocks until available — natural backpressure).
                        let reservation = budget.acquire(mem_bytes).await;

                        // Check if job already failed
                        {
                            let t = tracker.lock().await;
                            if let Some(state) = t.assemblers.get(&p_job_id) {
                                if state.failed {
                                    tracing::info!(
                                        job_id = %p_job_id,
                                        partition = p_idx,
                                        "skipping synthesis for failed job"
                                    );
                                    drop(reservation);
                                    continue;
                                }
                            }
                        }

                        // Hand to worker pool
                        if synth_dispatch_tx.send((item, reservation)).await.is_err() {
                            tracing::error!("synthesis worker channel closed");
                            return;
                        }
                        pacer.on_dispatch();

                        // Periodic logging
                        if target > 0 && pacer.total_dispatched % 5 == 0 {
                            let interval = pacer.interval();
                            let in_flight = pacer.total_dispatched.saturating_sub(
                                gpu_count.load(AtomicOrdering::Acquire));
                            tracing::info!(
                                total = pacer.total_dispatched,
                                in_flight = in_flight,
                                waiting = gpu_work_queue_for_dispatch.len(),
                                ema_waiting = format!("{:.1}", pacer.ema_waiting),
                                gpu_proc_ms = format!("{:.0}", pacer.ema_gpu_processing_s * 1000.0),
                                gpu_eff_ms = format!("{:.0}", pacer.ema_gpu_processing_s / pacer.num_gpu_workers as f64 * 1000.0),
                                ema_synth_ms = format!("{:.0}", pacer.ema_synth_interval_s * 1000.0),
                                interval_ms = interval.as_millis(),
                                integral = format!("{:.2}", pacer.integral_error),
                                bootstrap = pacer.in_bootstrap(),
                                rebootstraps = pacer.rebootstrap_count,
                                "pacer: status"
                            );
                        }
                    }
                });
            }

            // Synthesis worker pool: receive dispatched work, synthesize, push to GPU queue
            for sw_id in 0..synth_worker_count {
                let synth_dispatch_rx = synth_dispatch_rx.clone();
                let gpu_work_queue = gpu_work_queue.clone();
                let synth_done_count = synth_completion_count.clone();
                let tracker = self.tracker.clone();
                let st = self.status_tracker.clone();
                let mut shutdown_rx = self.shutdown_rx.clone();

                tokio::spawn(async move {
                    loop {
                        let (item, reservation) = {
                            let mut rx = synth_dispatch_rx.lock().await;
                            tokio::select! {
                                biased;
                                _ = shutdown_rx.changed() => {
                                    if *shutdown_rx.borrow() {
                                        tracing::info!(sw_id = sw_id, "synthesis worker received shutdown");
                                        break;
                                    }
                                    continue;
                                }
                                msg = rx.recv() => {
                                    match msg {
                                        Some(v) => v,
                                        None => {
                                            tracing::info!(sw_id = sw_id, "synthesis dispatch channel closed");
                                            break;
                                        }
                                    }
                                }
                            }
                        };

                        let p_idx = item.partition_idx;
                        let p_job_id = item.job_id.clone();
                        let p_job_seq = item.job_seq;
                        let proof_kind = item.request.proof_kind;

                        st.partition_synth_start(&p_job_id.0, p_idx);
                        crate::pipeline::buf_synth_start();
                        crate::pipeline::log_buffers("synth_start");
                        timeline_event(
                            "SYNTH_START",
                            &p_job_id.0,
                            &format!("partition={}", p_idx),
                        );

                        // Run synthesis on blocking thread
                        let synth_result = tokio::task::spawn_blocking(move || {
                            let pool_ref = item.pinned_pool.as_ref().map(|p| p);
                            let result = match &item.parsed {
                                ParsedProofInput::PoRep(parsed) => {
                                    crate::pipeline::synthesize_partition(
                                        parsed,
                                        item.partition_idx,
                                        &item.job_id.0,
                                        pool_ref,
                                    )
                                }
                                ParsedProofInput::SnapDeals(parsed) => {
                                    crate::pipeline::synthesize_snap_deals_partition(
                                        parsed,
                                        item.partition_idx,
                                        &item.job_id.0,
                                        pool_ref,
                                    )
                                }
                            };
                            result.map(|synth| (synth, item))
                        }).await;

                        match synth_result {
                            Ok(Ok((synth, item))) => {
                                st.partition_synth_end(&p_job_id.0, p_idx);
                                timeline_event(
                                    "SYNTH_END",
                                    &p_job_id.0,
                                    &format!("partition={},synth_ms={}", p_idx, synth.synthesis_duration.as_millis()),
                                );
                                crate::pipeline::buf_synth_done();
                                crate::pipeline::log_buffers("synth_done");
                                tracing::info!(
                                    job_id = %p_job_id,
                                    partition = p_idx,
                                    synth_ms = synth.synthesis_duration.as_millis(),
                                    "partition synthesis complete, pushing to GPU queue"
                                );
                                let job = SynthesizedJob {
                                    request: item.request,
                                    synth,
                                    params: item.params,
                                    circuit_id: item.circuit_id,
                                    batch_requests: vec![],
                                    sector_boundaries: vec![],
                                    partition_index: Some(item.partition_idx),
                                    total_partitions: Some(item.num_partitions),
                                    parent_job_id: Some(item.job_id),
                                    reservation: Some(reservation),
                                    job_seq: p_job_seq,
                                };
                                timeline_event("GPU_QUEUE", &p_job_id.0, &format!("partition={}", p_idx));
                                gpu_work_queue.push(p_job_seq, p_idx, job);
                                synth_done_count.fetch_add(1, AtomicOrdering::Release);
                            }
                            Ok(Err(e)) => {
                                st.partition_failed(&p_job_id.0, p_idx);
                                tracing::error!(
                                    error = %e,
                                    job_id = %p_job_id,
                                    partition = p_idx,
                                    "partition synthesis failed"
                                );
                                drop(reservation);
                                let mut t = tracker.lock().await;
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
                                        t.completed.insert(p_job_id.clone(), status);
                                    }
                                }
                            }
                            Err(e) => {
                                st.partition_failed(&p_job_id.0, p_idx);
                                tracing::error!(
                                    error = %e,
                                    job_id = %p_job_id,
                                    partition = p_idx,
                                    "partition synthesis task panicked"
                                );
                                drop(reservation);
                                let mut t = tracker.lock().await;
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
                                        t.completed.insert(p_job_id.clone(), status);
                                    }
                                }
                            }
                        }
                    }
                });
            }

            info!(
                configured_lookahead = configured_lookahead,
                max_partitions_in_budget = max_partitions_in_budget,
                effective_lookahead = lookahead,
                synth_worker_count = synth_worker_count,
                num_gpus = num_workers,
                "starting pipeline: synthesis task + GPU workers (budget-gated)"
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
                let budget = self.budget.clone();
                #[cfg(feature = "cuda-supraseal")]
                let pce_cache = self.pce_cache.clone();
                let st = self.status_tracker.clone();
                let pinned_pool = self.pinned_pool.clone();
                // Clone queues for the dispatcher (originals kept for GPU workers)
                let synth_work_queue = synth_work_queue.clone();
                let gpu_work_queue = gpu_work_queue.clone();
                let next_job_seq = next_job_seq.clone();

                // Semaphore limits concurrent synthesis tasks.
                // With concurrency=1, behavior is identical to the old sequential loop.
                let synth_semaphore = Arc::new(tokio::sync::Semaphore::new(synthesis_concurrency));

                tokio::spawn(async move {
                    info!(
                        max_batch_size = max_batch_size,
                        max_batch_wait_ms = max_batch_wait_ms,
                        slot_size = slot_size,
                        synthesis_concurrency = synthesis_concurrency,
                        "synthesis dispatcher started (budget-gated)"
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
                        synth_work_queue: &Arc<PriorityWorkQueue<PartitionWorkItem>>,
                        gpu_work_queue: &Arc<PriorityWorkQueue<SynthesizedJob>>,
                        next_job_seq: &Arc<AtomicU64>,
                        slot_size: u32,
                        budget: &Arc<crate::memory::MemoryBudget>,
                        #[cfg(feature = "cuda-supraseal")]
                        pce_cache: &Arc<crate::pipeline::PceCache>,
                        semaphore: &Arc<tokio::sync::Semaphore>,
                        concurrency: usize,
                        span: tracing::Span,
                        st: &Arc<crate::status::StatusTracker>,
                        pinned_pool: &Arc<crate::pinned_pool::PinnedPool>,
                    ) -> bool {
                        if concurrency <= 1 {
                            // Sequential mode: await inline (old behavior)
                            process_batch(
                                batch, tracker, srs_manager, param_cache,
                                synth_work_queue, gpu_work_queue, next_job_seq,
                                slot_size,
                                budget,
                                #[cfg(feature = "cuda-supraseal")]
                                pce_cache,
                                st,
                                pinned_pool,
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
                            let synth_work_queue = synth_work_queue.clone();
                            let gpu_work_queue = gpu_work_queue.clone();
                            let next_job_seq = next_job_seq.clone();
                            let budget = budget.clone();
                            #[cfg(feature = "cuda-supraseal")]
                            let pce_cache = pce_cache.clone();
                            let st = st.clone();
                            let pinned_pool = pinned_pool.clone();
                            tokio::spawn(async move {
                                let _permit = permit; // held until task completes
                                process_batch(
                                    batch, &tracker, &srs_manager, &param_cache,
                                    &synth_work_queue, &gpu_work_queue, &next_job_seq,
                                    slot_size,
                                    &budget,
                                    #[cfg(feature = "cuda-supraseal")]
                                    &pce_cache,
                                    &st,
                                    &pinned_pool,
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
                                                batch, &tracker, &srs_manager, &param_cache,
                                                &synth_work_queue, &gpu_work_queue, &next_job_seq,
                                                slot_size,
                                                &budget,
                                                #[cfg(feature = "cuda-supraseal")]
                                                &pce_cache,
                                                &synth_semaphore, synthesis_concurrency, span, &st,
                                                &pinned_pool,
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
                                            batch, &tracker, &srs_manager, &param_cache,
                                            &synth_work_queue, &gpu_work_queue, &next_job_seq,
                                            slot_size,
                                            &budget,
                                            #[cfg(feature = "cuda-supraseal")]
                                            &pce_cache,
                                            &synth_semaphore, synthesis_concurrency, span, &st,
                                            &pinned_pool,
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
                                    batch, &tracker, &srs_manager, &param_cache,
                                    &synth_work_queue, &gpu_work_queue, &next_job_seq,
                                    slot_size,
                                    &budget,
                                    #[cfg(feature = "cuda-supraseal")]
                                    &pce_cache,
                                    &synth_semaphore, synthesis_concurrency, span, &st,
                                    &pinned_pool,
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
                                    pending_batch, &tracker, &srs_manager, &param_cache,
                                    &synth_work_queue, &gpu_work_queue, &next_job_seq,
                                    slot_size,
                                    &budget,
                                    #[cfg(feature = "cuda-supraseal")]
                                    &pce_cache,
                                    &synth_semaphore, synthesis_concurrency, span, &st,
                                    &pinned_pool,
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
                                single_batch, &tracker, &srs_manager, &param_cache,
                                &synth_work_queue, &gpu_work_queue, &next_job_seq,
                                slot_size,
                                &budget,
                                #[cfg(feature = "cuda-supraseal")]
                                &pce_cache,
                                &synth_semaphore, synthesis_concurrency, span, &st,
                                &pinned_pool,
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
            /// Single-sector PoRep C2 and SnapDeals are dispatched as individual
            /// partitions through the engine's synthesis→GPU pipeline. Each partition
            /// acquires a memory reservation from the budget before synthesis, which
            /// naturally limits concurrency based on available RAM.
            ///
            /// Falls back to Phase 6 slotted pipeline when `slot_size > 0` and the
            /// request is single-sector PoRep, or to batch-all for other proof types.
            ///
            /// Returns `true` to continue, `false` to stop the synthesis task.
            async fn process_batch(
                batch: crate::batch_collector::ProofBatch,
                tracker: &Arc<Mutex<JobTracker>>,
                srs_manager: &Arc<Mutex<SrsManager>>,
                param_cache: &std::path::Path,
                synth_work_queue: &Arc<PriorityWorkQueue<PartitionWorkItem>>,
                gpu_work_queue: &Arc<PriorityWorkQueue<SynthesizedJob>>,
                next_job_seq: &Arc<AtomicU64>,
                slot_size: u32,
                budget: &Arc<crate::memory::MemoryBudget>,
                #[cfg(feature = "cuda-supraseal")]
                pce_cache: &Arc<crate::pipeline::PceCache>,
                st: &Arc<crate::status::StatusTracker>,
                pinned_pool: &Arc<crate::pinned_pool::PinnedPool>,
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

                    // ── Budget-gated per-partition dispatch ────────────────────────
                    //
                    // Single-sector PoRep C2 is dispatched as 10 independent partition
                    // work items through the engine's synthesis→GPU pipeline. Each
                    // partition acquires a memory reservation from the budget before
                    // synthesis, which naturally limits concurrency based on available
                    // RAM. No static partition_workers count needed.
                    //
                    // The GPU worker routes partition results to a ProofAssembler
                    // (registered in tracker.assemblers) and delivers the final proof
                    // when all partitions are complete.
                    //
                    // This eliminates the thundering-herd pattern: partitions from
                    // different sectors interleave on the GPU, achieving zero idle gaps.
                    if proof_kind == ProofKind::PoRepSealCommit
                        && requests.len() == 1
                    {
                        let req = requests[0].clone();
                        let job_id = req.job_id.clone();
                        let srs_mgr_clone = srs_mgr.clone();
                        let param_cache_owned = param_cache_str.clone();

                        // 1. Pre-acquire SRS budget if not loaded (async context)
                        let srs_reservation = {
                            let mgr = srs_mgr_clone.lock().await;
                            if !mgr.is_loaded(&CircuitId::Porep32G) {
                                let size = mgr.srs_file_size(&CircuitId::Porep32G);
                                drop(mgr);
                                Some(budget.acquire(size).await)
                            } else {
                                None
                            }
                        };

                        // 2. Parse C1 once (blocking) and load SRS
                        let parse_result = tokio::task::spawn_blocking({
                            let vanilla_proof = req.vanilla_proof.clone();
                            let srs_mgr = srs_mgr_clone.clone();
                            move || -> Result<(Arc<pipeline::ParsedC1Output>, Arc<bellperson::groth16::SuprasealParameters<blstrs::Bls12>>)> {
                                std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", &param_cache_owned);
                                let parsed = pipeline::parse_c1_output(&vanilla_proof)?;
                                let srs = {
                                    let mut mgr = srs_mgr.blocking_lock();
                                    mgr.ensure_loaded(&CircuitId::Porep32G, srs_reservation)?
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

                        // 2. Register ProofAssembler in JobTracker + StatusTracker
                        st.register_job(&job_id.0, &format!("{}", proof_kind), num_partitions);
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
                            budget_available_gib = budget.available_bytes() / crate::memory::GIB,
                            "dispatching per-partition synthesis (budget-gated)"
                        );

                        // 4. Trigger background PCE extraction if not yet cached
                        if pce_cache.get(&CircuitId::Porep32G).is_none() {
                            let c1_data = req.vanilla_proof.clone();
                            let sn = req.sector_number;
                            let mid = req.miner_id;
                            let pce_cache_bg = pce_cache.clone();
                            let budget_bg = budget.clone();
                            std::thread::spawn(move || {
                                info!("background PCE extraction starting for PoRep 32G");
                                // Acquire budget for transient extraction memory
                                loop {
                                    if let Some(reservation) = budget_bg.try_acquire(crate::memory::PCE_EXTRACTION_TRANSIENT_BYTES) {
                                        match pipeline::extract_and_cache_pce_from_c1(&c1_data, sn, mid, &pce_cache_bg) {
                                            Ok(()) => info!("background PCE extraction complete"),
                                            Err(e) => warn!(error = %e, "background PCE extraction failed"),
                                        }
                                        drop(reservation);
                                        break;
                                    }
                                    std::thread::sleep(Duration::from_secs(1));
                                }
                            });
                        }

                        // 5. Dispatch each partition to the priority synthesis queue.
                        // Workers always pick the lowest partition in the oldest pipeline,
                        // ensuring sequential job completion.
                        let job_seq = next_job_seq.fetch_add(1, AtomicOrdering::Relaxed);
                        info!(
                            job_id = %job_id,
                            job_seq = job_seq,
                            "assigned pipeline sequence number"
                        );
                        for partition_idx in 0..num_partitions {
                            let item = PartitionWorkItem {
                                parsed: ParsedProofInput::PoRep(parsed.clone()),
                                partition_idx,
                                job_id: job_id.clone(),
                                request: req.clone(),
                                params: srs.clone(),
                                circuit_id: CircuitId::Porep32G,
                                num_partitions,
                                job_seq,
                                pinned_pool: Some(pinned_pool.clone()),
                            };
                            synth_work_queue.push(job_seq, partition_idx, item);
                        }

                        // process_batch() returns immediately — completion is signaled
                        // asynchronously by the GPU worker when ProofAssembler collects
                        // all partitions.
                        return true;
                    }

                    // SnapDeals per-partition pipeline.
                    // Same architecture as PoRep: parse once, dispatch partitions to
                    // budget-gated workers, overlap synthesis with GPU proving.
                    // SnapDeals has 16 partitions of ~81M constraints each.
                    if proof_kind == ProofKind::SnapDealsUpdate
                        && requests.len() == 1
                    {
                        let req = requests[0].clone();
                        let job_id = req.job_id.clone();
                        let srs_mgr_clone = srs_mgr.clone();
                        let param_cache_owned = param_cache_str.clone();

                        // 1. Pre-acquire SRS budget if not loaded (async context)
                        let srs_reservation = {
                            let mgr = srs_mgr_clone.lock().await;
                            if !mgr.is_loaded(&CircuitId::SnapDeals32G) {
                                let size = mgr.srs_file_size(&CircuitId::SnapDeals32G);
                                drop(mgr);
                                Some(budget.acquire(size).await)
                            } else {
                                None
                            }
                        };

                        // 2. Parse SnapDeals input once (blocking) and load SRS
                        let parse_result = tokio::task::spawn_blocking({
                            let vanilla_proofs = req.vanilla_proofs.clone();
                            let registered_proof = req.registered_proof;
                            let comm_r_old = req.comm_r_old.clone();
                            let comm_r_new = req.comm_r_new.clone();
                            let comm_d_new = req.comm_d_new.clone();
                            let srs_mgr = srs_mgr_clone.clone();
                            move || -> Result<(Arc<pipeline::ParsedSnapDealsInput>, Arc<bellperson::groth16::SuprasealParameters<blstrs::Bls12>>)> {
                                std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", &param_cache_owned);
                                let parsed = pipeline::parse_snap_deals_input(
                                    &vanilla_proofs, registered_proof,
                                    &comm_r_old, &comm_r_new, &comm_d_new,
                                )?;
                                let srs = {
                                    let mut mgr = srs_mgr.blocking_lock();
                                    mgr.ensure_loaded(&CircuitId::SnapDeals32G, srs_reservation)?
                                };
                                Ok((Arc::new(parsed), srs))
                            }
                        }).await;

                        let (parsed, srs) = match parse_result {
                            Ok(Ok(v)) => v,
                            Ok(Err(e)) => {
                                error!(error = %e, "SnapDeals parse/SRS load failed");
                                let mut t = tracker_clone.lock().await;
                                t.record_failure(proof_kind);
                                let status = JobStatus::Failed(format!("SnapDeals parse failed: {}", e));
                                if let Some(senders) = t.pending.remove(&job_id) {
                                    for sender in senders {
                                        let _ = sender.send(status.clone());
                                    }
                                }
                                t.completed.insert(job_id, status);
                                return true;
                            }
                            Err(e) => {
                                error!(error = %e, "SnapDeals parse task panicked");
                                let mut t = tracker_clone.lock().await;
                                t.record_failure(proof_kind);
                                let status = JobStatus::Failed(format!("SnapDeals parse panicked: {}", e));
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

                        // 2. Register ProofAssembler in JobTracker + StatusTracker
                        st.register_job(&job_id.0, &format!("{}", proof_kind), num_partitions);
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
                            budget_available_gib = budget.available_bytes() / crate::memory::GIB,
                            "dispatching per-partition SnapDeals synthesis (budget-gated)"
                        );

                        // 4. Trigger background PCE extraction if not yet cached
                        if pce_cache.get(&CircuitId::SnapDeals32G).is_none() {
                            let vanilla_proofs = req.vanilla_proofs.clone();
                            let rp = req.registered_proof;
                            let cr_old = req.comm_r_old.clone();
                            let cr_new = req.comm_r_new.clone();
                            let cd_new = req.comm_d_new.clone();
                            let pce_cache_bg = pce_cache.clone();
                            let budget_bg = budget.clone();
                            std::thread::spawn(move || {
                                info!("background PCE extraction starting for SnapDeals");
                                loop {
                                    if let Some(reservation) = budget_bg.try_acquire(crate::memory::PCE_EXTRACTION_TRANSIENT_BYTES) {
                                        match pipeline::extract_and_cache_pce_from_snap_deals(vanilla_proofs, rp, &cr_old, &cr_new, &cd_new, &pce_cache_bg) {
                                            Ok(()) => info!("background PCE extraction complete for SnapDeals"),
                                            Err(e) => warn!(error = %e, "background PCE extraction failed for SnapDeals"),
                                        }
                                        drop(reservation);
                                        break;
                                    }
                                    std::thread::sleep(Duration::from_secs(1));
                                }
                            });
                        }

                        // 5. Dispatch each partition to the priority synthesis queue.
                        let job_seq = next_job_seq.fetch_add(1, AtomicOrdering::Relaxed);
                        info!(
                            job_id = %job_id,
                            job_seq = job_seq,
                            "assigned SnapDeals pipeline sequence number"
                        );
                        for partition_idx in 0..num_partitions {
                            let item = PartitionWorkItem {
                                parsed: ParsedProofInput::SnapDeals(parsed.clone()),
                                partition_idx,
                                job_id: job_id.clone(),
                                request: req.clone(),
                                params: srs.clone(),
                                circuit_id: CircuitId::SnapDeals32G,
                                num_partitions,
                                job_seq,
                                pinned_pool: Some(pinned_pool.clone()),
                            };
                            synth_work_queue.push(job_seq, partition_idx, item);
                        }

                        return true;
                    }

                    // Phase 6 fallback: Slotted partition pipeline (self-contained)
                    // Note: This path is now unreachable for single-sector PoRep C2
                    // since the budget-gated partition dispatch above catches it.
                    // Kept for multi-sector PoRep + slot_size > 0 edge case.
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
                                mgr.ensure_loaded(&CircuitId::Porep32G, None)?
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
                        if pce_cache.get(&CircuitId::Porep32G).is_none() {
                            let c1_data = requests[0].vanilla_proof.clone();
                            let sn = requests[0].sector_number;
                            let mid = requests[0].miner_id;
                            let pce_cache_bg = pce_cache.clone();
                            let budget_bg = budget.clone();
                            std::thread::spawn(move || {
                                info!("background PCE extraction starting for PoRep 32G");
                                loop {
                                    if let Some(reservation) = budget_bg.try_acquire(crate::memory::PCE_EXTRACTION_TRANSIENT_BYTES) {
                                        match pipeline::extract_and_cache_pce_from_c1(&c1_data, sn, mid, &pce_cache_bg) {
                                            Ok(()) => info!("background PCE extraction complete"),
                                            Err(e) => warn!(error = %e, "background PCE extraction failed"),
                                        }
                                        drop(reservation);
                                        break;
                                    }
                                    std::thread::sleep(Duration::from_secs(1));
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

                                // Self-check: verify the slotted PoRep proof before returning.
                                // If the self-check fails, return JobStatus::Failed so the
                                // caller doesn't receive an invalid proof.
                                let mut self_check_passed = true;
                                if proof_kind == ProofKind::PoRepSealCommit {
                                    let req = &requests[0];
                                    match crate::prover::verify_porep_proof(
                                        &req.vanilla_proof,
                                        &proof_bytes,
                                        req.sector_number,
                                        req.miner_id,
                                        &req.job_id.0,
                                    ) {
                                        Ok(true) => {
                                            info!(job_id = %req.job_id, "Phase 6: PoRep proof self-check PASSED");
                                        }
                                        Ok(false) => {
                                            self_check_passed = false;
                                            error!(
                                                job_id = %req.job_id,
                                                proof_len = proof_bytes.len(),
                                                sector = req.sector_number,
                                                miner = req.miner_id,
                                                "Phase 6: PoRep proof self-check FAILED — proof will NOT be returned to caller"
                                            );
                                        }
                                        Err(e) => {
                                            self_check_passed = false;
                                            error!(
                                                job_id = %req.job_id,
                                                error = %e,
                                                "Phase 6: PoRep proof self-check error — proof will NOT be returned to caller"
                                            );
                                        }
                                    }
                                }

                                let status = if self_check_passed {
                                    t.record_completion(proof_kind, pt.total);
                                    JobStatus::Completed(ProofResult {
                                        job_id: requests[0].job_id.clone(),
                                        proof_kind,
                                        proof_bytes,
                                        timings: pt,
                                    })
                                } else {
                                    t.record_failure(proof_kind);
                                    JobStatus::Failed(format!(
                                        "PoRep proof self-check failed: assembled proof did not verify (sector={}, miner={})",
                                        requests[0].sector_number, requests[0].miner_id,
                                    ))
                                };
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
                    //
                    // Acquire budget for working memory before synthesis.
                    // For multi-sector PoRep batch, use batch-level estimate.
                    // For single-sector, use per-proof estimate.
                    let working_circuit_id = match proof_kind {
                        ProofKind::PoRepSealCommit => CircuitId::Porep32G,
                        ProofKind::WinningPost => CircuitId::WinningPost32G,
                        ProofKind::WindowPostPartition => CircuitId::WindowPost32G,
                        ProofKind::SnapDealsUpdate => CircuitId::SnapDeals32G,
                    };
                    let working_bytes = if proof_kind == ProofKind::PoRepSealCommit && requests.len() > 1 {
                        crate::memory::POREP_BATCH_FULL_BYTES
                    } else {
                        crate::memory::proof_kind_full_bytes(&working_circuit_id)
                    };
                    let reservation = budget.acquire(working_bytes).await;

                    // Pre-acquire SRS budget if not loaded
                    let srs_reservation = {
                        let mgr = srs_mgr.lock().await;
                        if !mgr.is_loaded(&working_circuit_id) {
                            let size = mgr.srs_file_size(&working_circuit_id);
                            drop(mgr);
                            Some(budget.acquire(size).await)
                        } else {
                            None
                        }
                    };

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

                        // Load SRS (budget pre-acquired in async context)
                        let srs = {
                            let mut mgr = srs_mgr.blocking_lock();
                            mgr.ensure_loaded(&circuit_id, srs_reservation)?
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
                            reservation: None, // set below after unwrapping
                            job_seq: 0, // monolithic jobs use 0 (highest priority)
                        })
                    }).await;

                    match synth_result {
                        Ok(Ok(mut job)) => {
                            // Attach the working memory reservation to the job
                            job.reservation = Some(reservation);

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

                            // Trigger background PCE extraction if not yet cached.
                            // The extraction runs in a background thread so it doesn't block
                            // the GPU from processing this proof. Supports all proof types.
                            if pce_cache.get(&job.circuit_id).is_none() {
                                let req = requests[0].clone();
                                let pce_cache_bg = pce_cache.clone();
                                let budget_bg = budget.clone();
                                match proof_kind {
                                    ProofKind::PoRepSealCommit => {
                                        let c1_data = req.vanilla_proof;
                                        let sn = req.sector_number;
                                        let mid = req.miner_id;
                                        std::thread::spawn(move || {
                                            info!("background PCE extraction starting for PoRep 32G");
                                            loop {
                                                if let Some(res) = budget_bg.try_acquire(crate::memory::PCE_EXTRACTION_TRANSIENT_BYTES) {
                                                    match pipeline::extract_and_cache_pce_from_c1(&c1_data, sn, mid, &pce_cache_bg) {
                                                        Ok(()) => info!("background PCE extraction complete for PoRep 32G"),
                                                        Err(e) => warn!(error = %e, "background PCE extraction failed for PoRep 32G (non-fatal)"),
                                                    }
                                                    drop(res);
                                                    break;
                                                }
                                                std::thread::sleep(Duration::from_secs(1));
                                            }
                                        });
                                    }
                                    ProofKind::WinningPost => {
                                        let vanilla_proofs = req.vanilla_proofs;
                                        let rp = req.registered_proof;
                                        let mid = req.miner_id;
                                        let rand = req.randomness;
                                        std::thread::spawn(move || {
                                            info!("background PCE extraction starting for WinningPoSt");
                                            loop {
                                                if let Some(res) = budget_bg.try_acquire(crate::memory::PCE_EXTRACTION_TRANSIENT_BYTES) {
                                                    match pipeline::extract_and_cache_pce_from_winning_post(&vanilla_proofs, rp, mid, &rand, &pce_cache_bg) {
                                                        Ok(()) => info!("background PCE extraction complete for WinningPoSt"),
                                                        Err(e) => warn!(error = %e, "background PCE extraction failed for WinningPoSt (non-fatal)"),
                                                    }
                                                    drop(res);
                                                    break;
                                                }
                                                std::thread::sleep(Duration::from_secs(1));
                                            }
                                        });
                                    }
                                    ProofKind::WindowPostPartition => {
                                        let vanilla_proofs = req.vanilla_proofs;
                                        let rp = req.registered_proof;
                                        let mid = req.miner_id;
                                        let rand = req.randomness;
                                        let pi = req.partition_index;
                                        std::thread::spawn(move || {
                                            info!("background PCE extraction starting for WindowPoSt");
                                            loop {
                                                if let Some(res) = budget_bg.try_acquire(crate::memory::PCE_EXTRACTION_TRANSIENT_BYTES) {
                                                    match pipeline::extract_and_cache_pce_from_window_post(&vanilla_proofs, rp, mid, &rand, pi, &pce_cache_bg) {
                                                        Ok(()) => info!("background PCE extraction complete for WindowPoSt"),
                                                        Err(e) => warn!(error = %e, "background PCE extraction failed for WindowPoSt (non-fatal)"),
                                                    }
                                                    drop(res);
                                                    break;
                                                }
                                                std::thread::sleep(Duration::from_secs(1));
                                            }
                                        });
                                    }
                                    ProofKind::SnapDealsUpdate => {
                                        let vanilla_proofs = req.vanilla_proofs;
                                        let rp = req.registered_proof;
                                        let cr_old = req.comm_r_old;
                                        let cr_new = req.comm_r_new;
                                        let cd_new = req.comm_d_new;
                                        std::thread::spawn(move || {
                                            info!("background PCE extraction starting for SnapDeals");
                                            loop {
                                                if let Some(res) = budget_bg.try_acquire(crate::memory::PCE_EXTRACTION_TRANSIENT_BYTES) {
                                                    match pipeline::extract_and_cache_pce_from_snap_deals(vanilla_proofs, rp, &cr_old, &cr_new, &cd_new, &pce_cache_bg) {
                                                        Ok(()) => info!("background PCE extraction complete for SnapDeals"),
                                                        Err(e) => warn!(error = %e, "background PCE extraction failed for SnapDeals (non-fatal)"),
                                                    }
                                                    drop(res);
                                                    break;
                                                }
                                                std::thread::sleep(Duration::from_secs(1));
                                            }
                                        });
                                    }
                                }
                            }

                            crate::pipeline::buf_synth_done();
                            crate::pipeline::log_buffers("synth_done_mono");
                            timeline_event("GPU_QUEUE", &synth_job_id_str, "monolithic");
                            // Monolithic jobs get job_seq=0 (highest priority).
                            // partition_index=0 as there's only one partition.
                            gpu_work_queue.push(0, 0, job);
                        }
                        Ok(Err(e)) => {
                            drop(reservation);
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
                            drop(reservation);
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
                    let _ = (param_cache_str, srs_mgr, gpu_work_queue);
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

            // Create one C++ mutex per GPU, PLUS a single shared mutex for
            // partitioned proofs (num_circuits=1). The C++ SupraSeal code
            // picks GPUs internally via `n_gpus = min(ngpus(), num_circuits)`.
            // With num_circuits=1 (partition pipeline), it always selects
            // GPU 0 regardless of which Rust worker submits the job. If we
            // gave each worker its own per-GPU mutex, workers assigned to
            // different GPUs could run CUDA kernels on the SAME physical
            // device concurrently without serialization — corrupting proofs.
            //
            // Solution: one shared mutex for partition (num_circuits=1) jobs,
            // and per-GPU mutexes for batched (num_circuits>1) jobs where the
            // C++ code actually fans out to multiple GPUs.
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
                let gpu_work_queue = gpu_work_queue.clone();
                let mut shutdown_rx = self.shutdown_rx.clone();
                let st = self.status_tracker.clone();
                let gpu_done_for_worker = gpu_done_notify.clone();
                let gpu_count_for_worker = gpu_completion_count.clone();
                let gpu_proc_ns_for_worker = gpu_processing_total_ns.clone();
                // Phase 8: Convert GPU mutex pointer to usize for Send safety.
                // Raw pointers aren't Send, but the underlying C++ mutex lives
                // for the engine's lifetime, so the address is always valid.
                // One mutex per GPU — the C++ gpu_index parameter ensures each
                // worker's CUDA work lands on the correct physical device.
                #[cfg(feature = "cuda-supraseal")]
                let gpu_mutex_addr: usize = gpu_mutexes[gpu_idx].0 as usize;

                tokio::spawn(async move {
                    info!(worker_id = worker_id, gpu = gpu_ordinal, sub_id = worker_sub_id, "pipeline GPU worker started");
                    loop {
                        // Pop highest-priority synthesized job (oldest pipeline, lowest partition).
                        // Returns None on shutdown.
                        let synth_job_opt: Option<SynthesizedJob> = loop {
                            if let Some(job) = gpu_work_queue.try_pop() {
                                break Some(job);
                            }
                            tokio::select! {
                                biased;
                                _ = shutdown_rx.changed() => {
                                    if *shutdown_rx.borrow() {
                                        break None;
                                    }
                                }
                                _ = gpu_work_queue.notified() => {}
                            }
                        };
                        let mut synth_job = match synth_job_opt {
                            Some(j) => j,
                            None => {
                                info!(worker_id = worker_id, "pipeline GPU worker received shutdown signal");
                                break;
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
                        // Capture request for single-sector self-check in process_monolithic_result
                        let single_request_owned = if !is_batched && synth_job.parent_job_id.is_none() {
                            Some(synth_job.request.clone())
                        } else {
                            None
                        };
                        // Extract reservation and circuit info before moving synth_job
                        let reservation = synth_job.reservation.take();
                        let circuit_id_for_release = synth_job.circuit_id.clone();
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
                            let hot_path_start = Instant::now();

                            // Status tracker: mark partition as GPU-proving
                            if let (Some(ref pid), Some(pi)) = (&parent_job_id, partition_index) {
                                st.partition_gpu_start(&pid.0, pi, worker_id);
                            }
                            let t_status = hot_path_start.elapsed();

                            timeline_event("GPU_PICKUP", &job_id.0, &format!(
                                "worker={}{}", worker_id,
                                if let Some(pi) = partition_index { format!(",partition={}", pi) } else { String::new() }
                            ));

                            // Phase 7: Check if partitioned job is already failed — skip GPU proving
                            let t_before_fail_check = hot_path_start.elapsed();
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
                            let t_fail_check = hot_path_start.elapsed();

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
                            let t_mark_busy = hot_path_start.elapsed();

                            info!(
                                worker_id = worker_id,
                                partition = ?partition_index,
                                status_ms = t_status.as_millis(),
                                fail_check_ms = (t_fail_check - t_before_fail_check).as_millis(),
                                mark_busy_ms = (t_mark_busy - t_fail_check).as_millis(),
                                total_overhead_ms = t_mark_busy.as_millis(),
                                "GPU_TIMING hot_path pre-prove overhead"
                            );

                            let gpu_str = gpu_ordinal.to_string();
                            let gpu_job_id = job_id.0.clone();

                            // CUZK_DISABLE_SPLIT_PROVE=1: Use synchronous gpu_prove instead
                            // of the Phase 12 split API (gpu_prove_start/gpu_prove_finish)
                            // for debugging. This eliminates the async b_g2_msm finalization
                            // as a suspect.
                            #[cfg(feature = "cuda-supraseal")]
                            let split_disabled = std::env::var("CUZK_DISABLE_SPLIT_PROVE").map_or(false, |v| v == "1");

                            #[cfg(feature = "cuda-supraseal")]
                            if split_disabled {
                                let result = tokio::task::spawn_blocking(move || -> Result<(Vec<u8>, Duration)> {
                                    let gpu_mtx_ptr = gpu_mutex_addr as *mut std::ffi::c_void;
                                    let gpu_result = crate::pipeline::gpu_prove(synth_job.synth, &synth_job.params, gpu_mtx_ptr, gpu_ordinal as i32)?;
                                    Ok((gpu_result.proof_bytes, gpu_result.gpu_duration))
                                }).await;

                                // GPU prove complete — release full reservation
                                drop(reservation);

                                // Accumulate GPU processing time + completion (sync path)
                                if let Ok(Ok((_, ref gpu_dur))) = result {
                                    gpu_proc_ns_for_worker.fetch_add(
                                        gpu_dur.as_nanos() as u64,
                                        AtomicOrdering::Release,
                                    );
                                }
                                gpu_count_for_worker.fetch_add(1, AtomicOrdering::Release);
                                gpu_done_for_worker.notify_one();

                                // Process result inline (synchronous path)
                                let mut t = tracker.lock().await;
                                if let Some(w) = t.workers.get_mut(worker_id as usize) {
                                    w.current_job = None;
                                }
                                if let Some(ref parent_id) = parent_job_id {
                                    let p_idx = partition_index.unwrap();
                                    process_partition_result(
                                        &mut t, result, parent_id, p_idx,
                                        synth_duration, proof_kind, worker_id,
                                        &circuit_id_str, submitted_at, &st,
                                    );
                                } else {
                                    process_monolithic_result(
                                        &mut t, result, &job_id, proof_kind, worker_id,
                                        &circuit_id_str, synth_duration, submitted_at,
                                        is_batched, &batch_requests, &sector_boundaries,
                                        single_request_owned.as_ref(),
                                    );
                                }
                                return; // exit async block, loop continues
                            }

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
                                let spawn_t = Instant::now();
                                let r = tokio::task::spawn_blocking(move || -> Result<(crate::pipeline::PendingGpuProof, Instant, String)> {
                                    let enter_t = Instant::now();
                                    let gpu_mtx_ptr = gpu_mutex_addr as *mut std::ffi::c_void;
                                    let pending = crate::pipeline::gpu_prove_start(synth_job.synth, &synth_job.params, gpu_mtx_ptr, gpu_ordinal as i32)?;
                                    let prove_done_t = Instant::now();
                                    info!(
                                        worker_id = worker_id,
                                        partition = ?partition_index,
                                        spawn_to_enter_ms = (enter_t - spawn_t).as_millis(),
                                        prove_start_ms = (prove_done_t - enter_t).as_millis(),
                                        "GPU_TIMING spawn_blocking prove_start"
                                    );
                                    Ok((pending, enter_t, gpu_str2))
                                }).await;
                                info!(
                                    worker_id = worker_id,
                                    partition = ?partition_index,
                                    total_pre_prove_ms = hot_path_start.elapsed().as_millis(),
                                    spawn_blocking_wall_ms = spawn_t.elapsed().as_millis(),
                                    "GPU_TIMING prove_start returned"
                                );
                                // Re-map the result to strip the timing Instant
                                r.map(|inner| inner.map(|(pending, _enter_t, gpu_str)| (pending, gpu_str)))
                            };

                            #[cfg(feature = "cuda-supraseal")]
                            match start_result {
                                Ok(Ok(((pending_handle, pending_pi, gpu_start), _gpu_str_ret))) => {
                                    // Two-phase memory release:
                                    // Phase 1: a/b/c vectors freed inside prove_start — release that budget now
                                    if let Some(ref res) = reservation {
                                        let abc_bytes = crate::memory::proof_kind_abc_bytes(&circuit_id_for_release);
                                        res.release(abc_bytes);
                                    }

                                    // Spawn finalizer task — GPU worker loops immediately.
                                    // Move reservation into finalizer so remaining budget
                                    // (shell + aux) is released after prove_finish.
                                    let fin_tracker = tracker.clone();
                                    let fin_job_id = job_id.clone();
                                    let fin_parent_job_id = parent_job_id.clone();
                                    let fin_circuit_id_str = circuit_id_str.clone();
                                    let fin_batch_requests = batch_requests.clone();
                                    let fin_sector_boundaries = sector_boundaries.clone();
                                    let fin_gpu_job_id = gpu_job_id.clone();
                                    let fin_single_request = single_request_owned.clone();
                                    let fin_reservation = reservation; // moved into finalizer
                                    let fin_st = st.clone();
                                    let fin_gpu_done = gpu_done_for_worker.clone();
                                    let fin_gpu_count = gpu_count_for_worker.clone();
                                    let fin_gpu_proc_ns = gpu_proc_ns_for_worker.clone();
                                    tokio::spawn(async move {
                                        let fin_start = Instant::now();

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
                                        let t_finish = fin_start.elapsed();

                                        // Phase 2: Drop reservation to release remaining budget (shell + aux)
                                        drop(fin_reservation);

                                        // Accumulate actual GPU processing time for pacer.
                                        // Must be done BEFORE incrementing completion count
                                        // so the pacer sees consistent (count, total_ns) pairs.
                                        if let Ok(Ok((_, ref gpu_dur))) = result {
                                            fin_gpu_proc_ns.fetch_add(
                                                gpu_dur.as_nanos() as u64,
                                                AtomicOrdering::Release,
                                            );
                                        }

                                        // Increment completion counter and notify dispatcher.
                                        fin_gpu_count.fetch_add(1, AtomicOrdering::Release);
                                        fin_gpu_done.notify_one();
                                        let t_drop_res = fin_start.elapsed();

                                        // Process result (same logic as before)
                                        let mut t = fin_tracker.lock().await;
                                        let t_lock = fin_start.elapsed();

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
                                                &circuit_id_str, submitted_at, &fin_st,
                                            );
                                            let t_result = fin_start.elapsed();
                                            info!(
                                                worker_id = worker_id,
                                                partition = ?partition_index,
                                                finish_ms = t_finish.as_millis(),
                                                drop_res_ms = (t_drop_res - t_finish).as_millis(),
                                                lock_wait_ms = (t_lock - t_drop_res).as_millis(),
                                                process_ms = (t_result - t_lock).as_millis(),
                                                total_ms = t_result.as_millis(),
                                                "FIN_TIMING finalizer partition"
                                            );
                                            return;
                                        }

                                        crate::engine::process_monolithic_result(
                                            &mut t, result, &job_id, proof_kind, worker_id,
                                            &circuit_id_str, synth_duration, submitted_at,
                                            is_batched, &batch_requests, &sector_boundaries,
                                            fin_single_request.as_ref(),
                                        );
                                        let t_result = fin_start.elapsed();
                                        info!(
                                            worker_id = worker_id,
                                            finish_ms = t_finish.as_millis(),
                                            drop_res_ms = (t_drop_res - t_finish).as_millis(),
                                            lock_wait_ms = (t_lock - t_drop_res).as_millis(),
                                            process_ms = (t_result - t_lock).as_millis(),
                                            total_ms = t_result.as_millis(),
                                            "FIN_TIMING finalizer monolithic"
                                        );
                                    });

                                    // GPU worker continues — don't process result here
                                    return;
                                }
                                Ok(Err(e)) => {
                                    // gpu_prove_start itself failed (before GPU kernels)
                                    drop(reservation);
                                    gpu_done_for_worker.notify_one();
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
                                    drop(reservation);
                                    gpu_done_for_worker.notify_one();
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

                            drop(reservation);

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
                                    &circuit_id_str, submitted_at, &st,
                                );
                                return;
                            }

                            // ── Monolithic result delivery ─────────
                            process_monolithic_result(
                                &mut t, result, &job_id, proof_kind, worker_id,
                                &circuit_id_str, synth_duration, submitted_at,
                                is_batched, &batch_requests, &sector_boundaries,
                                single_request_owned.as_ref(),
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

    /// Preload an SRS entry via the budget-aware SrsManager.
    pub async fn preload_srs(&self, circuit_id: &str) -> Result<(bool, u64)> {
        let cid = match CircuitId::from_str_id(circuit_id) {
            Some(c) => c,
            None => return Err(anyhow::anyhow!("unknown circuit ID: {}", circuit_id)),
        };

        // Check if already loaded
        {
            let mgr = self.srs_manager.lock().await;
            if mgr.is_loaded(&cid) {
                return Ok((true, 0));
            }
        }

        // Pre-acquire budget in async context
        let srs_size = {
            let mgr = self.srs_manager.lock().await;
            mgr.srs_file_size(&cid)
        };
        let reservation = self.budget.acquire(srs_size).await;

        let srs_mgr = self.srs_manager.clone();
        let start = Instant::now();
        tokio::task::spawn_blocking(move || {
            let mut mgr = srs_mgr.blocking_lock();
            mgr.ensure_loaded(&cid, Some(reservation))
        })
        .await??;

        let elapsed = start.elapsed();
        let mut srs = self.preloaded_srs.write().await;
        let ms = elapsed.as_millis() as u64;
        srs.insert(circuit_id.to_string(), ms);
        Ok((false, ms))
    }

    /// Get a detailed status snapshot for the HTTP debug API.
    ///
    /// Collects memory budget, pipeline jobs, GPU workers, SRS/PCE allocations.
    pub async fn detailed_status(&self) -> crate::status::StatusSnapshot {
        let now = std::time::Instant::now();

        // SRS entries
        let srs_entries = {
            let mgr = self.srs_manager.try_lock();
            match mgr {
                Ok(mgr) => mgr.evictable_entries().iter().map(|(cid, bytes, last_used)| {
                    crate::status::SrsEntry {
                        circuit_id: cid.to_string(),
                        bytes: *bytes,
                        last_used_secs_ago: now.duration_since(*last_used).as_secs_f64(),
                        evictable: true,
                    }
                }).chain(
                    // Also include non-evictable (in-use) entries
                    mgr.loaded_ids().iter().filter_map(|cid| {
                        if mgr.evictable_entries().iter().any(|(c, _, _)| c == cid) {
                            None
                        } else {
                            Some(crate::status::SrsEntry {
                                circuit_id: cid.to_string(),
                                bytes: mgr.srs_file_size(cid),
                                last_used_secs_ago: 0.0,
                                evictable: false,
                            })
                        }
                    })
                ).collect(),
                Err(_) => vec![], // Mutex held, skip this poll
            }
        };

        // PCE entries
        #[cfg(feature = "cuda-supraseal")]
        let pce_entries: Vec<crate::status::PceEntry> = self.pce_cache.evictable_entries().iter().map(|(cid, bytes, last_used)| {
            crate::status::PceEntry {
                circuit_id: cid.to_string(),
                bytes: *bytes,
                last_used_secs_ago: now.duration_since(*last_used).as_secs_f64(),
                evictable: true,
            }
        }).collect();
        #[cfg(not(feature = "cuda-supraseal"))]
        let pce_entries: Vec<crate::status::PceEntry> = vec![];

        // Update queue depth
        self.status_tracker.set_queue_depth(self.scheduler.len().await as u32);

        self.status_tracker.snapshot(srs_entries, pce_entries)
    }

    /// Get the status tracker (for sharing with the HTTP server).
    pub fn status_tracker(&self) -> &Arc<crate::status::StatusTracker> {
        &self.status_tracker
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
