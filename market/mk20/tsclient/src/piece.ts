/**
 * PieceCID (Piece Commitment CID) utilities
 *
 * Helper functions for working with Filecoin Piece CIDs
 */

import type { LegacyPieceLink as LegacyPieceCIDType, PieceLink as PieceCIDType } from '@web3-storage/data-segment'
import * as Hasher from '@web3-storage/data-segment/multihash'
import { CID } from 'multiformats/cid'
import * as Raw from 'multiformats/codecs/raw'
import * as Digest from 'multiformats/hashes/digest'
import * as Link from 'multiformats/link'

const FIL_COMMITMENT_UNSEALED = 0xf101
const SHA2_256_TRUNC254_PADDED = 0x1012

/**
 * PieceCID - A constrained CID type for Piece Commitments.
 * This is implemented as a Link type which is made concrete by a CID. A
 * PieceCID uses the raw codec (0x55) and the fr32-sha256-trunc254-padbintree
 * multihash function (0x1011) which encodes the base content length (as
 * padding) of the original piece, and the height of the merkle tree used to
 * hash it.
 *
 * See https://github.com/filecoin-project/FIPs/blob/master/FRCs/frc-0069.md
 * for more information.
 */
export type PieceCID = PieceCIDType

/**
 * LegacyPieceCID - A constrained CID type for Legacy Piece Commitments.
 * This is implemented as a Link type which is made concrete by a CID. A
 * LegacyPieceCID uses the fil-commitment-unsealed codec (0xf101) and the
 * sha2-256-trunc254-padded (0x1012) multihash function.
 * This 32 bytes of the hash digest in a LegacyPieceCID is the same as the
 * equivalent PieceCID, but a LegacyPieceCID does not encode the length or
 * tree height of the original raw piece. A PieceCID can be converted to a
 * LegacyPieceCID, but not vice versa.
 * LegacyPieceCID is commonly known as "CommP" or simply "Piece Commitment"
 * in Filecoin.
 */
export type LegacyPieceCID = LegacyPieceCIDType

/**
 * Parse a PieceCID string into a CID and validate it
 * @param pieceCidString - The PieceCID as a string (base32 or other multibase encoding)
 * @returns The parsed and validated PieceCID CID or null if invalid
 */
function parsePieceCID(pieceCidString: string): PieceCID | null {
  try {
    const cid = CID.parse(pieceCidString)
    if (isValidPieceCID(cid)) {
      return cid as PieceCID
    }
  } catch {
    // ignore error
  }
  return null
}

/**
 * Parse a LegacyPieceCID string into a CID and validate it
 * @param pieceCidString - The LegacyPieceCID as a string (base32 or other multibase encoding)
 * @returns The parsed and validated LegacyPieceCID CID or null if invalid
 */
function parseLegacyPieceCID(pieceCidString: string): LegacyPieceCID | null {
  try {
    const cid = CID.parse(pieceCidString)
    if (isValidLegacyPieceCID(cid)) {
      return cid as LegacyPieceCID
    }
  } catch {
    // ignore error
  }
  return null
}

/**
 * Check if a CID is a valid PieceCID
 * @param cid - The CID to check
 * @returns True if it's a valid PieceCID
 */
function isValidPieceCID(cid: PieceCID | CID): cid is PieceCID {
  return cid.code === Raw.code && cid.multihash.code === Hasher.code
}

/**
 * Check if a CID is a valid LegacyPieceCID
 * @param cid - The CID to check
 * @returns True if it's a valid LegacyPieceCID
 */
function isValidLegacyPieceCID(cid: LegacyPieceCID | CID): cid is LegacyPieceCID {
  return cid.code === FIL_COMMITMENT_UNSEALED && cid.multihash.code === SHA2_256_TRUNC254_PADDED
}

/**
 * Convert a PieceCID input (string or CID) to a validated CID
 * This is the main function to use when accepting PieceCID inputs
 * @param pieceCidInput - PieceCID as either a CID object or string
 * @returns The validated PieceCID CID or null if not a valid PieceCID
 */
export function asPieceCID(pieceCidInput: PieceCID | CID | string): PieceCID | null {
  if (typeof pieceCidInput === 'string') {
    return parsePieceCID(pieceCidInput)
  }

  if (typeof pieceCidInput === 'object' && CID.asCID(pieceCidInput as CID) !== null) {
    // It's already a CID, validate it
    if (isValidPieceCID(pieceCidInput as CID)) {
      return pieceCidInput as PieceCID
    }
  }

  // Nope
  return null
}

/**
 * Convert a LegacyPieceCID input (string or CID) to a validated CID
 * This function can be used to parse a LegacyPieceCID (CommPv1) or to downgrade a PieceCID
 * (CommPv2) to a LegacyPieceCID.
 * @param pieceCidInput - LegacyPieceCID as either a CID object or string
 * @returns The validated LegacyPieceCID CID or null if not a valid LegacyPieceCID
 */
export function asLegacyPieceCID(pieceCidInput: PieceCID | LegacyPieceCID | CID | string): LegacyPieceCID | null {
  const pieceCid = asPieceCID(pieceCidInput as CID | string)
  if (pieceCid != null) {
    // downgrade to LegacyPieceCID
    const digest = Digest.create(SHA2_256_TRUNC254_PADDED, pieceCid.multihash.digest.subarray(-32))
    return Link.create(FIL_COMMITMENT_UNSEALED, digest) as LegacyPieceCID
  }

  if (typeof pieceCidInput === 'string') {
    return parseLegacyPieceCID(pieceCidInput)
  }

  if (typeof pieceCidInput === 'object' && CID.asCID(pieceCidInput as CID) !== null) {
    // It's already a CID, validate it
    if (isValidLegacyPieceCID(pieceCidInput as CID)) {
      return pieceCidInput as LegacyPieceCID
    }
  }

  // Nope
  return null
}

/**
 * Calculate the PieceCID (Piece Commitment) for a given data blob
 * @param data - The binary data to calculate the PieceCID for
 * @returns The calculated PieceCID CID
 */
export function calculate(data: Uint8Array): PieceCID {
  // TODO: consider https://github.com/storacha/fr32-sha2-256-trunc254-padded-binary-tree-multihash
  // for more efficient PieceCID calculation in WASM
  const hasher = Hasher.create()
  // We'll get slightly better performance by writing in chunks to let the
  // hasher do its work incrementally
  const chunkSize = 2048
  for (let i = 0; i < data.length; i += chunkSize) {
    hasher.write(data.subarray(i, i + chunkSize))
  }
  const digest = hasher.digest()
  return Link.create(Raw.code, digest)
}

/**
 * Create a TransformStream that calculates PieceCID while streaming data through it
 * This allows calculating PieceCID without buffering the entire data in memory
 *
 * @returns An object with the TransformStream and a getPieceCID function to retrieve the result
 */
export function createPieceCIDStream(): {
  stream: TransformStream<Uint8Array, Uint8Array>
  getPieceCID: () => PieceCID | null
} {
  const hasher = Hasher.create()
  let finished = false
  let pieceCid: PieceCID | null = null

  const stream = new TransformStream<Uint8Array, Uint8Array>({
    transform(chunk: Uint8Array, controller: TransformStreamDefaultController<Uint8Array>) {
      // Write chunk to hasher
      hasher.write(chunk)
      // Pass chunk through unchanged
      controller.enqueue(chunk)
    },

    flush() {
      // Calculate final PieceCID when stream ends
      const digest = hasher.digest()
      pieceCid = Link.create(Raw.code, digest)
      finished = true
    },
  })

  return {
    stream,
    getPieceCID: () => {
      if (!finished) {
        return null
      }
      return pieceCid
    },
  }
}
