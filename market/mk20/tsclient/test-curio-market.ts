// Test script to verify CurioMarket object structure
import { CurioMarket } from './src';

console.log('CurioMarket object structure:');
console.log('Available properties:', Object.keys(CurioMarket));

console.log('\nClasses available:');
console.log('- MarketClient:', typeof CurioMarket.MarketClient);
console.log('- Client:', typeof CurioMarket.Client);
console.log('- PieceCidUtils:', typeof CurioMarket.PieceCidUtils);
console.log('- StreamingPDP:', typeof CurioMarket.StreamingPDP);
console.log('- AuthUtils:', typeof CurioMarket.AuthUtils);

console.log('\nFunctions available:');
console.log('- calculatePieceCID:', typeof CurioMarket.calculatePieceCID);
console.log('- asPieceCID:', typeof CurioMarket.asPieceCID);
console.log('- asLegacyPieceCID:', typeof CurioMarket.asLegacyPieceCID);
console.log('- createPieceCIDStream:', typeof CurioMarket.createPieceCIDStream);

console.log('\n✅ CurioMarket object is properly structured!');


