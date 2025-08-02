// Copyright Supranational LLC

#ifndef __STREAMING_LAYER_READER_NVME_HPP__
#define __STREAMING_LAYER_READER_NVME_HPP__

#include <thread>
#include "../sealing/data_structures.hpp"

class nvme_controllers_t;
template<class C> struct streaming_node_reader_opaque_t;

// Encapsulate the SPDK portion of reading layers from NVMe
template<class C>
class streaming_node_reader_t {
  streaming_node_reader_opaque_t<C>* opaque;

  nvme_controllers_t* controllers;

  std::atomic<bool> terminator;
  std::thread reader_thread;

  // Packed indicates nodes within a single layer will be contiguous
  bool packed;
  size_t num_slots;
  size_t pages_per_slot;

public:
  streaming_node_reader_t(nvme_controllers_t* _controllers, size_t qpair,
                          size_t block_offset, int core_num, size_t idle_sleep);

  ~streaming_node_reader_t();

  bool data_is_big_endian() {
    return false;
  }

  // Allocate resource to perform N reads, each of size slot_node_count. These
  // will be indexed by slot_id
  // packed - indicates whether allocation should assume packed or unpacked node reads
  void alloc_slots(size_t N, size_t slot_node_count, bool _packed);

  uint8_t* get_slot(size_t slot);

  uint8_t* get_full_buffer(size_t &bytes);

  void free_slots();

  ////////////////////////////////////////
  // Used for PC2
  ////////////////////////////////////////

  uint8_t* load_layers(size_t slot, uint32_t layer, uint64_t node,
                       size_t node_count, size_t num_layers,
                       std::atomic<uint64_t>* valid, size_t* valid_count);

  ////////////////////////////////////////
  // Used for C1
  ////////////////////////////////////////

  // Load a vector of node IDs into the local buffer
  // The nodes are a vector of layer, node_id pairs
  // Since the nodes may be non-consecutive each node will use
  // an entire page in the buffer.
  int load_nodes(size_t slot, std::vector<std::pair<size_t, size_t>>& nodes);

  // Retrieve a sector and node from the local buffer
  //   nodes       - the vector of nodes originally read into the local buffer
  //   idx         - the index of the node to retrieve
  //   sector_slot - the slot to retrive
  node_t& get_node(size_t slot, std::vector<std::pair<size_t, size_t>>& nodes,
                   size_t idx, size_t sector_slot);
};

#endif
