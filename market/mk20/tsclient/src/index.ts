// Export the generated client and types
export * from '../generated';

// Re-export commonly used types for convenience
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
  Mk20DealCode as DealCode
} from '../generated';

// Export the main client class
export { DefaultApi as MarketClient } from '../generated';

// Export the custom client wrapper and utilities
export { MarketClient as Client, MarketClient, PieceCidUtils } from './client';
export type { MarketClientConfig } from './client';
export { StreamingPDP } from './streaming';
export { AuthUtils, Ed25519KeypairSigner, Secp256k1AddressSigner } from './auth';
export type { AuthSigner } from './auth';

// Export piece CID utilities from piece.ts
export { calculate as calculatePieceCID, asPieceCID, asLegacyPieceCID, createPieceCIDStream } from './piece';
export type { PieceCID, LegacyPieceCID } from './piece';

// Re-export configuration types
export type { Configuration } from '../generated';

// Re-export upload-related types for convenience
export type { 
  Mk20StartUpload as StartUpload, 
  Mk20UploadCode as UploadCode, 
  Mk20UploadStartCode as UploadStartCode, 
  Mk20UploadStatus as UploadStatus, 
  Mk20UploadStatusCode as UploadStatusCode 
} from '../generated';

// Top-level export that encompasses all exports with nice names
export namespace CurioMarket {
  // Re-export types with nice names
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

  // Re-export classes and utilities
  export { DefaultApi as MarketClient } from '../generated';
  export { MarketClient as Client, PieceCidUtils } from './client';
  export { StreamingPDP } from './streaming';
  export { AuthUtils, Ed25519KeypairSigner, Secp256k1AddressSigner } from './auth';
  export { calculate as calculatePieceCID, asPieceCID, asLegacyPieceCID, createPieceCIDStream } from './piece';
}
