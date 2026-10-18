# synapse-random-upload

Generate random piece payloads in memory and upload them to a Filecoin calibration storage provider with [`@filoz/synapse-sdk`](https://www.npmjs.com/package/@filoz/synapse-sdk).

Use this to put real PDP pieces on a specific provider: many pieces, several data sets, and on-chain commits in batches. Piece bytes are not written to disk. The data set ledger is.

## Setup

Node.js 20 or newer, and npm.

```bash
cd scripts/manual_test/synapse-random-upload
./run --help
```

`./run` installs dependencies on first use, then executes `upload.mjs`. `node upload.mjs` works after `npm install`.

The first run writes `wallet.json` (mode `0600`) next to the script and, when the balance is short, requests tFIL and tUSDFC from the Forest Explorer calibration faucet. That faucet caps an address at about 2 tFIL and 10 tUSDFC and will refuse repeat drips. Check coverage before a large run:

```bash
./run --fund-only --provider https://provider.example --total-bytes 1073741824 \
  --min-bytes 104857600 --max-bytes 524288000
```

`--fund-only` exits after the wallet and storage-deposit check. It does not upload.

`wallet.json`, `datasets.json`, and the commit/ledger lock directories are gitignored. Do not commit them. `SYNAPSE_PRIVATE_KEY` (a `0x` secp256k1 key) uses that key and does not write a key file.

## What a run does

Each piece is one random mebibyte, repeated to the planned size. The client hashes it, uploads it to the provider, then commits it into a data set on calibration (chain id 314159).

Progress logs go to stderr. Stdout is one JSON object per event (`piece`, `piece-error`, `finalize`, `datasets`, and so on).

Confirmed pieces are appended to the ledger (`datasets.json` by default): data set id, provider, service URL, piece CID, piece id, size, and retrieval URL. A file lock (`.synapse-ledger-lock`) lets several processes share that file.

## Targeting a provider

`--provider` accepts a registry id (`49`), a `0x` address, or a PDP service URL. Repeat it to spread `--datasets` across providers.

With one or more `--provider` values and no `--copies`, the copy count is the number of providers. One `--provider` is one copy, on that provider only.

```bash
./run --provider https://provider.example --count 3 \
  --min-bytes 104857600 --max-bytes 524288000
```

## Size and count

Default piece size is 500 MiB through 1 GiB. Minimum is 127 bytes.

| Flag | Meaning |
| --- | --- |
| `--count N` | Upload N pieces. Default 1. |
| `--total-bytes N` | Keep adding random sizes until the sum is at least N. |
| `--min-bytes N` / `--max-bytes N` | Inclusive size range, in bytes. |
| `--dry-run` | Plan sizes and check the stream locally. No network. |

Pass `--count` or `--total-bytes`, not both. `--total-bytes` can finish slightly over the target so the last piece stays inside the size range.

## Data sets

`--datasets N` creates N data sets. **Every planned piece is uploaded to each of them**, so stored bytes are about `N` times the planned total. Data set ids and piece CIDs are recorded for a later query or verify pass.

For N independent data sets that split a budget, start N processes, each with `--datasets 1` and its own `--total-bytes`. A single Node process hashes on one thread (on the order of 10 MiB/s). Separate processes hash at the same time.

```bash
bytes=$((7 * 1024 * 1024 * 1024))
for i in $(seq 0 9); do
  ./run --datasets 1 --parallel 1 --finalize-every 10 \
    --total-bytes "$bytes" \
    --min-bytes $((100 * 1024 * 1024)) \
    --max-bytes $((500 * 1024 * 1024)) \
    --provider https://provider.example \
    >"/tmp/upload-$i.json" 2>"/tmp/upload-$i.log" &
done
wait
```

`--datasets` and `--copies` cannot be combined. Without `--datasets`, `--copies` defaults to 2 (or the number of `--provider` values) and must be 8 or fewer.

## Commits

By default each stored piece is its own chain transaction (`addPieces`, or `createDataSetAndAddPieces` for the first piece of a data set).

`--finalize-every N` parks pieces and commits them in one transaction per N pieces. The last, shorter group is committed at the end of the run. The first batch of a new data set creates the data set and adds those pieces together.

Commits are serialized across processes with `.synapse-commit-lock`, including batched flushes. Uploads are not. `--parallel` (default 10) is how many piece uploads this process runs at once. Use `--parallel 1` when several processes are already uploading.

Fetch header and body timeouts are 30 minutes. Requests use separate HTTP/1.1 connections so a commit is not stuck behind a large upload body on one HTTP/2 connection.

## Useful flags

```text
--finalize-every N     Commit N pieces per chain transaction
--parallel N           Uploads in flight in this process (default 10)
--datasets N           Create N data sets; each receives every piece
--datasets-file PATH   Ledger path (default datasets.json)
--copies N             Copies per piece when not using --datasets
--provider SPEC        Registry id, 0x address, or PDP URL
--key-file PATH        Wallet JSON (default wallet.json)
--rpc URL              Calibration ETH JSON-RPC (SDK default if omitted)
--fund-only            Fund and exit
--dry-run              Local stream check only
```
