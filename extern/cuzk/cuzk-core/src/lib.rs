//! cuzk-core: Core engine library for the cuzk proving daemon.
//!
//! Provides the [`Engine`] type which manages proof submission, scheduling,
//! GPU worker dispatch, and SRS parameter residency.

#[cfg(all(feature = "cuda-supraseal", feature = "vulkan-cuzk"))]
compile_error!(
    "features `cuda-supraseal` and `vulkan-cuzk` are mutually exclusive — enable at most one GPU backend"
);

pub mod batch_collector;
pub mod config;
pub mod engine;
pub mod memory;
pub mod pinned_pool;
pub mod pipeline;
pub mod prover;
pub mod scheduler;
pub mod srs_manager;
pub mod status;
pub mod types;

pub use batch_collector::{BatchCollector, BatchConfig, ProofBatch};
pub use config::Config;
pub use engine::Engine;
pub use srs_manager::CircuitId;
pub use types::*;
