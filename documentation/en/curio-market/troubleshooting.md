---
description: Troubleshooting Curio Market ingestion, indexing, retrieval, and IPNI.
---

# Curio Market troubleshooting

This page targets the common “deal is stuck” / “indexing is failing” cases seen in support.

Before proceeding, collect the basics:
- [Collect debug info](../troubleshooting/collect-debug-info.md)
- deal UUID + piece CID
- failing task IDs from the UI

---

## 1) Deal stuck at indexing / “CheckIndex” errors

What CheckIndex is:
- Curio runs task-based indexing and health checks; a recurring task checks for missing indexes / announcements and schedules follow-up work.
- Implementation reference: `tasks/indexing/task_check_indexes.go` (task `CheckIndex`).

If you see errors in CheckIndex:
- Determine whether indexing is still progressing (tasks completing) vs stalled.
- Many “indexing failures” are actually DB health/latency problems. Verify Yugabyte health first.

### Common error: `duplicate key value violates unique constraint ... (SQLSTATE 23505)`

What it often means:
- Concurrent retries inserted the same identity row.

What to do:
1) Collect: deal UUID, piece CID, task ID(s), and the full error span.
2) Confirm whether the failing rows keep reappearing or are transient.
3) If stuck, restart only the market/indexing layer process (not the whole cluster).

---

## 2) IPNI failures

Symptoms:
- Indexing seems OK locally, but IPNI publishing is failing.

Checklist:
- Outbound HTTPS works from Curio host.
- The “public URL” for your provider is correct (reachable from the internet).
- DNS + firewall allow inbound connectivity to your HTTP server.

Temporary mitigation:
- If you need onboarding to proceed while IPNI is unstable, you may temporarily disable IPNI publishing (only if supported by your release/config). Document and keep track of the change so it isn’t forgotten.

---

## 3) ParkPiece: `no data URL found for piece_id`

What it means:
- Curio cannot find the configured URL+headers for fetching the piece data.

Most common causes:
- The URL was never registered, or it was registered on a different node than the one running ParkPiece.
- Ingest layer is not enabled on the node you think.

What to collect:
- deal UUID, piece CID
- whether offline/online deal
- the exact command used to add the URL (if online)

---

## 4) Retrieval looks broken, but indexing is fine

Checklist:
- Ensure unsealed data is available on at least one node that can serve retrieval.
- Confirm the Curio HTTP server is enabled and reachable.
- If behind a reverse proxy, confirm TLS delegation mode matches your config.

See also:
- [Curio HTTP Server](curio-http-server.md)
