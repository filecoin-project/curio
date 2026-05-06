//! Batch collector — accumulates same-circuit-type proof requests before
//! flushing them as a single batched operation.
//!
//! Phase 3: Cross-sector batching for PoRep C2.
//!
//! When multiple PoRep sectors arrive in quick succession, the batch collector
//! groups them and produces a single combined synthesis + GPU proving pass.
//! This amortizes fixed GPU costs (MSM setup, kernel launches, batch_addition)
//! and improves SM utilization.
//!
//! Batching is only meaningful for PoRep C2 and SnapDeals — circuits that
//! share the same R1CS structure and SRS. WindowPoSt and WinningPoSt
//! bypass the batch collector entirely.
//!
//! ## How It Works
//!
//! ```text
//!   Scheduler → [batch_collector] → flush → synthesize N sectors → GPU → split results
//!                ↑                    ↑
//!                accumulate          max_batch_size reached
//!                same-type jobs      or max_batch_wait expired
//! ```
//!
//! The batch collector does NOT change the gRPC API — callers still submit
//! individual proof requests. Batching is an internal engine optimization
//! that is transparent to clients.

use std::time::{Duration, Instant};
use tokio::sync::Notify;

use crate::types::{ProofKind, ProofRequest};

/// A batch of same-type proof requests ready for combined processing.
#[derive(Debug)]
pub struct ProofBatch {
    /// The proof kind (all requests in the batch share this).
    pub proof_kind: ProofKind,
    /// The accumulated proof requests.
    pub requests: Vec<ProofRequest>,
    /// When the first request in this batch was received.
    pub first_received_at: Instant,
}

impl ProofBatch {
    /// Number of sectors in this batch.
    pub fn len(&self) -> usize {
        self.requests.len()
    }

    pub fn is_empty(&self) -> bool {
        self.requests.is_empty()
    }
}

/// Configuration for batch collection.
#[derive(Debug, Clone)]
pub struct BatchConfig {
    /// Maximum number of proofs in a single batch.
    /// 1 = no batching (Phase 2 behavior).
    /// 2-5 = typical for PoRep on 128-256 GiB machines.
    pub max_batch_size: u32,
    /// Maximum time (ms) to wait for a batch to fill before flushing.
    /// 0 = flush immediately (no waiting for more requests).
    pub max_batch_wait_ms: u64,
}

impl Default for BatchConfig {
    fn default() -> Self {
        Self {
            max_batch_size: 1,
            max_batch_wait_ms: 10_000,
        }
    }
}

/// Determines whether a proof kind is eligible for cross-sector batching.
///
/// Only proof types that share identical R1CS structure and SRS can be
/// batched together. PoRep C2 (all 32G sectors have the same circuit)
/// and SnapDeals are eligible. PoSt proof types are not batched because
/// they are typically singleton or urgent.
pub fn is_batchable(kind: ProofKind) -> bool {
    matches!(
        kind,
        ProofKind::PoRepSealCommit | ProofKind::SnapDealsUpdate
    )
}

/// Synchronous batch collector.
///
/// Accumulates proof requests and flushes them when the batch is full or
/// the wait timer expires. This is used by the engine's synthesis task
/// to collect same-type requests before running combined synthesis.
///
/// The collector is NOT thread-safe — it is owned by the single synthesis
/// task and accessed sequentially.
pub struct BatchCollector {
    config: BatchConfig,
    /// Currently accumulating batch (None if empty).
    pending: Option<ProofBatch>,
    /// Notification sent when a new request is added (for timer wakeup).
    notify: Notify,
}

impl BatchCollector {
    /// Create a new batch collector with the given configuration.
    pub fn new(config: BatchConfig) -> Self {
        Self {
            config,
            pending: None,
            notify: Notify::new(),
        }
    }

    /// Add a request to the current batch.
    ///
    /// Returns `Some(batch)` if the batch is now full and should be flushed.
    /// Returns `None` if the batch needs more requests or is waiting.
    pub fn add(&mut self, request: ProofRequest) -> Option<ProofBatch> {
        let kind = request.proof_kind;

        match &mut self.pending {
            Some(batch) if batch.proof_kind == kind => {
                batch.requests.push(request);
                if batch.requests.len() as u32 >= self.config.max_batch_size {
                    // Batch is full — flush immediately
                    self.pending.take()
                } else {
                    self.notify.notify_one();
                    None
                }
            }
            Some(_batch) => {
                // Different proof kind — flush the existing batch, start new one
                let old = self.pending.take();
                self.pending = Some(ProofBatch {
                    proof_kind: kind,
                    requests: vec![request],
                    first_received_at: Instant::now(),
                });
                old
            }
            None => {
                // Empty — start a new batch
                self.pending = Some(ProofBatch {
                    proof_kind: kind,
                    requests: vec![request],
                    first_received_at: Instant::now(),
                });
                if self.config.max_batch_size <= 1 {
                    // No batching — flush immediately
                    self.pending.take()
                } else {
                    self.notify.notify_one();
                    None
                }
            }
        }
    }

    /// Check if the pending batch should be flushed due to timeout.
    ///
    /// Returns `Some(batch)` if the wait timer has expired.
    /// Returns `None` if the batch is not ready or is empty.
    pub fn check_timeout(&mut self) -> Option<ProofBatch> {
        if let Some(batch) = &self.pending {
            let elapsed = batch.first_received_at.elapsed();
            if elapsed >= Duration::from_millis(self.config.max_batch_wait_ms) {
                return self.pending.take();
            }
        }
        None
    }

    /// Force flush the pending batch, regardless of size or timer.
    ///
    /// Used when a high-priority request arrives that shouldn't wait,
    /// or during shutdown.
    pub fn force_flush(&mut self) -> Option<ProofBatch> {
        self.pending.take()
    }

    /// Time remaining before the current batch times out.
    /// Returns None if no batch is pending.
    pub fn time_until_flush(&self) -> Option<Duration> {
        self.pending.as_ref().map(|batch| {
            let elapsed = batch.first_received_at.elapsed();
            let timeout = Duration::from_millis(self.config.max_batch_wait_ms);
            timeout.saturating_sub(elapsed)
        })
    }

    /// Whether there is a pending batch.
    pub fn has_pending(&self) -> bool {
        self.pending.is_some()
    }

    /// Number of requests in the pending batch.
    pub fn pending_count(&self) -> usize {
        self.pending.as_ref().map_or(0, |b| b.requests.len())
    }

    /// Reference to the notify handle (for async timer integration).
    pub fn notify(&self) -> &Notify {
        &self.notify
    }

    /// The configured max batch size.
    pub fn max_batch_size(&self) -> u32 {
        self.config.max_batch_size
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::{JobId, Priority};

    fn make_porep_request(id: &str) -> ProofRequest {
        ProofRequest {
            job_id: JobId(id.to_string()),
            request_id: id.to_string(),
            proof_kind: ProofKind::PoRepSealCommit,
            priority: Priority::Normal,
            sector_size: 34359738368,
            registered_proof: 0,
            vanilla_proof: vec![1, 2, 3],
            vanilla_proofs: vec![],
            sector_number: 1,
            miner_id: 1000,
            randomness: vec![],
            partition_index: 0,
            comm_r_old: vec![],
            comm_r_new: vec![],
            comm_d_new: vec![],
            submitted_at: Instant::now(),
        }
    }

    fn make_winning_request(id: &str) -> ProofRequest {
        let mut req = make_porep_request(id);
        req.proof_kind = ProofKind::WinningPost;
        req
    }

    #[test]
    fn test_no_batching() {
        let mut collector = BatchCollector::new(BatchConfig {
            max_batch_size: 1,
            max_batch_wait_ms: 10_000,
        });

        // With max_batch_size=1, every add returns immediately
        let batch = collector.add(make_porep_request("job-1"));
        assert!(batch.is_some());
        let batch = batch.unwrap();
        assert_eq!(batch.len(), 1);
        assert_eq!(batch.requests[0].job_id.0, "job-1");
    }

    #[test]
    fn test_batch_fills() {
        let mut collector = BatchCollector::new(BatchConfig {
            max_batch_size: 3,
            max_batch_wait_ms: 60_000,
        });

        // First two don't flush
        assert!(collector.add(make_porep_request("job-1")).is_none());
        assert!(collector.add(make_porep_request("job-2")).is_none());

        // Third fills the batch
        let batch = collector.add(make_porep_request("job-3"));
        assert!(batch.is_some());
        let batch = batch.unwrap();
        assert_eq!(batch.len(), 3);
        assert_eq!(batch.proof_kind, ProofKind::PoRepSealCommit);
    }

    #[test]
    fn test_different_kind_flushes() {
        let mut collector = BatchCollector::new(BatchConfig {
            max_batch_size: 3,
            max_batch_wait_ms: 60_000,
        });

        // Add a PoRep request
        assert!(collector.add(make_porep_request("job-1")).is_none());

        // Adding a different kind flushes the existing batch
        let batch = collector.add(make_winning_request("job-2"));
        assert!(batch.is_some());
        let batch = batch.unwrap();
        assert_eq!(batch.len(), 1); // The flushed PoRep batch
        assert_eq!(batch.proof_kind, ProofKind::PoRepSealCommit);

        // The WinningPost is now pending
        assert_eq!(collector.pending_count(), 1);
    }

    #[test]
    fn test_force_flush() {
        let mut collector = BatchCollector::new(BatchConfig {
            max_batch_size: 5,
            max_batch_wait_ms: 60_000,
        });

        assert!(collector.add(make_porep_request("job-1")).is_none());
        assert!(collector.add(make_porep_request("job-2")).is_none());

        let batch = collector.force_flush();
        assert!(batch.is_some());
        assert_eq!(batch.unwrap().len(), 2);
        assert!(!collector.has_pending());
    }

    #[test]
    fn test_is_batchable() {
        assert!(is_batchable(ProofKind::PoRepSealCommit));
        assert!(is_batchable(ProofKind::SnapDealsUpdate));
        assert!(!is_batchable(ProofKind::WinningPost));
        assert!(!is_batchable(ProofKind::WindowPostPartition));
    }

    #[test]
    fn test_timeout_check() {
        let mut collector = BatchCollector::new(BatchConfig {
            max_batch_size: 5,
            max_batch_wait_ms: 0, // immediate timeout
        });

        assert!(collector.add(make_porep_request("job-1")).is_none());

        // With 0ms timeout, check should flush immediately
        let batch = collector.check_timeout();
        assert!(batch.is_some());
        assert_eq!(batch.unwrap().len(), 1);
    }

    #[test]
    fn test_time_until_flush() {
        let collector_empty = BatchCollector::new(BatchConfig {
            max_batch_size: 3,
            max_batch_wait_ms: 10_000,
        });
        assert!(collector_empty.time_until_flush().is_none());

        let mut collector = BatchCollector::new(BatchConfig {
            max_batch_size: 3,
            max_batch_wait_ms: 10_000,
        });
        collector.add(make_porep_request("job-1"));
        let remaining = collector.time_until_flush();
        assert!(remaining.is_some());
        assert!(remaining.unwrap() <= Duration::from_millis(10_000));
    }
}
