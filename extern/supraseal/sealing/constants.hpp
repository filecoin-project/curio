// Copyright Supranational LLC

#ifndef __CONSTANTS_HPP__
#define __CONSTANTS_HPP__

#include <cstddef>       // size_t
#include <cstdint>       // uint*
#include <cmath>         // log2
#include <atomic>
#include <arpa/inet.h>

///////////////////////////////////////////////////////////
// Graph constants
///////////////////////////////////////////////////////////

// Only 512MB and 32GB are broadly tested
enum SectorSizeLg {
  Sector2KB   = 11,
  Sector4KB   = 12,
  Sector16KB  = 14,
  Sector32KB  = 15,
  Sector8MB   = 23,
  Sector16MB  = 24,
  Sector512MB = 29,
  Sector1GB   = 30,
  Sector32GB  = 35,
  Sector64GB  = 36
};

const size_t NODE_SIZE_LG      = 5; // In Bytes. SHA-256 digest size
const size_t NODE_SIZE         = (1 << NODE_SIZE_LG); // In Bytes. SHA-256 digest size
const size_t NODE_WORDS        = NODE_SIZE / sizeof(uint32_t);

const size_t PARENT_COUNT_BASE = 6;  // Number of parents from same layer
const size_t PARENT_COUNT_EXP  = 8;  // Number of parents from previous layer
const size_t PARENT_COUNT      = PARENT_COUNT_BASE + PARENT_COUNT_EXP;
const size_t PARENT_SIZE       = sizeof(uint32_t);

const size_t NODE_0_REPEAT      = 1;
const size_t NODE_0_BLOCKS      = 2;
const size_t LAYER_1_REPEAT     = 3;
const size_t LAYERS_GT_1_REPEAT = 7;
const size_t NODE_GT_0_BLOCKS   = 20;

// Full padding block for the hash buffer of node 0 in each layer
const uint8_t NODE_0_PADDING[] __attribute__ ((aligned (32))) = {
  0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00
};

// Half padding block for the hash buffer of non node 0 in each layer
const uint8_t NODE_PADDING_X2[] __attribute__ ((aligned (32))) = {
  0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x27, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
  0x00, 0x00, 0x00, 0x00, 0x00, 0x27, 0x00, 0x00
};

// Smallest unit of memory we are working with. A page is typically 4KB
#ifndef PAGE_SIZE
#define PAGE_SIZE 4096
#endif

const size_t NODES_PER_HASHER  = 2;   // Number of nodes to calculate per hashing thread

/////////////////////////////////////////////////////////
// Constants solely for testing, these will be inputs
/////////////////////////////////////////////////////////

// ticket comes from on-chain randomness
const uint8_t TICKET[]  __attribute__ ((aligned (32))) = { 1, 1, 1, 1, 1, 1, 1, 1,
                              1, 1, 1, 1, 1, 1, 1, 1,
                              1, 1, 1, 1, 1, 1, 1, 1,
                              1, 1, 1, 1, 1, 1, 1, 1 };

// porep_seed is the config porep_id
// Mostly 0's with a single byte indicating the sector size and api version
// See porep_id() in rust-filecoin-proofs-api/src/registry.rs
// const uint8_t SEED[] = { 99, 99, 99, 99, 99, 99, 99, 99,
//                          99, 99, 99, 99, 99, 99, 99, 99,
//                          99, 99, 99, 99, 99, 99, 99, 99,
//                          99, 99, 99, 99, 99, 99, 99, 99 };
const uint8_t SEED[] __attribute__ ((aligned (32))) = { 1, 1, 1, 1, 1, 1, 1, 1,
                         1, 1, 1, 1, 1, 1, 1, 1,
                         1, 1, 1, 1, 1, 1, 1, 1,
                         1, 1, 1, 1, 1, 1, 1, 1 };

// Default ordering for atomics
static const std::memory_order DEFAULT_MEMORY_ORDER = std::memory_order_seq_cst;
//static const std::memory_order DEFAULT_MEMORY_ORDER = std::memory_order_relaxed;


/////////////////////////////////////////////////////////
// Constants for PC1 buffer sizing
/////////////////////////////////////////////////////////

// Operations between threads are batched for efficient coordination.
// BATCH_SIZE must be a multiple of PARENT_COUNT or PARENT_COUNT - 1
// Parent pointers contain pointers to all 14 parents
const size_t PARENT_PTR_BATCH_SIZE = PARENT_COUNT;
// We only need 13 nodes from disk/cache since 1 was always just hashed
const size_t PAGE_BATCH_SIZE = PARENT_COUNT - 1;

// Parents buffer stores base and expander parent values that are not located
//  in the node buffer. The intention is the parents buffer is filled with
//  values read from disk. The first parent is not required since it is the
//  previous node and we know for sure it is in the node buffer.
const size_t PARENT_BUFFER_BATCHES = 1<<18;
const size_t PARENT_BUFFER_NODES = PARENT_BUFFER_BATCHES * PAGE_BATCH_SIZE;

// Number of hashing buffers allocated in memory.
// Once hit then the buffer wraps around to the beginning
// This needs to be tuned based on available RAM and timing required to keep
//  hashing threads and reading threads in sync.
// Should be sized parent buffer + desired cache nodes
const size_t NODE_BUFFER_BATCHES = PARENT_BUFFER_BATCHES * 2;
const size_t NODE_BUFFER_NODES = NODE_BUFFER_BATCHES;
const size_t NODE_BUFFER_SYNC_LG_BATCH_SIZE = 2;
const size_t NODE_BUFFER_SYNC_BATCH_SIZE = 1<<NODE_BUFFER_SYNC_LG_BATCH_SIZE;
const size_t NODE_BUFFER_SYNC_BATCH_MASK = NODE_BUFFER_SYNC_BATCH_SIZE - 1;
const size_t NODE_BUFFER_SYNC_BATCHES = NODE_BUFFER_BATCHES / NODE_BUFFER_SYNC_BATCH_SIZE;
#define NODE_IDX_TO_SYNC_IDX(node) ((node) >> NODE_BUFFER_SYNC_LG_BATCH_SIZE)

// The coordinator will create a contiguous block of memory where it
// copies the data from the parent pointers so the hashers can simply
// walk through it. There will be COORD_BATCH_COUNT batches, each of
// size COORD_BATCH_SIZE. Further we guarantee all of these nodes will
// be present in the node buffer so we won't bother with
// reference/consumed counts.
static const size_t COORD_BATCH_SIZE = 256;
static const size_t COORD_BATCH_COUNT = 4;
static const size_t COORD_BATCH_NODE_COUNT = COORD_BATCH_SIZE * COORD_BATCH_COUNT;

/////////////////////////////////////////////////////////
// Constants for C1
/////////////////////////////////////////////////////////

const size_t LABEL_PARENTS        = 37;
const size_t LAYER_ONE_REPEAT_SEQ = 6;
const size_t LAYER_ONE_FINAL_SEQ  = LABEL_PARENTS %
                                    (LAYER_ONE_REPEAT_SEQ *
                                     PARENT_COUNT_BASE);
const size_t LAYER_N_REPEAT_SEQ   = 2;
const size_t LAYER_N_FINAL_SEQ    = LABEL_PARENTS %
                                    (LAYER_N_REPEAT_SEQ * PARENT_COUNT);

const uint32_t SINGLE_PROOF_DATA = 0; // storage-proofs-core/src/merkle/proof.rs

// node_t is here instead of data_structures because it is used to compute constants
// in various places and it is comprised of only primitive types (and so doesn't
// pull in a bunch of other includes).
struct node_t {
  uint32_t limbs[NODE_WORDS];
  void reverse_l() {
    for (size_t i = 0; i < NODE_WORDS; i++) {
      limbs[i] = htonl(limbs[i]);
    }
  }
  void reverse_s() {
    for (size_t i = 0; i < NODE_WORDS * 2; i++) {
      ((uint16_t*)limbs)[i] = htons(((uint16_t*)limbs)[i]);
    }
  }
};

#include "sector_parameters.hpp"

/////////////////////////////////////////////////////////
// Templated constants for number of parallel sectors
/////////////////////////////////////////////////////////

// Number of sectors being processed within the buffers.
// Used to determine stride between parents in node_buffer
// A good value here would be a multiple that fits in a 4KB page
// 128: 32B * 128    = 4096  Page consumed by single node index
// 64:  32B * 64 * 2 = 4096  Page consumed by two node indices
// 32:  32B * 32 * 4 = 4096  Page consumed by four node indices
// 16:  32B * 16 * 8 = 4096  Page consumed by eight node indices
// What we are trying to accomplish with this is to improve the efficiency of
//  random reads. Typcially when reading distant base parents and all
//  expander parents an entire page needs to be read to get only a single 32B
//  node.  If this is done across many sealing threads, then the read
//  efficiency is not good. With the interleaved approach the goal is for all
//  4KB page data to be useful.  This can reduce the number of system level
//  read operations by the interleaved node factor.
// Must evenly fit in the page!
template<int SECTORS, class sector_parameters_t>
class sealing_config_t : public sector_parameters_t {
public:
  static const size_t PARALLEL_SECTORS  = SECTORS;
  // Number of nodes stored per page (packed)
  static const size_t NODES_PER_PAGE    = PAGE_SIZE / (PARALLEL_SECTORS * NODE_SIZE);

  // // Sector parameters
  // static const sector_parameters_t P;
};

#ifdef RUNTIME_SECTOR_SIZE
typedef sealing_config_t<128, sector_parameters2KB  > sealing_config_128_2KB_t;
typedef sealing_config_t<128, sector_parameters4KB  > sealing_config_128_4KB_t;
typedef sealing_config_t<128, sector_parameters16KB > sealing_config_128_16KB_t;
typedef sealing_config_t<128, sector_parameters32KB > sealing_config_128_32KB_t;
typedef sealing_config_t<128, sector_parameters8MB  > sealing_config_128_8MB_t;
typedef sealing_config_t<128, sector_parameters16MB > sealing_config_128_16MB_t;
typedef sealing_config_t<128, sector_parameters1GB  > sealing_config_128_1GB_t;
typedef sealing_config_t<128, sector_parameters64GB > sealing_config_128_64GB_t;
typedef sealing_config_t< 64, sector_parameters2KB  > sealing_config_64_2KB_t;
typedef sealing_config_t< 64, sector_parameters4KB  > sealing_config_64_4KB_t;
typedef sealing_config_t< 64, sector_parameters16KB > sealing_config_64_16KB_t;
typedef sealing_config_t< 64, sector_parameters32KB > sealing_config_64_32KB_t;
typedef sealing_config_t< 64, sector_parameters8MB  > sealing_config_64_8MB_t;
typedef sealing_config_t< 64, sector_parameters16MB > sealing_config_64_16MB_t;
typedef sealing_config_t< 64, sector_parameters1GB  > sealing_config_64_1GB_t;
typedef sealing_config_t< 64, sector_parameters64GB > sealing_config_64_64GB_t;
typedef sealing_config_t< 32, sector_parameters2KB  > sealing_config_32_2KB_t;
typedef sealing_config_t< 32, sector_parameters4KB  > sealing_config_32_4KB_t;
typedef sealing_config_t< 32, sector_parameters16KB > sealing_config_32_16KB_t;
typedef sealing_config_t< 32, sector_parameters32KB > sealing_config_32_32KB_t;
typedef sealing_config_t< 32, sector_parameters8MB  > sealing_config_32_8MB_t;
typedef sealing_config_t< 32, sector_parameters16MB > sealing_config_32_16MB_t;
typedef sealing_config_t< 32, sector_parameters1GB  > sealing_config_32_1GB_t;
typedef sealing_config_t< 32, sector_parameters64GB > sealing_config_32_64GB_t;
typedef sealing_config_t< 16, sector_parameters2KB  > sealing_config_16_2KB_t;
typedef sealing_config_t< 16, sector_parameters4KB  > sealing_config_16_4KB_t;
typedef sealing_config_t< 16, sector_parameters16KB > sealing_config_16_16KB_t;
typedef sealing_config_t< 16, sector_parameters32KB > sealing_config_16_32KB_t;
typedef sealing_config_t< 16, sector_parameters8MB  > sealing_config_16_8MB_t;
typedef sealing_config_t< 16, sector_parameters16MB > sealing_config_16_16MB_t;
typedef sealing_config_t< 16, sector_parameters1GB  > sealing_config_16_1GB_t;
typedef sealing_config_t< 16, sector_parameters64GB > sealing_config_16_64GB_t;
typedef sealing_config_t<  8, sector_parameters2KB  > sealing_config_8_2KB_t;
typedef sealing_config_t<  8, sector_parameters4KB  > sealing_config_8_4KB_t;
typedef sealing_config_t<  8, sector_parameters16KB > sealing_config_8_16KB_t;
typedef sealing_config_t<  8, sector_parameters32KB > sealing_config_8_32KB_t;
typedef sealing_config_t<  8, sector_parameters8MB  > sealing_config_8_8MB_t;
typedef sealing_config_t<  8, sector_parameters16MB > sealing_config_8_16MB_t;
typedef sealing_config_t<  8, sector_parameters1GB  > sealing_config_8_1GB_t;
typedef sealing_config_t<  8, sector_parameters64GB > sealing_config_8_64GB_t;
typedef sealing_config_t<  4, sector_parameters2KB  > sealing_config_4_2KB_t;
typedef sealing_config_t<  4, sector_parameters4KB  > sealing_config_4_4KB_t;
typedef sealing_config_t<  4, sector_parameters16KB > sealing_config_4_16KB_t;
typedef sealing_config_t<  4, sector_parameters32KB > sealing_config_4_32KB_t;
typedef sealing_config_t<  4, sector_parameters8MB  > sealing_config_4_8MB_t;
typedef sealing_config_t<  4, sector_parameters16MB > sealing_config_4_16MB_t;
typedef sealing_config_t<  4, sector_parameters1GB  > sealing_config_4_1GB_t;
typedef sealing_config_t<  4, sector_parameters64GB > sealing_config_4_64GB_t;
typedef sealing_config_t<  2, sector_parameters2KB  > sealing_config_2_2KB_t;
typedef sealing_config_t<  2, sector_parameters4KB  > sealing_config_2_4KB_t;
typedef sealing_config_t<  2, sector_parameters16KB > sealing_config_2_16KB_t;
typedef sealing_config_t<  2, sector_parameters32KB > sealing_config_2_32KB_t;
typedef sealing_config_t<  2, sector_parameters8MB  > sealing_config_2_8MB_t;
typedef sealing_config_t<  2, sector_parameters16MB > sealing_config_2_16MB_t;
typedef sealing_config_t<  2, sector_parameters1GB  > sealing_config_2_1GB_t;
typedef sealing_config_t<  2, sector_parameters64GB > sealing_config_2_64GB_t;
typedef sealing_config_t<  1, sector_parameters2KB  > sealing_config_1_2KB_t;
typedef sealing_config_t<  1, sector_parameters4KB  > sealing_config_1_4KB_t;
typedef sealing_config_t<  1, sector_parameters16KB > sealing_config_1_16KB_t;
typedef sealing_config_t<  1, sector_parameters32KB > sealing_config_1_32KB_t;
typedef sealing_config_t<  1, sector_parameters8MB  > sealing_config_1_8MB_t;
typedef sealing_config_t<  1, sector_parameters16MB > sealing_config_1_16MB_t;
typedef sealing_config_t<  1, sector_parameters1GB  > sealing_config_1_1GB_t;
typedef sealing_config_t<  1, sector_parameters64GB > sealing_config_1_64GB_t;
#endif
typedef sealing_config_t<128, sector_parameters512MB> sealing_config_128_512MB_t;
typedef sealing_config_t<128, sector_parameters32GB > sealing_config_128_32GB_t;
typedef sealing_config_t< 64, sector_parameters512MB> sealing_config_64_512MB_t;
typedef sealing_config_t< 64, sector_parameters32GB > sealing_config_64_32GB_t;
typedef sealing_config_t< 32, sector_parameters512MB> sealing_config_32_512MB_t;
typedef sealing_config_t< 32, sector_parameters32GB > sealing_config_32_32GB_t;
typedef sealing_config_t< 16, sector_parameters512MB> sealing_config_16_512MB_t;
typedef sealing_config_t< 16, sector_parameters32GB > sealing_config_16_32GB_t;
typedef sealing_config_t<  8, sector_parameters512MB> sealing_config_8_512MB_t;
typedef sealing_config_t<  8, sector_parameters32GB > sealing_config_8_32GB_t;
typedef sealing_config_t<  4, sector_parameters512MB> sealing_config_4_512MB_t;
typedef sealing_config_t<  4, sector_parameters32GB > sealing_config_4_32GB_t;
typedef sealing_config_t<  2, sector_parameters512MB> sealing_config_2_512MB_t;
typedef sealing_config_t<  2, sector_parameters32GB > sealing_config_2_32GB_t;
typedef sealing_config_t<  1, sector_parameters512MB> sealing_config_1_512MB_t;
typedef sealing_config_t<  1, sector_parameters32GB > sealing_config_1_32GB_t;

#endif // __CONSTANTS_HPP__
