// Produce a FEE object and a FOC grant presentation with @filoz/keysmith
// and filecoin-encryption-envelope from the synapse-sdk checkout this file
// is copied into. Stdout is one JSON object. Diagnostics go to stderr.
import * as fee from 'filecoin-encryption-envelope'
import { privateKeyToAccount } from 'viem/accounts'
import * as Keysmith from '@filoz/keysmith'

const ownerKey = '0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80'
const delegateKey = '0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d'
const bearerKey = '0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a'

const owner = privateKeyToAccount(ownerKey)
const delegate = privateKeyToAccount(delegateKey)
const bearer = privateKeyToAccount(bearerKey)

const chainId = 314159n
const now = Math.floor(Date.now() / 1000)
const ref = { owner: owner.address, groupKeyId: '0x00112233445566778899aabbccddeeff' }

// Collect the bytes a transform stream writes for one input chunk.
async function through(bytes, pair) {
  const stream = ReadableStream.from([bytes]).pipeThrough(pair)
  return new Uint8Array(await new Response(stream).arrayBuffer())
}

const hex = (bytes) => Buffer.from(bytes).toString('hex')

const { groupKey } = await Keysmith.deriveGroupKey(owner, ref)
const salt = Keysmith.newSalt()
const target = Keysmith.writeTarget(Keysmith.grantDescriptor(ref, 'groupKey'), groupKey, 'team/alice')
const metadata = Keysmith.pieceMetadata(target.ref, { salt, group: target.group })
const plaintext = new TextEncoder().encode('curio keysmith itest')
const cek = Keysmith.pieceKey(target.key, salt)
const encrypted = await through(plaintext, fee.encrypt({ cek, appMetadata: metadata, contentLength: plaintext.length }))

const pieceCid = new Uint8Array(36)
pieceCid.set([0x01, 0x55, 0x12, 0x20], 0)
pieceCid.set(cek.subarray(0, 32), 4)

const rooted = await Keysmith.rootClaim(owner, {
  owner: owner.address,
  groupKeyId: ref.groupKeyId,
  chainId,
  groupKey,
  node: 'group:team',
  bearer: delegate.address,
  maxDepth: 1,
  expires: now + 7 * 86400,
})
const delegated = await Keysmith.delegateClaim(delegate, rooted.chain, {
  parentKey: rooted.nodeKey,
  node: 'group:team/alice',
  bearer: bearer.address,
  maxDepth: 0,
})
const request = await Keysmith.signRequest(bearer, delegated.chain, {
  pieceCid,
  expires: now + 120,
})
const presentation = Keysmith.encodePresentation(delegated.chain, request)

const otherPieceCid = new Uint8Array(pieceCid)
otherPieceCid[otherPieceCid.length - 1] ^= 0xff
const otherRequest = await Keysmith.signRequest(bearer, delegated.chain, {
  pieceCid: otherPieceCid,
  expires: now + 120,
})

process.stdout.write(
  JSON.stringify({
    fee: hex(encrypted),
    presentation: hex(presentation),
    otherPresentation: hex(Keysmith.encodePresentation(delegated.chain, otherRequest)),
    pieceCid: hex(pieceCid),
    otherPieceCid: hex(otherPieceCid),
    chainId: chainId.toString(),
    now,
    owner: owner.address.toLowerCase(),
    bearer: bearer.address.toLowerCase(),
    groupKey: metadata['foc/gkid'],
    group: metadata['foc/group'],
    salt: metadata['foc/salt'],
    node: delegated.chain.links.at(-1).node,
  }),
)
