// Copyright Supranational LLC

#include <cstdint>            // uint*
#include <cstring>            // memcpy
#include "replica_id.hpp"     // header
#include "../sha/sha_functions.hpp"  // SHA-256 functions

// Create replica ID
void create_replica_id(uint32_t* replica_id,
                       const uint8_t* prover_id,
                       const uint8_t* sector_id,
                       const uint8_t* ticket,
                       const uint8_t* comm_d,
                       const uint8_t* porep_seed) {

  uint8_t buf[192] = {0};

  std::memcpy(buf,       prover_id,  32);
  std::memcpy(buf +  32, sector_id,   8);
  std::memcpy(buf +  40, ticket,     32);
  std::memcpy(buf +  72, comm_d,     32);
  std::memcpy(buf + 104, porep_seed, 32);

  // Add padding and length (1088 bits -> 0x440)
  buf[136] = 0x80;
  buf[190] = 0x04;
  buf[191] = 0x40;

  // Initialize digest
  std::memcpy(replica_id, SHA256_INITIAL_DIGEST, 32);

  // Hash buffer, takes 3 SHA-256 blocks
  blst_sha256_block(replica_id, buf, 3);

  // Top two bits cutoff due to keeping in field
  replica_id[7] &= 0xFFFFFF3F;
}
