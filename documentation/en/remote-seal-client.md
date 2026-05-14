# Remote Seal Client Guide

This guide covers setting up and operating a Curio node as a **remote seal
client** -- a node that delegates SDR and tree computation to a remote provider
while handling the rest of the sealing pipeline locally.

## Prerequisites

- A Curio node with access to the shared HarmonyDB cluster
- HTTP/HTTPS enabled on the node (the provider sends completion callbacks to
  the client)
- A miner actor and wallet for on-chain messages
- Storage for sealed sectors (long-term) and cache (temporary)
- GPU for PoRep SNARK proof computation
- A configured remote seal provider (you need a connect string from the
  provider operator)

## Configuration

Enable the remote seal client in your Curio configuration layer:

```toml
[Subsystems]
# Core requirement: enable the remote seal client
EnableRemoteSealClient = true

# The client still needs the rest of the pipeline:
EnableSendPrecommitMsg = true
EnablePoRepProof = true
EnableSendCommitMsg = true
EnableMoveStorage = true

# Optional but recommended: enable finalize on this node
# FinalizeMaxTasks = 10

[HTTP]
# HTTP must be enabled -- the provider sends /complete callbacks here
Enable = true
DomainName = "seal-client.example.com"
ListenAddress = "0.0.0.0:12300"
```

The client does **not** need `EnableSealSDR` or `EnableSealSDRTrees` -- those
run on the provider.

## Adding a Provider

### Using the Web UI

1. Obtain a **connect string** from the provider operator.
2. Navigate to the Remote Seal section in the Curio web UI.
3. Click "Add Provider" and provide:
   - **SP ID**: The miner actor ID that should use this provider (e.g. `1234`)
   - **Connect String**: The base64 string from the provider operator
4. The system decodes the connect string and stores the provider URL and token.

The connect string contains:
```json
{"url": "https://seal-provider.example.com", "token": "hex-encoded-auth-token"}
```

### Enabling/Disabling Providers

Providers can be toggled on/off in the UI. When disabled, no new sectors will
be delegated to that provider, but existing in-flight sectors will complete
normally.

### Multiple Providers

You can configure multiple providers for the same miner, or different providers
for different miners in a multi-miner cluster. The delegate task picks an
available provider per-sector.

## How It Works

### Client Pipeline States

When a sector is delegated, two pipeline tables track its state:

**`rseal_client_pipeline`** (remote-seal-specific state):
```
RSealDelegate creates entry + sends /order
    |
    v
RSealClientPoll (polls /status, or receives /complete callback)
    |  Applies completion: after_sdr, after_tree_d, after_tree_c,
    |  after_tree_r = TRUE in BOTH rseal_client_pipeline
    |  AND sectors_sdr_pipeline
    v
RSealClientFetch (downloads sealed sector + cache from provider)
    |  Downloads 32 GiB sealed file (GET /sealed-data, supports Range)
    |  Downloads ~73 MiB cache tar (GET /cache-data)
    |  Writes c1.url file in cache directory
    |  after_fetch = TRUE
    v
[Normal pipeline: precommit -> PoRep -> commit -> finalize -> move-storage]
    v
RSealClientCleanup (after PoRep completes)
    |  POST /finalize (provider drops layers)
    |  POST /cleanup (provider removes all data)
    |  after_cleanup = TRUE
```

**`sectors_sdr_pipeline`** (standard pipeline, remote sectors are marked):
```
After ApplyRemoteCompletion:
    after_sdr = TRUE
    after_tree_d = TRUE
    after_tree_c = TRUE
    after_tree_r = TRUE
    after_synth = TRUE  (remote sectors skip local synth)
    ticket_epoch, ticket_value = from provider
    tree_d_cid, tree_r_cid = from provider
    all task_id_* = NULL (ready for next stages)

The SealPoller detects this sector has a rseal_client_pipeline entry
(is_remote = true) and skips SDR/tree/synth scheduling, but continues
with: precommit -> PoRep -> finalize -> move-storage -> commit
```

### Client Tasks

| Task | Schedule | Purpose |
|---|---|---|
| **RSealDelegate** | IAmBored (every 15s) | Finds unclaimed sectors, checks provider availability, sends orders |
| **RSealClientPoll** | Poller creates when `after_sdr=FALSE, task_id_sdr=NULL` | Polls provider status in a 30s loop until complete/failed |
| **RSealClientFetch** | Poller creates when `after_sdr=TRUE, after_fetch=FALSE` | Downloads sealed data and cache from provider |
| **RSealClientCleanup** | Poller creates when `after_porep=TRUE, after_cleanup=FALSE` | Sends finalize + cleanup to provider |

### How C1 (Commit Phase 1) Works

The PoRep SNARK proof requires a "vanilla proof" (C1 output) that can only be
computed where the full cache directory exists. For remote-sealed sectors,
the cache on the client does not have SDR layers, so C1 must be fetched from
the provider.

This is handled transparently:

1. During fetch, `RSealClientFetch` writes a `c1.url` file in the sector's
   cache directory containing the provider's `/commit1` endpoint URL and auth
   token.
2. When `PoRepSnark` runs, it calls `GeneratePoRepVanillaProof()`.
3. The function detects the `c1.url` file and makes an HTTP POST to the
   provider's `/commit1` endpoint with the interactive randomness seed.
4. The provider computes C1 and returns the raw bytes (~50-128 MiB).
5. The result is cached as `commit-phase1-output` in the cache directory.
6. The SNARK proof is computed normally.

The PoRep task itself is completely unaware of remote seal -- the C1 fetch is
transparent.

### Completion: Callback vs Polling

The client learns about provider completion through two paths:

1. **Callback** (primary): The provider sends `POST /complete` to the client's
   HTTP endpoint. This is immediate and carries the ticket + CIDs.
2. **Polling** (fallback): `RSealClientPoll` queries `POST /status` every 30s.
   If the callback was missed (network issue, restart), the poll task catches
   it.

Both paths call `ApplyRemoteCompletion()` which is idempotent.

### Failure Handling

If the provider reports a sector as `"failed"`:
- `rseal_client_pipeline.failed = TRUE` with reason from provider
- `sectors_sdr_pipeline` task_ids are cleared
- The sector can potentially be retried (manually or by a future retry
  mechanism)

If the provider is unreachable:
- The poll task retries with 30s intervals
- After 10 consecutive failures (`MaxFailures`), the task stops and needs
  manual intervention

## Network Requirements

The client makes these HTTP calls to the provider:

| Call | Data Size | Notes |
|---|---|---|
| `POST /available` | ~100 bytes | Quick availability check |
| `POST /order` | ~200 bytes | Creates seal order |
| `POST /status` | ~200 bytes | Polls state (every 30s while waiting) |
| `GET /sealed-data` | **32 GiB** | Supports HTTP Range for resumable downloads |
| `GET /cache-data` | **~73 MiB** | Tar archive |
| `POST /commit1` | **~50-128 MiB response** | Raw bytes, computed on demand |
| `POST /finalize` | ~200 bytes | Signals layer drop |
| `POST /cleanup` | ~200 bytes | Signals full cleanup |

The provider makes one callback to the client:

| Call | Data Size | Notes |
|---|---|---|
| `POST /complete` | ~500 bytes | CIDs + ticket data |

Ensure the network between client and provider can handle the 32 GiB sealed
sector transfer. HTTP Range headers are supported, so aria2c or similar
multi-connection download tools can be used.

## Monitoring

Client pipeline status is visible in the Curio web UI under the Remote Seal
section. Each delegated sector shows:

- Provider name
- Current state (after_sdr, after_fetch, etc.)
- Failure information if applicable

The standard `sectors_sdr_pipeline` view also shows remote sectors, but with
SDR/tree/synth stages already marked as complete.
