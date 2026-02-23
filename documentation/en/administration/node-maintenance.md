---
description: >-
  How to safely cordon Curio nodes for maintenance without disrupting in-progress sealing pipelines.
---

# Node Maintenance & Cordoning

## What cordoning does

Running `curio cordon` (or setting `unschedulable = true` in the WebUI) tells the Harmony scheduler to **stop scheduling new tasks** on a node. Tasks that are already running will finish, but no new work will be picked up.

This is intentionally simple: cordon → wait for running tasks to finish → do maintenance → restart → uncordon.

## How cordoning affects the sealing pipeline

Cordoning blocks **all** new task scheduling on the node, including pipeline continuation tasks like TreeD, TreeRC, SyntheticProofs, and Finalize. If a sector's data is **location-bound** to the cordoned node (the sector cache lives on local storage), those follow-up tasks cannot run on another node either.

**What this means in practice:**

- If you cordon a node that has sectors mid-pipeline (e.g., SDR completed but TreeRC not yet started), those sectors will be **paused** until the node is uncordoned.
- The sectors are not lost — they will resume once the node is uncordoned and the scheduler picks them up again.
- However, if the node stays cordoned for too long, sectors may **expire** (miss their precommit deadline), wasting the SDR work.

{% hint style="warning" %}
**Non-batched sealing operators:** SDR takes many hours. If you cordon a node and leave it cordoned past the sector's precommit deadline, that SDR work is lost. Plan maintenance windows accordingly.
{% endhint %}

## Recommended workflows

### Quick maintenance (restart, upgrade, config change)

For short interruptions where downtime is minutes, not hours:

1. **Check the pipeline** — in the WebUI, verify what stages are in progress on the node.
2. `curio cordon <node>` — stop new work from being scheduled.
3. **Wait** for currently-running tasks to complete (watch the WebUI or logs).
4. Do your maintenance (restart, upgrade, etc.).
5. `curio uncordon <node>` — resume scheduling.

Pipeline sectors that were paused (waiting for their next stage) will resume automatically after uncordon.

### Planned extended maintenance (hours)

If the node will be down for an extended period:

1. **Stop starting new sectors** first — pause deal intake or CC sector creation so no new SDR work begins on the node.
2. **Wait for in-progress pipelines to clear** — let sectors finish through Finalize before cordoning. Monitor via the pipeline view in the WebUI.
3. `curio cordon <node>` once pipelines are drained.
4. Do your maintenance.
5. `curio uncordon <node>` when ready.

### Decommissioning a node or long outage

If you know a node will be offline for a long time (longer than precommit deadlines):

1. **Attach the node's storage to another node** — use `curio attach` on a different machine to make the sector data accessible elsewhere.
2. Other nodes with access to the storage paths can then pick up the remaining pipeline tasks.
3. Cordon and shut down the original node.

See [Storage Configuration](../storage-configuration.md) for details on attaching storage.

## Key points to remember

| Aspect | Behavior |
|--------|----------|
| Running tasks | Finish normally on the cordoned node |
| New task scheduling | Blocked — no new work starts |
| Location-bound pipeline tasks | Paused until uncordon (data is on local storage, other nodes can't run them) |
| Sector data | Safe — nothing is deleted or moved by cordoning |
| Precommit deadlines | Still apply — sectors can expire if cordoned too long |

## Common mistakes

- **Cordoning during active SDR without a plan to uncordon quickly.** SDR is the longest pipeline stage. If you cordon right after SDR completes, the follow-up stages (TreeD, TreeRC, etc.) are blocked. Either wait for the full pipeline to clear first, or ensure you'll uncordon in time.

- **Forgetting to uncordon after maintenance.** The node will sit idle, and any paused pipelines will remain stuck. Set a reminder or check the WebUI after maintenance.

- **Cordoning when you should be migrating storage.** If the node is going away permanently, cordon alone won't help — you need to move or re-attach the storage paths so other nodes can access the sector data.
