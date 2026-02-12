---
description: Troubleshooting indexing/IPNI and the CheckIndex task in Curio.
---

# Indexing & CheckIndex troubleshooting

This page is built from recurring support incidents in `#fil-curio-help`.

## What indexing is (plain English)

Curio maintains a local index so retrieval can answer: “Which piece contains this multihash, and at what offset?”.

If you use IPNI, Curio may also publish advertisements so the wider retrieval ecosystem can discover your content.

## What `CheckIndex` does

`CheckIndex` is a background task that periodically checks whether indexing and announcements are complete and schedules follow-up work when something is missing or needs retrying.

Code reference:
- `tasks/indexing/task_check_indexes.go`

## First response playbook (safe steps)

When indexing looks broken, do these in order:

1) Check DB health first
- Many “indexing errors” are actually Yugabyte slowness/unavailability.
- See: `documentation/en/administration/yugabyte-troubleshooting.md`

2) Check whether you *have unsealed data* available
- Re-indexing typically requires access to an **unsealed copy**.
- If your cluster intentionally does not keep unsealed data, expect errors like “no unsealed copy found” when attempting reindex.

3) Reduce overload (temporary)
- If tasks are piling up, temporarily reduce indexing concurrency on the node(s) that run market/indexing.
- Knobs live in Curio configuration (see `configuration/default-curio-configuration.md`).

## Common symptoms and what they usually mean

### Many `CheckIndex` tasks at once / “storm”

Typical causes:
- DB health issues causing retries.
- A node repeatedly restarting and re-enqueueing work.
- A configuration mismatch causing work to never complete.

What to collect:
- counts of tasks by type
- the oldest CheckIndex task ID + its error logs

### `SQLSTATE 23505 duplicate key value violates unique constraint ...`

Plain-English meaning:
- Two workers attempted to create the same “identity” row concurrently.

What to do:
- Determine if it’s transient noise (pipeline still progresses) or if the same deal/piece is stuck.
- Collect: deal UUID, piece CID, task IDs, and the full error span.

### `piece missing in indexstore` / `no unsealed copy of sector found for reindexing`

Plain-English meaning:
- Curio is being asked to build/serve an index for content, but cannot find the bytes required to construct it.

What to do:
- Confirm at least one storage path allows `unsealed` and has the data.
- Confirm the node doing indexing can access the path.

## Quick DB inspection (YSQL)

> These queries assume default schema `curio`.

Count tasks by name:

```sql
select name, count(*)
from curio.harmony_task
group by name
order by count(*) desc;
```

List recent CheckIndex tasks:

```sql
select id, posted_time, update_time, owner_id, retries
from curio.harmony_task
where name = 'CheckIndex'
order by posted_time desc
limit 50;
```

## Last resort: deleting stuck CheckIndex tasks (use with caution)

Only do this if you understand the impact and have a DB backup.

1) Stop Curio on the node running indexing/market.
2) Delete tasks:

```sql
delete from curio.harmony_task where name = 'CheckIndex';
```

3) Start Curio again and monitor task creation rate.

If the storm returns immediately, the root cause is still present (DB health, configuration, or missing data).
