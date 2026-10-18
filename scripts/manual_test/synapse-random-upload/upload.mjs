#!/usr/bin/env node
// Upload random Filecoin calibration pieces through the Synapse SDK.
// Each payload repeats one random mebibyte and is generated while uploading.
// See README.md for wallet, dataset, and commit-batching behavior.

import { randomBytes, randomInt } from 'node:crypto'
import { chmod, mkdir, readFile, rename, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { Readable } from 'node:stream'
import { parseArgs } from 'node:util'
import { fileURLToPath } from 'node:url'

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url))
const DEFAULT_MIN_BYTES = 500 * 1024 * 1024
const DEFAULT_MAX_BYTES = 1024 * 1024 * 1024
// Node's built-in fetch stops waiting for response headers after 5 minutes.
// The storage server sends those headers only after the piece is stored.
const FETCH_HEADERS_TIMEOUT_MS = 30 * 60 * 1000
const FETCH_BODY_TIMEOUT_MS = 30 * 60 * 1000
const UPLOAD_PARALLELISM = 10
let commitTurn = Promise.resolve()
let ledgerWrite = Promise.resolve()
const PIECE_PATTERN = randomBytes(1024 * 1024)
const MIN_PIECE_BYTES = 127
const MIN_FIL = 50_000_000_000_000_000n // 0.05 tFIL, enough gas for setup and commits
const FAUCET_URL = 'https://forest-explorer.chainsafe.dev/api/claim_token'
const TOKEN_DECIMALS = 18

const { values } = parseArgs({
  options: {
    count: { type: 'string', default: '1' },
    'total-bytes': { type: 'string' },
    'min-bytes': { type: 'string', default: String(DEFAULT_MIN_BYTES) },
    'max-bytes': { type: 'string', default: String(DEFAULT_MAX_BYTES) },
    'finalize-every': { type: 'string' },
    copies: { type: 'string', default: '2' },
    parallel: { type: 'string', default: String(UPLOAD_PARALLELISM) },
    datasets: { type: 'string' },
    'datasets-file': { type: 'string', default: path.join(SCRIPT_DIR, 'datasets.json') },
    provider: { type: 'string', multiple: true },
    'key-file': { type: 'string', default: path.join(SCRIPT_DIR, 'wallet.json') },
    rpc: { type: 'string' },
    'fund-only': { type: 'boolean', default: false },
    'dry-run': { type: 'boolean', default: false },
    help: { type: 'boolean', default: false },
  },
  allowPositionals: false,
})

if (values.help) {
  console.log(`Usage: node upload.mjs [options]

Generate random payloads and upload them to Filecoin calibration with @filoz/synapse-sdk.
The wallet is created on first run and funded from the Forest Explorer calibration faucet
(tFIL for gas, tUSDFC for storage). Faucet caps are 2 tFIL and 10 tUSDFC per address.

--datasets N creates that many new data sets and adds every piece to each of them.
Their ids, provider, and piece CIDs are stored in the datasets file for a later query/verify pass.
Piece bytes upload --parallel at a time (default ${UPLOAD_PARALLELISM}). On-chain commits stay one at a time, including across separate processes.

Options:
  --count N         Pieces to generate (default 1). With --datasets, each data set receives all of them.
  --total-bytes N   Keep generating pieces until their sizes sum to at least N bytes.
  --min-bytes N     Minimum payload size in bytes (default ${DEFAULT_MIN_BYTES}, 500 MiB)
  --max-bytes N     Maximum payload size in bytes (default ${DEFAULT_MAX_BYTES}, 1 GiB)
  --finalize-every N
                    Park pieces, then commit them on-chain in batches of N (and a final shorter batch).
  --parallel N      Piece uploads in flight for this process (default ${UPLOAD_PARALLELISM}).
  --datasets N      Create N data sets, then add the pieces to each one.
  --datasets-file PATH
                    Ledger of data set ids and piece CIDs (default datasets.json next to this script)
  --copies N        Provider copies per piece when --datasets is omitted (default 2, or the number of --provider values)
  --provider SPEC   Upload only to this storage provider. Repeat to spread --datasets across providers.
                    SPEC is a registry id (9), a 0x address, or a PDP service URL.
  --key-file PATH   Wallet JSON path (default scripts/manual_test/synapse-random-upload/wallet.json)
  --rpc URL         Calibration ETH JSON-RPC URL (default is the SDK chain RPC)
  --fund-only       Fund the wallet and storage deposit, then exit
  --dry-run         Print piece sizes and check the byte stream locally
  --help            Show this help

Environment:
  SYNAPSE_PRIVATE_KEY   0x-prefixed secp256k1 key. When set, the key file is not written.

Stdout is one JSON object per event. Progress logs go to stderr.
`)
  process.exit(0)
}

const countWasSet = process.argv.some((arg) => arg === '--count' || arg.startsWith('--count='))
const totalBytesWasSet = process.argv.some((arg) => arg === '--total-bytes' || arg.startsWith('--total-bytes='))
const finalizeWasSet = process.argv.some((arg) => arg === '--finalize-every' || arg.startsWith('--finalize-every='))
const requestedCount = parsePositiveInt('count', values.count)
const totalBytes = totalBytesWasSet ? parsePositiveInt('total-bytes', values['total-bytes']) : null
const finalizeEvery = finalizeWasSet ? parsePositiveInt('finalize-every', values['finalize-every']) : null
const minBytes = parsePositiveInt('min-bytes', values['min-bytes'])
const maxBytes = parsePositiveInt('max-bytes', values['max-bytes'])
const providerSpecs = (values.provider ?? []).flatMap((value) => value.split(',')).map((value) => value.trim()).filter(Boolean)
const copiesWasSet = process.argv.some((arg) => arg === '--copies' || arg.startsWith('--copies='))
const datasetsWasSet = process.argv.some((arg) => arg === '--datasets' || arg.startsWith('--datasets='))
const datasetCount = datasetsWasSet ? parsePositiveInt('datasets', values.datasets) : null
const parallel = parsePositiveInt('parallel', values.parallel)
let copies = copiesWasSet ? parsePositiveInt('copies', values.copies) : providerSpecs.length > 0 ? null : 2

if (datasetCount != null && copiesWasSet) {
  throw new Error('pass either --datasets or --copies, not both')
}

if (minBytes < MIN_PIECE_BYTES) {
  throw new Error(`min-bytes must be at least ${MIN_PIECE_BYTES}`)
}
if (minBytes > maxBytes) {
  throw new Error('min-bytes must be less than or equal to max-bytes')
}
if (copies != null && copies > 8) {
  throw new Error('copies must be 8 or fewer')
}
if (countWasSet && totalBytes != null) {
  throw new Error('pass either --count or --total-bytes, not both')
}

const sizes = totalBytes == null
  ? Array.from({ length: requestedCount }, () => randomInt(minBytes, maxBytes + 1))
  : planSizes(totalBytes, minBytes, maxBytes)
const count = sizes.length
const totalPlannedBytes = sizes.reduce((sum, size) => sum + size, 0)

const isDirectRun = process.argv[1] != null && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
if (isDirectRun) {
  await main()
}

export { rememberDatasetPiece }

async function main() {
  await useLongFetchTimeouts()
  if (values['dry-run']) {
    for (const [index, size] of sizes.entries()) {
      const got = await readStreamLength(randomPieceStream(size))
      if (got !== size) {
        throw new Error(`stream length ${got} did not match planned size ${size}`)
      }
      emit({ event: 'dry-run', index: index + 1, count, bytes: size })
    }
    return
  }

  const { Synapse, calibration, formatUnits } = await import('@filoz/synapse-sdk')
  const { waitForTransactionReceipt } = await import('viem/actions')
  const account = await loadAccount()
  let transport
  if (values.rpc) {
    const { http } = await import('viem')
    transport = http(values.rpc)
  }
  const synapse = Synapse.create({
    account,
    chain: calibration,
    transport,
    source: 'curio-synapse-random-upload',
    // A long delay keeps parked pieces uncommitted until flush. The run flushes every
    // --finalize-every pieces, and once more for the tail.
    pieceBatching: finalizeEvery == null ? false : { wait: { kind: 'delay', ms: 24 * 60 * 60 * 1000 } },
  })

  log(`wallet ${account.address}`)
  log(`chain ${synapse.chain.name} (${synapse.chain.id})`)
  const providerIds = providerSpecs.length > 0 ? await resolveProviderIds(synapse, providerSpecs) : []
  if (datasetCount == null && providerIds.length > 0) {
    if (copies == null) {
      copies = providerIds.length
    }
    if (copies !== providerIds.length) {
      throw new Error(
        `--copies ${copies} does not match ${providerIds.length} targeted provider(s). Pass one --provider per copy, or omit --copies.`
      )
    }
    if (copies > 8) {
      throw new Error('copies must be 8 or fewer')
    }
  }
  const targets =
    datasetCount == null
      ? null
      : await createNamedDatasets(synapse, providerIds, datasetCount)
  const contexts = targets == null ? await synapse.storage.createContexts({
    copies,
    ...(providerIds.length > 0 ? { providerIds } : {}),
    callbacks: {
      onProviderSelected(provider) {
        log(`provider ${provider.id} ${provider.pdp?.serviceURL ?? ''}`)
      },
      onDataSetResolved(info) {
        log(`dataset ${info.dataSetId} on provider ${info.provider.id}`)
      },
    },
  }) : targets.map((target) => target.context)
  const sizeSummary = sizes.length > 8
    ? `${formatBytes(totalPlannedBytes)} across ${sizes.length} pieces (${formatBytes(minBytes)}–${formatBytes(maxBytes)})`
    : sizes.map((size) => formatBytes(size)).join(', ')
  log(
    targets == null
      ? `planned ${count} piece(s), ${copies} cop${copies === 1 ? 'y' : 'ies'}${
          providerIds.length > 0 ? ` on provider ${providerIds.join(', ')}` : ''
        }, ${sizeSummary}`
      : `planned ${count} piece(s) in each of ${datasetCount} data sets${
          providerIds.length > 0 ? ` on provider ${providerIds.join(', ')}` : ''
        }, ${sizeSummary}`
  )
  if (finalizeEvery != null) {
    log(`on-chain finalize every ${finalizeEvery} piece(s)`)
  }
  log(`uploading ${parallel} piece(s) at a time`)

  await ensureToken(synapse, formatUnits, waitForTransactionReceipt, {
    faucetInfo: 'CalibnetFIL',
    token: 'FIL',
    label: 'tFIL',
    minimum: MIN_FIL,
  })
  await ensureToken(synapse, formatUnits, waitForTransactionReceipt, {
    faucetInfo: 'CalibnetUSDFC',
    token: 'USDFC',
    label: 'tUSDFC',
    minimum: 1n,
  })

  await fundStorage(synapse, formatUnits, waitForTransactionReceipt, contexts)
  if (values['fund-only']) {
    emit({ event: 'funded', address: account.address, bytes: sizes, datasets: datasetCount })
    return
  }

  let failed = 0
  const recordedIds = []
  const uploads =
    targets == null
      ? sizes.map((size, index) => ({ size, index, contexts, metadata: null }))
      : targets.flatMap((target) =>
          sizes.map((size, index) => ({
            size,
            index,
            contexts: [target.context],
            metadata: target.metadata,
          }))
        )
  const parkedBatch = []
  let flushChain = Promise.resolve()
  const commits = []
  await runPool(uploads, parallel, async (upload) => {
    const where = upload.metadata?.run != null ? ` run ${upload.metadata.run}` : ''
    const label = `piece ${upload.index + 1}/${count} (${formatBytes(upload.size)})${where}`
    log(`uploading ${label}`)
    if (finalizeEvery == null) {
      if (upload.contexts.length !== 1) {
        let result
        try {
          result = await uploadWithRetry(synapse, formatUnits, waitForTransactionReceipt, upload.size, label, upload.contexts)
        } catch (error) {
          failed++
          const message = errorChain(error)
          emit({ event: 'piece-error', index: upload.index + 1, count, bytes: upload.size, error: message })
          log(`${label} failed: ${message}`)
          return
        }
        const added = await recordUploadResult(synapse, account, upload, label, result, recordedIds)
        failed += added
        return
      }
      let stored
      try {
        stored = await storeWithRetry(upload.size, label, upload.contexts[0])
      } catch (error) {
        failed++
        const message = errorChain(error)
        emit({ event: 'piece-error', index: upload.index + 1, count, bytes: upload.size, error: message })
        log(`${label} failed: ${message}`)
        return
      }
      commits.push((async () => {
        try {
          const result = await withFileLock('.synapse-commit-lock', () =>
            commitStoredPiece(synapse, formatUnits, waitForTransactionReceipt, upload.contexts[0], stored, label)
          )
          const added = await recordUploadResult(synapse, account, upload, label, result, recordedIds)
          failed += added
        } catch (error) {
          failed++
          const message = errorChain(error)
          emit({ event: 'piece-error', index: upload.index + 1, count, bytes: upload.size, error: message })
          log(`${label} failed: ${message}`)
        }
      })())
      return
    }

    let parked
    try {
      parked = await parkWithRetry(synapse, formatUnits, waitForTransactionReceipt, upload.size, label, upload.contexts)
    } catch (error) {
      failed++
      const message = errorChain(error)
      emit({ event: 'piece-error', index: upload.index + 1, count, bytes: upload.size, error: message })
      log(`${label} failed: ${message}`)
      return
    }
    parkedBatch.push({ upload, label, parked })
    if (parkedBatch.length < finalizeEvery) {
      return
    }
    const batch = parkedBatch.splice(0, finalizeEvery)
    const flushed = flushChain.then(async () => {
      const added = await finalizeParkedBatch(synapse, formatUnits, waitForTransactionReceipt, account, batch, recordedIds)
      failed += added
    })
    flushChain = flushed.then(() => {}, () => {})
    await flushed
  })
  await flushChain
  await Promise.all(commits)
  if (parkedBatch.length > 0) {
    const added = await finalizeParkedBatch(synapse, formatUnits, waitForTransactionReceipt, account, parkedBatch, recordedIds)
    failed += added
  }

  const datasetIds = [...new Set(recordedIds)]
  emit({ event: 'datasets', file: values['datasets-file'], datasetIds })
  log(`data set ledger ${values['datasets-file']} (${datasetIds.length} data set${datasetIds.length === 1 ? '' : 's'})`)

  if (failed > 0) {
    throw new Error(`${failed} of ${uploads.length} upload(s) failed`)
  }
}

async function createNamedDatasets(synapse, providerIds, datasetCount) {
  let ids = providerIds
  if (ids.length === 0) {
    const probe = await synapse.storage.createContext()
    ids = [probe.provider.id]
    log(`selected provider ${probe.provider.id} ${probe.provider.pdp?.serviceURL ?? ''} for ${datasetCount} data sets`)
  }
  const run = `${process.pid}-${randomInt(1000, 10000)}`
  const targets = []
  for (let i = 0; i < datasetCount; i++) {
    const providerId = ids[i % ids.length]
    const metadata = {
      purpose: 'random-pieces',
      run,
      dataset: String(i),
    }
    const context = await synapse.storage.createContext({
      providerId,
      metadata,
      callbacks: {
        onProviderSelected(provider) {
          log(`data set ${i + 1}/${datasetCount} on provider ${provider.id} ${provider.pdp?.serviceURL ?? ''}`)
        },
        onDataSetResolved(info) {
          log(`data set ${i + 1}/${datasetCount} resolved to existing data set ${info.dataSetId}`)
        },
      },
    })
    targets.push({ context, metadata, index: i })
  }
  return targets
}

function planSizes(total, min, max) {
  const planned = []
  let sum = 0
  while (sum < total) {
    const size = randomInt(min, max + 1)
    planned.push(size)
    sum += size
  }
  return planned
}

async function recordUploadResult(synapse, account, upload, label, result, recordedIds) {
  const piece = {
    event: 'piece',
    index: upload.index + 1,
    count,
    bytes: upload.size,
    pieceCid: String(result.pieceCid),
    complete: result.complete,
    copies: result.copies.map(copySummary),
    failedAttempts: (result.failedAttempts ?? []).map((attempt) => ({
      providerId: attempt.providerId?.toString?.() ?? String(attempt.providerId),
      role: attempt.role,
      error: attempt.error,
    })),
  }
  emit(piece)
  const contextByProvider = new Map(upload.contexts.map((context) => [context.provider.id.toString(), context]))
  for (const copy of result.copies) {
    const context = contextByProvider.get(copy.providerId.toString())
    if (context == null) {
      throw new Error(`piece confirmed on provider ${copy.providerId}, which was not targeted`)
    }
    const serviceURL = context.provider.pdp?.serviceURL ?? null
    const saved = await rememberDatasetPiece(values['datasets-file'], {
      chainId: synapse.chain.id,
      payer: account.address,
      dataSetId: copy.dataSetId.toString(),
      providerId: copy.providerId.toString(),
      serviceURL,
      metadata: upload.metadata ?? context?.dataSetMetadata ?? null,
      piece: {
        pieceCid: String(result.pieceCid),
        pieceId: copy.pieceId?.toString?.() ?? null,
        bytes: upload.size,
        retrievalUrl: copy.retrievalUrl ?? null,
        uploadedAt: new Date().toISOString(),
      },
    })
    recordedIds.push(saved.dataSetId)
    log(`saved data set ${saved.dataSetId} to ${values['datasets-file']}`)
  }
  if (!result.complete) {
    log(`${label} incomplete`)
    return 1
  }
  return 0
}

function startParkedUpload(synapse, size, label, contexts) {
  let markStored
  const stored = new Promise((resolve) => {
    markStored = resolve
  })
  const controller = new AbortController()
  const done = synapse.storage.upload(randomPieceStream(size), {
    contexts,
    signal: controller.signal,
    callbacks: {
      onProgress: progressLogger(label, size),
      onStored(providerId, pieceCid) {
        log(`${label} stored on provider ${providerId}: ${pieceCid}`)
        markStored()
      },
      onPiecesConfirmed(dataSetId, providerId) {
        log(`${label} confirmed in dataset ${dataSetId} on provider ${providerId}`)
      },
    },
  })
  const settled = done.then(
    (result) => ({ result }),
    (error) => ({ error })
  )
  return {
    stored,
    done,
    settled,
    abort() {
      controller.abort()
    },
  }
}

async function waitUntilParked(parked) {
  const outcome = await Promise.race([
    parked.stored.then(() => 'stored'),
    parked.settled.then((value) => {
      if (value.error) {
        throw value.error
      }
      return 'committed'
    }),
  ])
  if (outcome === 'stored') {
    // onStored runs before the batcher enqueues the piece. Yield once so the enqueue microtask lands first.
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
}

async function parkWithRetry(synapse, formatUnits, waitForTransactionReceipt, size, label, contexts) {
  let lastError
  for (let attempt = 1; attempt <= 2; attempt++) {
    const parked = startParkedUpload(synapse, size, label, contexts)
    try {
      await waitUntilParked(parked)
      return parked
    } catch (error) {
      parked.abort()
      lastError = error
      const message = errorChain(error)
      if (attempt === 1 && /insufficient funds|lockup/i.test(message)) {
        log(`${label} needs more lockup; depositing an extra 1 tUSDFC and retrying`)
        await topUpStorage(synapse, formatUnits, waitForTransactionReceipt, 10n ** 18n)
        continue
      }
      if (attempt === 1 && /network request failed/i.test(message)) {
        log(`${label} hit a network error; retrying once`)
        continue
      }
      break
    }
  }
  throw lastError
}

async function finalizeParkedBatch(synapse, formatUnits, waitForTransactionReceipt, account, batch, recordedIds) {
  log(`finalizing ${batch.length} piece(s)`)
  let lastError
  for (let attempt = 1; attempt <= 2; attempt++) {
    try {
      await withFileLock('.synapse-commit-lock', () => synapse.storage.flush())
      let failed = 0
      const results = await Promise.all(batch.map(async (item) => {
        const settled = await item.parked.settled
        if (settled.error) {
          throw settled.error
        }
        return { item, result: settled.result }
      }))
      for (const { item, result } of results) {
        failed += await recordUploadResult(synapse, account, item.upload, item.label, result, recordedIds)
      }
      emit({ event: 'finalize', pieces: batch.length })
      log(`finalized ${batch.length} piece(s)`)
      return failed
    } catch (error) {
      lastError = error
      const message = errorChain(error)
      if (attempt === 1 && /insufficient funds|lockup/i.test(message)) {
        log(`finalize needs more lockup; depositing an extra 1 tUSDFC and retrying`)
        await topUpStorage(synapse, formatUnits, waitForTransactionReceipt, 10n ** 18n)
        continue
      }
      break
    }
  }
  const message = errorChain(lastError)
  for (const item of batch) {
    emit({
      event: 'piece-error',
      index: item.upload.index + 1,
      count,
      bytes: item.upload.size,
      error: message,
    })
  }
  log(`finalize failed for ${batch.length} piece(s): ${message}`)
  return batch.length
}

function takeCommitTurn(fn) {
  const run = commitTurn.then(fn, fn)
  commitTurn = run.then(() => {}, () => {})
  return run
}

async function storeWithRetry(size, label, context) {
  let lastStoreError
  for (let attempt = 1; attempt <= 2; attempt++) {
    try {
      const stored = await context.store(randomPieceStream(size), {
        onProgress: progressLogger(label, size),
      })
      log(`${label} stored on provider ${context.provider.id}: ${stored.pieceCid}`)
      return stored
    } catch (error) {
      lastStoreError = error
      if (attempt === 1 && /network request failed/i.test(errorChain(error))) {
        log(`${label} hit a network error; retrying once`)
        continue
      }
      throw error
    }
  }
  throw lastStoreError
}

async function uploadWithRetry(synapse, formatUnits, waitForTransactionReceipt, size, label, contexts) {
  if (contexts.length !== 1) {
    return uploadThroughManager(synapse, formatUnits, waitForTransactionReceipt, size, label, contexts)
  }
  const stored = await storeWithRetry(size, label, contexts[0])
  return takeCommitTurn(() =>
    commitStoredPiece(synapse, formatUnits, waitForTransactionReceipt, contexts[0], stored, label)
  )
}

async function withFileLock(name, fn) {
  const dir = path.join(SCRIPT_DIR, name)
  const stamp = path.join(dir, 'owner')
  for (;;) {
    try {
      await mkdir(dir)
      await writeFile(stamp, `${process.pid} ${Date.now()}\n`)
      break
    } catch (error) {
      if (error?.code !== 'EEXIST') {
        throw error
      }
      await stealStaleFileLock(dir, stamp)
      await sleep(200 + randomInt(0, 200))
    }
  }
  try {
    return await fn()
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
}

async function stealStaleFileLock(dir, stamp) {
  let pid
  let createdAt
  try {
    const text = await readFile(stamp, 'utf8')
    const [pidText, timeText] = text.trim().split(' ')
    pid = Number(pidText)
    createdAt = Number(timeText)
  } catch (error) {
    if (error?.code === 'ENOENT') {
      return
    }
    throw error
  }
  let alive = true
  try {
    process.kill(pid, 0)
  } catch (error) {
    alive = error?.code !== 'ESRCH'
  }
  if (alive && Date.now() - createdAt < 15 * 60 * 1000) {
    return
  }
  await rm(dir, { recursive: true, force: true })
}

async function commitStoredPiece(synapse, formatUnits, waitForTransactionReceipt, context, stored, label) {
  let lastError
  for (let attempt = 1; attempt <= 2; attempt++) {
    try {
      const commitResult = await context.commit({
        pieces: [{ pieceCid: stored.pieceCid }],
      })
      const pieceId = commitResult.pieceIds[0]
      log(`${label} confirmed in dataset ${commitResult.dataSetId} on provider ${context.provider.id}`)
      return {
        pieceCid: stored.pieceCid,
        size: stored.size,
        requestedCopies: 1,
        complete: true,
        copies: [
          {
            providerId: context.provider.id,
            dataSetId: commitResult.dataSetId,
            pieceId,
            role: 'primary',
            retrievalUrl: context.getPieceUrl(stored.pieceCid),
            isNewDataSet: commitResult.isNewDataSet,
          },
        ],
        failedAttempts: [],
      }
    } catch (error) {
      lastError = error
      if (attempt === 1 && /insufficient funds|lockup/i.test(errorChain(error))) {
        log(`${label} needs more lockup; depositing an extra 1 tUSDFC and retrying`)
        await topUpStorage(synapse, formatUnits, waitForTransactionReceipt, 10n ** 18n)
        continue
      }
      break
    }
  }
  throw lastError
}

async function uploadThroughManager(synapse, formatUnits, waitForTransactionReceipt, size, label, contexts) {
  let lastError
  for (let attempt = 1; attempt <= 2; attempt++) {
    try {
      return await synapse.storage.upload(randomPieceStream(size), {
        contexts,
        callbacks: {
          onProgress: progressLogger(label, size),
          onStored(providerId, pieceCid) {
            log(`${label} stored on provider ${providerId}: ${pieceCid}`)
          },
          onPiecesConfirmed(dataSetId, providerId) {
            log(`${label} confirmed in dataset ${dataSetId} on provider ${providerId}`)
          },
        },
      })
    } catch (error) {
      lastError = error
      const message = errorChain(error)
      if (attempt === 1 && /insufficient funds|lockup/i.test(message)) {
        log(`${label} needs more lockup; depositing an extra 1 tUSDFC and retrying`)
        await topUpStorage(synapse, formatUnits, waitForTransactionReceipt, 10n ** 18n)
        continue
      }
      if (attempt === 1 && /network request failed/i.test(message)) {
        log(`${label} hit a network error; retrying once`)
        continue
      }
      break
    }
  }
  throw lastError
}

async function runPool(items, limit, worker) {
  let next = 0
  async function run() {
    while (next < items.length) {
      const index = next
      next += 1
      await worker(items[index])
    }
  }
  const workers = Math.min(limit, items.length)
  await Promise.all(Array.from({ length: workers }, () => run()))
}

async function rememberDatasetPiece(file, entry) {
  const run = ledgerWrite.then(() => withFileLock('.synapse-ledger-lock', () => persistDatasetPiece(file, entry)))
  ledgerWrite = run.then(() => {}, () => {})
  return run
}

async function persistDatasetPiece(file, entry) {
  let ledger = {
    schemaVersion: 1,
    chainId: Number(entry.chainId),
    payer: entry.payer,
    datasets: [],
  }
  try {
    ledger = JSON.parse(await readFile(file, 'utf8'))
  } catch (error) {
    if (error?.code !== 'ENOENT') {
      throw error
    }
  }
  if (!Array.isArray(ledger.datasets)) {
    ledger.datasets = []
  }
  ledger.schemaVersion = 1
  ledger.chainId = Number(entry.chainId)
  ledger.payer = entry.payer
  let dataset = ledger.datasets.find((item) => item.dataSetId === entry.dataSetId)
  if (dataset == null) {
    dataset = {
      dataSetId: entry.dataSetId,
      providerId: entry.providerId,
      serviceURL: entry.serviceURL,
      payer: entry.payer,
      metadata: entry.metadata,
      pieces: [],
    }
    ledger.datasets.push(dataset)
  }
  dataset.providerId = entry.providerId
  dataset.serviceURL = entry.serviceURL ?? dataset.serviceURL
  dataset.payer = entry.payer
  if (entry.metadata != null) {
    dataset.metadata = entry.metadata
  }
  const pieces = dataset.pieces ?? []
  const existing = pieces.find((piece) => piece.pieceCid === entry.piece.pieceCid)
  if (existing == null) {
    pieces.push(entry.piece)
  } else {
    existing.pieceId = entry.piece.pieceId
    existing.bytes = entry.piece.bytes
    existing.retrievalUrl = entry.piece.retrievalUrl
    existing.uploadedAt = entry.piece.uploadedAt
  }
  dataset.pieces = pieces
  ledger.datasetIds = ledger.datasets.map((item) => item.dataSetId)
  await mkdir(path.dirname(file), { recursive: true })
  const tmp = `${file}.tmp`
  await writeFile(tmp, `${JSON.stringify(ledger, null, 2)}\n`, { mode: 0o644 })
  await rename(tmp, file)
  return dataset
}

async function resolveProviderIds(synapse, specs) {
  const ids = []
  const urls = []
  for (const spec of specs) {
    if (/^\d+$/.test(spec)) {
      const provider = await synapse.providers.getProvider({ providerId: BigInt(spec) })
      if (provider == null) {
        throw new Error(`provider ${spec} is not in the calibration registry`)
      }
      ids.push(provider.id)
      log(`target provider ${provider.id} ${provider.pdp?.serviceURL ?? ''}`)
      continue
    }
    if (/^0x[0-9a-fA-F]{40}$/.test(spec)) {
      const provider = await synapse.providers.getProviderByAddress({ address: spec })
      if (provider == null) {
        throw new Error(`no calibration provider for ${spec}`)
      }
      ids.push(provider.id)
      log(`target provider ${provider.id} ${provider.pdp?.serviceURL ?? ''}`)
      continue
    }
    urls.push(spec)
  }

  if (urls.length > 0) {
    for (const spec of urls) {
      let needle
      try {
        needle = normalizeServiceURL(spec)
      } catch {
        throw new Error(`provider ${spec} must be a registry id, a 0x address, or a PDP service URL`)
      }
      const matches = await findProvidersByServiceURL(synapse, needle)
      if (matches.length === 0) {
        throw new Error(`no calibration provider with PDP URL ${spec}`)
      }
      if (matches.length > 1) {
        throw new Error(
          `PDP URL ${spec} matches providers ${matches.map((provider) => provider.id.toString()).join(', ')}`
        )
      }
      ids.push(matches[0].id)
      log(`target provider ${matches[0].id} ${matches[0].pdp.serviceURL}`)
    }
  }

  const seen = new Set()
  for (const id of ids) {
    const key = id.toString()
    if (seen.has(key)) {
      throw new Error(`provider ${key} was given more than once. Each copy needs a different provider.`)
    }
    seen.add(key)
  }
  return ids
}

async function findProvidersByServiceURL(synapse, needle) {
  const matches = []
  const seen = new Set()
  const consider = (provider) => {
    if (provider == null) return
    const key = provider.id.toString()
    if (seen.has(key)) return
    let url = ''
    try {
      url = normalizeServiceURL(provider.pdp?.serviceURL ?? '')
    } catch {
      return
    }
    if (url !== needle) return
    seen.add(key)
    matches.push(provider)
  }

  try {
    for (const provider of await synapse.providers.getAllActiveProviders()) {
      consider(provider)
    }
    if (matches.length > 0) return matches
  } catch (error) {
    log(`active provider list failed (${error instanceof Error ? error.message : error}); scanning the registry`)
  }

  const count = await synapse.providers.getProviderCount()
  for (let id = 1n; id <= count; id++) {
    try {
      consider(await synapse.providers.getProvider({ providerId: id }))
    } catch {
      // Some registry rows fail SDK decoding. Skip them.
    }
  }
  return matches
}

function normalizeServiceURL(value) {
  const withScheme = /^https?:\/\//i.test(value) ? value : `https://${value}`
  const url = new URL(withScheme)
  const pathname = url.pathname.replace(/\/+$/, '')
  return `${url.protocol}//${url.host}${pathname}`.toLowerCase()
}

async function loadAccount() {
  const { generatePrivateKey, privateKeyToAccount } = await import('viem/accounts')
  const { nonceManager } = await import('viem')
  const accountFromKey = (privateKey) => privateKeyToAccount(normalizePrivateKey(privateKey), { nonceManager })
  const fromEnv = process.env.SYNAPSE_PRIVATE_KEY?.trim()
  if (fromEnv) {
    return accountFromKey(fromEnv)
  }

  const keyFile = values['key-file']
  try {
    const parsed = JSON.parse(await readFile(keyFile, 'utf8'))
    if (typeof parsed.privateKey !== 'string') {
      throw new Error(`missing privateKey in ${keyFile}`)
    }
    const account = accountFromKey(parsed.privateKey)
    await chmod(keyFile, 0o600)
    log(`loaded wallet from ${keyFile}`)
    return account
  } catch (error) {
    if (error?.code !== 'ENOENT') {
      throw error
    }
  }

  const privateKey = generatePrivateKey()
  const account = accountFromKey(privateKey)
  await writeFile(keyFile, `${JSON.stringify({ address: account.address, privateKey }, null, 2)}\n`, {
    mode: 0o600,
    flag: 'wx',
  })
  await chmod(keyFile, 0o600)
  log(`wrote new wallet to ${keyFile}`)
  return account
}

async function ensureToken(synapse, formatUnits, waitForTransactionReceipt, spec) {
  let balance = await synapse.payments.walletBalance({ token: spec.token })
  if (balance >= spec.minimum) {
    log(`${spec.label} balance ${formatUnits(balance, TOKEN_DECIMALS)}`)
    return
  }

  log(`${spec.label} balance ${formatUnits(balance, TOKEN_DECIMALS)}; requesting faucet drip`)
  await claimAndWait(synapse, waitForTransactionReceipt, spec.faucetInfo, synapse.client.account.address)
  balance = await pollBalance(synapse, spec.token, spec.minimum)
  if (balance < spec.minimum) {
    throw new Error(
      `${spec.label} balance is ${formatUnits(balance, TOKEN_DECIMALS)} after the faucet drip (need at least ${formatUnits(spec.minimum, TOKEN_DECIMALS)})`
    )
  }
  log(`${spec.label} balance ${formatUnits(balance, TOKEN_DECIMALS)}`)
}

async function fundStorage(synapse, formatUnits, waitForTransactionReceipt, contexts) {
  const pieceSizes = sizes.map((size) => BigInt(size))
  for (let attempt = 1; attempt <= 4; attempt++) {
    const prep = await synapse.storage.prepare({ context: contexts, pieceSizes })
    if (prep.transaction == null) {
      log(
        `storage account already covers these pieces (deposit ${formatUnits(prep.costs.depositNeeded, TOKEN_DECIMALS)} tUSDFC)`
      )
      return
    }

    const needed = prep.transaction.depositAmount
    const approval = prep.transaction.includesApproval ? ' and service approval' : ''
    log(`storage deposit ${formatUnits(needed, TOKEN_DECIMALS)} tUSDFC${approval}`)

    let balance = await synapse.payments.walletBalance({ token: 'USDFC' })
    if (balance < needed) {
      log(`tUSDFC balance ${formatUnits(balance, TOKEN_DECIMALS)}; requesting faucet drip`)
      const before = balance
      try {
        await claimAndWait(
          synapse,
          waitForTransactionReceipt,
          'CalibnetUSDFC',
          synapse.client.account.address
        )
      } catch (error) {
        const latest = await synapse.payments.walletBalance({ token: 'USDFC' })
        const reason = error instanceof Error ? error.message : String(error)
        throw new Error(
          `need ${formatUnits(needed, TOKEN_DECIMALS)} tUSDFC, wallet has ${formatUnits(latest, TOKEN_DECIMALS)}. ${reason} The calibration faucet caps a wallet at 10 tUSDFC.`
        )
      }
      balance = await pollBalance(synapse, 'USDFC', before + 1n)
      if (balance <= before) {
        throw new Error('faucet drip did not increase the tUSDFC balance')
      }
      continue
    }

    const { hash } = await prep.transaction.execute()
    log(`storage deposit tx ${hash}`)
    return
  }
  throw new Error('could not fund the storage account')
}

async function topUpStorage(synapse, formatUnits, waitForTransactionReceipt, amount) {
  let balance = await synapse.payments.walletBalance({ token: 'USDFC' })
  if (balance < amount) {
    log(`tUSDFC balance ${formatUnits(balance, TOKEN_DECIMALS)}; requesting faucet drip`)
    const before = balance
    await claimAndWait(synapse, waitForTransactionReceipt, 'CalibnetUSDFC', synapse.client.account.address)
    balance = await pollBalance(synapse, 'USDFC', before + 1n)
    if (balance < amount) {
      throw new Error(
        `need ${formatUnits(amount, TOKEN_DECIMALS)} tUSDFC to cover lockup, wallet has ${formatUnits(balance, TOKEN_DECIMALS)}`
      )
    }
  }
  log(`depositing extra ${formatUnits(amount, TOKEN_DECIMALS)} tUSDFC to cover lockup`)
  const { hash } = await synapse.payments.fundSync({
    amount,
    needsFwssMaxApproval: false,
  })
  log(`lockup top-up tx ${hash}`)
}

async function useLongFetchTimeouts() {
  const { Agent, fetch: undiciFetch, setGlobalDispatcher } = await import('undici')
  setGlobalDispatcher(
    new Agent({
      headersTimeout: FETCH_HEADERS_TIMEOUT_MS,
      bodyTimeout: FETCH_BODY_TIMEOUT_MS,
      connectTimeout: 60_000,
      // One connection per request. HTTP/2 puts the finalize and commit posts
      // behind the piece bodies on a single connection, and those posts time out.
      allowH2: false,
      connections: 32,
    })
  )
  // iso-web calls fetch with a Node Request. undici's fetch cannot read those.
  globalThis.fetch = (input, init) => {
    if (typeof input === 'string' || input instanceof URL) {
      return undiciFetch(input, init)
    }
    if (!(input instanceof Request)) {
      return undiciFetch(input, init)
    }
    const body = init?.body ?? input.body ?? undefined
    return undiciFetch(input.url, {
      method: init?.method ?? input.method,
      headers: init?.headers ?? input.headers,
      body,
      duplex: body ? 'half' : undefined,
      signal: init?.signal ?? input.signal,
    })
  }
}

async function claimAndWait(synapse, waitForTransactionReceipt, faucetInfo, address) {
  for (let attempt = 1; attempt <= 4; attempt++) {
    const result = await claimToken(faucetInfo, address)
    if (result.ok) {
      log(`${faucetInfo} drip ${result.hash}`)
      const receipt = await waitForTransactionReceipt(synapse.client, {
        hash: result.hash,
        timeout: 180_000,
        pollingInterval: 4_000,
      })
      if (receipt.status !== 'success') {
        throw new Error(`${faucetInfo} drip reverted: ${result.hash}`)
      }
      return
    }
    if (result.status === 429 && attempt < 4) {
      const waitSec = retrySeconds(result.body)
      log(`${faucetInfo} rate limited; waiting ${waitSec}s`)
      await sleep(waitSec * 1000)
      continue
    }
    throw new Error(`${faucetInfo} faucet failed (${result.status}): ${result.body}`)
  }
}

async function claimToken(faucetInfo, address) {
  const url = new URL(FAUCET_URL)
  url.searchParams.set('faucet_info', faucetInfo)
  url.searchParams.set('address', address)
  const response = await fetch(url, { signal: AbortSignal.timeout(30_000) })
  const body = (await response.text()).trim()
  if (!response.ok) {
    return { ok: false, status: response.status, body }
  }
  const hash = parseFaucetHash(body)
  if (hash == null) {
    throw new Error(`${faucetInfo} faucet returned an unexpected body: ${body}`)
  }
  return { ok: true, hash }
}

function parseFaucetHash(body) {
  let value = body
  if (value.startsWith('"')) {
    try {
      value = JSON.parse(value)
    } catch {
      return null
    }
  }
  return /^0x[0-9a-fA-F]{64}$/.test(value) ? value : null
}

function retrySeconds(body) {
  const match = /try again in (\d+) second/i.exec(body)
  return match ? Number(match[1]) + 1 : 65
}

async function pollBalance(synapse, token, minimum) {
  let balance = 0n
  for (let attempt = 0; attempt < 10; attempt++) {
    balance = await synapse.payments.walletBalance({ token })
    if (balance >= minimum) {
      return balance
    }
    await sleep(3_000)
  }
  return balance
}

function randomPieceStream(size) {
  let remaining = size
  return Readable.toWeb(
    new Readable({
      read() {
        if (remaining <= 0) {
          this.push(null)
          return
        }
        const n = Math.min(PIECE_PATTERN.length, remaining)
        this.push(Buffer.from(PIECE_PATTERN.subarray(0, n)))
        remaining -= n
      },
    })
  )
}

async function readStreamLength(stream) {
  const reader = stream.getReader()
  let total = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) {
      return total
    }
    total += value.byteLength
  }
}

function progressLogger(label, total) {
  let lastBucket = -1
  let started = 0
  return (bytes) => {
    const sent = Number(bytes)
    if (started === 0) {
      started = performance.now()
    }
    const bucket = Math.floor((sent / total) * 5)
    if (bucket === lastBucket) {
      return
    }
    lastBucket = bucket
    const seconds = (performance.now() - started) / 1000
    const mebibytesPerSecond = sent / (1 << 20) / seconds
    log(`${label} uploaded ${formatBytes(sent)} in ${seconds.toFixed(1)}s (${mebibytesPerSecond.toFixed(1)} MiB/s)`)
  }
}

function copySummary(copy) {
  return {
    providerId: copy.providerId?.toString?.() ?? String(copy.providerId),
    dataSetId: copy.dataSetId?.toString?.() ?? String(copy.dataSetId),
    pieceId: copy.pieceId?.toString?.() ?? null,
    role: copy.role,
    retrievalUrl: copy.retrievalUrl ?? null,
  }
}

function parsePositiveInt(name, value) {
  if (!/^\d+$/.test(value)) {
    throw new Error(`${name} must be a positive integer, got ${value}`)
  }
  const n = Number(value)
  if (!Number.isSafeInteger(n) || n <= 0) {
    throw new Error(`${name} is out of range`)
  }
  return n
}

function normalizePrivateKey(value) {
  const key = value.startsWith('0x') ? value : `0x${value}`
  if (!/^0x[0-9a-fA-F]{64}$/.test(key)) {
    throw new Error('private key must be a 32-byte hex string')
  }
  return key
}

function formatBytes(size) {
  const GiB = 1024 * 1024 * 1024
  if (size >= GiB) {
    return `${(size / GiB).toFixed(2)} GiB`
  }
  if (size >= 1024 * 1024) {
    return `${(size / (1024 * 1024)).toFixed(2)} MiB`
  }
  return `${size} B`
}

function errorChain(error) {
  let deepest = error instanceof Error ? error.message : String(error)
  const seen = new Set()
  let current = error
  while (current instanceof Error && current.cause != null && !seen.has(current)) {
    seen.add(current)
    current = current.cause
    if (current instanceof Error && current.message) {
      deepest = current.message
    }
  }
  return deepest.trim()
}

function emit(value) {
  console.log(JSON.stringify(value))
}

function log(message) {
  console.error(`${new Date().toISOString()} ${message}`)
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
