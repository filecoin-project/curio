# Inspect and operate a PDP provider

Use this reference for health checks, incidents, payment risk, earnings, and stopping service. Read [api.md](api.md) for connection and method details; use [maintenance.md](maintenance.md) for software or host maintenance. Apply these recipes to the deployed release and variant: the linked source describes behavior to verify, not a promise that every release exposes it.

## Establish current health

1. Recover the target and standing authority from the deployment record. Identify the running version/variant if it is unknown or changed. Keep a requested check read-only unless remediation is requested or already authorized.
2. Make one bounded baseline pass: `PDPGuideStatus`, `AlertOngoingList`, `ChainStatus`, `ClusterMachines`, `StoragePathList`, and `PDPProvingStatus`. Run independent reads concurrently. Add `MessageQueueSummary` for stalled work and dataset/task details for the affected identifiers; a routine check need not enumerate all history, datasets, or logs.
3. Record observation time, affected identifiers, next relevant deadline, and whether work is advancing. Compare a second observation only where progress is uncertain, allowing for the operation's retry interval or chain confirmation. Schedule follow-up early enough to leave time for remediation and confirmation before the deadline; escalate immediately when that margin is doubtful.
4. Resolve the result into healthy, expected waiting, actionable problem, or unknown. For a problem, continue with the incident recipe. For unknown, name the failed observation and its consequence; failed RPC calls are not empty results.
5. Report current impact and the next action. If recovery depends on a later proof window, retain that verification as pending. Future monitoring requires the actual schedule described in the main skill.

Interpret the observations together:

| Observation | Operator interpretation |
| --- | --- |
| No proof activity | Check active datasets first. An idle provider has nothing to prove; an active dataset near a missed deadline needs attention. |
| Historical failed task | Correlate its task/dataset with current ownership, retries, later success, and alerts. A failed attempt may already have recovered. |
| Queue entries | Use total pending counts and age/progress. `MessageQueueSummary` returns only a recent sample, currently up to 20 of each kind; the sample is neither the total nor a complete failed-send history. |
| Successful `/pdp/ping` | It checks wallet configuration and the alert task's last computed problem state. It is not a fresh end-to-end storage test. The guide probes from the provider itself; distinguish that from an external client probe. |
| Wallet marked funded | This establishes a positive balance, not adequate operating runway. Compare known balance with the operator's reserve policy and expected workload. An unavailable balance is unknown. |
| Old error reappears in a report | Compare timestamp and identifiers before declaring recurrence. Healthy current checks do not rule out a new transient upload failure that those checks did not exercise. |

Initial readiness and successful customer operation are separate milestones. With authorized test data and spending, verify upload completion, confirmed piece membership, the intended retrieval path, and a successful proof when due. A registration, transaction hash, or green setup checklist alone does not establish those outcomes.

## Diagnose and recover

Use this loop for any incident, including symptoms absent from the examples below.

1. Establish expected versus observed behavior, current customer impact, and the nearest proof, payment, or capacity deadline. Distinguish a new failure from historical attempts that already recovered.
2. Bound the cluster scope: affected datasets, pieces, tasks, transactions, machines, storage paths, and shared dependencies. Identify the responsible service and current task owner where observable. If an API is unavailable, use the deployment record and host observations; keep unknown ownership explicit while continuing diagnosis.
3. Correlate current API state by identifiers and timestamps with recent software, configuration, and infrastructure changes. Use targeted logs for the relevant interval; preserve the original operation identifiers across retries and replacements.
4. Rank plausible causes and select read-only checks that distinguish them. Compare failing and healthy components where useful. Prefer supported API observations; use host checks for supervisor, connectivity, mounts, and targeted logs. Revise the explanation when evidence contradicts it or leaves a dependency unknown.
5. Reuse a verified procedure when its release and prerequisites still match. For an unfamiliar or unresolved failure, consult matching official documentation, error meanings, release notes, and relevant known fixes. Check the remedy's prerequisites against the evidence before applying it.
6. Apply the smallest supported correction within standing authority through semantic APIs or the existing host procedure. Keep inspection-only requests read-only. Reconcile ambiguous mutation results before retrying; leave protocol scheduling, transaction replacement, and lifecycle cleanup to Curio. Observe the effect before another change; stop repeating an ineffective action without new evidence.
7. Verify the original failing workflow and required persisted/confirmed outcome. Record impact, evidence, changes, and remaining uncertainty. If verification awaits a proof window or confirmation, record the next observation and time; arrange follow-up only through an available scheduler. If blocked or the deadline margin is doubtful, escalate the concrete missing capability, decision, or help needed; continue unaffected authorized work.

These examples guide evidence collection and completion checks:

| Symptom | Useful evidence and recovery check |
| --- | --- |
| Admin API unavailable | Supervisor/startup state, tunnel, DB, chain startup; verify API readiness, chain freshness, storage, and affected work. |
| Accepted upload missing | Upload/dataset state, receipts, sync, original IDs; reconcile before resubmission; verify confirmed membership and retrieval. |
| Proof overdue/failing | Deadline, chain freshness, storage, attempts, error category; verify a confirmed successful proof or keep verification pending. |
| Transaction pending/replaced | Original wait, replacement hash, age, funding, receipt/nonce evidence; verify receipt and resulting dataset/payment state. |
| Disk filling/deletion pending | Free space, growth, references, pending additions, cleanup; use authorized capacity/intake controls and verify headroom; never remove customer pieces or edit reference counts to free space. |
| Endpoint/retrieval failing | Probe location, DNS/TLS/tunnel, HTTP, affected piece; verify the failing external route with authorized data. |
| Payment/termination alert | Dataset detail, lifecycle stage, chain state, timestamp; distinguish grace, client termination, and proving failure; verify progression. |

For credible unresolved software behavior, use the optional [GitHub workflow](github.md). An incident alone does not warrant an upstream report.

## Leave protocol execution with Curio

Curio owns proof scheduling, transaction sending/replacement, receipt watching, settlement, reconciliation, and cleanup. The agent supplies diagnosis, operational changes, release decisions, and human communication around those mechanisms. Use existing semantic APIs; do not edit pipeline rows or build a competing retry loop.

- **Proof errors have distinct meanings.** Too-early proofs retry through Harmony; an already-submitted proof can complete the current attempt; missing proving initialization can reset the schedule; terminal dataset/payment errors stop proving and initiate termination. Authorization, proof-validation, and unexpected contract-invariant errors need investigation. Check the current outcome before requesting any supported task retry.
- **Settlement is already periodic.** In the inspected implementation an hourly singleton checks rails, skips tracked in-flight settlements, and follows partial settlements after confirmation. Live rails normally settle on a three-day cadence shortened by the lockup safety margin. Absence of a settlement on every check is expected; compare rail progress with its actual due state.
- **A missing receipt can remain pending legitimately.** Curio reconciles stale create/add waits with chain state and client nonces before classifying them as lost. An ETH transaction marked confirmed uses the watcher's confidence threshold, not chain finality.
- **Reorg recovery is incomplete in some paths.** The reorg checker detects canonical-inclusion changes, but several rollback branches only log the needed change; deletion cases can require manual recovery. Preserve the transaction/dataset evidence and seek the documented recovery path when local state remains inconsistent.
- **Deletion is staged.** Settlement and termination watchers gate dataset deletion; piece GC checks chain liveness, remaining references, pending additions, and retention. A terminated service or requested deletion does not mean the files are already eligible for manual removal.

Task histories and singleton status can establish whether these mechanisms are running. A stalled prerequisite or exhausted retry is an incident to diagnose, not permission to bypass coordination.

## Payments and earnings

For a requested financial check, use public wallet status, `PDPDashboardFinancial`, and relevant dataset payment APIs. Keep FIL gas, USDFC payments, wallet balances, settled receipts, and estimates distinct. A rolling 30-day figure does not answer a calendar-month request; use matching-period evidence or report that gap. Include electricity, disks, network, RPC, or agent costs only when supplied or measured, with the accounting period.

The inspected dashboard has material limits that change the report:

- `income30dUsdfc` sums settlement events from locally tracked confirmed transactions; it is not a complete external wallet ledger or proof of withdrawal to the operator.
- `accruedUnsettledUsdfc` currently reads the payee account's `LockupCurrent`, not a per-rail calculation of earned but unsettled income. Report it as the dashboard's value with that caveat; establish rail-level entitlement before calling it collectible earnings.
- Gas expense extrapolates seven-day transaction volume and sampled receipt costs to 30 days. Values are formatted and financial results are cached, currently for 15 minutes. Keep the estimate label and avoid frequent polling for false precision.
- `…` and failed reads are unavailable data. Some registry/account lookup failures leave zero-valued income components without an API error; corroborate zero when it would drive a spending or shutdown decision.

Use the existing reserve and payout policy. Replenish operating funds from an explicitly delegated source when authorized; otherwise request funding early enough to protect upcoming work. A positive wallet balance is not permission to spend without limit or use another wallet.

Distinguish agent-initiated transfers from Curio's autonomous transaction costs. Track measured spending for the user's budget period and pending exposure; the dashboard estimate cannot enforce a monthly allowance. If the user requires a hard cap, verify supported enforcement before promising it. Without an established reserve threshold, report the known balance and runway uncertainty rather than inventing a sufficient reserve.

For payment risk, inspect the affected dataset's detail and distinguish `grace`, `terminating`, and `pending_delete`, including the stated reason. The inspected implementation allows temporary grace after the lockup threshold and waits for settlement/termination conditions before deletion. Use the actual projected epoch and mark an unknown deletion date as pending.

At-risk scans are bounded and cached; defaults exclude datasets below 100 KiB and entries whose projected deletion is already past. Individual chain-resolution failures may be omitted. Check scan completion, filters, and errors, and use dataset detail for an implicated dataset. An empty scan alone cannot establish universal payment health.

## Stop or unwind service

When the operator requests a stop or the budget is exhausted, first identify active datasets, proof/payment obligations, pending transactions, and which storage must remain available. Use existing supported controls to limit new commitments within authority; discovery or unregistration does not settle existing obligations.

Present or execute the authorized migration/termination plan while retaining required data and operation until its chain and cleanup conditions are met. If a needed control is unavailable, state the concrete limitation and operator decision. A stopped container is not a completed provider exit, and a restart loop is not a response to an exhausted budget.

## Source anchors

Use these upstream paths at the deployed tag/commit; `main` is for discovery:

- [Guide and reachability](https://github.com/filecoin-project/curio/blob/main/web/api/webrpc/pdp_guide.go), [ping handler](https://github.com/filecoin-project/curio/blob/main/pdp/handlers.go), [message queue](https://github.com/filecoin-project/curio/blob/main/web/api/webrpc/message_queue.go).
- [Proving behavior](https://github.com/filecoin-project/curio/blob/main/tasks/pdpv0/task_prove.go), [chain reconciliation](https://github.com/filecoin-project/curio/blob/main/tasks/pdpv0/task_chain_sync.go), [reorg handling](https://github.com/filecoin-project/curio/blob/main/tasks/pdpv0/task_reorg_check.go).
- [Settlement task](https://github.com/filecoin-project/curio/blob/main/tasks/pay/settle_task.go), [settlement policy](https://github.com/filecoin-project/curio/blob/main/lib/filecoinpayment/utils.go), [payment watcher](https://github.com/filecoin-project/curio/blob/main/tasks/pay/watcher.go), [piece GC](https://github.com/filecoin-project/curio/blob/main/tasks/pdpv0/task_piece_gc.go).
- [Financial computation](https://github.com/filecoin-project/curio/blob/main/web/api/webrpc/pdp_dashboard.go), [financial cache](https://github.com/filecoin-project/curio/blob/main/web/api/webrpc/pdp_dashboard_financial.go), [payment-risk resolver](https://github.com/filecoin-project/curio/blob/main/pdp/paymentstatus/resolver.go), [payment status](https://github.com/filecoin-project/curio/blob/main/pdp/paymentstatus/classifier.go).
