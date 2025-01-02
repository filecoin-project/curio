// Copyright Supranational LLC

#include <thread>
#include <assert.h>
#include "../sealing/constants.hpp"
#include "../nvme/nvme.hpp"
#include "../sealing/data_structures.hpp"
#include "../util/stats.hpp"
#include "../util/util.hpp"
#include "../pc1/node_rw_t.hpp"
#include "streaming_node_reader_nvme.hpp"

typedef batch_t<node_io_t, 1> node_io_batch_t;
int g_spdk_error = 0;

template<class C>
struct streaming_node_reader_opaque_t {
  // Fixed size FIFOs for requests to the parent reader
  mt_fifo_t<node_io_batch_t> node_read_fifo;
  node_rw_t<C, node_io_batch_t>* node_reader;

  spdk_ptr_t<page_t<C>> local_buffer;
  std::vector<node_io_batch_t> node_ios;
};

template<class C> streaming_node_reader_t<C>::
streaming_node_reader_t(nvme_controllers_t* _controllers, size_t qpair,
                        size_t block_offset, int core_num, size_t idle_sleep)
  : controllers(_controllers), terminator(false)
  {
    num_slots = 0;
    opaque = new streaming_node_reader_opaque_t<C>();

    // Streaming reads
    SPDK_ASSERT(opaque->node_read_fifo.create("node_read_fifo", 4 * nvme_controller_t::queue_size));
    opaque->node_reader = new node_rw_t<C, node_io_batch_t>
      (terminator, *controllers, opaque->node_read_fifo,
       qpair, block_offset);

    reader_thread = std::thread([&, core_num, idle_sleep]() {
      set_core_affinity(core_num);
      assert(opaque->node_reader->process(idle_sleep) == 0);
    });
  }

template<class C> streaming_node_reader_t<C>::
~streaming_node_reader_t() {
  terminator = true;
  reader_thread.join();
  delete opaque->node_reader;
  delete opaque;
}

template<class C> void streaming_node_reader_t<C>::
alloc_slots(size_t _num_slots, size_t slot_node_count, bool _packed) {
  packed = _packed;
  // Round up to an even number of pages
  if (packed) {
    pages_per_slot = (slot_node_count + C::NODES_PER_PAGE - 1) / C::NODES_PER_PAGE;
    num_slots = _num_slots;
  } else {
    pages_per_slot = slot_node_count;
    num_slots = _num_slots;
  }

  // Allocate storage
  opaque->local_buffer.alloc(num_slots * pages_per_slot);

  // Allocate one node_io per page
  opaque->node_ios.resize(num_slots * pages_per_slot);
}

template<class C> void streaming_node_reader_t<C>::
free_slots() {
  opaque->local_buffer.free();
  opaque->node_ios.clear();
}

template<class C> uint8_t* streaming_node_reader_t<C>::
get_full_buffer(size_t &bytes) {
  bytes = num_slots * pages_per_slot * sizeof(page_t<C>);
  return (uint8_t*)&opaque->local_buffer[0];
}

template<class C> uint8_t* streaming_node_reader_t<C>::
get_slot(size_t slot) {
  return (uint8_t*)&opaque->local_buffer[slot * pages_per_slot];
}

template<class C> uint8_t* streaming_node_reader_t<C>::
load_layers(size_t slot, uint32_t layer, uint64_t node,
            size_t node_count, size_t num_layers,
            std::atomic<uint64_t>* valid, size_t* valid_count) {
  assert (packed);
  assert (slot < num_slots);
  node_io_batch_t* node_ios = &opaque->node_ios[slot * pages_per_slot];
  page_t<C>* pages = &opaque->local_buffer[slot * pages_per_slot];

  size_t total_pages = num_layers * node_count / C::NODES_PER_PAGE;
  assert (total_pages <= pages_per_slot);

  // Valid counter
  valid->store(0);

  node_id_t<C> node_to_read(layer, node);

  size_t idx = 0;
  uint32_t cur_layer = layer;
  for (size_t i = 0; i < num_layers; i++) {
    while (opaque->node_read_fifo.free_count() < node_count) {
      usleep(100);
    }
    for (size_t j = 0; j < node_count; j += C::NODES_PER_PAGE) {
      node_io_t& io = node_ios[idx].batch[0];
      io.type = node_io_t::type_e::READ;
      io.node = node_to_read;
      io.valid = valid;
      io.tracker.buf = (uint8_t*)&pages[idx];

      SPDK_ASSERT(opaque->node_read_fifo.enqueue(&node_ios[idx]));

      node_to_read += C::NODES_PER_PAGE;
      idx++;
    }
    // Increment the layer
    cur_layer++;
    node_to_read = node_id_t<C>(cur_layer, node);
  }
  *valid_count = total_pages;

  return (uint8_t*)pages;
}

template<class C> int streaming_node_reader_t<C>::
load_nodes(size_t slot, std::vector<std::pair<size_t, size_t>>& nodes) {
  assert (!packed);
  page_t<C>* pages = &opaque->local_buffer[slot * pages_per_slot];
  node_io_batch_t* node_ios = &opaque->node_ios[slot * pages_per_slot];

  assert (nodes.size() <= pages_per_slot);
  std::atomic<uint64_t> valid(0);
  for (size_t i = 0; i < nodes.size(); i++) {
    if (!opaque->node_read_fifo.is_full()) {
      node_io_t& io = node_ios[i].batch[0];
      io.type = node_io_t::type_e::READ;
      io.node = node_id_t<C>(nodes[i].first, nodes[i].second);
      io.valid = &valid;
      io.tracker.buf = (uint8_t*)&pages[i];

      SPDK_ERROR(opaque->node_read_fifo.enqueue(&node_ios[i]));
    }
  }
  while (valid < nodes.size()) {}
  return 0;
}

template<class C> node_t& streaming_node_reader_t<C>::
get_node(size_t slot, std::vector<std::pair<size_t, size_t>>& nodes,
         size_t idx, size_t sector_slot) {
  assert (!packed);
  page_t<C>* pages = &opaque->local_buffer[slot * pages_per_slot];
  size_t node = nodes[idx].second;
  node_t& n = pages[idx].
    parallel_nodes[node % C::NODES_PER_PAGE]
    .sectors[sector_slot];
  // From NVMe the node needs to still be byte reversed
  n.reverse_l();
  return n;
}

#ifdef RUNTIME_SECTOR_SIZE
template class streaming_node_reader_t<sealing_config_128_2KB_t>;
template class streaming_node_reader_t<sealing_config_128_4KB_t>;
template class streaming_node_reader_t<sealing_config_128_16KB_t>;
template class streaming_node_reader_t<sealing_config_128_32KB_t>;
template class streaming_node_reader_t<sealing_config_128_8MB_t>;
template class streaming_node_reader_t<sealing_config_128_16MB_t>;
template class streaming_node_reader_t<sealing_config_128_1GB_t>;
template class streaming_node_reader_t<sealing_config_128_64GB_t>;
template class streaming_node_reader_t<sealing_config_64_2KB_t>;
template class streaming_node_reader_t<sealing_config_64_4KB_t>;
template class streaming_node_reader_t<sealing_config_64_16KB_t>;
template class streaming_node_reader_t<sealing_config_64_32KB_t>;
template class streaming_node_reader_t<sealing_config_64_8MB_t>;
template class streaming_node_reader_t<sealing_config_64_16MB_t>;
template class streaming_node_reader_t<sealing_config_64_1GB_t>;
template class streaming_node_reader_t<sealing_config_64_64GB_t>;
template class streaming_node_reader_t<sealing_config_32_2KB_t>;
template class streaming_node_reader_t<sealing_config_32_4KB_t>;
template class streaming_node_reader_t<sealing_config_32_16KB_t>;
template class streaming_node_reader_t<sealing_config_32_32KB_t>;
template class streaming_node_reader_t<sealing_config_32_8MB_t>;
template class streaming_node_reader_t<sealing_config_32_16MB_t>;
template class streaming_node_reader_t<sealing_config_32_1GB_t>;
template class streaming_node_reader_t<sealing_config_32_64GB_t>;
template class streaming_node_reader_t<sealing_config_16_2KB_t>;
template class streaming_node_reader_t<sealing_config_16_4KB_t>;
template class streaming_node_reader_t<sealing_config_16_16KB_t>;
template class streaming_node_reader_t<sealing_config_16_32KB_t>;
template class streaming_node_reader_t<sealing_config_16_8MB_t>;
template class streaming_node_reader_t<sealing_config_16_16MB_t>;
template class streaming_node_reader_t<sealing_config_16_1GB_t>;
template class streaming_node_reader_t<sealing_config_16_64GB_t>;
template class streaming_node_reader_t<sealing_config_8_2KB_t>;
template class streaming_node_reader_t<sealing_config_8_4KB_t>;
template class streaming_node_reader_t<sealing_config_8_16KB_t>;
template class streaming_node_reader_t<sealing_config_8_32KB_t>;
template class streaming_node_reader_t<sealing_config_8_8MB_t>;
template class streaming_node_reader_t<sealing_config_8_16MB_t>;
template class streaming_node_reader_t<sealing_config_8_1GB_t>;
template class streaming_node_reader_t<sealing_config_8_64GB_t>;
template class streaming_node_reader_t<sealing_config_4_2KB_t>;
template class streaming_node_reader_t<sealing_config_4_4KB_t>;
template class streaming_node_reader_t<sealing_config_4_16KB_t>;
template class streaming_node_reader_t<sealing_config_4_32KB_t>;
template class streaming_node_reader_t<sealing_config_4_8MB_t>;
template class streaming_node_reader_t<sealing_config_4_16MB_t>;
template class streaming_node_reader_t<sealing_config_4_1GB_t>;
template class streaming_node_reader_t<sealing_config_4_64GB_t>;
template class streaming_node_reader_t<sealing_config_2_2KB_t>;
template class streaming_node_reader_t<sealing_config_2_4KB_t>;
template class streaming_node_reader_t<sealing_config_2_16KB_t>;
template class streaming_node_reader_t<sealing_config_2_32KB_t>;
template class streaming_node_reader_t<sealing_config_2_8MB_t>;
template class streaming_node_reader_t<sealing_config_2_16MB_t>;
template class streaming_node_reader_t<sealing_config_2_1GB_t>;
template class streaming_node_reader_t<sealing_config_2_64GB_t>;
template class streaming_node_reader_t<sealing_config_1_2KB_t>;
template class streaming_node_reader_t<sealing_config_1_4KB_t>;
template class streaming_node_reader_t<sealing_config_1_16KB_t>;
template class streaming_node_reader_t<sealing_config_1_32KB_t>;
template class streaming_node_reader_t<sealing_config_1_8MB_t>;
template class streaming_node_reader_t<sealing_config_1_16MB_t>;
template class streaming_node_reader_t<sealing_config_1_1GB_t>;
template class streaming_node_reader_t<sealing_config_1_64GB_t>;
#endif
template class streaming_node_reader_t<sealing_config_128_512MB_t>;
template class streaming_node_reader_t<sealing_config_128_32GB_t>;
template class streaming_node_reader_t<sealing_config_64_512MB_t>;
template class streaming_node_reader_t<sealing_config_64_32GB_t>;
template class streaming_node_reader_t<sealing_config_32_512MB_t>;
template class streaming_node_reader_t<sealing_config_32_32GB_t>;
template class streaming_node_reader_t<sealing_config_16_512MB_t>;
template class streaming_node_reader_t<sealing_config_16_32GB_t>;
template class streaming_node_reader_t<sealing_config_8_512MB_t>;
template class streaming_node_reader_t<sealing_config_8_32GB_t>;
template class streaming_node_reader_t<sealing_config_4_512MB_t>;
template class streaming_node_reader_t<sealing_config_4_32GB_t>;
template class streaming_node_reader_t<sealing_config_2_512MB_t>;
template class streaming_node_reader_t<sealing_config_2_32GB_t>;
template class streaming_node_reader_t<sealing_config_1_512MB_t>;
template class streaming_node_reader_t<sealing_config_1_32GB_t>;
