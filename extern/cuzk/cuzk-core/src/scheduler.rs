//! Scheduler — dispatches proof requests to GPU workers.
//!
//! Phase 0: Simple FIFO with priority ordering. Single GPU worker.
//! Phase 1+: Multi-GPU, affinity-aware, batch collection.

use std::collections::BinaryHeap;
use std::cmp::Ordering;
use tokio::sync::Mutex;
use tracing::info;

use crate::types::{ProofKind, ProofRequest};

/// A prioritized entry in the scheduler queue.
struct QueueEntry {
    request: ProofRequest,
}

// Higher priority → dequeued first. Within same priority, earlier submission first (FIFO).
impl PartialEq for QueueEntry {
    fn eq(&self, other: &Self) -> bool {
        self.request.job_id == other.request.job_id
    }
}

impl Eq for QueueEntry {}

impl PartialOrd for QueueEntry {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for QueueEntry {
    fn cmp(&self, other: &Self) -> Ordering {
        // Higher priority first
        let prio_cmp = (self.request.priority as u8).cmp(&(other.request.priority as u8));
        if prio_cmp != Ordering::Equal {
            return prio_cmp;
        }
        // Earlier submission first (reverse: earlier instant is "greater" priority)
        other.request.submitted_at.cmp(&self.request.submitted_at)
    }
}

/// The proof scheduler.
///
/// Phase 0: Single priority queue, single consumer.
/// Phase 1+: Per-GPU queues, affinity tracking, batch collection.
pub struct Scheduler {
    queue: Mutex<BinaryHeap<QueueEntry>>,
    /// Notify channel: signal that a new job is available.
    notify: tokio::sync::Notify,
}

impl Scheduler {
    pub fn new() -> Self {
        Self {
            queue: Mutex::new(BinaryHeap::new()),
            notify: tokio::sync::Notify::new(),
        }
    }

    /// Enqueue a proof request. Returns queue position.
    pub async fn submit(&self, request: ProofRequest) -> u32 {
        let mut queue = self.queue.lock().await;
        let position = queue.len() as u32;
        info!(
            job_id = %request.job_id,
            proof_kind = %request.proof_kind,
            priority = ?request.priority,
            queue_position = position,
            "job enqueued"
        );
        queue.push(QueueEntry { request });
        self.notify.notify_one();
        position
    }

    /// Dequeue the highest-priority request. Blocks if queue is empty.
    pub async fn next(&self) -> ProofRequest {
        loop {
            {
                let mut queue = self.queue.lock().await;
                if let Some(entry) = queue.pop() {
                    info!(
                        job_id = %entry.request.job_id,
                        proof_kind = %entry.request.proof_kind,
                        "job dequeued for proving"
                    );
                    return entry.request;
                }
            }
            // Queue is empty — wait for notification
            self.notify.notified().await;
        }
    }

    /// Get current queue depth.
    pub async fn len(&self) -> usize {
        self.queue.lock().await.len()
    }

    /// Get per-proof-kind queue stats.
    pub async fn stats(&self) -> Vec<(ProofKind, u32)> {
        let queue = self.queue.lock().await;
        let mut counts = std::collections::HashMap::new();
        for entry in queue.iter() {
            *counts.entry(entry.request.proof_kind).or_insert(0u32) += 1;
        }
        counts.into_iter().collect()
    }
}
