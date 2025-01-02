// Copyright Supranational LLC

#ifndef __REPLICA_ID_HPP__
#define __REPLICA_ID_HPP__

#include <cstdint>            // uint*

// Create replica ID
void create_replica_id(uint32_t* replica_id,
                       const uint8_t* prover_id,
                       const uint8_t* sector_id,
                       const uint8_t* ticket,
                       const uint8_t* comm_d,
                       const uint8_t* porep_seed);

#endif // __REPLICA_ID_HPP__
