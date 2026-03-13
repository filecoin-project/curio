//! Lightweight status tracker for the cuzk engine.
//!
//! Provides a [`StatusTracker`] that records pipeline, GPU worker, and memory
//! state as proof jobs flow through the engine. The tracker is designed for
//! high-frequency polling (500 ms) from an external monitoring UI via a simple
//! HTTP JSON endpoint.
//!
//! All mutable state is behind a single `std::sync::RwLock` — writes are
//! infrequent (pipeline lifecycle events) and reads are cheap snapshots.

use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use std::time::Instant;

use serde::Serialize;

use crate::memory::MemoryBudget;

// ─── Snapshot types (JSON response) ─────────────────────────────────────────

/// Top-level status snapshot returned by `GET /status`.
#[derive(Serialize)]
pub struct StatusSnapshot {
    /// Seconds since engine start.
    pub uptime_secs: f64,

    // ── Memory budget ──────────────────────────────────────────────────
    pub memory: MemoryStatus,

    // ── Synthesis limiter ──────────────────────────────────────────────
    pub synthesis: SynthesisStatus,

    // ── In-flight pipeline jobs ────────────────────────────────────────
    pub pipelines: Vec<PipelineSnapshot>,

    // ── GPU workers ────────────────────────────────────────────────────
    pub gpu_workers: Vec<GpuWorkerSnapshot>,

    // ── Major allocations (SRS, PCE) ───────────────────────────────────
    pub allocations: AllocationsSnapshot,

    // ── Aggregate counters ─────────────────────────────────────────────
    pub counters: CountersSnapshot,

    // ── Buffer flight counters (from pipeline.rs atomics) ──────────────
    pub buffers: BuffersSnapshot,
}

#[derive(Serialize)]
pub struct MemoryStatus {
    pub total_bytes: u64,
    pub used_bytes: u64,
    pub available_bytes: u64,
}

#[derive(Serialize)]
pub struct SynthesisStatus {
    pub max_concurrent: u32,
    pub active: u32,
}

#[derive(Serialize)]
pub struct PipelineSnapshot {
    pub job_id: String,
    pub proof_kind: String,
    pub started_secs_ago: f64,
    pub total_partitions: usize,
    pub partitions_done: usize,
    pub partitions: Vec<PartitionSnapshot>,
}

#[derive(Serialize)]
pub struct PartitionSnapshot {
    pub index: usize,
    pub state: &'static str, // "pending","synthesizing","synth_done","gpu","done","failed"
    pub synth_ms: Option<u64>,
    pub gpu_ms: Option<u64>,
}

#[derive(Serialize)]
pub struct GpuWorkerSnapshot {
    pub worker_id: u32,
    pub gpu_ordinal: u32,
    pub state: &'static str, // "idle" or "proving"
    pub current_job: Option<String>,
    pub current_partition: Option<usize>,
    pub busy_secs: Option<f64>,
}

#[derive(Serialize)]
pub struct AllocationsSnapshot {
    pub srs: Vec<SrsEntry>,
    pub pce: Vec<PceEntry>,
}

#[derive(Serialize)]
pub struct SrsEntry {
    pub circuit_id: String,
    pub bytes: u64,
    pub last_used_secs_ago: f64,
    pub evictable: bool,
}

#[derive(Serialize)]
pub struct PceEntry {
    pub circuit_id: String,
    pub bytes: u64,
    pub last_used_secs_ago: f64,
    pub evictable: bool,
}

#[derive(Serialize)]
pub struct CountersSnapshot {
    pub total_completed: u64,
    pub total_failed: u64,
    pub completed_by_kind: HashMap<String, u64>,
    pub failed_by_kind: HashMap<String, u64>,
    pub queue_depth: u32,
}

#[derive(Serialize)]
pub struct BuffersSnapshot {
    pub synth_in_flight: usize,
    pub provers_in_flight: usize,
    pub aux_in_flight: usize,
    pub shells_in_flight: usize,
    pub pending_handles: usize,
}

// ─── Internal mutable state ────────────────────────────────────────────────

/// Per-partition phase in the pipeline.
#[derive(Clone, Copy, PartialEq)]
enum PartitionPhase {
    Pending,
    Synthesizing,
    SynthDone,
    Gpu,
    Done,
    Failed,
}

impl PartitionPhase {
    fn as_str(self) -> &'static str {
        match self {
            Self::Pending => "pending",
            Self::Synthesizing => "synthesizing",
            Self::SynthDone => "synth_done",
            Self::Gpu => "gpu",
            Self::Done => "done",
            Self::Failed => "failed",
        }
    }
}

/// Tracked state for one partition within a pipeline job.
#[derive(Clone)]
struct PartitionState {
    phase: PartitionPhase,
    synth_start: Option<Instant>,
    synth_end: Option<Instant>,
    gpu_start: Option<Instant>,
    gpu_end: Option<Instant>,
}

impl PartitionState {
    fn new() -> Self {
        Self {
            phase: PartitionPhase::Pending,
            synth_start: None,
            synth_end: None,
            gpu_start: None,
            gpu_end: None,
        }
    }
}

/// Tracked state for one pipeline job (proof request).
#[derive(Clone)]
struct JobState {
    proof_kind: String,
    started_at: Instant,
    total_partitions: usize,
    partitions: Vec<PartitionState>,
    completed_at: Option<Instant>,
}

/// Tracked state for one GPU worker.
#[derive(Clone)]
struct GpuWorkerState {
    worker_id: u32,
    gpu_ordinal: u32,
    busy: bool,
    current_job_id: Option<String>,
    current_partition: Option<usize>,
    busy_since: Option<Instant>,
}

/// Interior mutable state protected by `RwLock`.
struct Inner {
    jobs: HashMap<String, JobState>,
    gpu_workers: Vec<GpuWorkerState>,
    synth_max: u32,
    synth_active: u32,
    total_completed: u64,
    total_failed: u64,
    completed_by_kind: HashMap<String, u64>,
    failed_by_kind: HashMap<String, u64>,
    queue_depth: u32,
}

// ─── StatusTracker ─────────────────────────────────────────────────────────

/// Shared, lock-free-read status tracker.
///
/// Created once during engine init and shared with all pipeline components.
/// Update methods take `&self` and acquire a short write lock.
pub struct StatusTracker {
    inner: RwLock<Inner>,
    started_at: Instant,
    budget: Arc<MemoryBudget>,
}

impl StatusTracker {
    /// Create a new tracker.
    pub fn new(budget: Arc<MemoryBudget>, synth_max: u32) -> Self {
        Self {
            inner: RwLock::new(Inner {
                jobs: HashMap::new(),
                gpu_workers: Vec::new(),
                synth_max,
                synth_active: 0,
                total_completed: 0,
                total_failed: 0,
                completed_by_kind: HashMap::new(),
                failed_by_kind: HashMap::new(),
                queue_depth: 0,
            }),
            started_at: Instant::now(),
            budget,
        }
    }

    // ── Update methods (called from engine lifecycle points) ────────────

    /// Register GPU workers at startup.
    pub fn register_workers(&self, workers: &[(u32, u32)]) {
        let mut inner = self.inner.write().unwrap();
        inner.gpu_workers = workers
            .iter()
            .map(|&(worker_id, gpu_ordinal)| GpuWorkerState {
                worker_id,
                gpu_ordinal,
                busy: false,
                current_job_id: None,
                current_partition: None,
                busy_since: None,
            })
            .collect();
    }

    /// Register a new partitioned pipeline job.
    pub fn register_job(&self, job_id: &str, proof_kind: &str, num_partitions: usize) {
        let mut inner = self.inner.write().unwrap();
        inner.jobs.insert(
            job_id.to_string(),
            JobState {
                proof_kind: proof_kind.to_string(),
                started_at: Instant::now(),
                total_partitions: num_partitions,
                partitions: (0..num_partitions).map(|_| PartitionState::new()).collect(),
                completed_at: None,
            },
        );
    }

    /// Mark a partition as synthesizing.
    pub fn partition_synth_start(&self, job_id: &str, partition: usize) {
        let mut inner = self.inner.write().unwrap();
        inner.synth_active = inner.synth_active.saturating_add(1);
        if let Some(job) = inner.jobs.get_mut(job_id) {
            if let Some(p) = job.partitions.get_mut(partition) {
                p.phase = PartitionPhase::Synthesizing;
                p.synth_start = Some(Instant::now());
            }
        }
    }

    /// Mark a partition as synthesis done (waiting for GPU).
    pub fn partition_synth_end(&self, job_id: &str, partition: usize) {
        let mut inner = self.inner.write().unwrap();
        inner.synth_active = inner.synth_active.saturating_sub(1);
        if let Some(job) = inner.jobs.get_mut(job_id) {
            if let Some(p) = job.partitions.get_mut(partition) {
                p.phase = PartitionPhase::SynthDone;
                p.synth_end = Some(Instant::now());
            }
        }
    }

    /// Mark a partition as being GPU-proved + update worker state.
    pub fn partition_gpu_start(&self, job_id: &str, partition: usize, worker_id: u32) {
        let mut inner = self.inner.write().unwrap();
        if let Some(job) = inner.jobs.get_mut(job_id) {
            if let Some(p) = job.partitions.get_mut(partition) {
                p.phase = PartitionPhase::Gpu;
                p.gpu_start = Some(Instant::now());
            }
        }
        if let Some(w) = inner
            .gpu_workers
            .iter_mut()
            .find(|w| w.worker_id == worker_id)
        {
            w.busy = true;
            w.current_job_id = Some(job_id.to_string());
            w.current_partition = Some(partition);
            w.busy_since = Some(Instant::now());
        }
    }

    /// Mark a partition as GPU-done + free worker.
    pub fn partition_gpu_end(&self, job_id: &str, partition: usize, worker_id: u32) {
        let mut inner = self.inner.write().unwrap();
        if let Some(job) = inner.jobs.get_mut(job_id) {
            if let Some(p) = job.partitions.get_mut(partition) {
                p.phase = PartitionPhase::Done;
                p.gpu_end = Some(Instant::now());
            }
        }
        if let Some(w) = inner
            .gpu_workers
            .iter_mut()
            .find(|w| w.worker_id == worker_id)
        {
            w.busy = false;
            w.current_job_id = None;
            w.current_partition = None;
            w.busy_since = None;
        }
    }

    /// Mark a partition as failed.
    pub fn partition_failed(&self, job_id: &str, partition: usize) {
        let mut inner = self.inner.write().unwrap();
        if let Some(job) = inner.jobs.get_mut(job_id) {
            if let Some(p) = job.partitions.get_mut(partition) {
                p.phase = PartitionPhase::Failed;
            }
        }
    }

    /// Record that a proof job completed (success or failure).
    /// Marks it for eventual cleanup.
    pub fn job_completed(&self, job_id: &str, proof_kind: &str, success: bool) {
        let mut inner = self.inner.write().unwrap();
        if success {
            inner.total_completed += 1;
            *inner
                .completed_by_kind
                .entry(proof_kind.to_string())
                .or_insert(0) += 1;
        } else {
            inner.total_failed += 1;
            *inner
                .failed_by_kind
                .entry(proof_kind.to_string())
                .or_insert(0) += 1;
        }
        if let Some(job) = inner.jobs.get_mut(job_id) {
            job.completed_at = Some(Instant::now());
        }
    }

    /// Update the scheduler queue depth (call periodically or on submit/dequeue).
    pub fn set_queue_depth(&self, depth: u32) {
        let mut inner = self.inner.write().unwrap();
        inner.queue_depth = depth;
    }

    /// Produce a JSON-serializable snapshot. Cleans up jobs completed > 30 s ago.
    pub fn snapshot(
        &self,
        srs_entries: Vec<SrsEntry>,
        pce_entries: Vec<PceEntry>,
    ) -> StatusSnapshot {
        let now = Instant::now();
        let mut inner = self.inner.write().unwrap();

        // Garbage-collect jobs completed more than 30 seconds ago.
        inner.jobs.retain(|_id, job| match job.completed_at {
            Some(t) => now.duration_since(t).as_secs() < 30,
            None => true,
        });

        let pipelines: Vec<PipelineSnapshot> = inner
            .jobs
            .iter()
            .map(|(id, job)| {
                let partitions_done = job
                    .partitions
                    .iter()
                    .filter(|p| p.phase == PartitionPhase::Done)
                    .count();
                PipelineSnapshot {
                    job_id: id.clone(),
                    proof_kind: job.proof_kind.clone(),
                    started_secs_ago: now.duration_since(job.started_at).as_secs_f64(),
                    total_partitions: job.total_partitions,
                    partitions_done,
                    partitions: job
                        .partitions
                        .iter()
                        .enumerate()
                        .map(|(i, p)| PartitionSnapshot {
                            index: i,
                            state: p.phase.as_str(),
                            synth_ms: match (p.synth_start, p.synth_end) {
                                (Some(s), Some(e)) => Some(e.duration_since(s).as_millis() as u64),
                                (Some(s), None) => Some(now.duration_since(s).as_millis() as u64),
                                _ => None,
                            },
                            gpu_ms: match (p.gpu_start, p.gpu_end) {
                                (Some(s), Some(e)) => Some(e.duration_since(s).as_millis() as u64),
                                (Some(s), None) => Some(now.duration_since(s).as_millis() as u64),
                                _ => None,
                            },
                        })
                        .collect(),
                }
            })
            .collect();

        let gpu_workers: Vec<GpuWorkerSnapshot> = inner
            .gpu_workers
            .iter()
            .map(|w| GpuWorkerSnapshot {
                worker_id: w.worker_id,
                gpu_ordinal: w.gpu_ordinal,
                state: if w.busy { "proving" } else { "idle" },
                current_job: w.current_job_id.clone(),
                current_partition: w.current_partition,
                busy_secs: w.busy_since.map(|t| now.duration_since(t).as_secs_f64()),
            })
            .collect();

        // Read buffer flight counters from pipeline.rs atomics.
        use std::sync::atomic::Ordering::Relaxed;
        let buffers = BuffersSnapshot {
            synth_in_flight: crate::pipeline::SYNTH_IN_FLIGHT.load(Relaxed),
            provers_in_flight: crate::pipeline::PROVERS_IN_FLIGHT.load(Relaxed),
            aux_in_flight: crate::pipeline::AUX_IN_FLIGHT.load(Relaxed),
            shells_in_flight: crate::pipeline::PROVERS_SHELL_IN_FLIGHT.load(Relaxed),
            pending_handles: crate::pipeline::PENDING_HANDLES.load(Relaxed),
        };

        StatusSnapshot {
            uptime_secs: now.duration_since(self.started_at).as_secs_f64(),
            memory: MemoryStatus {
                total_bytes: self.budget.total_bytes(),
                used_bytes: self.budget.used_bytes(),
                available_bytes: self.budget.available_bytes(),
            },
            synthesis: SynthesisStatus {
                max_concurrent: inner.synth_max,
                active: inner.synth_active,
            },
            pipelines,
            gpu_workers,
            allocations: AllocationsSnapshot {
                srs: srs_entries,
                pce: pce_entries,
            },
            counters: CountersSnapshot {
                total_completed: inner.total_completed,
                total_failed: inner.total_failed,
                completed_by_kind: inner.completed_by_kind.clone(),
                failed_by_kind: inner.failed_by_kind.clone(),
                queue_depth: inner.queue_depth,
            },
            buffers,
        }
    }
}
