//! tonic gRPC service implementation for the ProvingEngine.

use std::sync::Arc;
use std::time::Instant;
use tonic::{Request, Response, Status};
use tracing::{error, info};

use cuzk_core::engine::Engine;
use cuzk_core::types::*;
use cuzk_proto::cuzk::v1 as pb;
use cuzk_proto::cuzk::v1::proving_engine_server::ProvingEngine;

/// The gRPC service implementation.
pub struct ProvingService {
    engine: Arc<Engine>,
}

impl ProvingService {
    pub fn new(engine: Arc<Engine>) -> Self {
        Self { engine }
    }
}

/// Convert a protobuf SubmitProofRequest into a core ProofRequest.
fn proto_to_request(req: &pb::SubmitProofRequest) -> Result<ProofRequest, Status> {
    let proof_kind = ProofKind::from_proto(req.proof_kind)
        .ok_or_else(|| Status::invalid_argument(format!("invalid proof_kind: {}", req.proof_kind)))?;

    let priority = if req.priority == 0 {
        Priority::default_for(proof_kind)
    } else {
        Priority::from_proto(req.priority)
    };

    Ok(ProofRequest {
        job_id: JobId(req.request_id.clone()),
        request_id: req.request_id.clone(),
        proof_kind,
        priority,
        sector_size: req.sector_size,
        registered_proof: req.registered_proof,
        vanilla_proof: req.vanilla_proof.clone(),
        sector_number: req.sector_number,
        miner_id: req.miner_id,
        randomness: req.randomness.clone(),
        partition_index: req.partition_index,
        sector_key_cid: req.sector_key_cid.clone(),
        new_sealed_cid: req.new_sealed_cid.clone(),
        new_unsealed_cid: req.new_unsealed_cid.clone(),
        submitted_at: Instant::now(),
    })
}

/// Convert a core JobStatus into a protobuf AwaitProofResponse.
fn status_to_proto(job_id: &str, status: &JobStatus) -> pb::AwaitProofResponse {
    match status {
        JobStatus::Completed(result) => pb::AwaitProofResponse {
            job_id: job_id.to_string(),
            status: pb::await_proof_response::Status::Completed as i32,
            proof: result.proof_bytes.clone(),
            error_message: String::new(),
            queue_wait_ms: result.timings.queue_wait.as_millis() as u64,
            srs_load_ms: result.timings.srs_load.as_millis() as u64,
            synthesis_ms: result.timings.synthesis.as_millis() as u64,
            gpu_compute_ms: result.timings.gpu_compute.as_millis() as u64,
            total_ms: result.timings.total.as_millis() as u64,
        },
        JobStatus::Failed(msg) => pb::AwaitProofResponse {
            job_id: job_id.to_string(),
            status: pb::await_proof_response::Status::Failed as i32,
            proof: vec![],
            error_message: msg.clone(),
            queue_wait_ms: 0,
            srs_load_ms: 0,
            synthesis_ms: 0,
            gpu_compute_ms: 0,
            total_ms: 0,
        },
        JobStatus::Cancelled => pb::AwaitProofResponse {
            job_id: job_id.to_string(),
            status: pb::await_proof_response::Status::Cancelled as i32,
            proof: vec![],
            error_message: String::new(),
            queue_wait_ms: 0,
            srs_load_ms: 0,
            synthesis_ms: 0,
            gpu_compute_ms: 0,
            total_ms: 0,
        },
        _ => pb::AwaitProofResponse {
            job_id: job_id.to_string(),
            status: pb::await_proof_response::Status::Unknown as i32,
            proof: vec![],
            error_message: String::new(),
            queue_wait_ms: 0,
            srs_load_ms: 0,
            synthesis_ms: 0,
            gpu_compute_ms: 0,
            total_ms: 0,
        },
    }
}

#[tonic::async_trait]
impl ProvingEngine for ProvingService {
    async fn submit_proof(
        &self,
        request: Request<pb::SubmitProofRequest>,
    ) -> Result<Response<pb::SubmitProofResponse>, Status> {
        let req = request.into_inner();
        info!(request_id = %req.request_id, proof_kind = req.proof_kind, "SubmitProof");

        let proof_request = proto_to_request(&req)?;
        let (job_id, position) = self.engine.submit(proof_request).await
            .map_err(|e| Status::internal(e.to_string()))?;

        Ok(Response::new(pb::SubmitProofResponse {
            job_id: job_id.0,
            queue_position: position,
            assigned_gpu: String::new(),
        }))
    }

    async fn await_proof(
        &self,
        request: Request<pb::AwaitProofRequest>,
    ) -> Result<Response<pb::AwaitProofResponse>, Status> {
        let req = request.into_inner();
        info!(job_id = %req.job_id, "AwaitProof");

        let job_id = JobId(req.job_id.clone());
        let status = self.engine.await_proof(&job_id).await
            .map_err(|e| Status::not_found(e.to_string()))?;

        Ok(Response::new(status_to_proto(&req.job_id, &status)))
    }

    async fn prove(
        &self,
        request: Request<pb::ProveRequest>,
    ) -> Result<Response<pb::ProveResponse>, Status> {
        let req = request.into_inner();
        let submit_req = req.submit
            .ok_or_else(|| Status::invalid_argument("missing submit field"))?;

        info!(request_id = %submit_req.request_id, proof_kind = submit_req.proof_kind, "Prove");

        let proof_request = proto_to_request(&submit_req)?;
        let job_id_str = proof_request.job_id.0.clone();

        let status = self.engine.prove(proof_request).await
            .map_err(|e| {
                error!(error = %e, "prove failed");
                Status::internal(e.to_string())
            })?;

        Ok(Response::new(pb::ProveResponse {
            result: Some(status_to_proto(&job_id_str, &status)),
        }))
    }

    async fn cancel_proof(
        &self,
        request: Request<pb::CancelProofRequest>,
    ) -> Result<Response<pb::CancelProofResponse>, Status> {
        let req = request.into_inner();
        info!(job_id = %req.job_id, "CancelProof (not yet implemented)");
        // Phase 0: cancellation not implemented
        Ok(Response::new(pb::CancelProofResponse {
            was_running: false,
        }))
    }

    async fn get_status(
        &self,
        _request: Request<pb::GetStatusRequest>,
    ) -> Result<Response<pb::GetStatusResponse>, Status> {
        let status = self.engine.get_status().await;

        let srs_entries: Vec<pb::SrsStatus> = status.preloaded_srs.iter().map(|cid| {
            pb::SrsStatus {
                circuit_id: cid.clone(),
                tier: pb::srs_status::Tier::Hot as i32,
                size_bytes: 0,  // TODO: track actual sizes
                ref_count: 0,
            }
        }).collect();

        let queue_entries: Vec<pb::QueueStatus> = status.queue_stats.iter().map(|(kind, count)| {
            pb::QueueStatus {
                proof_kind: kind.to_string(),
                pending: *count,
                in_progress: if status.running_job.is_some() { 1 } else { 0 },
            }
        }).collect();

        Ok(Response::new(pb::GetStatusResponse {
            gpus: vec![],  // TODO: detect GPUs
            loaded_srs: srs_entries,
            queues: queue_entries,
            total_proofs_completed: status.total_completed,
            total_proofs_failed: status.total_failed,
            uptime_seconds: status.uptime_seconds,
            pinned_memory_bytes: 0,
            pinned_memory_limit_bytes: 0,
        }))
    }

    async fn get_metrics(
        &self,
        _request: Request<pb::GetMetricsRequest>,
    ) -> Result<Response<pb::GetMetricsResponse>, Status> {
        // Phase 0: minimal metrics
        let status = self.engine.get_status().await;
        let text = format!(
            "# HELP cuzk_proofs_completed_total Total proofs completed\n\
             # TYPE cuzk_proofs_completed_total counter\n\
             cuzk_proofs_completed_total {}\n\
             # HELP cuzk_proofs_failed_total Total proofs failed\n\
             # TYPE cuzk_proofs_failed_total counter\n\
             cuzk_proofs_failed_total {}\n\
             # HELP cuzk_uptime_seconds Daemon uptime\n\
             # TYPE cuzk_uptime_seconds gauge\n\
             cuzk_uptime_seconds {}\n\
             # HELP cuzk_queue_depth Current queue depth\n\
             # TYPE cuzk_queue_depth gauge\n\
             cuzk_queue_depth {}\n",
            status.total_completed,
            status.total_failed,
            status.uptime_seconds,
            status.queue_depth,
        );
        Ok(Response::new(pb::GetMetricsResponse {
            prometheus_text: text,
        }))
    }

    async fn preload_srs(
        &self,
        request: Request<pb::PreloadSrsRequest>,
    ) -> Result<Response<pb::PreloadSrsResponse>, Status> {
        let req = request.into_inner();
        info!(circuit_id = %req.circuit_id, "PreloadSRS");

        let (already_loaded, load_time_ms) = self.engine.preload_srs(&req.circuit_id).await
            .map_err(|e| Status::internal(e.to_string()))?;

        Ok(Response::new(pb::PreloadSrsResponse {
            already_loaded,
            load_time_ms,
        }))
    }

    async fn evict_srs(
        &self,
        request: Request<pb::EvictSrsRequest>,
    ) -> Result<Response<pb::EvictSrsResponse>, Status> {
        let req = request.into_inner();
        info!(circuit_id = %req.circuit_id, "EvictSRS (not yet implemented)");
        // Phase 0: eviction not implemented
        Ok(Response::new(pb::EvictSrsResponse {
            was_loaded: false,
            freed_bytes: 0,
        }))
    }
}
