---
description: Troubleshooting and operational playbooks for common Curio issues.
---

# Troubleshooting

This section is built from real operator issues seen in `#fil-curio-help`, with notes tied back to the actual Curio code paths.

Start here if you are:
- stuck during setup/migration,
- seeing task failures in the UI,
- seeing repeated errors/warnings in logs,
- unsure what information to collect before asking for help.

## A simple mental model (helps you debug faster)

Most Curio incidents fall into one of these buckets:

1) Chain / Lotus connectivity
- Curio can’t talk to the full node, or the full node is unhealthy/out of sync.

2) Control plane (DB + scheduler)
- Yugabyte is slow/unhealthy, schema upgrades failed, or tasks are not being scheduled/claimed.

3) Data plane (storage + retrieval)
- the bytes aren’t where the system thinks they are (paths, indexes, URLs, permissions).

If you don’t know where to start: check **DB health** first. A sick DB creates “random” symptoms everywhere.

## Quick links

- [Collect debug info](collect-debug-info.md) (copy/paste checklist)
- [Common errors](common-errors.md) (error string → meaning → next step)
- [Market / indexing troubleshooting](../curio-market/troubleshooting.md)
- [YugabyteDB troubleshooting](../administration/yugabyte-troubleshooting.md)
