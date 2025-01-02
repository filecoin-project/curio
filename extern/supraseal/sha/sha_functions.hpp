// Copyright Supranational LLC

#ifndef __SHA_FUNCTIONS_HPP__ 
#define __SHA_FUNCTIONS_HPP__

#include <cstdint>       // uint*

#if (defined(__x86_64__) || defined(__x86_64) || defined(_M_X64)) && \
     defined(__SHA__)
# define blst_sha256_block blst_sha256_block_data_order_shaext
#elif defined(__aarch64__) && defined(__ARM_FEATURE_CRYPTO)
# define blst_sha256_block blst_sha256_block_armv8
#else
# define blst_sha256_block blst_sha256_block_data_order
#endif

const uint32_t SHA256_INITIAL_DIGEST[8] = {
  0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
  0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19
};

// Modified to be in ABEF, CDGH format for SHA Extensions
const uint32_t SHA256_INITIAL_DIGEST_MB[16] = {
  0x9b05688c, 0x510e527f, 0xbb67ae85, 0x6a09e667,
  0x5be0cd19, 0x1f83d9ab, 0xa54ff53a, 0x3c6ef372,
  0x9b05688c, 0x510e527f, 0xbb67ae85, 0x6a09e667,
  0x5be0cd19, 0x1f83d9ab, 0xa54ff53a, 0x3c6ef372
};

extern "C" {
  void blst_sha256_block(uint32_t* h, const void* in, size_t blocks);
  void blst_sha256_emit(uint8_t* md, const uint32_t* h);

  void sha_ext_mbx2(uint32_t* digest, uint32_t** replica_id_buf,
                    uint32_t** data_buf, size_t offset,
                    size_t blocks, size_t repeat);
}

#endif // __SHA_FUNCTIONS_HPP__
