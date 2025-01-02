// Copyright Supranational LLC

#ifndef __STREAMING_LAYER_READER_FILES_HPP__
#define __STREAMING_LAYER_READER_FILES_HPP__

#include <vector>
#include <string>
#include <string.h>
#include "../util/mmap_t.hpp"
#include <util/thread_pool_t.hpp>

// Encapsulate the SPDK portion of reading layers from files
// C is not used here but is retained to be consistent with
// multi-sector c1
template<class C>
class streaming_node_reader_t {
  std::vector<mmap_t<node_t>> layer_files;
  // Packed indicates nodes within a single layer will be contiguous
  bool packed;
  size_t num_slots;
  size_t pages_per_slot;

  node_t* buffer;

  thread_pool_t pool;

public:
  streaming_node_reader_t(size_t sector_size, std::vector<std::string> layer_filenames)
    : buffer(nullptr)
  {
    layer_files.resize(layer_filenames.size());
    for (size_t i = 0; i < layer_filenames.size(); i++) {
      layer_files[i].mmap_read(layer_filenames[i], sector_size);
    }
  }

  ~streaming_node_reader_t() {
    free_slots();
  }

  bool data_is_big_endian() {
    return true;
  }

  // Allocate resource to perform N reads, each of size slot_node_count. These
  // will be indexed by slot_id
  // For C1 (load_nodes, get_node), we don't need local storage because it can
  // just use the mmapped files.
  // For PC2 create buffers to consolidate the data.
  void alloc_slots(size_t _num_slots, size_t slot_node_count, bool _packed) {
    packed = _packed;
    if (!packed) {
      // Reading will occur directly from files, so do nothing
    } else {
      pages_per_slot = (slot_node_count + C::NODES_PER_PAGE - 1) / C::NODES_PER_PAGE;
      num_slots = _num_slots;
      assert (posix_memalign((void **)&buffer, PAGE_SIZE,
                             num_slots * pages_per_slot * PAGE_SIZE) == 0);
    }
  }

  node_t* get_full_buffer(size_t &bytes) {
    bytes = num_slots * pages_per_slot * PAGE_SIZE;
    return buffer;
  }

  node_t* get_slot(size_t slot) {
    return &buffer[slot * pages_per_slot * C::NODES_PER_PAGE];
  }

  void free_slots() {
    free(buffer);
    buffer = nullptr;
  }

  ////////////////////////////////////////
  // Used for PC2
  ////////////////////////////////////////
  node_t* load_layers(size_t slot, uint32_t layer, uint64_t node,
                      size_t node_count, size_t num_layers,
                      std::atomic<uint64_t>* valid, size_t* valid_count) {
    if (num_layers == 1) {
      // Simply return a pointer to the mmap'd file data
      // This is used by pc2 when bulding just tree-r
      assert (layer == C::GetNumLayers() - 1);
      assert (C::PARALLEL_SECTORS == 1);
      assert (layer_files.size() == 1);

      *valid = 1;
      *valid_count = 1;

      return &layer_files[0][node];
    } else {
      // Consolidate the layer data into the buffer
      assert (C::PARALLEL_SECTORS == 1);
      assert (layer_files.size() == num_layers);
      // Nodes in each layer are expected to evenly fit in a page so that
      // the result is packed
      assert (node_count % C::NODES_PER_PAGE == 0);
      node_t* dest = &buffer[slot * pages_per_slot * C::NODES_PER_PAGE];

      pool.par_map(num_layers, 1, [&](size_t i) {
        layer_files[i].read_data(node, &dest[i * node_count], node_count);
      });

      *valid = 1;
      *valid_count = 1;

      return dest;
    }
  }

  ////////////////////////////////////////
  // Used for C1
  ////////////////////////////////////////

  // Load a vector of node IDs into the local buffer
  // The nodes are a vector of layer, node_id pairs
  // Since the nodes may be non-consecutive each node will use
  // an entire page in the buffer.
  int load_nodes(size_t slot, std::vector<std::pair<size_t, size_t>>& nodes) {
    assert (!packed);
    return 0;
  }

  // Retrieve a sector and node from the local buffer
  //   nodes       - the vector of nodes originally read into the local buffer
  //   idx         - the index of the node to retrieve
  //   sector_slot - the slot to retrive
  node_t& get_node(size_t slot, std::vector<std::pair<size_t, size_t>>& nodes,
                   size_t idx, size_t sector_slot) {
    assert (!packed);
    size_t layer = nodes[idx].first;
    size_t node = nodes[idx].second;
    node_t& n = layer_files[layer][node];
    return n;
  }
};

#endif
