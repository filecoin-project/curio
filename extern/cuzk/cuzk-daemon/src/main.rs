//! cuzk-daemon: Standalone binary for the cuzk proving daemon.
//!
//! Loads configuration, starts the engine and gRPC server, handles signals.

use anyhow::{Context, Result};
use clap::Parser;
use std::path::PathBuf;
use std::sync::Arc;
use tonic::transport::Server;
use tracing::info;

use cuzk_core::config::Config;
use cuzk_core::engine::Engine;
use cuzk_proto::cuzk::v1::proving_engine_server::ProvingEngineServer;
use cuzk_server::service::ProvingService;

#[derive(Parser, Debug)]
#[command(name = "cuzk-daemon", about = "cuzk proving engine daemon")]
struct Cli {
    /// Path to configuration file (TOML).
    #[arg(short, long, default_value = "/data/zk/cuzk.toml")]
    config: PathBuf,

    /// Override listen address (e.g. "0.0.0.0:9820" or "unix:///run/curio/cuzk.sock").
    #[arg(short, long)]
    listen: Option<String>,

    /// Override log level.
    #[arg(long)]
    log_level: Option<String>,
}

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();

    // Load configuration
    let mut config = if cli.config.exists() {
        Config::from_file(&cli.config)
            .with_context(|| format!("failed to load config from {:?}", cli.config))?
    } else {
        info!("config file not found at {:?}, using defaults", cli.config);
        Config::default()
    };

    // Apply CLI overrides
    if let Some(listen) = &cli.listen {
        config.daemon.listen = listen.clone();
    }
    if let Some(level) = &cli.log_level {
        config.logging.level = level.clone();
    }

    // Initialize logging
    let env_filter = tracing_subscriber::EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new(&config.logging.level));

    tracing_subscriber::fmt()
        .with_env_filter(env_filter)
        .with_target(true)
        .init();

    info!("cuzk-daemon starting");
    info!(listen = %config.daemon.listen, "configuration loaded");

    // ─── Thread pool isolation ──────────────────────────────────────────
    //
    // Two separate CPU thread pools compete for cores during parallel proving:
    //
    //   1. Rayon global pool — used by synthesis (bellperson, PCE SpMV)
    //   2. C++ groth16_pool (sppark) — used by b_g2_msm and preprocessing
    //      during GPU proving
    //
    // When synthesis_concurrency > 1, both pools run simultaneously and
    // contend for CPU time. Partitioning threads between them eliminates
    // contention and improves throughput.
    //
    // synthesis.threads → rayon global pool size (default: all CPUs)
    // gpus.gpu_threads  → C++ groth16_pool size via CUZK_GPU_THREADS env
    //                      (read at library load time, must be set early)

    // Configure C++ GPU thread pool BEFORE any supraseal code runs.
    // The static thread_pool_t in groth16_cuda.cu reads CUZK_GPU_THREADS
    // at construction time (library load).
    if config.gpus.gpu_threads > 0 {
        std::env::set_var("CUZK_GPU_THREADS", config.gpus.gpu_threads.to_string());
        info!(
            gpu_threads = config.gpus.gpu_threads,
            "set CUZK_GPU_THREADS for C++ groth16_pool"
        );
    }

    // Configure rayon global thread pool for synthesis work.
    // Must be called before any rayon::par_iter or rayon::join.
    {
        let rayon_threads = if config.synthesis.threads > 0 {
            config.synthesis.threads as usize
        } else {
            // Default: all available CPUs (same as rayon default).
            // When gpu_threads is set, the user should also set synthesis.threads
            // to partition cores, but we don't enforce it.
            std::thread::available_parallelism()
                .map(|n| n.get())
                .unwrap_or(4)
        };
        rayon::ThreadPoolBuilder::new()
            .num_threads(rayon_threads)
            .thread_name(|i| format!("rayon-synth-{}", i))
            .build_global()
            .expect("failed to configure rayon global thread pool (must be called before any rayon work)");
        info!(
            rayon_threads = rayon_threads,
            "rayon global thread pool configured"
        );
    }

    // Create and start the engine
    let engine = Arc::new(Engine::new(config.clone()));
    engine.start().await?;

    // Create gRPC service
    let service = ProvingService::new(engine.clone());

    // Parse listen address and start server
    let listen_addr = &config.daemon.listen;

    if let Some(socket_path) = listen_addr.strip_prefix("unix://") {
        // Unix domain socket
        info!(path = socket_path, "listening on unix socket");

        // Remove stale socket file if it exists
        let _ = std::fs::remove_file(socket_path);

        // Ensure parent directory exists
        if let Some(parent) = std::path::Path::new(socket_path).parent() {
            std::fs::create_dir_all(parent)
                .with_context(|| format!("failed to create socket dir {:?}", parent))?;
        }

        // Use tokio's UnixListener for UDS support with tonic
        let uds = tokio::net::UnixListener::bind(socket_path)
            .with_context(|| format!("failed to bind unix socket {}", socket_path))?;

        let uds_stream = tokio_stream::wrappers::UnixListenerStream::new(uds);

        info!("cuzk-daemon ready, serving on unix://{}", socket_path);

        // Spawn signal handler
        let engine_shutdown = engine.clone();
        tokio::spawn(async move {
            tokio::signal::ctrl_c().await.ok();
            info!("received SIGINT, shutting down");
            engine_shutdown.shutdown().await;
        });

        // Max message size: 128 MiB (PoRep C1 output is ~51 MB)
        let svc = ProvingEngineServer::new(service)
            .max_decoding_message_size(128 * 1024 * 1024)
            .max_encoding_message_size(128 * 1024 * 1024);

        Server::builder()
            .add_service(svc)
            .serve_with_incoming(uds_stream)
            .await
            .context("gRPC server error")?;
    } else {
        // TCP socket
        let addr = listen_addr
            .parse()
            .with_context(|| format!("invalid listen address: {}", listen_addr))?;

        info!(%addr, "listening on TCP");
        info!("cuzk-daemon ready, serving on {}", addr);

        // Spawn signal handler
        let engine_shutdown = engine.clone();
        tokio::spawn(async move {
            tokio::signal::ctrl_c().await.ok();
            info!("received SIGINT, shutting down");
            engine_shutdown.shutdown().await;
        });

        // Max message size: 128 MiB (PoRep C1 output is ~51 MB)
        let svc = ProvingEngineServer::new(service)
            .max_decoding_message_size(128 * 1024 * 1024)
            .max_encoding_message_size(128 * 1024 * 1024);

        Server::builder()
            .add_service(svc)
            .serve(addr)
            .await
            .context("gRPC server error")?;
    }

    info!("cuzk-daemon stopped");
    Ok(())
}
