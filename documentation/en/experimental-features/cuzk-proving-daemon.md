---
description: >-
  This page explains how to set up the cuzk persistent GPU SNARK proving daemon
  to accelerate proof computation in Curio.
---

# cuzk Proving Daemon

> **Experimental Feature**\
> This feature is currently experimental and under active development. Configuration, behavior, and interfaces may change without notice.

---

## What is cuzk?

cuzk is a persistent GPU-resident SNARK proving daemon. It acts as a "proving server" that Curio delegates proof computations to over gRPC.

The key difference from the default proving path (`ffiselect`) is that cuzk loads Groth16 SRS parameters **once at startup** and keeps them resident in CUDA-pinned host memory across all proofs. The default Curio code path spawns a fresh child process per proof, each of which loads the SRS from disk (30-90 seconds for 32 GiB PoRep), runs one proof, and exits. cuzk eliminates this repeated loading overhead entirely.

### Supported proof types

| Proof type | Curio task | Description |
|---|---|---|
| PoRep C2 | `PoRep` | Seal commit phase 2 SNARK |
| SnapDeals Prove | `UpdateProve` | CC sector update proof |
| PSProve | `PSProve` | Snark Market proof share compute |

### How integration works

When cuzk is enabled in Curio's configuration:

1. **Resource accounting bypassed**: `TypeDetails()` reports zero GPU and minimal RAM for proving tasks. Curio's harmony scheduler no longer gates these tasks on local GPU availability.
2. **Backpressure via polling**: `CanAccept()` queries the cuzk daemon's queue via `GetStatus` and rejects tasks when the queue is full (controlled by `MaxPending`).
3. **Vanilla proofs stay local**: The `Do()` method generates vanilla proofs locally (requires sector data on disk), sends them to cuzk for SNARK computation, then verifies the returned proof locally.

When cuzk is **not** configured (default), all three tasks behave exactly as before. There is no behavioral change for existing deployments.

---

## Requirements

- **NVIDIA GPU** with CUDA support (the cuzk daemon itself runs on the GPU machine)
- **CUDA toolkit** (`nvcc` must be in PATH)
- **Rust toolchain** (1.86.0 or later; managed automatically via `rust-toolchain.toml`)
- **Filecoin proof parameters** downloaded (same parameters as standard Curio proving)
- Sufficient system RAM for SRS residency (minimum 128 GiB, recommended 256+ GiB)

---

## Building

From the Curio repository root:

```bash
# Build both Curio and the cuzk daemon
make curio cuzk
```

The `make cuzk` target:
- Checks for `cargo` (Rust) and `nvcc` (CUDA) in PATH
- Runs `cargo build --release` in `extern/cuzk/`
- Copies the resulting binary to `./cuzk`

To install:

```bash
sudo make install        # installs curio and sptool
sudo make install-cuzk   # installs cuzk to /usr/local/bin/cuzk
```

Note: `make cuzk` is intentionally **not** part of `make build` or `make buildall` since it requires CUDA and Rust, which are not available in all build environments (e.g., CI).

---

## Daemon Configuration

The cuzk daemon reads its configuration from a TOML file (default: `/data/zk/cuzk.toml`). An example configuration is provided at `extern/cuzk/cuzk.example.toml`.

### Minimal configuration

```toml
[daemon]
listen = "0.0.0.0:9820"

[srs]
param_cache = "/var/tmp/filecoin-proof-parameters"
preload = ["porep-32g"]

[synthesis]
partition_workers = 7   # Adjust based on RAM (see table below)
```

### RAM-based tuning

The primary tuning knob is `partition_workers`, which controls how many PoRep partitions are synthesized concurrently on the CPU. More workers keep the GPU fed but use more RAM.

| System RAM | partition\_workers | gpu\_workers\_per\_device | Peak RSS | Throughput |
|---|---|---|---|---|
| 128 GiB | 2 | 1 | ~110 GiB | ~152 s/proof |
| 256 GiB | 7 | 1 | ~208 GiB | ~53 s/proof |
| 384 GiB | 10 | 2 | ~271 GiB | ~43 s/proof |
| 512+ GiB | 12 | 2 | ~400 GiB | ~38 s/proof |

Memory formula: `Peak RSS = 69 + (partition_workers x 20) GiB`

### Running the daemon

```bash
# With default config path
cuzk

# With custom config
cuzk --config /path/to/cuzk.toml

# Override listen address
cuzk --listen unix:///run/curio/cuzk.sock

# Override log level
cuzk --log-level debug
```

For production, run cuzk as a systemd service:

```ini
[Unit]
Description=cuzk SNARK proving daemon
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/cuzk --config /data/zk/cuzk.toml
Restart=on-failure
RestartSec=10
LimitNOFILE=1048576
# Ensure CUDA libraries are available
Environment=LD_LIBRARY_PATH=/usr/local/cuda/lib64

[Install]
WantedBy=multi-user.target
```

---

## Curio Configuration

Add the following to your Curio configuration layer to connect to the cuzk daemon:

```toml
[Cuzk]
  # gRPC endpoint of the cuzk daemon.
  # TCP:  "127.0.0.1:9820"
  # Unix: "unix:///run/curio/cuzk.sock"
  Address = "127.0.0.1:9820"

  # Maximum total pending proofs in the cuzk queue before Curio stops
  # sending new tasks (backpressure). When the daemon's queue reaches
  # this level, CanAccept rejects new tasks until capacity frees up.
  MaxPending = 10

  # Maximum time to wait for a proof result from the daemon.
  # If exceeded, the task is retried.
  ProveTimeout = "30m"
```

When `Address` is empty (the default), cuzk integration is disabled and all proving tasks use the standard local GPU path.

### Which Curio subsystems are affected

The cuzk client is used by tasks on nodes that have these subsystems enabled:

- `EnablePoRepProof = true` -- PoRep C2 proving
- `EnableUpdateProve = true` -- SnapDeals update proving
- `EnableProofShare = true` -- Snark Market proof computation

These subsystems must still be enabled as usual. The `[Cuzk]` configuration only changes *how* the SNARK computation is performed (local GPU vs. remote daemon).

---

## Deployment Patterns

### Co-located (single machine)

Run both Curio and cuzk on the same GPU machine. Use TCP localhost or a Unix socket:

```
Curio (Go) --gRPC--> cuzk (Rust) --CUDA--> GPU
```

```toml
# Curio config
[Cuzk]
Address = "127.0.0.1:9820"
```

This is the simplest deployment and avoids any network overhead.

### Dedicated prover (separate machines)

Run Curio on CPU-only machines for sealing tasks (SDR, TreeD, etc.) and cuzk on a dedicated GPU machine. Curio connects over the network:

```
Curio (CPU node) --gRPC/TCP--> cuzk (GPU node)
```

```toml
# Curio config (on CPU node)
[Cuzk]
Address = "gpu-prover.internal:9820"
```

Note: vanilla proof data (up to ~200 MB for PoRep C2) is sent over gRPC, so ensure sufficient network bandwidth between the nodes.

---

## Monitoring

The cuzk daemon exposes its status via the gRPC `GetStatus` RPC. Curio queries this automatically for backpressure. You can also query it manually:

```bash
grpcurl -plaintext 127.0.0.1:9820 cuzk.v1.ProvingEngine/GetStatus
```

This returns the current queue state for each proof type (pending count, in-progress count).

---

## Troubleshooting

### cuzk build fails

- Verify `nvcc` is in PATH: `nvcc --version`
- Verify Rust toolchain: `rustup show` (should show 1.86.0 or later)
- The Cargo workspace in `extern/cuzk/` depends on vendored forks of `bellperson`, `bellpepper-core`, and `supraseal-c2` under `extern/`. If you see missing crate errors, ensure `git submodule update --init --recursive` was run.

### Curio cannot connect to cuzk

- Check that the daemon is running: `systemctl status cuzk`
- Verify the address matches between Curio's `[Cuzk].Address` and the daemon's `[daemon].listen`
- Check firewall rules if using TCP across machines
- Look at Curio logs for `cuzk` entries: `journalctl -u curio | grep cuzk`

### Proofs are slow or timing out

- Increase `ProveTimeout` in Curio's config if proofs legitimately take longer
- Check daemon logs for queue depth. If proofs pile up, reduce `MaxPending` or add more GPU capacity
- Tune `partition_workers` based on the RAM table above. Too many workers can cause memory pressure; too few starve the GPU

### Curio tasks rejected (backpressure)

If Curio logs show "cuzk pipeline full, backpressuring", the daemon's queue is at capacity. Either:
- Increase `MaxPending` (allows more queued proofs, uses more memory)
- Add GPU capacity (second GPU, second daemon instance)
- Reduce the rate of incoming sealing/snap work
