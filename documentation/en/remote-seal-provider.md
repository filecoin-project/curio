# Remote Seal Provider Guide

This guide covers setting up and operating a Curio node as a **remote seal
provider** -- a node that performs SDR and tree computation on behalf of remote
clients.

## Prerequisites

- A Curio node with access to the shared HarmonyDB cluster
- HTTP/HTTPS enabled on the node (required for the seal market API)
- Sufficient hardware for SDR + tree computation (GPU recommended, large
  temporary storage for layers and sealed sectors)
- A chain node connection (the provider computes SDR tickets from chain
  randomness)

## Configuration

Enable the remote seal provider in your Curio configuration layer:

```toml
[Subsystems]
# Core requirement: enable the remote seal provider
EnableRemoteSealProvider = true

# The provider uses the existing SDR and tree tasks.
# These MUST be enabled on the same node or cluster:
EnableSealSDR = true
EnableSealSDRTrees = true

[HTTP]
# HTTP must be enabled for the seal market API endpoints
Enable = true
DomainName = "seal-provider.example.com"  # used in connect strings
ListenAddress = "0.0.0.0:12300"

# TLS configuration (recommended for production)
# DelegateTLS = false  # set to true if using a reverse proxy for TLS
```

## Managing Partners (Clients)

Partners are clients authorized to send seal orders to this provider. Each
partner has an auth token and a sector allowance.

### Adding a Partner

Use the Curio web UI or the RPC API:

1. Navigate to the Remote Seal section in the UI.
2. Click "Add Partner" and provide:
   - **Name**: A human-readable label for this client
   - **URL**: The client's base HTTP URL (the provider uses this to send
     completion callbacks via `POST /complete`)
   - **Allowance**: Maximum number of concurrent sectors this client can have
     in the pipeline

The system generates a random auth token for the partner.

### Generating a Connect String

After creating a partner, generate a **connect string** to share with the
client operator:

1. In the UI, click "Get Connect String" for the partner.
2. The connect string is a base64-encoded JSON payload containing:
   - Your provider's HTTPS URL (derived from `HTTP.DomainName`)
   - The partner's auth token
3. Share this string with the client operator through a secure channel.

### Adjusting Allowance

You can update a partner's allowance at any time. The `allowance_remaining`
counter is decremented each time the partner sends a new order. To allow more
sectors, increase both `allowance_total` and `allowance_remaining`.

### Removing a Partner

A partner can only be removed if it has no active pipeline rows (sectors where
`after_cleanup = FALSE`). Ensure all sectors have completed the full lifecycle
before removing.

## How It Works

### Provider Pipeline States

Each sector delegated to the provider goes through these states in
`rseal_provider_pipeline`:

```
Order received (row inserted via /order handler)
    |
    v
SDR (task_id_sdr assigned by provider poller)
    |  ticket_epoch, ticket_value written by SDR task
    |  after_sdr = TRUE
    v
TreeD (task_id_tree_d assigned)
    |  tree_d_cid written
    |  after_tree_d = TRUE
    v
TreeRC (task_id_tree_c, task_id_tree_r assigned - same task)
    |  tree_r_cid written
    |  after_tree_c = TRUE, after_tree_r = TRUE
    v
NotifyClient (RSealProvNotify)
    |  POST /complete to client with CIDs + ticket
    |  after_notify_client = TRUE
    |  cleanup_timeout = NOW() + 72 hours
    v
[Wait for client]
    |  Client fetches sealed data (GET /sealed-data)
    |  Client fetches cache (GET /cache-data)
    |  Client requests C1 (POST /commit1) -> after_c1_supplied = TRUE
    |  Client sends /finalize -> after_c1_supplied = TRUE (if not already)
    v
Finalize (RSealProvFinalize)
    |  Drops SDR layers from storage
    |  after_finalize = TRUE
    v
[Wait for cleanup]
    |  Client sends /cleanup -> cleanup_requested = TRUE
    |  OR cleanup_timeout expires (72h after notify)
    v
Cleanup (RSealProvCleanup)
    |  Removes sealed, cache, unsealed data
    |  Releases batch slot
    |  after_cleanup = TRUE
    v
[GC removes row after 24h]
```

### Computation Tasks

The provider does not have its own SDR or tree task implementations. Instead,
the existing `SDR`, `TreeD`, and `TreeRC` tasks query both `sectors_sdr_pipeline`
and `rseal_provider_pipeline` via SQL `UNION ALL`. This means:

- The same task code handles both local and remote sectors
- The provider node must have `EnableSealSDR = true` and
  `EnableSealSDRTrees = true` to register these task types with harmonytask
- Resource management (GPU, storage) is shared between local and remote sectors

### Ticket Computation

The provider computes the SDR ticket directly from its own chain node. There
is no ticket exchange with the client. The ticket (epoch + randomness value)
is stored in `rseal_provider_pipeline` and sent to the client in the
`/complete` notification and `/status` response.

### C1 (Commit Phase 1)

When the client needs to compute the PoRep SNARK proof, it sends a
`POST /commit1` request with the interactive randomness seed. The provider:

1. Looks up the sector's sealed/unsealed CIDs, ticket, and proof type
2. Calls `GeneratePoRepVanillaProof()` (the C1 computation)
3. Returns the result as raw bytes (`application/octet-stream`, ~50-128 MiB)
4. Sets `after_c1_supplied = TRUE`

The provider must still have the sector's cache directory available at this
point (layers can be present or already finalized, but cache must exist).

## API Endpoints Served

These endpoints are served by the provider node under
`/remoteseal/delegated/v0/`:

| Endpoint | Method | Called by | Purpose |
|---|---|---|---|
| `/capabilities` | GET | Client | Returns supported proof types and batch size |
| `/authorize` | POST | Client | Validates partner token |
| `/available` | POST | Client | Checks slot availability, returns 30s reservation token |
| `/order` | POST | Client | Creates a new seal order |
| `/status` | POST | Client | Returns sector state (pending/sdr/trees/complete/failed) |
| `/sealed-data/{sp}/{sn}` | GET | Client | Serves sealed sector file (32 GiB, supports Range headers) |
| `/cache-data/{sp}/{sn}` | GET | Client | Serves finalized cache as tar (~73 MiB) |
| `/commit1` | POST | Client | Accepts C1 seed, returns C1 output as raw bytes |
| `/finalize` | POST | Client | Client signals layers can be dropped |
| `/cleanup` | POST | Client | Client requests full data removal |

Additionally, the provider calls this endpoint on the **client**:

| Endpoint | Method | Called by | Purpose |
|---|---|---|---|
| `/complete` | POST | Provider | Notifies client that SDR+trees are done (includes CIDs + ticket) |

## Storage Considerations

The provider needs temporary storage for:

- **SDR layers**: ~352 GiB per 32 GiB sector (11 layers x 32 GiB)
- **Sealed sector**: 32 GiB
- **Cache**: ~73 MiB (p_aux, t_aux, tree-r-last after finalize)

After the client fetches the data and C1 is supplied:
- **Finalize** drops the SDR layers (~352 GiB freed)
- **Cleanup** removes everything else (~32 GiB freed)

The 72-hour cleanup timeout ensures data is available for C1 retries even if
the client is slow to finalize.

## Monitoring

Provider pipeline status is visible in the Curio web UI under the Remote Seal
section. Each sector shows its current state (after_sdr, after_tree_d, etc.)
and any failure information.

The `PipelineGC` task automatically removes completed `rseal_provider_pipeline`
rows 24 hours after the cleanup timeout expires.
