---
description: What to collect before reporting a Curio issue.
---

# Collect debug info

When asking for help, posting the right info up front saves *hours*.

Copy/paste this checklist and fill it in.

## Environment

- Curio version:

```bash
curio --version
```

- Deployment type:
  - systemd / docker / k8s / other

- Enabled layers (and where you set them):
  - `/etc/curio.env` (`CURIO_LAYERS=...`) or docker env / service unit

## Config snapshot

If you use layered config, include the layer list and the relevant sections.

- If available in your build:

```bash
curio config view --layers <comma-separated-layers>
```

- Otherwise: paste the relevant layer TOML sections (Subsystems/Fees/Market/Ingest/HTTP/etc.).

## Logs (include context)

- systemd:

```bash
journalctl -u curio -n 2000 --no-pager
```

- docker:

```bash
docker logs --tail 2000 <container>
```

Include:
- the **exact error line**
- ~30–100 lines before/after

## DB connectivity and health

Confirm the Curio host can reach Yugabyte (YSQL):

```bash
ysqlsh -h "$CURIO_DB_HOST" -p "${CURIO_DB_PORT:-5433}" -U "$CURIO_DB_USER" -d "${CURIO_DB_NAME:-yugabyte}" -c "select 1;"
```

If you have multiple DB hosts, include the host list and whether load-balancing is enabled.

## UI / task context

- Screenshot of failing task(s)
- The task ID(s) from the UI

### Sealing issues
Include:
- `sp_id` (miner id)
- sector number(s)
- which stage (SDR/TreeD/PC1/PC2/C2/PreCommit/Commit/WdPoSt)

### Market / deal ingestion issues
Include:
- deal UUID
- piece CID
- whether offline/online
- the URL you provided (if any) and headers (redact secrets)
- indexing status (IPNI enabled? CheckIndex errors?)

### PDP issues
Include:
- PDP endpoint URL you are testing
- the command used (e.g. `pdptool ping ...`)
- whether you are testing locally or from outside the network

## Redaction

Before posting publicly:
- redact API tokens, passwords, and private IPs (if needed)
- keep message CIDs / actor IDs (they’re useful for debugging)
