//! Common types used throughout the cuzk engine.

use std::fmt;
use std::time::{Duration, Instant};

/// Unique identifier for a proof job.
#[derive(Debug, Clone, Hash, PartialEq, Eq)]
pub struct JobId(pub String);

impl fmt::Display for JobId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.0)
    }
}

/// The type of proof being requested.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum ProofKind {
    PoRepSealCommit,
    SnapDealsUpdate,
    WindowPostPartition,
    WinningPost,
}

impl fmt::Display for ProofKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ProofKind::PoRepSealCommit => write!(f, "porep-c2"),
            ProofKind::SnapDealsUpdate => write!(f, "snap-update"),
            ProofKind::WindowPostPartition => write!(f, "window-post"),
            ProofKind::WinningPost => write!(f, "winning-post"),
        }
    }
}

impl ProofKind {
    /// Return the SRS circuit ID string used for SRS management.
    pub fn circuit_id(&self, sector_size_gib: u32) -> String {
        match self {
            ProofKind::PoRepSealCommit => format!("porep-{}g", sector_size_gib),
            ProofKind::SnapDealsUpdate => format!("snap-{}g", sector_size_gib),
            ProofKind::WindowPostPartition => format!("wpost-{}g", sector_size_gib),
            ProofKind::WinningPost => format!("winning-{}g", sector_size_gib),
        }
    }

    /// Convert from protobuf ProofKind enum value.
    pub fn from_proto(v: i32) -> Option<Self> {
        match v {
            1 => Some(ProofKind::PoRepSealCommit),
            2 => Some(ProofKind::SnapDealsUpdate),
            3 => Some(ProofKind::WindowPostPartition),
            4 => Some(ProofKind::WinningPost),
            _ => None,
        }
    }
}

/// Priority level for scheduling.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Priority {
    Low = 1,
    Normal = 2,
    High = 3,
    Critical = 4,
}

impl Priority {
    /// Convert from protobuf Priority enum value.
    pub fn from_proto(v: i32) -> Self {
        match v {
            1 => Priority::Low,
            3 => Priority::High,
            4 => Priority::Critical,
            _ => Priority::Normal,
        }
    }

    /// Default priority for a proof kind.
    pub fn default_for(kind: ProofKind) -> Self {
        match kind {
            ProofKind::WinningPost => Priority::Critical,
            ProofKind::WindowPostPartition => Priority::High,
            ProofKind::PoRepSealCommit | ProofKind::SnapDealsUpdate => Priority::Normal,
        }
    }
}

/// A proof request submitted to the engine.
#[derive(Debug, Clone)]
pub struct ProofRequest {
    pub job_id: JobId,
    pub request_id: String,
    pub proof_kind: ProofKind,
    pub priority: Priority,
    pub sector_size: u64,
    pub registered_proof: u64,
    pub vanilla_proof: Vec<u8>,
    pub sector_number: u64,
    pub miner_id: u64,
    pub randomness: Vec<u8>,
    pub partition_index: u32,
    // SnapDeals fields
    pub sector_key_cid: Vec<u8>,
    pub new_sealed_cid: Vec<u8>,
    pub new_unsealed_cid: Vec<u8>,
    /// When the request was submitted
    pub submitted_at: Instant,
}

/// Status of a proof job.
#[derive(Debug, Clone)]
pub enum JobStatus {
    Queued { position: u32 },
    Running { gpu_ordinal: u32 },
    Completed(ProofResult),
    Failed(String),
    Cancelled,
}

/// Result of a completed proof.
#[derive(Debug, Clone)]
pub struct ProofResult {
    pub job_id: JobId,
    pub proof_bytes: Vec<u8>,
    pub timings: ProofTimings,
}

/// Timing breakdown for a proof.
#[derive(Debug, Clone, Default)]
pub struct ProofTimings {
    pub queue_wait: Duration,
    pub srs_load: Duration,
    pub synthesis: Duration,
    pub gpu_compute: Duration,
    pub total: Duration,
}

/// Error type for engine operations.
#[derive(Debug, thiserror::Error)]
pub enum EngineError {
    #[error("job not found: {0}")]
    JobNotFound(JobId),
    #[error("invalid proof kind: {0}")]
    InvalidProofKind(i32),
    #[error("proving failed: {0}")]
    ProvingFailed(String),
    #[error("SRS load failed: {0}")]
    SrsLoadFailed(String),
    #[error("engine shutting down")]
    ShuttingDown,
    #[error("internal error: {0}")]
    Internal(String),
}
