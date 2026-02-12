---
description: Operational troubleshooting for YugabyteDB when used as Curio’s control plane.
---

# YugabyteDB troubleshooting

Many “Curio issues” are actually **control-plane issues** (YugabyteDB health, schema, connectivity, or performance).

Before doing anything risky:
- take a backup (see [YugabyteDB Backup](yugabyte-backup.md))
- stop Curio writers if you are restoring/downgrading

---

## 1) Quick health checklist

On the Yugabyte nodes:
- `yugabyted status`
- check `yb-master` and `yb-tserver` logs for FATALs

From a Curio node:

```bash
ysqlsh -h "$CURIO_DB_HOST" -p "${CURIO_DB_PORT:-5433}" -U "$CURIO_DB_USER" -d "${CURIO_DB_NAME:-yugabyte}" -c "select 1;"
```

If the above is slow/intermittent, fix DB health first.

---

## 2) Connection string / multi-host setups

Curio commonly uses a comma-separated host list:

```bash
CURIO_DB_HOST=yugabyte1,yugabyte2,yugabyte3
CURIO_DB_PORT=5433
CURIO_DB_NAME=yugabyte
```

Tips:
- Ensure all hosts are reachable from all Curio nodes.
- If you disable load balancing in Curio, it may pin to the first host; keep that host stable.

---

## 3) ulimit / file descriptor exhaustion

Symptoms:
- DB is unstable under load, or tserver logs mention file descriptor issues.

What to do:
- Ensure you applied recommended `nofile` limits (see PDP guide and Yugabyte docs).
- Restart services after changing persistent limits.

---

## 4) Upgrade / downgrade safety

Rules of thumb:
- Back up before changing Curio or Yugabyte versions.
- Stop Curio before restoring a DB dump.
- If a schema migration fails, capture:
  - the migration filename
  - full error output
  - Yugabyte version

Common migration blocker:
- `Rewriting of YB table is not yet implemented (SQLSTATE 0A000)`

---

## 5) Common SQLSTATE errors in support

### `SQLSTATE 23505` (duplicate key)
Often indicates:
- concurrency/retries inserting the same identity row.

Action:
- verify whether the app is progressing or stuck.
- if stuck, correlate with task errors and consider a targeted restart of the relevant Curio subsystem.

### `relation "curio.<table>" does not exist`
Usually indicates:
- schema migrations didn’t run, or you are connected to the wrong DB name/schema.

Action:
- confirm `CURIO_DB_NAME` and that schema `curio` exists.

---

## 6) Backups

Follow the backup guidance here:
- [YugabyteDB Backup](yugabyte-backup.md)
