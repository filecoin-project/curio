// Copyright Supranational LLC

#ifndef __DATA_STRUCTURES_HPP__
#define __DATA_STRUCTURES_HPP__

#include <cstdint>
#include "constants.hpp"
#include "../nvme/ring_t.hpp"
#include "../nvme/nvme_io_tracker_t.hpp"

// One node worth of parallel sectors
template<class C>
struct parallel_node_t {
  node_t sectors[C::PARALLEL_SECTORS];
};

// One page of nodes. In the end page will be a contiguous block of memory
// so it's easy to access uint32_t's starting at &nodes[0].
template<class C>
struct page_t {
  parallel_node_t<C> parallel_nodes[C::NODES_PER_PAGE];
};

// Buffer for replica IDs for each sector
// First  32B = replica_id
// Second 32B = current_layer || current_node || 0's (20B)
// Third  32B = padding (0x80) || 0's (31B)                Only for node 0
// Fourth 32B = 0's || length (512b = 64B = 0x200)         Only for node 0
// Final element is padding for all nodes > 0
// This structure gets replicated per hasher rather than a single instance
// for all parallel sectors. This is because the coordinator creates a
// packed buffer when staging data for parents that contains only the parents
// needed for that hasher, not all parallel sectors. This means the offsets between
// parents is not the same as for all PARALLEL_SECTORS.
struct replica_id_buffer_t {
  uint32_t ids[NODES_PER_HASHER][NODE_WORDS];
  uint32_t cur_loc[NODES_PER_HASHER][NODE_WORDS];
  uint32_t pad_0[NODES_PER_HASHER][NODE_WORDS];
  uint32_t pad_1[NODES_PER_HASHER][NODE_WORDS];
  uint32_t padding[NODES_PER_HASHER][NODE_WORDS];
};

template<class T, int sz>
struct batch_t {
  // Note BATCH_SIZE does not add to sizeof(batch_t)
  static const int BATCH_SIZE = sz;
  T batch[sz];
};

// Type to store pointers to parent pages
//
// **Handling of very recent nodes**
//
// The coordinator will create a local copy of parents for the hasher to
// access. For parents that are far from the head it's fine to just copy
// the data. For local parents the data might be exist at the time the
// coordinator is setting up the local buffer so we still need to pass the
// parent pointer to the hasher.
//
// Storage core
// - Sets up parent pointers as usual
// - For nodes there are local do not record reference counts
//
// Coordinator
// - Copies data into local buffer for hashers to use
// - For nodes that are local it does not copy the data. Instead it
//   passes pointer to the hashers in a side struct
//
// Hashers
// - Set up parent pointers into local buffer or from side struct

template<class C>
struct parent_ptr_t {
  // Pointer to the parent in the node buffer or parent buffer
  parallel_node_t<C>* ptr;
};
// The parent pointers must be contiguous so synchronization data is
// stored in a separate struct
struct parent_ptr_sync_t {
  static const uint32_t NOT_NODE_BUFFER = (uint32_t)-1;
  static const uint32_t LOCAL_NODE      = (uint32_t)-2;
  // When a parent pointer points into the node buffer record the
  // node buffer index
  uint32_t node_buffer_idx;
  inline bool is_node_buffer() {
    return node_buffer_idx != NOT_NODE_BUFFER && node_buffer_idx != LOCAL_NODE;
  }
};

// Structure to iterate over node and layer
// To disambiguate nodes on the various layers we combine the layer and
// node into a single id. In this way all nodes are unique and can be
// added, subtracted, etc. This is useful for managing the cache across
// layers.
template<class C>
class node_id_t {
  uint64_t _id;

public:
  node_id_t() noexcept {
    _id = 0;
  }
  node_id_t(uint64_t node) {
    _id = node;
  }
  node_id_t(uint32_t layer, uint32_t node) {
    _id = ((uint64_t)layer << C::GetNodeBits()) | node;
  }

  uint64_t id() {
    return _id;
  }
  uint32_t node() {
    return _id & C::GetNodeMask();
  }
  uint32_t layer() {
    return _id >> C::GetNodeBits();
  }
  bool operator<(const node_id_t x) const {
    return _id < x._id;
  }
  void operator++(int) {
    _id++;
  }
  void operator--(int) {
    _id--;
  }
  void operator+=(uint64_t x) {
    _id += x;
  }
  operator uint64_t() {
    return _id;
  }
};

struct node_io_t {
  enum type_e {
    READ = 0,
    WRITE,
    NOP
  };

  // Node to read/write
  uint64_t node;
  // Read or write
  type_e type;

  // Used for callbacks to signal when data is valid
  ring_buffer_valid_t* valid;

  // Used for SPDK calls
  nvme_io_tracker_t  tracker;
};


#endif
