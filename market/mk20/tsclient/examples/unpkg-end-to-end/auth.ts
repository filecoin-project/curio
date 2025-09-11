// Re-export all auth utilities from the main src module
export { 
  AuthConfig, 
  buildAuthHeader, 
  createClient, 
  sanitizeAuthHeader, 
  runPreflightChecks 
} from '../../src/auth';

/**
 * Get authentication configuration from environment variables
 * This is the only environment-specific function that stays in examples
 */
export function getAuthConfigFromEnv(): import('../../src/auth').AuthConfig {
  if (process.env.PDP_INSECURE_TLS === '1') {
    // Disable TLS verification (use only for debugging!)
    process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
    console.warn('WARNING: PDP_INSECURE_TLS=1 set. TLS verification disabled.');
  }

  const keyType = (process.env.PDP_KEY_TYPE || 'ed25519').toLowerCase() as 'ed25519' | 'secp256k1';
  
  const recordKeeper = process.env.PDP_RECORD_KEEPER;
  if (!recordKeeper) {
    console.error('❌ REQUIRED ENVIRONMENT VARIABLE MISSING: PDP_RECORD_KEEPER');
    console.error('   The record keeper is required for PDPv1 deals.');
    console.error('   Set it with: export PDP_RECORD_KEEPER=your-record-keeper-address');
    throw new Error('REQUIRED ENVIRONMENT VARIABLE MISSING: PDP_RECORD_KEEPER');
  }

  return {
    serverUrl: process.env.PDP_URL || 'http://localhost:8080',
    clientAddr: process.env.PDP_CLIENT || 'f1client...',
    recordKeeper,
    contractAddress: process.env.PDP_CONTRACT || '0x0000000000000000000000000000000000000000',
    keyType,
    publicKeyB64: process.env.PDP_PUBLIC_KEY_B64,
    privateKeyB64: process.env.PDP_PRIVATE_KEY_B64,
    secpPrivateKeyHex: process.env.PDP_SECP_PRIVATE_KEY_HEX,
    secpPrivateKeyB64: process.env.PDP_SECP_PRIVATE_KEY_B64,
  };
}
