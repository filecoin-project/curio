# GUI APIs for PDP operations

Source baseline: Curio commit `6b078a0f4528d6aad239cd41d89809e748fac5c3`. All method names, request examples, response fields, and implementation limits below describe that checkout. Discover the administration connection from the deployment record or matching release guide, identify the running version/variant, and apply the entrypoint's source-selection rule before using an unfamiliar operation. Prefer the matching API documentation and runtime schema; use the versioned examples below when compatibility is established. If documentation leaves a specific behavior unresolved, consult the matching handler or GUI call site. A missing or changed method is a compatibility question, not a reason to replay an older mutation, change Curio, or edit its database.

## Connect and identify

Use the existing private GUI address or tunnel. JSON-RPC lives at `/api/webrpc/v0`, uses the `CurioWeb.` prefix, and takes positional parameter arrays. The GUI uses WebSocket; ordinary calls can also use HTTP POST with `Content-Type: application/json`. Send these as separate requests:

```json
{"jsonrpc":"2.0","id":1,"method":"CurioWeb.Version","params":[]}
{"jsonrpc":"2.0","id":2,"method":"CurioWeb.UIVariant","params":[]}
```

Check transport errors and the JSON-RPC `error` member before consuming `result`. Failed reads remain unknown. The public PDP endpoint is separate from this administration interface. Skiff's machine address is a scheduler identity; discover actual listeners instead of assuming a full Curio worker RPC or `/remote` storage API exists there. Keep the administration interface private.

## Select the smallest useful read

Method names below omit `CurioWeb.`. `()` means `params: []`; other arguments retain their listed order. Follow pagination where completeness matters. Skiff uses these shared PDP dataset methods; older similarly named methods may belong to MK20.

| Need | Methods / parameter order | Useful observations |
| --- | --- | --- |
| Setup readiness | `PDPGuideStatus()` | `wallet`, `storage`, `dns`, `registry`: each has `ok` and `detail`; inspect details on failure. |
| Wallet | `PDPKeyStatus()` | `configured`, public `address`/`filAddress`, `balanceKnown`, `balance`, `usdfcKnown`, `usdfcBalance`. |
| Registry | `FSRegistryStatus()` | Successful `null` means currently unregistered; an object has `id`, boolean `status` (active), `pdp_service`, `capabilities`. |
| Storage | `StorageCandidates()`, `StoragePathList()` | Candidate `Path`, `Attached`, `Writable`; attached `StorageID`, `LocalPath`, `HealthOK`, `HeartbeatErr`, `Available`, `Capacity`. |
| Chain | `ChainStatus()`, `SyncerState()`, `BlockDelaySecs()` | `networkName`, `epoch`, `syncStatus`, reachable/total nodes; detailed RPC state and network epoch duration. |
| Machines | `ClusterMachines()`, `ClusterNodeInfo(id)` | `ID`, `SinceContact`, `Version`, `Layers`, `Unschedulable`, `Restarting`; inspect the selected node. |
| Workload | `PDPDashboardSummary()`, `ClusterTaskSummary()` | Dataset/storage activity and currently queued/owned tasks. |
| Proving | `PDPProvingStatus()`, `PDPProvingTimeline24h()`, `PDPProvingFailures()` | `headEpoch`, `sessionState`, `activeDataSetCount`, `inWindowCount`, `overdueCount`, `nextDeadlineEpoch`; correlate task/dataset outcomes. |
| Dataset inventory | `PDPDataSetList(limit, offset, filter, sortBy, ascending)` | `items`, `total`; example params `[50,0,"","id",true]`. Filter is empty, dataset ID, or wallet address. |
| Dataset detail | `PDPDataSetDetail(id)`, `PDPDataSetPayments(id)`, `PDPDataSetInteractions(id)` | Proving/payment state and interactions with `taskId`, `txHash`, nullable `success`, `err`, `timestamp`. |
| Payment risk | `PDPDataSetAtRiskCount()`, `PDPDataSetAtRiskList(limit, offset, sortBy, ascending)` | Use the matching dataset GUI for supported sorting and progressive scan arguments. |
| Message queue | `MessageQueueSummary()` | `filPendingCount`, `ethPendingCount`; bounded `filPending`/`ethPending` samples with timestamps and identifiers. |
| Alerts | `AlertOngoingList()`, `AlertHistoryListPaginated(limit, offset, includeAcknowledged)` | Ongoing `AlertName`, `Message`, `CreatedAt`, `LastUpdatedAt`; recover history IDs before comment/acknowledgment operations. |
| Task diagnosis | `GetTaskStatus(taskID)`, `HarmonyTaskDetails(taskID)`, `HarmonyTaskHistoryById(taskID)` | Current `status` is `pending`, `running`, `done`, or `failed`; compare latest history and ownership. |
| Task history | `ClusterTaskHistory(limit, offset)` | Bounded recent outcomes; use task-specific history for an incident. |
| Earnings | `PDPDashboardFinancial()` | `income30dUsdfc`, `accruedUnsettledUsdfc`, `expense30dFil`, `expense30dUsdfcNote`. |
| Index publication | `IPNISummary()` | Publisher identity and publication progress; confirm retrieval separately when needed. |

Interpretation that changes decisions:

- Wallet `balanceKnown: false` is unavailable balance. The guide's DNS/endpoint probe runs from the provider, so it does not establish reachability from an outside client.
- Proving can be `idle`, `upcoming`, `in-window`, or `overdue`. Inspect counts and dataset detail as well as the summary state; an open window can coexist with overdue datasets.
- Queue samples currently contain at most 20 FIL and 20 ETH entries, independently of total counts. They do not cover all failed or unbroadcast sends. `MessageByCid` is a Filecoin message view, not an ETH transaction lookup.
- Financial values are formatted strings, can contain `…`, and may be cached for 15 minutes. Follow [financial interpretation](operations.md#payments-and-earnings) before reporting earnings: the baseline's accrued field is `LockupCurrent`, and gas costs are estimated. Unavailable values and display rounding are not exact accounting.

## Core mutation shapes

Use the operator's existing authorization and inspect prerequisites before submitting. Replace illustrative names, paths, URLs, and capacities with inspected deployment values. Verify the resulting state after each operation.

Storage attachment initializes and attaches an existing directory on the GUI-serving node, inside its container when applicable. Mounting disks and arranging container volumes are host operations. After attachment, inspect `StoragePathList()` and guide storage status:

```json
{"jsonrpc":"2.0","id":3,"method":"CurioWeb.StorageAttachLocal","params":["/data/disk1"]}
```

### Wallet handling

Inspect `PDPKeyStatus()` first and preserve an existing wallet. Route key operations through local processing that keeps secret values out of model-visible tool input and output:

- **Create:** call `CreatePDPKey` with `params: []` inside the local process. Its response contains `privateKeyHex`; save the recovery material directly to the delegated protected storage. Return only public addresses and the storage reference to the model.
- **Import:** pass a protected storage reference to the local process, which reads the key and constructs `ImportPDPKey` with a one-element parameter array. The current parser accepts a hex private key or Lotus wallet export and returns the public address. Disable RPC parameter tracing before import. Keep raw key values out of shell arguments and command history.

Sanitize success and error output locally before returning it to the model; do not emit raw request/response bodies, keys, or exports in transcripts or logs. Redacting after a model-visible tool call is too late. If the available tools cannot keep secrets out of model context, have the operator perform only the key step and resume from public status.

### Registry handling

Registration currently takes **four** arguments, with a positive whole-number capacity in **TiB**. The service URL comes from effective HTTP configuration. The handler supplies other offering defaults, including price, token, piece limits, and proving period; inspect those defaults for the matching release before registration:

```json
{"jsonrpc":"2.0","id":4,"method":"CurioWeb.FSRegister","params":["Example provider","PDP storage service","C=US;ST=California;L=San Francisco",100]}
```

For offering updates, first read `FSRegistryStatus()`. Preserve unchanged URL/location/capacity and pass the full merged custom `capabilities` map: the contract replaces that map rather than patching it. The example below applies when the only custom capability is the agent marker; include every other existing capability when present:

```json
{"jsonrpc":"2.0","id":5,"method":"CurioWeb.FSUpdatePDP","params":[{"service_url":"https://pdp.example.com","location":"C=US;ST=California;L=San Francisco","capacity_tib":100},{"curioOperator":"filecoin-pdp-agent"}]}
```

At this baseline, `FSRegister` does not accept custom capabilities. For the marker described in [deployment.md](deployment.md#6-register-or-reconcile-the-offering), confirm registration first, then use `FSUpdatePDP` if the marker is still missing or different. This requires an additional transaction and gas; skip it when the desired marker is already present. Check the deployed registry's capability limits before adding the entry.

At this baseline, `FSUpdatePDP` takes only `service_url`, `location`, and `capacity_tib` from the submitted offering; submitting price or piece-limit fields does not update them. Custom capabilities are a separate second argument. The handler also derives IPNI identity and the network's USDFC token. Inspect the full `pdp_service` and capabilities after confirmation. Provider name/description use `FSUpdateProvider` with params `[name, description]`. A successful registration/update call is submission evidence; reconcile registry state before repeating after a lost response.

Registry mutations at this baseline send directly through the ETH client and log the transaction hash; they do not necessarily appear in `MessageQueueSummary`. Recover the hash from targeted logs around the request (`Sent Register Service Provider transaction` or the corresponding update message), then use the configured chain endpoint's read-only transaction/receipt lookup and registry readback. An empty queue or currently unregistered status does not exclude an earlier pending submission. If its outcome cannot be established, retain the pending action and report the specific reconciliation gap instead of resubmitting blindly.

`RestartFailedTask` takes `[taskID]` and needs a functioning task engine attached to the GUI handler. At this baseline, Skiff starts its scheduler but its GUI dependency adapter does not populate `TaskEngine`, so an otherwise eligible retry returns `task engine not available`. Verify the deployed release before relying on this operation; use a documented supported procedure or report the limitation. See [operations.md](operations.md) for retry decisions and [maintenance.md](maintenance.md) for node controls (`Cordon`, `Uncordon`, `Restart`, `AbortRestart`, each taking `[machineID]`).

## Configuration HTTP API

These are REST requests, separate from JSON-RPC:

| Purpose | Request |
| --- | --- |
| Discover shape/defaults/layers | `GET /api/config/schema`, `GET /api/config/default`, `GET /api/config/layers` |
| Read a layer | `GET /api/config/layers/{layer}` |
| Save a layer | `POST /api/config/layers/{layer}` with `Content-Type: application/json` and the complete edited layer document |
| Inspect prior state | `GET /api/config/history/{layer}`, `GET /api/config/history/{layer}/{id}` |

Read schema and the current layer, retain a protected copy, edit only intended fields, then compare against a fresh read before saving to detect concurrent edits. Preserve all unrelated fields: POST is a complete save, not a partial patch, and omitted settings can revert to defaults. Read back and compare the intended fields after saving. Configuration and its history can contain API credentials: process full documents locally, return only the needed non-secret fields to the model, and preserve secret fields in the saved document. Keep raw payloads out of transcripts and reports.

Skiff runs from `base`; its schema exposes a subset of Curio configuration and the save adapter preserves other Curio sections. Full Curio can combine layers; discover the node's active layers before editing. Schema dynamic values use their ordinary JSON value, not a wrapper object. Settings marked “Updates will affect running instances” can reload; static settings need a controlled restart. Confirm matching-release reload behavior and effective service state: a successful save or readback establishes stored configuration, not that every running component applied it.

## Source navigation

Use the corresponding deployed tag/revision at these public paths. Links pin the baseline above so this reference remains usable outside a local checkout:

- [RPC handlers and response structs](https://github.com/filecoin-project/curio/tree/6b078a0f4528d6aad239cd41d89809e748fac5c3/web/api/webrpc), [GUI JSON-RPC client](https://github.com/filecoin-project/curio/blob/6b078a0f4528d6aad239cd41d89809e748fac5c3/web/static/lib/jsonrpc.mjs).
- [Registry/wallet handlers](https://github.com/filecoin-project/curio/blob/6b078a0f4528d6aad239cd41d89809e748fac5c3/web/api/webrpc/pdp.go), [registration GUI](https://github.com/filecoin-project/curio/blob/6b078a0f4528d6aad239cd41d89809e748fac5c3/web/static/pages/pdp/register.mjs), [wallet parser](https://github.com/filecoin-project/curio/blob/6b078a0f4528d6aad239cd41d89809e748fac5c3/pdp/wallet/keys.go).
- [Registry transaction submission](https://github.com/filecoin-project/curio/blob/6b078a0f4528d6aad239cd41d89809e748fac5c3/pdp/contract/utils.go).
- [Storage attachment](https://github.com/filecoin-project/curio/blob/6b078a0f4528d6aad239cd41d89809e748fac5c3/web/api/webrpc/storage_attach.go), [dataset GUI](https://github.com/filecoin-project/curio/blob/6b078a0f4528d6aad239cd41d89809e748fac5c3/web/static/pages/datasets/datasets-list.mjs).
- [Configuration handlers/adapters](https://github.com/filecoin-project/curio/tree/6b078a0f4528d6aad239cd41d89809e748fac5c3/web/api/config), [configuration and reload guidance](https://github.com/filecoin-project/curio/blob/6b078a0f4528d6aad239cd41d89809e748fac5c3/documentation/en/configuration/README.md).
- [Skiff startup and GUI dependency wiring](https://github.com/filecoin-project/curio/tree/6b078a0f4528d6aad239cd41d89809e748fac5c3/pdpnode), [task retry handler](https://github.com/filecoin-project/curio/blob/6b078a0f4528d6aad239cd41d89809e748fac5c3/web/api/webrpc/tasks.go).
