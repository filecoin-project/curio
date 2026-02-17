//! cuzk-core: Core engine library for the cuzk proving daemon.
//!
//! Provides the [`Engine`] type which manages proof submission, scheduling,
//! GPU worker dispatch, and SRS parameter residency.

pub mod batch_collector;
pub mod config;
pub mod engine;
pub mod pipeline;
pub mod prover;
pub mod scheduler;
pub mod srs_manager;
pub mod types;

pub use batch_collector::{BatchCollector, BatchConfig, ProofBatch};
pub use config::Config;
pub use engine::Engine;
pub use srs_manager::CircuitId;
pub use types::*;
