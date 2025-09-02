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

// Export the custom client wrapper
export { MarketClient as Client } from './client';
export type { MarketClientConfig } from './client';

// Export piece CID utilities
export { PieceCidUtils } from './client';

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
