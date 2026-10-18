---
description: Calibration piece uploader used to exercise a PDP provider.
---

# synapse-random-upload

`scripts/manual_test/synapse-random-upload/` uploads random pieces to a Filecoin calibration PDP provider with the Synapse SDK. Payloads are generated in memory. Data set ids and piece CIDs are appended to a local ledger.

It is a developer load and integration tool, not part of the Curio binary. The full reference is [`scripts/manual_test/synapse-random-upload/README.md`](../../../scripts/manual_test/synapse-random-upload/README.md).

Requirements are Node.js 20+ and npm. From the repo:

```bash
cd scripts/manual_test/synapse-random-upload
./run --provider https://provider.example --count 1 \
  --min-bytes 104857600 --max-bytes 524288000
```

`--provider` is a registry id, a `0x` address, or the provider PDP URL. `--finalize-every N` commits N pieces in one chain transaction. `--datasets N` in a single process stores every piece into each data set; use one process per data set when the data sets should hold different pieces.

`wallet.json` and `datasets.json` in that directory are local and gitignored. Do not commit them. `--fund-only` checks the wallet and storage deposit before a large run. The calibration faucet rate-limits drips.
