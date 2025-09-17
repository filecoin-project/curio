// Export the generated client and types
export * from '../generated';

// Import everything we need for the CurioMarket object
import { DefaultApi as MarketClient } from '../generated';
import { MarketClient as Client, PieceCidUtils } from './client';
import { StreamingPDP } from './streaming';
import { AuthUtils, Ed25519KeypairSigner, Secp256k1AddressSigner } from './auth';
import { calculate as calculatePieceCID, asPieceCID, asLegacyPieceCID, createPieceCIDStream } from './piece';

// Top-level export that encompasses all exports with nice names
export const CurioMarket = {
  // Classes and utilities
  MarketClient,
  Client,
  PieceCidUtils,
  StreamingPDP,
  AuthUtils,
  Ed25519KeypairSigner,
  Secp256k1AddressSigner,
  calculatePieceCID,
  asPieceCID,
  asLegacyPieceCID,
  createPieceCIDStream,
} as const;

// Export types with nice names
export type {
  Mk20Deal as Deal,
  Mk20DataSource as DataSource,
  Mk20Products as Products,
  Mk20DDOV1 as DDOV1,
  Mk20PDPV1 as PDPV1,
  Mk20RetrievalV1 as RetrievalV1,
  Mk20DealProductStatusResponse as DealProductStatusResponse,
  Mk20SupportedContracts as SupportedContracts,
  Mk20SupportedProducts as SupportedProducts,
  Mk20SupportedDataSources as SupportedDataSources,
  Mk20DealCode as DealCode,
  Mk20StartUpload as StartUpload,
  Mk20UploadCode as UploadCode,
  Mk20UploadStartCode as UploadStartCode,
  Mk20UploadStatus as UploadStatus,
  Mk20UploadStatusCode as UploadStatusCode,
  Configuration
} from '../generated';

export type { MarketClientConfig } from './client';
export type { AuthSigner } from './auth';
export type { PieceCID, LegacyPieceCID } from './piece';