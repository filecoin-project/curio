import { PieceCidUtils } from '../src';

// Example: Compute piece CID v2 from blobs
async function computePieceCidExample() {
  try {
    console.log('🔍 Computing piece CID v2 from blobs...\n');

    // Create mock blobs (in real usage, these would be actual files)
    const mockBlobs = [
      new Blob(['Hello, this is file 1 content'], { type: 'text/plain' }),
      new Blob(['This is file 2 with different content'], { type: 'text/plain' }),
      new Blob(['And here is file 3 content'], { type: 'text/plain' })
    ];

    console.log('📁 Input blobs:');
    mockBlobs.forEach((blob, index) => {
      console.log(`  File ${index + 1}: ${blob.size} bytes`);
    });

    // Compute piece CID v2
    const pieceCid = await PieceCidUtils.computePieceCidV2(mockBlobs);
    
    console.log('\n✅ Piece CID v2 computed successfully!');
    console.log(`🔗 Piece CID: ${pieceCid}`);
    console.log(`📊 Total size: ${mockBlobs.reduce((sum, blob) => sum + blob.size, 0)} bytes`);

    return pieceCid;

  } catch (error) {
    console.error('❌ Failed to compute piece CID:', error);
    throw error;
  }
}

// Example: Convert existing CID v1 to piece CID v2
async function convertCidV1ToV2Example() {
  try {
    console.log('\n🔄 Converting CID v1 to piece CID v2...\n');

    // Create a mock CID v1 (in practice, this would come from somewhere)
    const { CID } = await import('multiformats/cid');
    const { sha256 } = await import('multiformats/hashes/sha2');
    
    const mockData = new TextEncoder().encode('Sample data for CID computation');
    const hash = await sha256.digest(mockData);
    const cidV1 = CID.create(1, 0x55, hash); // raw codec

    console.log(`📥 Input CID v1: ${cidV1.toString()}`);
    console.log(`🔍 Codec: ${cidV1.code}`);
    console.log(`🔍 Hash: ${cidV1.multihash.name}`);

    // Convert to piece CID v2
    const pieceCidV2 = await PieceCidUtils.pieceCidV2FromV1(cidV1, mockData.length);
    
    console.log('\n✅ Conversion successful!');
    console.log(`📤 Output piece CID v2: ${pieceCidV2.toString()}`);
    console.log(`🔍 Output codec: ${pieceCidV2.code}`);
    console.log(`🔍 Output hash: ${pieceCidV2.multihash.name}`);

    return pieceCidV2;

  } catch (error) {
    console.error('❌ Failed to convert CID:', error);
    throw error;
  }
}

// Example: Handle different blob types and sizes
async function handleDifferentBlobTypesExample() {
  try {
    console.log('\n🎭 Handling different blob types and sizes...\n');

    const blobs = [
      new Blob(['Small text file'], { type: 'text/plain' }),
      new Blob(['Medium sized content here'], { type: 'text/plain' }),
      new Blob(['Large content with many characters to make it bigger'], { type: 'text/plain' }),
      new Blob(['Another file with content'], { type: 'text/plain' })
    ];

    console.log('📁 Blob details:');
    blobs.forEach((blob, index) => {
      console.log(`  Blob ${index + 1}: ${blob.size} bytes, type: ${blob.type}`);
    });

    // Compute piece CID v2
    const pieceCid = await PieceCidUtils.computePieceCidV2(blobs);
    
    console.log('\n✅ Piece CID computed for mixed blob types!');
    console.log(`🔗 Piece CID: ${pieceCid}`);
    console.log(`📊 Total size: ${blobs.reduce((sum, blob) => sum + blob.size, 0)} bytes`);

    return pieceCid;

  } catch (error) {
    console.error('❌ Failed to handle different blob types:', error);
    throw error;
  }
}

// Example: Error handling for invalid inputs
async function errorHandlingExample() {
  try {
    console.log('\n⚠️  Testing error handling...\n');

    // Test with empty blob array
    try {
      await PieceCidUtils.computePieceCidV2([]);
      console.log('❌ Should have thrown error for empty blobs');
    } catch (error) {
      console.log('✅ Correctly handled empty blob array:', error.message);
    }

    // Test with invalid CID
    try {
      const { CID } = await import('multiformats/cid');
      const invalidCid = CID.create(1, 0x999, { code: 0x999, digest: new Uint8Array(16) });
      await PieceCidUtils.pieceCidV2FromV1(invalidCid, 100);
      console.log('❌ Should have thrown error for invalid CID');
    } catch (error) {
      console.log('✅ Correctly handled invalid CID:', error.message);
    }

  } catch (error) {
    console.error('❌ Error handling test failed:', error);
  }
}

export { 
  computePieceCidExample, 
  convertCidV1ToV2Example, 
  handleDifferentBlobTypesExample, 
  errorHandlingExample 
};
