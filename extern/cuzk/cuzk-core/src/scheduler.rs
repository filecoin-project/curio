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

#[cfg(test)]
mod step_p02_scheduler_tests {
    use super::*;
    use crate::types::{JobId, Priority, ProofKind, ProofRequest};
    use std::time::{Duration, Instant};

    fn mk_req(job: &str, priority: Priority, kind: ProofKind, submitted_at: Instant) -> ProofRequest {
        ProofRequest {
            job_id: JobId(job.to_string()),
            request_id: job.to_string(),
            proof_kind: kind,
            priority,
            submitted_at,
            ..Default::default()
        }
    }

    #[tokio::test]
    async fn step_p02_scheduler_priority_ordering() {
        let s = Scheduler::new();
        let t0 = Instant::now();
        s.submit(mk_req("low", Priority::Low, ProofKind::PoRepSealCommit, t0))
            .await;
        s.submit(mk_req(
            "critical",
            Priority::Critical,
            ProofKind::PoRepSealCommit,
            t0,
        ))
        .await;
        s.submit(mk_req("normal", Priority::Normal, ProofKind::PoRepSealCommit, t0))
            .await;
        assert_eq!(s.next().await.job_id.0, "critical");
        assert_eq!(s.next().await.job_id.0, "normal");
        assert_eq!(s.next().await.job_id.0, "low");
    }

    #[tokio::test]
    async fn step_p02_scheduler_fifo_within_priority() {
        let s = Scheduler::new();
        let t0 = Instant::now();
        s.submit(mk_req("a", Priority::Normal, ProofKind::PoRepSealCommit, t0))
            .await;
        tokio::time::sleep(Duration::from_millis(5)).await;
        s.submit(mk_req("b", Priority::Normal, ProofKind::PoRepSealCommit, Instant::now()))
            .await;
        assert_eq!(s.next().await.job_id.0, "a");
        assert_eq!(s.next().await.job_id.0, "b");
    }

    #[tokio::test]
    async fn step_p02_scheduler_stats_per_kind() {
        let s = Scheduler::new();
        let t0 = Instant::now();
        s.submit(mk_req(
            "1",
            Priority::Normal,
            ProofKind::PoRepSealCommit,
            t0,
        ))
        .await;
        s.submit(mk_req(
            "2",
            Priority::Normal,
            ProofKind::WinningPost,
            t0,
        ))
        .await;
        let mut stats = s.stats().await;
        stats.sort_by(|a, b| format!("{:?}", a.0).cmp(&format!("{:?}", b.0)));
        assert_eq!(stats.len(), 2);
        let sum: u32 = stats.iter().map(|(_, c)| c).sum();
        assert_eq!(sum, 2);
    }

    #[tokio::test]
    async fn step_p02_scheduler_len_zero_after_drain() {
        let s = Scheduler::new();
        let t0 = Instant::now();
        s.submit(mk_req("x", Priority::High, ProofKind::PoRepSealCommit, t0))
            .await;
        assert_eq!(s.len().await, 1);
        let _ = s.next().await;
        assert_eq!(s.len().await, 0);
    }
}
