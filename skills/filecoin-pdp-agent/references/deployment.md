# Deploy or adopt a PDP provider

Use this workflow to turn the selected release's official deployment instructions into a verified provider. Obtain the actual installation commands, prerequisites, profile files, configuration inputs, and API parameters from those instructions. Use [api.md](api.md) as a versioned API lookup aid and [maintenance.md](maintenance.md) when existing datasets or shared services make an interruption consequential.

## 1. Identify the deployment and its authoritative procedure

Read the operating record and inspect the target host separately from the agent host. Identify the OS/architecture, runtime/supervisor, network, allocated resources, persistent mounts, and existing installation. If the admin API responds, inspect version/variant, setup status, storage, wallet, registry and current workload. If it does not, inspect the recorded service and targeted startup logs before deciding that a new deployment is needed.

Adopt existing Skiff or full Curio in place. Retain active configuration layers and account for other cluster workloads. For a new PDP-only provider, select the supported PDP deployment path documented for the chosen release and host capabilities.

Read the official deployment guide and applicable release notes. Obtain the matching release's deployment artifacts and configuration guidance; resolve any difference from examples bundled with this skill before executing the affected step. Record the chosen artifact/version, source references, host-specific procedure and current incomplete stage. Use the entrypoint's documentation fallback when the site is unavailable or unversioned.

Obtain the deployment files yourself. When the selected guide starts from a source checkout, install Git if missing and clone the [official Curio repository](https://github.com/filecoin-project/curio) into the target's operational workspace, selecting the chosen release's tag or commit. Reuse an appropriate existing checkout without discarding local changes. Keep the checkout, deployment configuration and persistent service data outside the installed skill directory; place them on the storage required by the guide and record their locations. A checkout supplies deployment files and documentation; use published images or binaries when the guide supports them, and build only when the selected procedure requires it.

**Advance when:** the target, existing identities, intended network/release and applicable procedure are known. Resume from the incomplete stage rather than repeating completed setup.

## 2. Prepare resources and start the selected deployment

Install missing prerequisites for the selected procedure using their supported host-specific installation instructions, such as the container engine, Linux VM or Compose tooling when required. Reuse working installations and verify tools in the target host's execution context. Complete automated setup within the user's delegation; request human action only for an unavailable credential, permission or genuinely interactive installation step, and continue independent preparation.

Follow the selected guide for service startup. Before execution, resolve its commands against the actual host, allocations and artifacts:

- Verify architecture support for every required component. For containers, inspect the daemon/VM as well as the host and the selected image manifests. A working container runtime alone does not establish image compatibility.
- Map the documented persistent state and piece-data locations to the allocated disks. Verify mount identity, usable space, permissions and runtime visibility. Preserve these mappings across service recreation; account for multiple paths sharing one filesystem.
- Determine which database and chain services the profile actually starts and which existing services it uses. Apply the guide's environment/configuration precedence. Selecting an external endpoint does not by itself demonstrate that a bundled service or startup dependency has been removed.
- Use the guide's actual profile paths, settings, ports and network selection. Keep the administration interface private. Inspect resolved configuration without exposing credentials, then start the authorized components under the existing supervisor.

On Linux, check the applicable host mounts and service manager. On macOS, also check the Linux VM's resource allocation, volume sharing, networking and lifecycle where used. Preserve a functioning supported installation instead of replacing it to match a preferred recipe.

**Advance when:** required services stay running, persistent mappings match the record, and the private admin interface reports the intended build/network. Observe progressing initialization. For repeated exits, address the first actionable startup error before trying the same launch again.

## 3. Verify chain access and attach the allocated storage

Use the release's supported status interfaces to check the selected chain network, freshness and required ETH/contract RPC access. A listening RPC socket or running Curio process is insufficient. Inspect the effective service environment and configuration if the endpoint in use differs from the intended one. Continue independent setup while snapshot import or synchronization progresses; investigate stalled progress or explicit authentication/network errors.

Discover existing storage and candidates through the GUI API. Match container-visible paths to their intended host disks, and attach only missing selected paths through the supported operation. Attachment is distinct from mounting or listing a candidate; it establishes Curio's persistent storage identity. Target the node that owns the path, since a GUI connection to another machine does not make a local storage operation cluster-wide.

**Advance when:** attached storage is healthy and accessible under the service's effective credentials, with the expected identity and capacity. Obtain successful chain/contract observations before dependent paid operations; report failed reads as unknown.

## 4. Establish the funded provider identity

Inspect public wallet status before creating or importing anything. Preserve an existing provider key. Use the release's supported wallet operation and the protected input/output handling described in [api.md](api.md); keep recovery material in the delegated secret storage and public identifiers in the operating record.

Verify the network and known available funds against the release's required transactions and the user's operating allowance. Replenish from an authorized source when covered; otherwise provide the public address and exact funding need. A balance lookup failure does not establish an empty wallet. Reconcile an interrupted key operation through status before attempting it again.

**Advance when:** the intended wallet is configured, its recovery reference is retained, and known funding covers the next authorized operations. Endpoint preparation can continue while funding or chain access is pending.

## 5. Configure and verify the effective public endpoint

Read the release's public-service/TLS guidance, current active configuration, and runtime schema. Determine the actual TLS terminator, public hostname, listener/routing, and configuration precedence. Initialization inputs may only seed an empty installation; verify how an existing deployment accepts updates.

Preserve unrelated values when saving configuration, following the actual API's save/patch semantics. Determine whether each changed field reloads or requires a restart. If interruption is required, follow [maintenance.md](maintenance.md); a restart is not an automatic step for a dynamically applied setting.

Verify the effective service URL and listener after applying the change, then test the documented public health route through the intended DNS/TLS path. Use an independent external vantage point when available. If only provider-local probing is possible, record local readiness with internet availability unverified. Interpret health responses alongside fresh operational observations; cached health and reachability do not establish successful storage/proving.

**Advance when:** the intended endpoint is applied and the available probes support the stated readiness claim. Complete this verification before publishing or updating the provider's service URL.

## 6. Register or reconcile the offering

Read registry state first. For an existing provider, change only the intended differences. For a new one, obtain the release's registration arguments, units, required funds, offering defaults and confirmation procedure from its documentation. Resolve any missing published identity information from the user's mandate.

When deploying or taking over a provider for ongoing agent operation, identify it with the single custom registry capability `curioOperator` set to `filecoin-pdp-agent`. Read current capabilities first; if this value already matches, no marker update is needed. Preserve all unrelated capabilities and offering values. Include the marker in the initial registration or another already-required offering update when the deployed API supports it; otherwise follow the versioned procedure in [api.md](api.md). Keep it stable across sessions and upgrades, without adding model, runtime, skill-version or heartbeat metadata. Submit any required update within the delegated transaction budget and verify its readback; a failed marker update remains pending independently of provider health.

Derive offered capacity from the actual allocation and obligations, allowing working space for the offered piece sizes and concurrent ingestion. Do not round an allocation up to satisfy a field requirement. A local readiness threshold does not validate an advertised offering. Inspect which fields the deployed update operation actually changes and read the resulting offering back.

Record submission identifiers and observe the receipt plus registry outcome through the documented interfaces. Some registration paths may submit directly rather than through Curio's task message queue; use the applicable transaction history or targeted logs for reconciliation. An empty queue, lost response or momentary unregistered status cannot establish that an earlier submission never occurred. Keep unresolved submissions pending instead of repeating them blindly.

**Advance when:** confirmation and registry readback show the intended active provider, endpoint and offer. If required behavior or a prior submission's outcome remains unresolved, report that specific gap while continuing independent setup.

## Completion

Verify the selected services, chain, storage, wallet, public route and registry, and account for any task/message/alert condition preventing service. State unverified stages precisely. An empty provider can be ready for its first customer without proof history. With existing datasets, check proving progress and upcoming obligations.

Follow authorized test data through upload, confirmed piece membership, retrieval and proof confirmation before claiming those paths were exercised. Record verified deployment state and persistent service supervision. Establish recurring agent operation only when included in the user's mandate, using the entrypoint's continuity instructions.

## Official starting points

Use these to locate the applicable release guidance, following the site's navigation or release tree if pages move:

- [Curio documentation](https://docs.curiostorage.org/), including [Curio-PDP](https://docs.curiostorage.org/curio-pdp) and [Skiff](https://docs.curiostorage.org/skiff-binary).
- [Curio releases](https://github.com/filecoin-project/curio/releases) for published versions, artifacts and migration notes.
- [Documentation source](https://github.com/filecoin-project/curio/tree/main/documentation/en) for the repository copy; select the installed/target tag before following version-dependent instructions. Use the deployment artifacts linked by that release's guide.
