---
name: filecoin-pdp-agent
description: Run a Filecoin PDP provider with Curio as an AI agent, including deployment, monitoring, recovery, and upgrades.
---

# Filecoin PDP Agent

Take responsibility for a Curio PDP provider within the user's allocated resources and funds. Execute routine operations, verify their outcomes, and involve the human when a necessary decision or unresolved failure exceeds the delegation. Support Linux, macOS, and other compatible hosts by discovering their runtime and capabilities.

Curio owns proving, ingestion, transaction tracking and replacement, indexing, settlement, and data cleanup. Keep those services running independently of agent sessions; supervise their progress through Curio's existing interfaces.

## First-run acceptance

Before using any operational recipe, check the non-secret deployment record for the user's acceptance of risk notice `1` for this deployment. For withdrawn acceptance, follow the withdrawal procedure below. If acceptance is missing or incomplete, identify the intended deployment and allocated resources/funds from the request, show the notice below, and wait for an explicit **“I understand and accept”** or equivalent affirmative response:

> This agent can deploy and operate Curio using the hardware, storage, and funds you allocate, including access to Curio's wallet keys through local tools. Agent errors or compromised tools can expose keys, lose funds or stored data, or interrupt service. Key-handling instructions cannot guarantee isolation; secrets sent to a hosted model leave your host. Earnings are not guaranteed, and operating costs may exceed them. Do you understand and accept these risks within the deployment scope and limits stated above?

Until accepted, limit work to reading the skill, the user's request, and the non-secret acceptance record, and explaining the proposed scope and risks. A setup request, supplied funds, silence, or an unattended run does not constitute acceptance. If declined or awaiting a response, stop agent operations; leave existing Curio services running.

On withdrawal, stop provider operations immediately. The read-only restriction above explicitly permits this revocation cleanup: first persist the withdrawn status, time, and user response/reference for this deployment, retaining prior acceptance as history; then disable its recurring agent jobs and verify cancellation. Attempt cancellation even if the record write fails. Record and report any write or cancellation failure without claiming it succeeded. Queued or later invocations must honor the withdrawn status and exit before provider inspection or operations. Reconcile only unfinished revocation cleanup, and leave Curio services, proving, and their supervision running. Request renewed acceptance only when the user asks to resume.

Record the notice version, acceptance time, affirmative response or its reference, and accepted deployment scope in the user's local deployment record. Bind a fresh deployment to the requested host/allocation until public provider identifiers are known. Reuse acceptance across sessions, scheduled runs, and routine upgrades; request it again only when missing or incomplete, the scope expands, or the risk notice materially changes. Increment the notice version for material changes. An unattended run without applicable acceptance must report that human acceptance is needed through an already-authorized channel and stop; for recorded withdrawal, follow the cleanup above without repeatedly requesting acceptance.

Acceptance does not expand operational or spending authority, authorize secret disclosure, or replace the key-handling rules below.

## Start or resume

1. Recover the user's request, standing authority, and deployment record, and satisfy [first-run acceptance](#first-run-acceptance). Then inspect the host and live provider before changing anything. Distinguish a new installation, incomplete setup, existing provider, and interrupted maintenance.
2. Discover the OS/architecture, runtime or supervisor, Curio version/build, network, API endpoint, storage mappings, and cluster scope. Preserve existing identities and installations. Ask only for a missing choice that affects the next action, such as allocated storage, funding, or upgrade authority.
3. Choose the relevant recipe below. Prefer GUI JSON-RPC and configuration HTTP APIs for Curio operations; use host tools for bootstrap, mounts, service supervision, backups, software replacement, and targeted logs.
4. Inspect prerequisites, perform the authorized action, and verify its effect. After an ambiguous response, reconcile live state before repeating a mutation. Record unfinished verification and its next observation time.

For a fresh host, carry out bootstrap through the deployment recipe: install missing supported prerequisites, obtain Curio, and deploy the required services. The installed skill may be available without any Curio checkout or runtime on the target host.

## Recipes

| Situation                                                                    | Read                                     |
|------------------------------------------------------------------------------|------------------------------------------|
| Connect to Curio, select methods, interpret arguments, or save configuration | [API reference](references/api.md)       |
| Deploy, adopt, resume setup, or configure storage/wallet/public endpoint     | [Deployment](references/deployment.md)   |
| Routine check, incident, proving/payment progress, or funds report           | [Operations](references/operations.md)   |
| Release check, upgrade, restart, backup, or recovery                         | [Maintenance](references/maintenance.md) |
| Credible suspected bug with optional upstream reporting                      | [GitHub process](references/github.md)   |

Read only the references needed for the current work. Use the installed release for current operations and the selected target release for deployment or upgrade. Before an unfamiliar procedure or mutation, read the relevant official guide and release notes; obtain commands, paths, inputs, prerequisites, API arguments/units, and reload behavior from matching documentation and shipped artifacts or runtime schema. Bundled version-specific examples are lookup aids; they do not override those sources.

If the documentation site is unavailable or describes another release, use the matching packaged or repository documentation. Inspect a matching handler only to resolve a specific unanswered operational question. When required behavior remains uncertain, continue independent work and report the gap before the dependent mutation. Record the source and applicable release in the deployment record; reuse verified procedures until the version, deployment profile, schema, or observed behavior changes.

## Operating boundaries

- Carry standing authorization across sessions. Routine remediation and upgrades can run autonomously when covered; an inspection-only request remains read-only. Continue independent authorized work while awaiting a necessary human decision.
- Local tools may create/import and operate the dedicated funded provider wallet. Preserve its identity and protected recovery material. Keep spending and payouts within the user's delegation.
- Prefer an operator-controlled local or privately hosted model; check actual routing and fallbacks. Keep private keys, wallet exports, and credentials out of model context, including tool arguments/results, regardless of model hosting. Handle secret values inside local tools and return only public fields or secret references. Filter secret-bearing output before it reaches the model; keep secrets out of operating records, source control, and issue reports. Follow [wallet handling](references/api.md#wallet-handling) for creation or import.
- Respect shared Curio infrastructure and existing storage obligations. Use semantic operations that preserve task ownership, transaction tracking, and dataset lifecycle. Direct DB access, including `SQLQuery`, is exceptional and limited to a specific documented diagnostic or repair; an API gap does not authorize improvised state edits.
- Use supported operational remedies. A missing capability or unresolved bug can be reported with evidence; software changes and PRs are separate work.

## Continuity and recurring execution

Maintain one concise, non-secret deployment record in the user's operational workspace, outside this reusable skill. Reuse an existing record and store:

- Host/cluster identity, network, build and artifact version, API address, supervisor, volume mappings, storage IDs, and references to credentials/recovery material.
- Public wallet/provider identifiers, allocated resources, spending limits/reserve, upgrade policy, and reporting/notification authority already granted.
- Pending actions and their task/dataset/transaction IDs, last observations and timestamps, unresolved incidents, next checks, and any issue links with evidence already posted.
- Actual scheduler job IDs, cadence, notification destination, and last successful run.

Treat the record as operating memory; Curio and the chain remain authoritative for current provider state. Update pending actions before initiating a consequential change and record observed results afterward. On resumption, reconcile them before proceeding.

For delegated ongoing operation, register recurring invocation using the agent host's supported scheduler. Reuse or update an existing job. Schedule routine health checks separately from less frequent release/financial reviews; shorten follow-up during incidents according to progress and the nearest proof deadline. Prevent overlapping mutations for the same deployment and verify that both service supervision and agent scheduling persist as intended across reboot.

Test the scheduled invocation and its configured notification path within the user's communication authority. Record execution and delivery separately: verified jobs may be active while notification setup remains pending. If no scheduler is verified, do not claim checks will continue. Identify missing delivery capability without discarding functioning checks. A one-off inspection does not itself establish recurring work.

Report changes, recoveries, and actionable problems without repeating unchanged incidents on every run. Include the affected service, evidence, action taken, and next check or human decision. An unavailable observation remains unknown; setup readiness, verified customer workflow, and earned income are separate outcomes.
