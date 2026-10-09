# Maintain and upgrade a PDP provider

Use [api.md](api.md) for Curio controls and [operations.md](operations.md) for health and incident interpretation. Read the existing deployment record for upgrade authority, maintenance constraints, supervisor, shared services, and recovery procedure. Recurring execution and continuity records are governed by the main skill.

## Decide whether to change anything

1. Read the installed Curio version, build variant, network, and deployment method. Identify chain/DB versions when compatibility or the requested change depends on them. Inventory the affected components rather than auditing the entire host on every check.
2. Consult official release notes and matching compatibility guidance. Distinguish released artifacts from changes on `main` or merged PRs. Verify the selected artifact supports the host architecture, network, and variant; a full-Curio sealing fix alone does not establish urgency for healthy Skiff.
3. State the available version, applicable benefit/fix, urgency or network deadline, dependencies, and expected interruption. For a notify-only policy, finish with that recommendation. When an applicable upgrade is requested or covered by standing automatic-upgrade authority, continue through preparation and verification without asking again.
4. If a required decision exceeds that authority, prepare the exact target and recovery options first, then bring only the unresolved decision to the operator. Unavailable release information means the recommendation is unverified; preserve that uncertainty.

Select a concrete image digest/tag, package version, or binary release and record both current and target versions. A mutable `latest` tag alone does not identify what will run or what previously ran. Coordinate with an existing updater rather than adding an overlapping update mechanism.

## Prepare the affected deployment

1. Take a current health snapshot and locate the next proof obligations, owned tasks, and pending transactions. On a single provider, an empty task queue can simply mean the next proof is not due yet. Choose enough time for the change, startup/sync, validation, and recovery.
2. In a cluster, determine which nodes can actually run the displaced work and access its storage. Shared DB migrations affect other nodes. In full Curio, also consider PoSt, sealing deadlines, and location-bound work. A healthy peer does not by itself prove coverage.
3. Confirm the existing supervisor's stop/relaunch behavior and the intended maintenance sequence. Prepare the replacement artifact and required configuration before interrupting service when possible. Preserve network, wallets, storage mounts, repo identity, and dependency endpoints.
4. Follow the deployment's existing backup procedure and confirm completion before a schema-changing upgrade or major operation. Determine the recovery scope: YSQL state, YCQL piece indexes, piece payloads, and repo/storage metadata. A YSQL dump alone does not cover the whole PDP provider. Keep secret-bearing backups in their protected destination and record references, not contents.
5. Check the relevant restore evidence and release's recovery/downgrade procedure. A successful backup command does not prove restoration was tested. If the required recovery path is missing or incompatible, report the gap and settle that decision before the risky change; a routine version check does not require a new restore exercise.

Curio applies DB migrations on startup. Switching back to the old image may not reverse the migration or be compatible with it. Use the matching release's supported downgrade or restore procedure when needed, and establish the scope of shared writers before restoring. Do not repair this by editing task or pipeline rows.

## Use maintenance controls precisely

| Control            | Behavior to account for                                                                                                                                                                                                                                                                    |
|--------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `Cordon(id)`       | Marks the node unschedulable. It suppresses normal new claims and periodic work; running tasks may finish or yield cooperatively. Scheduler overrides can permit dependent work in some versions. Observe actual owned work rather than treating the acknowledgement as a completed drain. |
| `Restart(id)`      | Records an asynchronous request. The engine waits until the node owns no tasks, clears the restart request **and cordon flag**, then exits. It neither installs a new binary nor guarantees the supervisor will relaunch it.                                                               |
| `AbortRestart(id)` | Clears a pending request. Verify the node is still running; it cannot undo an exit that already happened.                                                                                                                                                                                  |
| `Uncordon(id)`     | Allows ordinary scheduling again. Verify resulting work and liveness; API success alone does not establish recovery.                                                                                                                                                                       |

Do not rely on `Restart` alone to drain: its request does not set the cordon flag. When this control fits the maintenance plan, cordon first and observe the drain. Account for automatic uncordoning before restart exit: scheduling may resume immediately when the supervisor relaunches the process. Use the established supervisor/deployment procedure to control replacement timing, rather than racing an automatic restart.

Cordoning a single provider also delays new proving work; it is not a way to keep proofs running during downtime. Read the deployed release's handler/scheduler behavior if drain semantics affect the plan. Watch the next deadline while waiting for owned work to clear; if the change no longer fits, abort/defer safely and restore normal scheduling.

## Perform the authorized change

1. Coordinate with other active maintenance and use the prepared deployment procedure. For a rolling change, proceed one compatible node at a time and verify its coverage before moving on; for a single node, honor the chosen downtime budget.
2. If draining is appropriate, cordon the target and observe completion/yield of owned work. Watch pending transactions through Curio's existing sender/watcher controls. Allow critical work to finish rather than forcing repeated restarts to clear a queue.
3. Stop/relaunch through the established supervisor and install the pinned artifact or apply the intended host/configuration change. Preserve volumes and storage mappings. Use targeted status/log checks if startup fails; repeat a failed action only when new evidence supports it.
4. If the change fails, take the prepared recovery branch appropriate to the actual migration state. Report an unsafe or unavailable recovery path explicitly and preserve data. An old image is a recovery option only when its schema compatibility is established.

A dependency upgrade is its own compatibility decision, even if shipped in the same Compose stack. Change only dependencies required by the selected release or authorized maintenance scope. An API configuration save can require restart; verify the deployed setting's reload behavior through [api.md](api.md).

## Verify before calling it complete

Verify what the change affected, using the baseline as comparison:

- Running version/digest and intended network/variant match the target; supervisor reports a stable process.
- Admin API is ready; chain is current; attached storage is reachable with expected capacity; relevant alerts and pending work are understood.
- Read the actual cordon/restart state after relaunch. Uncordon when still required by the chosen procedure and verify scheduling/progress resumes.
- Recheck the external route if networking, TLS, tunnel, or HTTP settings changed. Use the affected upload/retrieval workflow when those paths changed and testing is authorized.
- For active datasets, verify timely proof progress. If the next proof is later, record “service restored; next proof verification pending” rather than claiming that proof succeeded.

When a verification fails, retain an active incident with the evidence and next recovery action. When checks pass, report the change, resulting version, observed outcome, and any later verification still due. Finish routine checks without installing a scheduler or claiming future upgrade monitoring unless it is actually configured under the main skill.

## Source anchors

Use the deployed release/tag for behavior; inspect these upstream entry points when needed:

- [Curio releases](https://github.com/filecoin-project/curio/releases) and [version/migration guidance](https://github.com/filecoin-project/curio/blob/main/documentation/en/versions.md).
- [Node maintenance](https://github.com/filecoin-project/curio/blob/main/documentation/en/administration/node-maintenance.md), [GUI maintenance handlers](https://github.com/filecoin-project/curio/blob/main/web/api/webrpc/cluster.go), [scheduler restart behavior](https://github.com/filecoin-project/curio/blob/main/harmony/harmonytask/harmonytask.go), [cooperative task yielding](https://github.com/filecoin-project/curio/blob/main/harmony/harmonytask/task_type_handler.go).
- [Yugabyte backup/restore](https://github.com/filecoin-project/curio/blob/main/documentation/en/administration/yugabyte-backup.md), [PDP storage and recovery scope](https://github.com/filecoin-project/curio/blob/main/documentation/en/curio-pdp.md), [PDP Compose supervisor settings](https://github.com/filecoin-project/curio/blob/main/docker/skiff/docker-compose.yaml).
