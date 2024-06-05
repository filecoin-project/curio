---
description: Curio best practices
---

# Best Practices

1. YugybteDB backing the Curio cluster should be multi node to avoid single point of failure.
2. All miner IDs should be part of the base layer. We highly recommend not creating separate layers for different MinerIDs but using different layers for control addresses if required.
3. No worker should be dedicated to specific minerIDs. All Curio nodes should be setup to allow jobs for any minerIDs.
4. Multiple workers should be started with `--post` layer to allow fast wdPost and winPost turn around time.&#x20;
5. We recommend running 1 GUI layer enabled node. A cluster wide GUI can be access via this node without putting any additional strain of read operations on the DB.
6. The unsealed and sealed copies should not be stored in the same storage location. Curio will allow automatic regeneration of sealed and unsealed in future if one is lost.
