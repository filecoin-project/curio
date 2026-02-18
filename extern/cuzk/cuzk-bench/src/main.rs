//! cuzk-bench: Testing and benchmarking utility for the cuzk proving daemon.
//!
//! Commands:
//!   single      — Run a single proof through the daemon
//!   batch       — Run N identical proofs and report throughput
//!   status      — Query daemon status
//!   preload     — Pre-warm SRS parameters
//!   metrics     — Get Prometheus metrics
//!   gen-vanilla — Generate vanilla proof test data (requires `gen-vanilla` feature)

use anyhow::{Context, Result};
use clap::{Parser, Subcommand};
use std::path::PathBuf;
use std::time::Instant;
use tracing::info;

use cuzk_proto::cuzk::v1 as pb;

#[cfg(feature = "gen-vanilla")]
mod gen_vanilla;

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

        /// Path to C1 output JSON (for PoRep).
        #[arg(long)]
        c1: Option<PathBuf>,

        /// Path to vanilla proof JSON file (for PoSt/SnapDeals).
        /// PoSt: JSON array of base64-encoded proofs, or single base64 proof.
        /// SnapDeals: JSON array of base64-encoded partition proofs.
        #[arg(long)]
        vanilla: Option<PathBuf>,

        /// Registered proof type (numeric, matches Go abi enum values).
        /// Winning 32G=3, Window 32G V1.1=13, Update 32G=3.
        #[arg(long, default_value = "0")]
        registered_proof: u64,

        /// Sector number.
        #[arg(long, default_value = "1")]
        sector_num: u64,

        /// Miner ID.
        #[arg(long, default_value = "1000")]
        miner_id: u64,

        /// Partition index (for WindowPoSt).
        #[arg(long, default_value = "0")]
        partition: u32,

        /// Hex-encoded 32-byte randomness (for PoSt).
        #[arg(long)]
        randomness: Option<String>,

        /// Hex-encoded 32-byte comm_r_old (for SnapDeals).
        #[arg(long)]
        comm_r_old: Option<String>,

        /// Hex-encoded 32-byte comm_r_new (for SnapDeals).
        #[arg(long)]
        comm_r_new: Option<String>,

        /// Hex-encoded 32-byte comm_d_new (for SnapDeals).
        #[arg(long)]
        comm_d_new: Option<String>,
    },

    /// Run N identical proofs and report throughput statistics.
    Batch {
        /// Proof type: porep, snap, wpost, winning.
        #[arg(short = 't', long = "type")]
        proof_type: String,

        /// Path to C1 output JSON (for PoRep).
        #[arg(long)]
        c1: Option<PathBuf>,

        /// Path to vanilla proof JSON file (for PoSt/SnapDeals).
        #[arg(long)]
        vanilla: Option<PathBuf>,

        /// Registered proof type (numeric, matches Go abi enum values).
        #[arg(long, default_value = "0")]
        registered_proof: u64,

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

        /// Hex-encoded 32-byte randomness (for PoSt).
        #[arg(long)]
        randomness: Option<String>,

        /// Hex-encoded 32-byte comm_r_old (for SnapDeals).
        #[arg(long)]
        comm_r_old: Option<String>,

        /// Hex-encoded 32-byte comm_r_new (for SnapDeals).
        #[arg(long)]
        comm_r_new: Option<String>,

        /// Hex-encoded 32-byte comm_d_new (for SnapDeals).
        #[arg(long)]
        comm_d_new: Option<String>,
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

    /// Generate vanilla proof test data for PoSt/SnapDeals (requires `gen-vanilla` feature).
    ///
    /// Calls filecoin-proofs-api CPU-only functions to produce vanilla proofs
    /// from sealed sector data on disk. Output is a JSON array of base64-encoded
    /// proof bytes, suitable for use with the `--vanilla` flag of single/batch.
    #[command(subcommand)]
    GenVanilla(GenVanillaCommands),

    /// PCE (Pre-Compiled Constraint Evaluator) extraction and benchmark.
    ///
    /// Extracts the R1CS constraint matrices from a circuit into CSR format,
    /// then benchmarks the WitnessCS + MatVec pipeline against traditional
    /// synthesis. Requires 'pce-bench' feature.
    PceBench {
        /// Path to C1 output JSON.
        #[arg(long)]
        c1: PathBuf,

        /// Sector number.
        #[arg(long, default_value = "1")]
        sector_num: u64,

        /// Miner ID.
        #[arg(long, default_value = "1000")]
        miner_id: u64,

        /// Save extracted PCE to this path (bincode format).
        #[arg(long)]
        save_pce: Option<PathBuf>,

        /// Validate PCE correctness: run both old and new paths and compare a/b/c.
        #[arg(long, default_value = "true")]
        validate: bool,
    },

    /// PCE pipeline memory + performance benchmark (requires 'pce-bench' feature).
    ///
    /// Runs N sequential proofs through the PCE path, logging RSS at each stage.
    /// First proof triggers PCE extraction; subsequent proofs reuse the cache.
    /// Each proof's results are dropped before starting the next, demonstrating
    /// production-like memory behavior. Designed to be run alongside cuzk-memmon.sh.
    PcePipeline {
        /// Path to C1 output JSON.
        #[arg(long)]
        c1: PathBuf,

        /// Sector number.
        #[arg(long, default_value = "1")]
        sector_num: u64,

        /// Miner ID.
        #[arg(long, default_value = "1000")]
        miner_id: u64,

        /// Number of proofs to run.
        #[arg(short, long, default_value = "3")]
        num_proofs: u32,

        /// Run N syntheses concurrently (simulates N GPU pipelines needing
        /// synthesis results simultaneously). Shows peak memory under heavy
        /// pipelining. Default 0 = sequential.
        #[arg(short = 'j', long, default_value = "0")]
        parallel: u32,

        /// Also run the old-path synthesis for comparison (first iteration only).
        /// Old-path results are dropped before PCE path runs.
        #[arg(long)]
        compare_old: bool,
    },

    /// Pipelined partition pipeline benchmark (requires 'pce-bench' feature).
    ///
    /// Runs the pipelined partition proving (Phase 6) which overlaps parallel
    /// partition synthesis with per-partition GPU proving. Tests various
    /// max_concurrent values vs batch-all (slot_size=10).
    ///
    /// Each partition is synthesized independently (num_circuits=1) and
    /// all partitions run in parallel, bounded by max_concurrent.
    /// GPU takes ~3s/partition (fast b_g2_msm), synth takes ~29s/partition.
    SlottedBench {
        /// Path to C1 output JSON.
        #[arg(long)]
        c1: PathBuf,

        /// Sector number.
        #[arg(long, default_value = "1")]
        sector_num: u64,

        /// Miner ID.
        #[arg(long, default_value = "1000")]
        miner_id: u64,

        /// Max concurrent values to benchmark (comma-separated).
        /// Use 10 for batch-all baseline. Default: 1,2,3,10.
        #[arg(long, default_value = "1,2,3,10")]
        slot_sizes: String,

        /// Number of proofs per configuration.
        #[arg(short, long, default_value = "1")]
        num_proofs: u32,
    },

    /// In-process synthesis microbenchmark (requires `synth-bench` feature).
    ///
    /// Runs only the CPU synthesis step (circuit construction + R1CS witness
    /// generation) without daemon, GPU, or SRS. Useful for A/B testing
    /// bellpepper-core / bellperson changes.
    SynthOnly {
        /// Path to C1 output JSON.
        #[arg(long)]
        c1: PathBuf,

        /// Sector number.
        #[arg(long, default_value = "1")]
        sector_num: u64,

        /// Miner ID.
        #[arg(long, default_value = "1000")]
        miner_id: u64,

        /// Number of iterations (synthesis runs). Each run drops the result
        /// before starting the next, so only one partition set is live at a time.
        #[arg(short, long, default_value = "1")]
        iterations: u32,

        /// Synthesize only a single partition (0-9) instead of all 10.
        #[arg(long)]
        partition: Option<usize>,
    },
}

#[derive(Subcommand, Debug)]
enum GenVanillaCommands {
    /// Generate WinningPoSt vanilla proofs.
    ///
    /// Determines which sectors are challenged, generates Merkle inclusion
    /// proofs from the sealed sector's tree-r-last. Typically produces 1 proof
    /// for our single-sector test setup.
    WinningPost {
        /// Registered proof type (numeric). Winning 32G=3.
        #[arg(long, default_value = "3")]
        registered_proof: u64,

        /// Sector number.
        #[arg(long, default_value = "1")]
        sector_number: u64,

        /// Miner ID.
        #[arg(long, default_value = "1000")]
        miner_id: u64,

        /// Hex-encoded 32-byte randomness. If omitted, uses all zeros.
        #[arg(long)]
        randomness: Option<String>,

        /// CommR as a Filecoin CID string (bagboea4b5abc...) or path to commdr.txt.
        #[arg(long)]
        comm_r: String,

        /// Path to the sealed sector file.
        #[arg(long)]
        sealed: PathBuf,

        /// Path to the sealing cache directory (contains tree-r-last-*, p_aux, t_aux).
        #[arg(long)]
        cache: PathBuf,

        /// Output file path for the vanilla proofs JSON.
        #[arg(short, long, default_value = "winning-vanilla.json")]
        output: PathBuf,
    },

    /// Generate WindowPoSt vanilla proofs.
    ///
    /// WindowPoSt challenges all sectors. For our single-sector test setup,
    /// this generates challenges and vanilla proofs for the one sector.
    WindowPost {
        /// Registered proof type (numeric). Window 32G V1.1=13 (maps to V1_2).
        #[arg(long, default_value = "13")]
        registered_proof: u64,

        /// Sector number.
        #[arg(long, default_value = "1")]
        sector_number: u64,

        /// Miner ID.
        #[arg(long, default_value = "1000")]
        miner_id: u64,

        /// Hex-encoded 32-byte randomness. If omitted, uses all zeros.
        #[arg(long)]
        randomness: Option<String>,

        /// CommR as a Filecoin CID string (bagboea4b5abc...) or path to commdr.txt.
        #[arg(long)]
        comm_r: String,

        /// Path to the sealed sector file.
        #[arg(long)]
        sealed: PathBuf,

        /// Path to the sealing cache directory (contains tree-r-last-*, p_aux, t_aux).
        #[arg(long)]
        cache: PathBuf,

        /// Output file path for the vanilla proofs JSON.
        #[arg(short, long, default_value = "wpost-vanilla.json")]
        output: PathBuf,
    },

    /// Generate SnapDeals (sector update) vanilla partition proofs.
    ///
    /// Reads both the original sealed sector and the updated replica to produce
    /// partition-level vanilla proofs for the empty sector update circuit.
    SnapProve {
        /// Registered update proof type (numeric). Update 32G=3.
        #[arg(long, default_value = "3")]
        registered_proof: u64,

        /// Path to commdr.txt for the ORIGINAL sealed sector.
        /// Format: "d:<CID> r:<CID>". Only the r: (CommR) is used as comm_r_old.
        #[arg(long)]
        orig_commdr: PathBuf,

        /// Path to update-commdr.txt for the UPDATED sector.
        /// Format: "d:<CID> r:<CID>". CommD → comm_d_new, CommR → comm_r_new.
        #[arg(long)]
        update_commdr: PathBuf,

        /// Path to the original sealed sector file (sector key).
        #[arg(long)]
        sector_key: PathBuf,

        /// Path to the original sealing cache directory.
        #[arg(long)]
        sector_key_cache: PathBuf,

        /// Path to the updated replica file.
        #[arg(long)]
        replica: PathBuf,

        /// Path to the updated replica cache directory.
        #[arg(long)]
        replica_cache: PathBuf,

        /// Output file path for the vanilla partition proofs JSON.
        #[arg(short, long, default_value = "snap-vanilla.json")]
        output: PathBuf,
    },
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
            registered_proof,
            sector_num,
            miner_id,
            partition,
            randomness,
            comm_r_old,
            comm_r_new,
            comm_d_new,
        } => {
            let proof_kind = proof_kind_from_str(&proof_type)?;
            let params = build_proof_params(
                proof_kind,
                &c1,
                &vanilla,
                registered_proof,
                sector_num,
                miner_id,
                partition,
                &randomness,
                &comm_r_old,
                &comm_r_new,
                &comm_d_new,
            )?;

            info!(
                proof_type = %proof_type,
                "submitting proof"
            );

            let request_id = uuid::Uuid::new_v4().to_string();
            let start = Instant::now();

            let mut client = connect(&cli.addr).await?;
            let resp = do_prove(&mut client, request_id, params).await?;

            print_result(&resp, start.elapsed());
        }

        Commands::Batch {
            proof_type,
            c1,
            vanilla,
            registered_proof,
            count,
            concurrency,
            sector_num,
            miner_id,
            randomness,
            comm_r_old,
            comm_r_new,
            comm_d_new,
        } => {
            let proof_kind = proof_kind_from_str(&proof_type)?;
            let params = build_proof_params(
                proof_kind,
                &c1,
                &vanilla,
                registered_proof,
                sector_num,
                miner_id,
                0,
                &randomness,
                &comm_r_old,
                &comm_r_new,
                &comm_d_new,
            )?;

            println!("=== Batch Benchmark ===");
            println!("proof type:  {}", proof_type);
            println!("count:       {}", count);
            println!("concurrency: {}", concurrency);
            println!();

            let batch_start = Instant::now();
            let mut results: Vec<(u32, pb::AwaitProofResponse, std::time::Duration)> = Vec::new();

            if concurrency <= 1 {
                // Sequential mode
                for i in 0..count {
                    let request_id = uuid::Uuid::new_v4().to_string();
                    let start = Instant::now();
                    let mut client = connect(&cli.addr).await?;
                    let resp = do_prove(&mut client, request_id, params.clone()).await?;
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
                    let params = params.clone();
                    handles.push(tokio::spawn(async move {
                        let request_id = uuid::Uuid::new_v4().to_string();
                        let start = Instant::now();
                        let mut client = connect(&addr).await?;
                        let resp = do_prove(&mut client, request_id, params).await?;
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

        Commands::GenVanilla(subcmd) => {
            run_gen_vanilla(subcmd)?;
        }

        Commands::PceBench { c1, sector_num, miner_id, save_pce, validate } => {
            run_pce_bench(c1, sector_num, miner_id, save_pce, validate)?;
        }

        Commands::PcePipeline { c1, sector_num, miner_id, num_proofs, parallel, compare_old } => {
            run_pce_pipeline(c1, sector_num, miner_id, num_proofs, parallel, compare_old)?;
        }

        Commands::SlottedBench { c1, sector_num, miner_id, slot_sizes, num_proofs } => {
            run_slotted_bench(c1, sector_num, miner_id, &slot_sizes, num_proofs)?;
        }

        Commands::SynthOnly { c1, sector_num, miner_id, iterations, partition } => {
            run_synth_only(c1, sector_num, miner_id, iterations, partition)?;
        }
    }

    Ok(())
}

/// Parse a hex string to 32 bytes, or return zeros if None.
#[cfg(feature = "gen-vanilla")]
fn parse_randomness_hex(hex_str: &Option<String>) -> Result<[u8; 32]> {
    match hex_str {
        Some(s) => {
            let bytes = hex::decode(s).context("invalid hex for randomness")?;
            if bytes.len() != 32 {
                anyhow::bail!("randomness must be exactly 32 bytes, got {}", bytes.len());
            }
            let mut arr = [0u8; 32];
            arr.copy_from_slice(&bytes);
            Ok(arr)
        }
        None => Ok([0u8; 32]),
    }
}

/// Resolve a CommR argument: either a CID string directly, or a path to a commdr.txt file.
/// If it looks like a file path (exists on disk), read it and extract CommR.
/// Otherwise, treat it as a CID string.
#[cfg(feature = "gen-vanilla")]
fn resolve_comm_r(comm_r_arg: &str) -> Result<[u8; 32]> {
    let path = std::path::Path::new(comm_r_arg);
    if path.exists() {
        let contents = std::fs::read_to_string(path)
            .with_context(|| format!("failed to read {}", path.display()))?;
        let (_comm_d, comm_r) = gen_vanilla::parse_commdr_file(&contents)?;
        Ok(comm_r)
    } else {
        gen_vanilla::parse_commitment_cid(comm_r_arg)
    }
}

#[cfg(feature = "gen-vanilla")]
fn run_gen_vanilla(cmd: GenVanillaCommands) -> Result<()> {
    // Set FIL_PROOFS_PARAMETER_CACHE if not already set (needed for proof type registration)
    if std::env::var("FIL_PROOFS_PARAMETER_CACHE").is_err() {
        // Default to the standard location
        if std::path::Path::new("/data/zk/params").exists() {
            std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", "/data/zk/params");
        }
    }

    match cmd {
        GenVanillaCommands::WinningPost {
            registered_proof,
            sector_number,
            miner_id,
            randomness,
            comm_r,
            sealed,
            cache,
            output,
        } => {
            let randomness = parse_randomness_hex(&randomness)?;
            let comm_r = resolve_comm_r(&comm_r)?;
            gen_vanilla::gen_winning_post_vanilla(
                registered_proof,
                sector_number,
                miner_id,
                randomness,
                comm_r,
                cache,
                sealed,
                output,
            )
        }

        GenVanillaCommands::WindowPost {
            registered_proof,
            sector_number,
            miner_id,
            randomness,
            comm_r,
            sealed,
            cache,
            output,
        } => {
            let randomness = parse_randomness_hex(&randomness)?;
            let comm_r = resolve_comm_r(&comm_r)?;
            gen_vanilla::gen_window_post_vanilla(
                registered_proof,
                sector_number,
                miner_id,
                randomness,
                comm_r,
                cache,
                sealed,
                output,
            )
        }

        GenVanillaCommands::SnapProve {
            registered_proof,
            orig_commdr,
            update_commdr,
            sector_key,
            sector_key_cache,
            replica,
            replica_cache,
            output,
        } => {
            let orig_contents = std::fs::read_to_string(&orig_commdr)
                .with_context(|| format!("failed to read {}", orig_commdr.display()))?;
            let (_orig_comm_d, comm_r_old) = gen_vanilla::parse_commdr_file(&orig_contents)?;

            let update_contents = std::fs::read_to_string(&update_commdr)
                .with_context(|| format!("failed to read {}", update_commdr.display()))?;
            let (comm_d_new, comm_r_new) = gen_vanilla::parse_commdr_file(&update_contents)?;

            gen_vanilla::gen_snap_vanilla(
                registered_proof,
                comm_r_old,
                comm_r_new,
                comm_d_new,
                sector_key,
                sector_key_cache,
                replica,
                replica_cache,
                output,
            )
        }
    }
}

#[cfg(not(feature = "gen-vanilla"))]
fn run_gen_vanilla(_cmd: GenVanillaCommands) -> Result<()> {
    anyhow::bail!(
        "gen-vanilla subcommand requires the 'gen-vanilla' feature.\n\
         Rebuild with: cargo build -p cuzk-bench --features gen-vanilla"
    );
}

use std::sync::Arc;

/// Proof parameters for submitting a proof request.
#[derive(Clone)]
struct ProofParams {
    proof_kind: i32,
    registered_proof: u64,
    vanilla_proof: Vec<u8>,
    vanilla_proofs: Vec<Vec<u8>>,
    sector_number: u64,
    miner_id: u64,
    partition_index: u32,
    randomness: Vec<u8>,
    comm_r_old: Vec<u8>,
    comm_r_new: Vec<u8>,
    comm_d_new: Vec<u8>,
}

/// Build proof parameters from CLI arguments.
#[allow(clippy::too_many_arguments)]
fn build_proof_params(
    proof_kind: i32,
    c1: &Option<PathBuf>,
    vanilla: &Option<PathBuf>,
    registered_proof: u64,
    sector_num: u64,
    miner_id: u64,
    partition_index: u32,
    randomness: &Option<String>,
    comm_r_old: &Option<String>,
    comm_r_new: &Option<String>,
    comm_d_new: &Option<String>,
) -> Result<ProofParams> {
    let is_porep = proof_kind == pb::ProofKind::PorepSealCommit as i32;

    let (vanilla_proof, vanilla_proofs) = if is_porep {
        // PoRep: single monolithic C1 output
        let path = c1.as_ref()
            .ok_or_else(|| anyhow::anyhow!("--c1 required for PoRep proof type"))?;
        info!(path = %path.display(), "loading C1 output");
        let data = std::fs::read(path)
            .with_context(|| format!("failed to read {}", path.display()))?;
        (data, vec![])
    } else {
        // PoSt / SnapDeals: load vanilla proofs JSON file.
        // Expected format: JSON array of base64-encoded proof bytes,
        // e.g. ["base64data1", "base64data2", ...]
        // Or for raw binary: a single file is treated as one proof.
        let path = vanilla.as_ref()
            .ok_or_else(|| anyhow::anyhow!("--vanilla required for PoSt/SnapDeals proof types"))?;
        info!(path = %path.display(), "loading vanilla proofs");
        let data = std::fs::read(path)
            .with_context(|| format!("failed to read {}", path.display()))?;

        // Try to parse as JSON array of base64 strings (Go's json.Marshal([][]byte))
        let proofs: Vec<Vec<u8>> = match serde_json::from_slice::<Vec<String>>(&data) {
            Ok(b64_strings) => {
                use base64::Engine as _;
                b64_strings.iter().map(|s| {
                    base64::engine::general_purpose::STANDARD.decode(s)
                        .with_context(|| "failed to decode base64 vanilla proof entry")
                }).collect::<Result<Vec<_>>>()?
            }
            Err(_) => {
                // Fall back: treat the entire file as a single raw proof
                info!("vanilla file is not JSON array, treating as single raw proof");
                vec![data]
            }
        };

        info!(num_proofs = proofs.len(), "loaded vanilla proofs");
        (vec![], proofs)
    };

    let randomness = match randomness {
        Some(hex_str) => hex::decode(hex_str).context("invalid hex for --randomness")?,
        None => vec![0u8; 32], // default: zero randomness for testing
    };

    let comm_r_old = match comm_r_old {
        Some(hex_str) => hex::decode(hex_str).context("invalid hex for --comm-r-old")?,
        None => vec![],
    };
    let comm_r_new = match comm_r_new {
        Some(hex_str) => hex::decode(hex_str).context("invalid hex for --comm-r-new")?,
        None => vec![],
    };
    let comm_d_new = match comm_d_new {
        Some(hex_str) => hex::decode(hex_str).context("invalid hex for --comm-d-new")?,
        None => vec![],
    };

    Ok(ProofParams {
        proof_kind,
        registered_proof,
        vanilla_proof,
        vanilla_proofs,
        sector_number: sector_num,
        miner_id,
        partition_index,
        randomness,
        comm_r_old,
        comm_r_new,
        comm_d_new,
    })
}

async fn do_prove(
    client: &mut pb::proving_engine_client::ProvingEngineClient<tonic::transport::Channel>,
    request_id: String,
    params: ProofParams,
) -> Result<pb::AwaitProofResponse> {
    let resp = client
        .prove(pb::ProveRequest {
            submit: Some(pb::SubmitProofRequest {
                request_id,
                proof_kind: params.proof_kind,
                sector_size: 34359738368, // 32 GiB
                registered_proof: params.registered_proof,
                priority: 0, // use default
                vanilla_proof: params.vanilla_proof,
                vanilla_proofs: params.vanilla_proofs,
                sector_number: params.sector_number,
                miner_id: params.miner_id,
                randomness: params.randomness,
                partition_index: params.partition_index,
                comm_r_old: params.comm_r_old,
                comm_r_new: params.comm_r_new,
                comm_d_new: params.comm_d_new,
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

#[cfg(feature = "synth-bench")]
fn run_synth_only(
    c1: PathBuf,
    sector_num: u64,
    miner_id: u64,
    iterations: u32,
    partition: Option<usize>,
) -> Result<()> {
    use std::time::Instant;

    // Set FIL_PROOFS_PARAMETER_CACHE for proof type registration
    if std::env::var("FIL_PROOFS_PARAMETER_CACHE").is_err() {
        if std::path::Path::new("/data/zk/params").exists() {
            unsafe { std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", "/data/zk/params") };
        }
    }

    println!("=== Synthesis Microbenchmark ===");
    println!("c1:         {}", c1.display());
    println!("sector:     {} (miner {})", sector_num, miner_id);
    println!("partition:  {}", partition.map_or("all".to_string(), |p| p.to_string()));
    println!("iterations: {}", iterations);
    println!();

    let c1_data = std::fs::read(&c1)
        .with_context(|| format!("failed to read {}", c1.display()))?;
    println!("c1 loaded:  {} bytes", c1_data.len());

    let mut times = Vec::with_capacity(iterations as usize);

    for i in 0..iterations {
        let wall_start = Instant::now();

        let synth = if let Some(part_idx) = partition {
            cuzk_core::pipeline::synthesize_porep_c2_partition(
                &c1_data, sector_num, miner_id, part_idx,
                &format!("synth-bench-{}", i),
            )?
        } else {
            cuzk_core::pipeline::synthesize_porep_c2_batch(
                &c1_data, sector_num, miner_id,
                &format!("synth-bench-{}", i),
            )?
        };

        let wall_time = wall_start.elapsed();
        let synth_time = synth.synthesis_duration;
        let num_circuits = synth.provers.len();
        let num_constraints = if !synth.provers.is_empty() { synth.provers[0].a.len() } else { 0 };

        // Drop the heavy data before next iteration
        drop(synth);

        println!(
            "  [{}] wall={:.1}s  synth={:.1}s  circuits={}  constraints={}",
            i + 1,
            wall_time.as_secs_f64(),
            synth_time.as_secs_f64(),
            num_circuits,
            num_constraints,
        );
        times.push((wall_time, synth_time));
    }

    if iterations > 1 {
        let wall_avg = times.iter().map(|(w, _)| w.as_secs_f64()).sum::<f64>() / times.len() as f64;
        let synth_avg = times.iter().map(|(_, s)| s.as_secs_f64()).sum::<f64>() / times.len() as f64;
        let wall_min = times.iter().map(|(w, _)| w.as_secs_f64()).fold(f64::INFINITY, f64::min);
        let synth_min = times.iter().map(|(_, s)| s.as_secs_f64()).fold(f64::INFINITY, f64::min);
        println!();
        println!("=== Summary ({} iterations) ===", iterations);
        println!("wall:   avg={:.1}s  min={:.1}s", wall_avg, wall_min);
        println!("synth:  avg={:.1}s  min={:.1}s", synth_avg, synth_min);
    }

    Ok(())
}

#[cfg(feature = "pce-bench")]
fn run_pce_bench(
    c1: PathBuf,
    sector_num: u64,
    miner_id: u64,
    save_pce: Option<PathBuf>,
    validate: bool,
) -> Result<()> {
    use std::time::Instant;

    // Set FIL_PROOFS_PARAMETER_CACHE for proof type registration
    if std::env::var("FIL_PROOFS_PARAMETER_CACHE").is_err() {
        if std::path::Path::new("/data/zk/params").exists() {
            unsafe { std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", "/data/zk/params") };
        }
    }

    println!("=== PCE Benchmark ===");
    println!("c1:       {}", c1.display());
    println!("sector:   {} (miner {})", sector_num, miner_id);
    println!();

    let c1_data = std::fs::read(&c1)
        .with_context(|| format!("failed to read {}", c1.display()))?;
    println!("c1 loaded: {} bytes", c1_data.len());

    // Step 1: Run traditional synthesis to get baseline timing and a/b/c vectors
    println!("\n--- Step 1: Baseline synthesis (old path) ---");
    let baseline_start = Instant::now();
    let baseline_synth = cuzk_core::pipeline::synthesize_porep_c2_batch(
        &c1_data, sector_num, miner_id, "pce-baseline",
    )?;
    let _baseline_time = baseline_start.elapsed();
    let num_circuits = baseline_synth.provers.len();
    let num_constraints = if num_circuits > 0 { baseline_synth.provers[0].a.len() } else { 0 };
    println!(
        "  synth={:.1}s  circuits={}  constraints={}",
        baseline_synth.synthesis_duration.as_secs_f64(),
        num_circuits,
        num_constraints,
    );

    // Step 2: Extract PCE from a single circuit
    println!("\n--- Step 2: PCE extraction (RecordingCS) ---");
    let extract_start = Instant::now();
    // Build one circuit with the same vanilla proof data for extraction
    // We use extract_and_cache_pce which will cache it in the global OnceLock
    cuzk_core::pipeline::extract_and_cache_pce_from_c1(&c1_data, sector_num, miner_id)?;
    let extract_time = extract_start.elapsed();
    println!("  extract={:.1}s", extract_time.as_secs_f64());

    // Step 3: Run PCE synthesis path
    println!("\n--- Step 3: PCE synthesis (WitnessCS + MatVec) ---");
    let pce_start = Instant::now();
    let pce_synth = cuzk_core::pipeline::synthesize_porep_c2_batch(
        &c1_data, sector_num, miner_id, "pce-fast",
    )?;
    let _pce_time = pce_start.elapsed();
    println!(
        "  synth={:.1}s  circuits={}  constraints={}",
        pce_synth.synthesis_duration.as_secs_f64(),
        pce_synth.provers.len(),
        if !pce_synth.provers.is_empty() { pce_synth.provers[0].a.len() } else { 0 },
    );

    // Step 4: Compare timings
    println!("\n--- Results ---");
    let speedup = baseline_synth.synthesis_duration.as_secs_f64()
        / pce_synth.synthesis_duration.as_secs_f64();
    println!(
        "baseline synth: {:.1}s",
        baseline_synth.synthesis_duration.as_secs_f64(),
    );
    println!(
        "PCE synth:      {:.1}s",
        pce_synth.synthesis_duration.as_secs_f64(),
    );
    println!("speedup:        {:.2}x", speedup);
    println!(
        "PCE extract:    {:.1}s (one-time cost)",
        extract_time.as_secs_f64(),
    );

    // Step 5: Validate correctness (compare a/b/c vectors)
    if validate && !baseline_synth.provers.is_empty() && !pce_synth.provers.is_empty() {
        println!("\n--- Correctness Validation ---");
        let mut all_match = true;
        let check_circuits = baseline_synth.provers.len().min(pce_synth.provers.len());

        for c in 0..check_circuits {
            let ba = &baseline_synth.provers[c].a;
            let pa = &pce_synth.provers[c].a;
            let bb = &baseline_synth.provers[c].b;
            let pb_b = &pce_synth.provers[c].b;
            let bc = &baseline_synth.provers[c].c;
            let pc = &pce_synth.provers[c].c;

            if ba.len() != pa.len() || bb.len() != pb_b.len() || bc.len() != pc.len() {
                println!("  circuit {}: LENGTH MISMATCH (a: {} vs {}, b: {} vs {}, c: {} vs {})",
                    c, ba.len(), pa.len(), bb.len(), pb_b.len(), bc.len(), pc.len());
                all_match = false;
                continue;
            }

            let mut a_mismatches = 0usize;
            let mut b_mismatches = 0usize;
            let mut c_mismatches = 0usize;
            let mut first_a_mismatch = None;

            for i in 0..ba.len() {
                if ba[i] != pa[i] {
                    a_mismatches += 1;
                    if first_a_mismatch.is_none() {
                        first_a_mismatch = Some(i);
                    }
                }
                if bb[i] != pb_b[i] {
                    b_mismatches += 1;
                }
                if bc[i] != pc[i] {
                    c_mismatches += 1;
                }
            }

            if a_mismatches == 0 && b_mismatches == 0 && c_mismatches == 0 {
                println!("  circuit {}: PASS (all {} constraints match)", c, ba.len());
            } else {
                println!(
                    "  circuit {}: FAIL (a: {} mismatches, b: {} mismatches, c: {} mismatches, first_a_at: {:?})",
                    c, a_mismatches, b_mismatches, c_mismatches, first_a_mismatch
                );
                all_match = false;
            }
        }

        if all_match {
            println!("  ALL CIRCUITS MATCH — PCE is correct!");
        } else {
            println!("  VALIDATION FAILED — PCE output differs from baseline");
        }
    }

    // Optionally save PCE to disk
    if let Some(path) = save_pce {
        println!("\n--- Saving PCE to disk ---");
        let pce_ref = cuzk_core::pipeline::get_pce(&cuzk_core::srs_manager::CircuitId::Porep32G)
            .expect("PCE should be cached after extraction");
        let save_start = Instant::now();
        cuzk_pce::save_to_disk(pce_ref, &path)?;
        let save_time = save_start.elapsed();
        let file_size = std::fs::metadata(&path).map(|m| m.len()).unwrap_or(0);
        println!(
            "  saved to: {}  size={:.1} GiB  time={:.1}s  speed={:.1} GB/s",
            path.display(),
            file_size as f64 / (1024.0 * 1024.0 * 1024.0),
            save_time.as_secs_f64(),
            file_size as f64 / save_time.as_secs_f64() / 1e9,
        );

        // Test round-trip: load it back and verify dimensions
        println!("\n--- Verifying round-trip load ---");
        let load_start = Instant::now();
        let loaded: cuzk_pce::PreCompiledCircuit<blstrs::Scalar> =
            cuzk_pce::load_from_disk(&path)?;
        let load_time = load_start.elapsed();
        println!(
            "  loaded in {:.1}s  speed={:.1} GB/s",
            load_time.as_secs_f64(),
            file_size as f64 / load_time.as_secs_f64() / 1e9,
        );
        assert_eq!(loaded.num_inputs, pce_ref.num_inputs);
        assert_eq!(loaded.num_aux, pce_ref.num_aux);
        assert_eq!(loaded.num_constraints, pce_ref.num_constraints);
        assert_eq!(loaded.total_nnz(), pce_ref.total_nnz());
        println!("  round-trip verification: PASS");
    }

    Ok(())
}

#[cfg(not(feature = "pce-bench"))]
fn run_pce_bench(
    _c1: PathBuf,
    _sector_num: u64,
    _miner_id: u64,
    _save_pce: Option<PathBuf>,
    _validate: bool,
) -> Result<()> {
    anyhow::bail!(
        "pce-bench subcommand requires the 'pce-bench' feature.\n\
         Rebuild with: cargo build --release -p cuzk-bench --features pce-bench --no-default-features"
    );
}

/// Read current process RSS from /proc/self/status, return as GiB.
fn rss_gib() -> f64 {
    let status = std::fs::read_to_string("/proc/self/status").unwrap_or_default();
    for line in status.lines() {
        if line.starts_with("VmRSS:") {
            let kb: f64 = line
                .split_whitespace()
                .nth(1)
                .and_then(|s| s.parse().ok())
                .unwrap_or(0.0);
            return kb / 1048576.0;
        }
    }
    0.0
}

/// Print an RSS snapshot with a label.
fn log_rss(label: &str) {
    let rss = rss_gib();
    println!("  [RSS] {:<40} {:>7.1} GiB", label, rss);
}

#[cfg(feature = "pce-bench")]
fn run_pce_pipeline(
    c1: PathBuf,
    sector_num: u64,
    miner_id: u64,
    num_proofs: u32,
    parallel: u32,
    compare_old: bool,
) -> Result<()> {
    // Set FIL_PROOFS_PARAMETER_CACHE
    if std::env::var("FIL_PROOFS_PARAMETER_CACHE").is_err() {
        if std::path::Path::new("/data/zk/params").exists() {
            unsafe { std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", "/data/zk/params") };
        }
    }

    let parallel = if parallel == 0 { 1 } else { parallel };
    let is_parallel = parallel > 1;

    println!("=== PCE Pipeline Benchmark ===");
    println!("c1:         {}", c1.display());
    println!("sector:     {} (miner {})", sector_num, miner_id);
    println!("num_proofs: {}", num_proofs);
    println!("parallel:   {}{}", parallel, if is_parallel { " (concurrent pipelines)" } else { " (sequential)" });
    println!("compare:    {}", if compare_old { "old + PCE" } else { "PCE only" });
    println!();

    let c1_data = std::fs::read(&c1)
        .with_context(|| format!("failed to read {}", c1.display()))?;
    println!("c1 loaded: {} bytes", c1_data.len());
    log_rss("after c1 load");

    // Optional: run old path first for comparison, then drop
    if compare_old {
        println!("\n--- Old-path synthesis (for comparison, will drop) ---");
        let old_start = Instant::now();
        let old_synth = cuzk_core::pipeline::synthesize_porep_c2_batch(
            &c1_data, sector_num, miner_id, "pipeline-old",
        )?;
        let old_time = old_synth.synthesis_duration;
        let old_circuits = old_synth.provers.len();
        let old_constraints = if old_circuits > 0 { old_synth.provers[0].a.len() } else { 0 };
        log_rss("old-path synthesis complete (held)");
        println!(
            "  old-path: synth={:.1}s  circuits={}  constraints={}  wall={:.1}s",
            old_time.as_secs_f64(),
            old_circuits,
            old_constraints,
            old_start.elapsed().as_secs_f64(),
        );

        // Drop old-path results to free memory
        drop(old_synth);
        #[cfg(target_os = "linux")]
        unsafe { libc::malloc_trim(0); }
        log_rss("old-path results DROPPED");
    }

    // Step 1: PCE extraction (first time only — cached in OnceLock)
    println!("\n--- PCE Extraction (one-time) ---");
    let extract_start = Instant::now();
    cuzk_core::pipeline::extract_and_cache_pce_from_c1(&c1_data, sector_num, miner_id)?;
    let extract_time = extract_start.elapsed();
    println!("  extract={:.1}s", extract_time.as_secs_f64());
    log_rss("after PCE extraction (cached in OnceLock)");

    if is_parallel {
        // ── Parallel mode: launch `parallel` syntheses concurrently ──
        // This simulates N GPU pipelines each needing a synthesis result
        // at the same time. Peak memory = PCE static + N × per-pipeline.
        println!(
            "\n--- Parallel PCE proofs ({} proofs, {} concurrent) ---",
            num_proofs, parallel,
        );

        // Process in waves of `parallel` concurrent proofs
        let mut all_synth_times: Vec<std::time::Duration> = Vec::new();
        let mut peak_rss_val: f64 = 0.0;
        let mut wave = 0u32;
        let mut remaining = num_proofs;

        while remaining > 0 {
            let batch = remaining.min(parallel);
            remaining -= batch;
            wave += 1;

            println!("\n  wave {} ({} concurrent syntheses)", wave, batch);
            let wave_start = Instant::now();

            // Launch batch concurrent syntheses using scoped threads
            let c1_ref = &c1_data;
            let results: Vec<_> = std::thread::scope(|s| {
                let handles: Vec<_> = (0..batch)
                    .map(|j| {
                        let job_id = format!("pipeline-pce-w{}-j{}", wave, j);
                        s.spawn(move || {
                            cuzk_core::pipeline::synthesize_porep_c2_batch(
                                c1_ref, sector_num, miner_id, &job_id,
                            )
                        })
                    })
                    .collect();

                handles.into_iter().map(|h| h.join().unwrap()).collect()
            });

            let wave_wall = wave_start.elapsed();
            let current_rss = rss_gib();
            if current_rss > peak_rss_val {
                peak_rss_val = current_rss;
            }
            log_rss(&format!("wave {} all {} syntheses complete (held)", wave, batch));

            // Log individual times
            for (j, res) in results.iter().enumerate() {
                match res {
                    Ok(synth) => {
                        let t = synth.synthesis_duration;
                        println!(
                            "    pipeline {}: synth={:.1}s  circuits={}",
                            j, t.as_secs_f64(), synth.provers.len(),
                        );
                        all_synth_times.push(t);
                    }
                    Err(e) => {
                        println!("    pipeline {}: FAILED: {:?}", j, e);
                    }
                }
            }
            println!("    wave wall={:.1}s", wave_wall.as_secs_f64());

            // Drop all results (simulate GPU handoff)
            drop(results);
            #[cfg(target_os = "linux")]
            unsafe { libc::malloc_trim(0); }
            log_rss(&format!("wave {} results DROPPED", wave));
        }

        // Summary
        println!("\n--- Summary (parallel, j={}) ---", parallel);
        println!("PCE extraction:     {:.1}s (one-time)", extract_time.as_secs_f64());
        println!("total proofs:       {}", all_synth_times.len());
        if !all_synth_times.is_empty() {
            let avg: f64 = all_synth_times.iter().map(|t| t.as_secs_f64()).sum::<f64>()
                / all_synth_times.len() as f64;
            let max = all_synth_times.iter().map(|t| t.as_secs_f64())
                .max_by(|a, b| a.partial_cmp(b).unwrap()).unwrap_or(0.0);
            println!("avg synth/proof:    {:.1}s", avg);
            println!("max synth/proof:    {:.1}s", max);
        }
        println!("peak RSS:           {:.1} GiB", peak_rss_val);
        log_rss("final (after all dropped)");
    } else {
        // ── Sequential mode ──
        println!("\n--- Sequential PCE proofs ({} iterations) ---", num_proofs);
        let mut synth_times = Vec::with_capacity(num_proofs as usize);
        let mut peak_rss_val: f64 = 0.0;

        for i in 0..num_proofs {
            println!("\n  proof {}/{}", i + 1, num_proofs);

            let proof_start = Instant::now();
            let synth = cuzk_core::pipeline::synthesize_porep_c2_batch(
                &c1_data, sector_num, miner_id, &format!("pipeline-pce-{}", i),
            )?;
            let synth_time = synth.synthesis_duration;
            synth_times.push(synth_time);

            let current_rss = rss_gib();
            if current_rss > peak_rss_val {
                peak_rss_val = current_rss;
            }
            println!(
                "    synth={:.1}s  circuits={}  constraints={}  wall={:.1}s",
                synth_time.as_secs_f64(),
                synth.provers.len(),
                if !synth.provers.is_empty() { synth.provers[0].a.len() } else { 0 },
                proof_start.elapsed().as_secs_f64(),
            );
            log_rss(&format!("proof {} synthesis complete (held)", i + 1));

            // Simulate GPU handoff: drop synthesis results
            drop(synth);
            #[cfg(target_os = "linux")]
            unsafe { libc::malloc_trim(0); }
            log_rss(&format!("proof {} results DROPPED (GPU handoff)", i + 1));
        }

        // Summary
        println!("\n--- Summary (sequential) ---");
        println!("PCE extraction:  {:.1}s (one-time, amortized)", extract_time.as_secs_f64());
        for (i, t) in synth_times.iter().enumerate() {
            println!("  proof {:>2} synth:  {:.1}s", i + 1, t.as_secs_f64());
        }
        if synth_times.len() > 1 {
            let avg: f64 = synth_times.iter().map(|t| t.as_secs_f64()).sum::<f64>()
                / synth_times.len() as f64;
            println!("  average synth:  {:.1}s", avg);
        }
        println!("  peak RSS:       {:.1} GiB", peak_rss_val);
        log_rss("final (after all proofs dropped)");
    }

    Ok(())
}

#[cfg(not(feature = "pce-bench"))]
fn run_pce_pipeline(
    _c1: PathBuf,
    _sector_num: u64,
    _miner_id: u64,
    _num_proofs: u32,
    _parallel: u32,
    _compare_old: bool,
) -> Result<()> {
    anyhow::bail!(
        "pce-pipeline subcommand requires the 'pce-bench' feature.\n\
         Rebuild with: cargo build --release -p cuzk-bench --features pce-bench --no-default-features"
    );
}

#[cfg(feature = "pce-bench")]
fn run_slotted_bench(
    c1: PathBuf,
    sector_num: u64,
    miner_id: u64,
    slot_sizes_str: &str,
    num_proofs: u32,
) -> Result<()> {
    use std::time::Instant;

    // Set FIL_PROOFS_PARAMETER_CACHE
    if std::env::var("FIL_PROOFS_PARAMETER_CACHE").is_err() {
        if std::path::Path::new("/data/zk/params").exists() {
            unsafe { std::env::set_var("FIL_PROOFS_PARAMETER_CACHE", "/data/zk/params") };
        }
    }

    // Parse max_concurrent values (still called "slot_sizes" in CLI for compat)
    let max_concurrents: Vec<usize> = slot_sizes_str
        .split(',')
        .filter_map(|s| s.trim().parse().ok())
        .collect();
    anyhow::ensure!(
        !max_concurrents.is_empty(),
        "no valid values provided (expected comma-separated integers)"
    );

    println!("=== Pipelined Partition Pipeline Benchmark (Phase 6) ===");
    println!("c1:              {}", c1.display());
    println!("sector:          {} (miner {})", sector_num, miner_id);
    println!("max_concurrent:  {:?}", max_concurrents);
    println!("num_proofs:      {} per configuration", num_proofs);
    println!();

    let c1_data = std::fs::read(&c1)
        .with_context(|| format!("failed to read {}", c1.display()))?;
    println!("c1 loaded: {} bytes", c1_data.len());
    log_rss("after c1 load");

    // Ensure PCE is extracted first (one-time cost)
    println!("\n--- PCE Extraction (one-time) ---");
    let extract_start = Instant::now();
    cuzk_core::pipeline::extract_and_cache_pce_from_c1(&c1_data, sector_num, miner_id)?;
    let extract_time = extract_start.elapsed();
    println!("  extract={:.1}s", extract_time.as_secs_f64());
    log_rss("after PCE extraction");

    // Load SRS parameters
    println!("\n--- Loading SRS ---");
    let srs_start = Instant::now();
    let srs = {
        let mut mgr = cuzk_core::srs_manager::SrsManager::new(
            std::path::PathBuf::from(
                std::env::var("FIL_PROOFS_PARAMETER_CACHE")
                    .unwrap_or_else(|_| "/data/zk/params".to_string()),
            ),
            50 * 1024 * 1024 * 1024, // 50 GiB budget
        );
        mgr.ensure_loaded(&cuzk_core::srs_manager::CircuitId::Porep32G)?
    };
    let srs_time = srs_start.elapsed();
    println!("  SRS loaded in {:.1}s", srs_time.as_secs_f64());
    log_rss("after SRS load");

    // Results table
    struct BenchResult {
        max_concurrent: usize,
        _proof_idx: u32,
        total_s: f64,
        synth_s: f64,
        gpu_s: f64,
        gpu_pct: f64,
        peak_rss: f64,
        proof_len: usize,
    }
    let mut all_results: Vec<BenchResult> = Vec::new();

    for &max_concurrent in &max_concurrents {
        println!(
            "\n========== max_concurrent = {} {} ==========",
            max_concurrent,
            if max_concurrent >= 10 { "(batch-all)" } else { "" },
        );

        for proof_idx in 0..num_proofs {
            println!(
                "\n  proof {}/{} (max_concurrent={})",
                proof_idx + 1,
                num_proofs,
                max_concurrent,
            );
            log_rss("before proof");

            let proof_start = Instant::now();

            // For max_concurrent >= 10, prove_porep_c2_slotted falls back to batch-all.
            // For max_concurrent < 10, it uses prove_porep_c2_pipelined.
            let (proof_bytes, timings) = cuzk_core::pipeline::prove_porep_c2_slotted(
                &c1_data,
                sector_num,
                miner_id,
                &srs,
                max_concurrent,
                &format!("pipe-bench-c{}-p{}", max_concurrent, proof_idx),
            )?;

            let total_time = proof_start.elapsed();
            let peak_rss = rss_gib();
            let gpu_pct = timings.gpu_compute.as_secs_f64() / total_time.as_secs_f64() * 100.0;
            let overlap = (timings.synthesis.as_secs_f64() + timings.gpu_compute.as_secs_f64())
                / total_time.as_secs_f64();

            println!(
                "    total={:.1}s  synth_sum={:.1}s  gpu_sum={:.1}s  gpu_active={:.0}%  proof={}B  overlap={:.2}x",
                total_time.as_secs_f64(),
                timings.synthesis.as_secs_f64(),
                timings.gpu_compute.as_secs_f64(),
                gpu_pct,
                proof_bytes.len(),
                overlap,
            );
            log_rss("after proof (before drop)");

            all_results.push(BenchResult {
                max_concurrent,
                _proof_idx: proof_idx,
                total_s: total_time.as_secs_f64(),
                synth_s: timings.synthesis.as_secs_f64(),
                gpu_s: timings.gpu_compute.as_secs_f64(),
                gpu_pct,
                peak_rss,
                proof_len: proof_bytes.len(),
            });

            // Drop proof data and reclaim memory
            drop(proof_bytes);
            #[cfg(target_os = "linux")]
            unsafe {
                libc::malloc_trim(0);
            }
            log_rss("after drop + malloc_trim");
        }
    }

    // Summary table
    println!("\n========== Summary ==========");
    println!(
        "{:<12} {:>8} {:>10} {:>8} {:>8} {:>8} {:>8} {:>8}",
        "max_concur", "total_s", "synth_sum", "gpu_sum", "gpu_%", "overlap", "peak_GiB", "proof_B"
    );
    println!("{}", "-".repeat(84));

    // Group by max_concurrent and average
    for &mc in &max_concurrents {
        let entries: Vec<_> = all_results
            .iter()
            .filter(|r| r.max_concurrent == mc)
            .collect();
        if entries.is_empty() {
            continue;
        }
        let n = entries.len() as f64;
        let avg_total = entries.iter().map(|r| r.total_s).sum::<f64>() / n;
        let avg_synth = entries.iter().map(|r| r.synth_s).sum::<f64>() / n;
        let avg_gpu = entries.iter().map(|r| r.gpu_s).sum::<f64>() / n;
        let avg_gpu_pct = entries.iter().map(|r| r.gpu_pct).sum::<f64>() / n;
        let avg_overlap = (avg_synth + avg_gpu) / avg_total;
        let peak_rss = entries
            .iter()
            .map(|r| r.peak_rss)
            .fold(0.0f64, f64::max);
        let proof_len = entries[0].proof_len;

        println!(
            "{:<12} {:>8.1} {:>10.1} {:>8.1} {:>7.0}% {:>7.2}x {:>8.1} {:>8}",
            if mc >= 10 {
                format!("{}(batch)", mc)
            } else {
                mc.to_string()
            },
            avg_total,
            avg_synth,
            avg_gpu,
            avg_gpu_pct,
            avg_overlap,
            peak_rss,
            proof_len,
        );
    }

    // Comparison vs batch-all
    if let Some(baseline) = all_results.iter().find(|r| r.max_concurrent >= 10) {
        let baseline_total = baseline.total_s;
        println!("\n--- Speedup vs batch-all ---");
        for &mc in &max_concurrents {
            if mc >= 10 {
                continue;
            }
            let entries: Vec<_> = all_results
                .iter()
                .filter(|r| r.max_concurrent == mc)
                .collect();
            if entries.is_empty() {
                continue;
            }
            let avg_total = entries.iter().map(|r| r.total_s).sum::<f64>() / entries.len() as f64;
            let peak_rss = entries
                .iter()
                .map(|r| r.peak_rss)
                .fold(0.0f64, f64::max);
            println!(
                "  max_concurrent={}: {:.1}s vs {:.1}s = {:.2}x, peak {:.1} GiB vs {:.1} GiB",
                mc,
                avg_total,
                baseline_total,
                baseline_total / avg_total,
                peak_rss,
                baseline.peak_rss,
            );
        }
    }

    Ok(())
}

#[cfg(not(feature = "pce-bench"))]
fn run_slotted_bench(
    _c1: PathBuf,
    _sector_num: u64,
    _miner_id: u64,
    _slot_sizes: &str,
    _num_proofs: u32,
) -> Result<()> {
    anyhow::bail!(
        "slotted-bench subcommand requires the 'pce-bench' feature.\n\
         Rebuild with: cargo build --release -p cuzk-bench --features pce-bench --no-default-features"
    );
}

#[cfg(not(feature = "synth-bench"))]
fn run_synth_only(
    _c1: PathBuf,
    _sector_num: u64,
    _miner_id: u64,
    _iterations: u32,
    _partition: Option<usize>,
) -> Result<()> {
    anyhow::bail!(
        "synth-only subcommand requires the 'synth-bench' feature.\n\
         Rebuild with: cargo build --release -p cuzk-bench --features synth-bench"
    );
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
