//! cuzk-bench: Testing and benchmarking utility for the cuzk proving daemon.
//!
//! Commands:
//!   single   — Run a single proof through the daemon
//!   batch    — Run N identical proofs and report throughput
//!   status   — Query daemon status
//!   preload  — Pre-warm SRS parameters
//!   metrics  — Get Prometheus metrics

use anyhow::{Context, Result};
use clap::{Parser, Subcommand};
use std::path::PathBuf;
use std::time::Instant;
use tracing::info;

use cuzk_proto::cuzk::v1 as pb;

#[derive(Parser, Debug)]
#[command(name = "cuzk-bench", about = "cuzk proving engine test/benchmark utility")]
struct Cli {
    /// Daemon address (e.g. "unix:///run/curio/cuzk.sock" or "http://localhost:9820").
    #[arg(short, long, default_value = "http://127.0.0.1:9820")]
    addr: String,

    /// Log level.
    #[arg(long, default_value = "info")]
    log_level: String,

    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand, Debug)]
enum Commands {
    /// Run a single proof through the daemon.
    Single {
        /// Proof type: porep, snap, wpost, winning.
        #[arg(short = 't', long = "type")]
        proof_type: String,

        /// Path to C1 output JSON (for PoRep) or vanilla proof file.
        #[arg(long)]
        c1: Option<PathBuf>,

        /// Path to vanilla proof file (for PoSt/SnapDeals).
        #[arg(long)]
        vanilla: Option<PathBuf>,

        /// Sector number.
        #[arg(long, default_value = "1")]
        sector_num: u64,

        /// Miner ID.
        #[arg(long, default_value = "1000")]
        miner_id: u64,

        /// Partition index (for WindowPoSt).
        #[arg(long, default_value = "0")]
        partition: u32,
    },

    /// Run N identical proofs and report throughput statistics.
    Batch {
        /// Proof type: porep, snap, wpost, winning.
        #[arg(short = 't', long = "type")]
        proof_type: String,

        /// Path to C1 output JSON (for PoRep) or vanilla proof file.
        #[arg(long)]
        c1: Option<PathBuf>,

        /// Path to vanilla proof file (for PoSt/SnapDeals).
        #[arg(long)]
        vanilla: Option<PathBuf>,

        /// Number of proofs to run.
        #[arg(short, long, default_value = "3")]
        count: u32,

        /// Number of concurrent proof requests (pipelining).
        #[arg(short = 'j', long, default_value = "1")]
        concurrency: u32,

        /// Sector number.
        #[arg(long, default_value = "1")]
        sector_num: u64,

        /// Miner ID.
        #[arg(long, default_value = "1000")]
        miner_id: u64,
    },

    /// Query daemon status.
    Status,

    /// Pre-warm SRS parameters.
    Preload {
        /// Circuit ID to preload (e.g. "porep-32g", "wpost-32g").
        #[arg(short, long)]
        circuit_id: String,
    },

    /// Get Prometheus metrics from the daemon.
    Metrics,
}

fn proof_kind_from_str(s: &str) -> Result<i32> {
    match s.to_lowercase().as_str() {
        "porep" | "porep-c2" | "seal-commit" => Ok(pb::ProofKind::PorepSealCommit as i32),
        "snap" | "snap-deals" | "update" => Ok(pb::ProofKind::SnapDealsUpdate as i32),
        "wpost" | "window-post" | "windowpost" => Ok(pb::ProofKind::WindowPostPartition as i32),
        "winning" | "winning-post" | "winningpost" => Ok(pb::ProofKind::WinningPost as i32),
        _ => anyhow::bail!("unknown proof type: {}. Use: porep, snap, wpost, winning", s),
    }
}

/// Create a TCP gRPC client.
async fn make_tcp_client(addr: &str) -> Result<pb::proving_engine_client::ProvingEngineClient<tonic::transport::Channel>> {
    let channel = tonic::transport::Endpoint::from_shared(addr.to_string())
        .with_context(|| format!("invalid daemon address: {}", addr))?
        .connect()
        .await
        .with_context(|| format!("failed to connect to daemon at {}", addr))?;
    Ok(
        pb::proving_engine_client::ProvingEngineClient::new(channel)
            .max_decoding_message_size(128 * 1024 * 1024)
            .max_encoding_message_size(128 * 1024 * 1024),
    )
}

/// Create a UDS gRPC client.
async fn make_uds_client(socket_path: &str) -> Result<pb::proving_engine_client::ProvingEngineClient<tonic::transport::Channel>> {
    let path = socket_path.to_string();
    let channel = tonic::transport::Endpoint::try_from("http://[::]:50051")
        .context("failed to create endpoint")?
        .connect_with_connector(tower::service_fn(move |_| {
            let path = path.clone();
            async move {
                tokio::net::UnixStream::connect(path)
                    .await
                    .map(|s| hyper_util::rt::TokioIo::new(s))
            }
        }))
        .await
        .context("failed to connect to daemon via unix socket")?;
    Ok(
        pb::proving_engine_client::ProvingEngineClient::new(channel)
            .max_decoding_message_size(128 * 1024 * 1024)
            .max_encoding_message_size(128 * 1024 * 1024),
    )
}

/// Connect to the daemon, handling both TCP and UDS.
async fn connect(addr: &str) -> Result<pb::proving_engine_client::ProvingEngineClient<tonic::transport::Channel>> {
    if let Some(socket_path) = addr.strip_prefix("unix://") {
        make_uds_client(socket_path).await
    } else {
        make_tcp_client(addr).await
    }
}

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();

    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new(&cli.log_level)),
        )
        .init();

    match cli.command {
        Commands::Single {
            proof_type,
            c1,
            vanilla,
            sector_num,
            miner_id,
            partition,
        } => {
            let proof_kind = proof_kind_from_str(&proof_type)?;

            let vanilla_bytes = load_proof_input(&c1, &vanilla)?;

            info!(
                proof_type = %proof_type,
                input_size = vanilla_bytes.len(),
                "submitting proof"
            );

            let request_id = uuid::Uuid::new_v4().to_string();
            let start = Instant::now();

            let mut client = connect(&cli.addr).await?;
            let resp = do_prove(
                &mut client,
                request_id,
                proof_kind,
                vanilla_bytes,
                sector_num,
                miner_id,
                partition,
            )
            .await?;

            print_result(&resp, start.elapsed());
        }

        Commands::Batch {
            proof_type,
            c1,
            vanilla,
            count,
            concurrency,
            sector_num,
            miner_id,
        } => {
            let proof_kind = proof_kind_from_str(&proof_type)?;
            let vanilla_bytes = load_proof_input(&c1, &vanilla)?;

            println!("=== Batch Benchmark ===");
            println!("proof type:  {}", proof_type);
            println!("count:       {}", count);
            println!("concurrency: {}", concurrency);
            println!("input size:  {} bytes", vanilla_bytes.len());
            println!();

            let batch_start = Instant::now();
            let mut results: Vec<(u32, pb::AwaitProofResponse, std::time::Duration)> = Vec::new();

            if concurrency <= 1 {
                // Sequential mode
                for i in 0..count {
                    let request_id = uuid::Uuid::new_v4().to_string();
                    let start = Instant::now();
                    let mut client = connect(&cli.addr).await?;
                    let resp = do_prove(
                        &mut client,
                        request_id,
                        proof_kind,
                        vanilla_bytes.clone(),
                        sector_num,
                        miner_id,
                        0,
                    )
                    .await?;
                    let elapsed = start.elapsed();

                    let status_str = status_label(resp.status);
                    println!(
                        "  [{}/{}] {} — {:.1}s (prove={} ms, queue={} ms)",
                        i + 1,
                        count,
                        status_str,
                        elapsed.as_secs_f64(),
                        resp.gpu_compute_ms,
                        resp.queue_wait_ms,
                    );
                    results.push((i, resp, elapsed));
                }
            } else {
                // Concurrent mode — submit `concurrency` proofs at a time
                use tokio::sync::Semaphore;
                let sem = Arc::new(Semaphore::new(concurrency as usize));
                let addr = cli.addr.clone();
                let mut handles = Vec::new();

                for i in 0..count {
                    let permit = sem.clone().acquire_owned().await.unwrap();
                    let addr = addr.clone();
                    let vanilla = vanilla_bytes.clone();
                    handles.push(tokio::spawn(async move {
                        let request_id = uuid::Uuid::new_v4().to_string();
                        let start = Instant::now();
                        let mut client = connect(&addr).await?;
                        let resp = do_prove(
                            &mut client,
                            request_id,
                            proof_kind,
                            vanilla,
                            sector_num,
                            miner_id,
                            0,
                        )
                        .await?;
                        let elapsed = start.elapsed();
                        drop(permit);
                        Ok::<_, anyhow::Error>((i, resp, elapsed))
                    }));
                }

                for handle in handles {
                    let (i, resp, elapsed) = handle.await??;
                    let status_str = status_label(resp.status);
                    println!(
                        "  [{}/{}] {} — {:.1}s (prove={} ms, queue={} ms)",
                        i + 1,
                        count,
                        status_str,
                        elapsed.as_secs_f64(),
                        resp.gpu_compute_ms,
                        resp.queue_wait_ms,
                    );
                    results.push((i, resp, elapsed));
                }
            }

            let batch_elapsed = batch_start.elapsed();
            let completed: Vec<_> = results.iter().filter(|(_, r, _)| r.status == 1).collect();
            let failed = results.len() - completed.len();

            println!();
            println!("=== Batch Summary ===");
            println!("total time:    {:.1}s", batch_elapsed.as_secs_f64());
            println!("completed:     {}", completed.len());
            println!("failed:        {}", failed);

            if !completed.is_empty() {
                let wall_times: Vec<f64> = completed.iter().map(|(_, _, d)| d.as_secs_f64()).collect();
                let prove_times: Vec<f64> = completed.iter().map(|(_, r, _)| r.gpu_compute_ms as f64 / 1000.0).collect();

                let wall_avg = wall_times.iter().sum::<f64>() / wall_times.len() as f64;
                let wall_min = wall_times.iter().cloned().fold(f64::INFINITY, f64::min);
                let wall_max = wall_times.iter().cloned().fold(f64::NEG_INFINITY, f64::max);

                let prove_avg = prove_times.iter().sum::<f64>() / prove_times.len() as f64;
                let prove_min = prove_times.iter().cloned().fold(f64::INFINITY, f64::min);
                let prove_max = prove_times.iter().cloned().fold(f64::NEG_INFINITY, f64::max);

                println!("wall time:     avg={:.1}s min={:.1}s max={:.1}s", wall_avg, wall_min, wall_max);
                println!("prove time:    avg={:.1}s min={:.1}s max={:.1}s", prove_avg, prove_min, prove_max);
                println!(
                    "throughput:    {:.3} proofs/min ({:.1}s/proof)",
                    completed.len() as f64 / batch_elapsed.as_secs_f64() * 60.0,
                    batch_elapsed.as_secs_f64() / completed.len() as f64,
                );
            }
        }

        Commands::Status => {
            let mut client = connect(&cli.addr).await?;

            let resp = client
                .get_status(pb::GetStatusRequest {})
                .await
                .context("GetStatus RPC failed")?
                .into_inner();

            println!("=== cuzk daemon status ===");
            println!("uptime:           {}s", resp.uptime_seconds);
            println!("proofs completed: {}", resp.total_proofs_completed);
            println!("proofs failed:    {}", resp.total_proofs_failed);
            println!("pinned memory:    {} / {} bytes", resp.pinned_memory_bytes, resp.pinned_memory_limit_bytes);

            if !resp.gpus.is_empty() {
                println!("\nGPUs:");
                for gpu in &resp.gpus {
                    let vram_total_mib = gpu.vram_total_bytes / (1024 * 1024);
                    let vram_free_mib = gpu.vram_free_bytes / (1024 * 1024);
                    let job_info = if gpu.current_job_id.is_empty() {
                        "idle".to_string()
                    } else {
                        format!("proving {} ({})", gpu.current_proof_kind, &gpu.current_job_id[..8.min(gpu.current_job_id.len())])
                    };
                    println!(
                        "  [{}] {} — {} MiB / {} MiB VRAM — {}",
                        gpu.ordinal, gpu.name, vram_free_mib, vram_total_mib, job_info
                    );
                }
            }

            if !resp.loaded_srs.is_empty() {
                println!("\nLoaded SRS:");
                for srs in &resp.loaded_srs {
                    println!("  {} (tier={}, size={} bytes, refs={})",
                        srs.circuit_id, srs.tier, srs.size_bytes, srs.ref_count);
                }
            }

            if !resp.queues.is_empty() {
                println!("\nQueues:");
                for q in &resp.queues {
                    println!("  {}: pending={}, in_progress={}", q.proof_kind, q.pending, q.in_progress);
                }
            }
        }

        Commands::Preload { circuit_id } => {
            let mut client = connect(&cli.addr).await?;

            let resp = client
                .preload_srs(pb::PreloadSrsRequest {
                    circuit_id: circuit_id.clone(),
                })
                .await
                .context("PreloadSRS RPC failed")?
                .into_inner();

            if resp.already_loaded {
                println!("{} already loaded", circuit_id);
            } else {
                println!("{} loaded in {} ms", circuit_id, resp.load_time_ms);
            }
        }

        Commands::Metrics => {
            let mut client = connect(&cli.addr).await?;

            let resp = client
                .get_metrics(pb::GetMetricsRequest {})
                .await
                .context("GetMetrics RPC failed")?
                .into_inner();

            print!("{}", resp.prometheus_text);
        }
    }

    Ok(())
}

use std::sync::Arc;

/// Load proof input from --c1 or --vanilla path.
fn load_proof_input(c1: &Option<PathBuf>, vanilla: &Option<PathBuf>) -> Result<Vec<u8>> {
    if let Some(path) = c1.as_ref().or(vanilla.as_ref()) {
        info!(path = %path.display(), "loading proof input");
        std::fs::read(path)
            .with_context(|| format!("failed to read {}", path.display()))
    } else {
        anyhow::bail!("must specify --c1 (for PoRep) or --vanilla (for PoSt/SnapDeals)");
    }
}

async fn do_prove(
    client: &mut pb::proving_engine_client::ProvingEngineClient<tonic::transport::Channel>,
    request_id: String,
    proof_kind: i32,
    vanilla_proof: Vec<u8>,
    sector_number: u64,
    miner_id: u64,
    partition_index: u32,
) -> Result<pb::AwaitProofResponse> {
    let resp = client
        .prove(pb::ProveRequest {
            submit: Some(pb::SubmitProofRequest {
                request_id,
                proof_kind,
                sector_size: 34359738368, // 32 GiB
                registered_proof: 0,
                priority: 0, // use default
                vanilla_proof,
                sector_number,
                miner_id,
                randomness: vec![],
                partition_index,
                sector_key_cid: vec![],
                new_sealed_cid: vec![],
                new_unsealed_cid: vec![],
            }),
        })
        .await
        .context("Prove RPC failed")?
        .into_inner();

    resp.result.ok_or_else(|| anyhow::anyhow!("empty response"))
}

fn status_label(status: i32) -> &'static str {
    match status {
        1 => "COMPLETED",
        2 => "FAILED",
        3 => "CANCELLED",
        4 => "TIMEOUT",
        _ => "UNKNOWN",
    }
}

fn print_result(resp: &pb::AwaitProofResponse, wall_time: std::time::Duration) {
    let status_str = status_label(resp.status);

    println!("\n=== Proof Result ===");
    println!("status:    {}", status_str);
    println!("job_id:    {}", resp.job_id);

    if resp.status == 1 {
        println!(
            "timings:   total={} ms (queue={} ms, srs={} ms, synth={} ms, gpu={} ms)",
            resp.total_ms,
            resp.queue_wait_ms,
            resp.srs_load_ms,
            resp.synthesis_ms,
            resp.gpu_compute_ms,
        );
        println!("wall time: {} ms", wall_time.as_millis());
        println!(
            "proof:     {} bytes (hex: {})",
            resp.proof.len(),
            if resp.proof.len() <= 256 {
                hex::encode(&resp.proof)
            } else {
                format!("{}...", hex::encode(&resp.proof[..128]))
            }
        );
    } else {
        println!("error:     {}", resp.error_message);
    }
}
