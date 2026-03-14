//! tonic gRPC service implementation for the ProvingEngine.

use std::sync::Arc;
use std::time::Instant;
use tonic::{Request, Response, Status};
use tracing::{error, info, warn};

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
        vanilla_proofs: req.vanilla_proofs.clone(),
        sector_number: req.sector_number,
        miner_id: req.miner_id,
        randomness: req.randomness.clone(),
        partition_index: req.partition_index,
        comm_r_old: req.comm_r_old.clone(),
        comm_r_new: req.comm_r_new.clone(),
        comm_d_new: req.comm_d_new.clone(),
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
        info!(
            request_id = %req.request_id,
            proof_kind = req.proof_kind,
            input_size = req.vanilla_proof.len(),
            "SubmitProof"
        );

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

        info!(
            request_id = %submit_req.request_id,
            proof_kind = submit_req.proof_kind,
            input_size = submit_req.vanilla_proof.len(),
            "Prove"
        );

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
        warn!(job_id = %req.job_id, "CancelProof (not yet implemented)");
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
                size_bytes: 0,  // TODO: track actual sizes in Phase 1
                ref_count: 0,
            }
        }).collect();

        // Count in-progress jobs per proof kind from worker state
        let running_by_kind: std::collections::HashMap<ProofKind, u32> = {
            let mut m = std::collections::HashMap::new();
            for w in &status.workers {
                if let Some((_, kind)) = &w.current_job {
                    *m.entry(*kind).or_insert(0) += 1;
                }
            }
            m
        };

        let queue_entries: Vec<pb::QueueStatus> = status.queue_stats.iter().map(|(kind, count)| {
            pb::QueueStatus {
                proof_kind: kind.to_string(),
                pending: *count,
                in_progress: running_by_kind.get(kind).copied().unwrap_or(0),
            }
        }).collect();

        // GPU info — detect GPUs and annotate with running jobs from worker state.
        let detected_gpus = detect_gpus();

        // Build GPU status from worker state, falling back to detected GPU info
        let gpus: Vec<pb::GpuStatus> = if status.workers.is_empty() {
            detected_gpus
        } else {
            status.workers.iter().map(|worker| {
                // Find matching detected GPU for VRAM info
                let detected = detected_gpus.iter().find(|g| g.ordinal == worker.gpu_ordinal);
                let (current_job_id, current_proof_kind) = match &worker.current_job {
                    Some((job_id, kind)) => (job_id.0.clone(), kind.to_string()),
                    None => (String::new(), String::new()),
                };
                pb::GpuStatus {
                    ordinal: worker.gpu_ordinal,
                    name: detected.map_or_else(|| format!("GPU {}", worker.gpu_ordinal), |g| g.name.clone()),
                    vram_total_bytes: detected.map_or(0, |g| g.vram_total_bytes),
                    vram_free_bytes: detected.map_or(0, |g| g.vram_free_bytes),
                    current_job_id,
                    current_proof_kind,
                }
            }).collect()
        };

        Ok(Response::new(pb::GetStatusResponse {
            gpus,
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
        let status = self.engine.get_status().await;
        let stats = self.engine.get_proof_stats().await;

        let mut text = String::with_capacity(4096);

        // --- Global counters ---
        text.push_str("# HELP cuzk_proofs_completed_total Total proofs completed\n");
        text.push_str("# TYPE cuzk_proofs_completed_total counter\n");
        text.push_str(&format!("cuzk_proofs_completed_total {}\n", status.total_completed));

        text.push_str("# HELP cuzk_proofs_failed_total Total proofs failed\n");
        text.push_str("# TYPE cuzk_proofs_failed_total counter\n");
        text.push_str(&format!("cuzk_proofs_failed_total {}\n", status.total_failed));

        text.push_str("# HELP cuzk_uptime_seconds Daemon uptime\n");
        text.push_str("# TYPE cuzk_uptime_seconds gauge\n");
        text.push_str(&format!("cuzk_uptime_seconds {}\n", status.uptime_seconds));

        text.push_str("# HELP cuzk_queue_depth Current queue depth\n");
        text.push_str("# TYPE cuzk_queue_depth gauge\n");
        text.push_str(&format!("cuzk_queue_depth {}\n", status.queue_depth));

        // --- Per proof-kind counters ---
        text.push_str("# HELP cuzk_proofs_completed Per proof-kind completions\n");
        text.push_str("# TYPE cuzk_proofs_completed counter\n");
        for (kind, count) in &stats.completed_by_kind {
            text.push_str(&format!(
                "cuzk_proofs_completed{{proof_kind=\"{}\"}} {}\n",
                kind.metric_label(), count
            ));
        }

        text.push_str("# HELP cuzk_proofs_failed Per proof-kind failures\n");
        text.push_str("# TYPE cuzk_proofs_failed counter\n");
        for (kind, count) in &stats.failed_by_kind {
            text.push_str(&format!(
                "cuzk_proofs_failed{{proof_kind=\"{}\"}} {}\n",
                kind.metric_label(), count
            ));
        }

        // --- Duration histogram approximation ---
        // Emit recent proof durations as a summary (last N proofs).
        if !stats.recent_durations.is_empty() {
            text.push_str("# HELP cuzk_proof_duration_seconds Proof duration\n");
            text.push_str("# TYPE cuzk_proof_duration_seconds summary\n");

            // Group by kind, compute count/sum
            let mut kind_stats: std::collections::HashMap<ProofKind, (u64, f64)> =
                std::collections::HashMap::new();
            for (kind, dur) in &stats.recent_durations {
                let entry = kind_stats.entry(*kind).or_insert((0, 0.0));
                entry.0 += 1;
                entry.1 += dur.as_secs_f64();
            }
            for (kind, (count, sum)) in &kind_stats {
                text.push_str(&format!(
                    "cuzk_proof_duration_seconds_count{{proof_kind=\"{}\"}} {}\n",
                    kind.metric_label(), count
                ));
                text.push_str(&format!(
                    "cuzk_proof_duration_seconds_sum{{proof_kind=\"{}\"}} {:.3}\n",
                    kind.metric_label(), sum
                ));
            }
        }

        // --- Running jobs ---
        let running_count = status.workers.iter().filter(|w| w.current_job.is_some()).count();
        text.push_str("# HELP cuzk_running_jobs Currently running proofs\n");
        text.push_str("# TYPE cuzk_running_jobs gauge\n");
        text.push_str(&format!("cuzk_running_jobs {}\n", running_count));

        // --- Worker count ---
        text.push_str("# HELP cuzk_gpu_workers Number of GPU workers\n");
        text.push_str("# TYPE cuzk_gpu_workers gauge\n");
        text.push_str(&format!("cuzk_gpu_workers {}\n", status.workers.len()));

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
        warn!(circuit_id = %req.circuit_id, "EvictSRS (not yet implemented)");
        // Phase 0: eviction not implemented
        Ok(Response::new(pb::EvictSrsResponse {
            was_loaded: false,
            freed_bytes: 0,
        }))
    }
}

/// Detect GPUs by parsing nvidia-smi output.
///
/// Phase 0: subprocess call, cached per status request.
/// Phase 1: NVML bindings for real-time data.
fn detect_gpus() -> Vec<pb::GpuStatus> {
    let output = std::process::Command::new("nvidia-smi")
        .args([
            "--query-gpu=index,name,memory.total,memory.free",
            "--format=csv,noheader,nounits",
        ])
        .output();

    match output {
        Ok(out) if out.status.success() => {
            let text = String::from_utf8_lossy(&out.stdout);
            text.lines()
                .filter_map(|line| {
                    let parts: Vec<&str> = line.split(',').map(str::trim).collect();
                    if parts.len() >= 4 {
                        let ordinal = parts[0].parse::<u32>().unwrap_or(0);
                        let name = parts[1].to_string();
                        let total_mib = parts[2].parse::<u64>().unwrap_or(0);
                        let free_mib = parts[3].parse::<u64>().unwrap_or(0);
                        Some(pb::GpuStatus {
                            ordinal,
                            name,
                            vram_total_bytes: total_mib * 1024 * 1024,
                            vram_free_bytes: free_mib * 1024 * 1024,
                            current_job_id: String::new(),
                            current_proof_kind: String::new(),
                        })
                    } else {
                        None
                    }
                })
                .collect()
        }
        _ => vec![],
    }
}
