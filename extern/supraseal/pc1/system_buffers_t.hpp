// Copyright Supranational LLC

#ifndef __SYSTEM_BUFFERS_T_HPP__
#define __SYSTEM_BUFFERS_T_HPP__

// Shared buffers for communicating between various system processes

template<class C>
class system_buffers_t {
public:
  // Type to store parent node data coming from disk
  typedef batch_t<page_t<C>, PAGE_BATCH_SIZE> page_batch_t;
  typedef page_batch_t* page_batch_ptr_t;
  // Type to store nodes that have been hashed, one per batch of parents
  typedef batch_t<page_t<C>, 1> node_batch_t;
  typedef node_batch_t* node_batch_ptr_t;

  // Ring buffer for pages.
  typedef ring_buffer_t<page_batch_t,
                        page_batch_t::BATCH_SIZE,
                        PARENT_BUFFER_BATCHES> parent_buffer_t;

  typedef batch_t<parent_ptr_t<C>, PARENT_PTR_BATCH_SIZE> parent_ptr_batch_t;
  typedef batch_t<parent_ptr_sync_t, PARENT_PTR_BATCH_SIZE> parent_ptr_sync_batch_t;

  typedef batch_t<node_io_t, node_batch_t::BATCH_SIZE> node_io_batch_t;
  typedef batch_t<node_io_t, page_batch_t::BATCH_SIZE> page_io_batch_t;

  typedef ring_buffer_t<node_batch_t, node_batch_t::BATCH_SIZE,
                        NODE_BUFFER_BATCHES> node_buffer_t;
  typedef ring_counter_t<node_id_t<C>, NODE_BUFFER_BATCHES * C::NODES_PER_PAGE,
                         C::NODES_PER_PAGE> node_buffer_iterator_t;


  // Ring buffer for the parent pages
  parent_buffer_t parent_buffer;
  // Contiguous storage for page buffers
  spdk_ptr_t<page_batch_t> parent_buffer_store;

  // Parallel array to the ring buffer hold IO meta data
  spdk_ptr_t<page_io_batch_t> parent_buffer_io;

  // Parent pointers. This array is parallel to parent_buffer and indexed in the same way
  spdk_ptr_t<parent_ptr_batch_t> parent_ptrs;
  // Parent synchronization structures. This array is parallel to parent_buffer and
  // indexed in the same way
  spdk_ptr_t<parent_ptr_sync_batch_t> parent_ptr_syncs;

  // Ring buffer for sealed nodes
  node_buffer_t node_buffer;

  // Contiguous storage for the node buffer
  spdk_ptr_t<node_batch_t> node_buffer_store;
  // Parallel array to the node buffer hold IO meta data
  spdk_ptr_t<node_io_batch_t> node_buffer_io;
  // Parallel array to the node buffer hold synchronization meta data. This is
  // is stored per node buffer batch to reduce synchronization overhead.
  struct node_sync_t {
    // Count of references to the node by parent pointers
    uint16_t reference_count;
    // Count of consumed references by hashers
    uint16_t consumed_count;
  };
  spdk_ptr_t<node_sync_t> node_buffer_sync;

  // Fixed size FIFOs for requests to the parent reader
  mt_fifo_t<page_io_batch_t> parent_read_fifo;

  // Fixed size FIFOs for requests to the note writer
  mt_fifo_t<node_io_batch_t> node_write_fifo;

  // Number of NVME controllers to use
  size_t num_controllers;

  // Hashing status, from hashing threads to storage core.
  // Records the latest hashed node
  std::vector<std::atomic<node_id_t<C>>*> coordinator_node;

  // Coordinator pointers
  std::vector<coordinator_t<C>*> coordinators;

  // Pointer to the parent reader
  node_rw_t<C, page_io_batch_t>* parent_reader;

  orchestrator_t<C>* orchestrator;
  queue_stat_t parent_buffer_stats;
  queue_stat_t node_buffer_stats;
  queue_stat_t read_fifo_stats;
  queue_stat_t write_fifo_stats;
  queue_stat_t head_minus_hashed_stats;
  queue_stat_t hashed_minus_written_stats;
  queue_stat_t written_minus_tail_stats;
  counter_stat_t parent_buffer_full_stats;
  counter_stat_t node_buffer_full_stats;
  counter_stat_t parent_read_fifo_full_stats;

public:
  system_buffers_t(topology_t::sector_config_t& topology) :
    coordinator_node(topology.num_coordinators(), nullptr),
    coordinators(topology.num_coordinators(), nullptr),
    parent_buffer_full_stats("parent_buffer_full"),
    node_buffer_full_stats("node_buffer_full"),
    parent_read_fifo_full_stats("parent_read_fifo_full")
  {
    parent_reader = nullptr;
    orchestrator = nullptr;
  }

  ~system_buffers_t() {
    for (size_t i = 0; i < coordinator_node.size(); i++) {
      delete coordinator_node[i];
    }
  }

  int init(size_t num_disks) {
    // FIFO depth from storage core to the disk IO core(s)
    size_t disk_fifo_padding = 16;
    size_t disk_fifo_depth = (num_disks *
                              nvme_controller_t::queue_size / page_batch_t::BATCH_SIZE *
                              disk_fifo_padding);
    SPDK_ERROR(parent_read_fifo.create("parent_read_fifo", disk_fifo_depth));
    //printf("parent_read_fifo depth %ld batches\n", disk_fifo_depth);
    num_controllers = num_disks;

    // Allocate the parent_buffer
    parent_buffer_store.alloc(PARENT_BUFFER_BATCHES);
    SPDK_ERROR(parent_buffer.create(parent_buffer_store));

    // Allocate an equal number of IO buffers
    parent_buffer_io.alloc(PARENT_BUFFER_BATCHES);

    // Allocate an equal number of parent pointer batches and syncs
    parent_ptrs.alloc(PARENT_BUFFER_BATCHES);
    parent_ptr_syncs.alloc(PARENT_BUFFER_BATCHES);

    // Set up parent buffer io structs to point into the parent buffer array
    for (size_t i = 0; i < PARENT_BUFFER_BATCHES; i++) {
      for (size_t j = 0; j < page_batch_t::BATCH_SIZE; j++) {
        parent_buffer_io[i].batch[j].valid = parent_buffer.get_valid_ptr(i);
        parent_buffer_io[i].batch[j].type = node_io_t::type_e::READ;
        page_t<C>* parent_buffer_page = &parent_buffer.get_entry(i)->batch[j];
        parent_buffer_io[i].batch[j].tracker.buf = (uint8_t*)parent_buffer_page;
      }
    }

    // Allocate the node writer fifo
    SPDK_ERROR(node_write_fifo.create("node_write_fifo", disk_fifo_depth));
    //printf("node_write_fifo depth %ld batches\n", disk_fifo_depth);

    // Allocate the node_buffer
    node_buffer_store.alloc(NODE_BUFFER_BATCHES);
    SPDK_ERROR(node_buffer.create(node_buffer_store));

    // Allocate an equal number of IO buffers
    node_buffer_io.alloc(NODE_BUFFER_BATCHES);

    for (size_t i = 0; i < NODE_BUFFER_BATCHES; i++) {
      for (size_t j = 0; j < node_io_batch_t::BATCH_SIZE; j++) {
        node_buffer_io[i].batch[j].valid = node_buffer.get_valid_ptr(i);
        node_buffer_io[i].batch[j].type = node_io_t::type_e::WRITE;
        page_t<C>* node_buffer_page = &node_buffer.get_entry(i)->batch[j];
        node_buffer_io[i].batch[j].tracker.buf =
          (uint8_t*)node_buffer_page;
      }
    }

    // Allocate an equal number of node buffer sync entries, one per node batch
    node_buffer_sync.alloc(NODE_BUFFER_SYNC_BATCHES);
    for (size_t i = 0; i < NODE_BUFFER_SYNC_BATCHES; i++) {
      node_buffer_sync[i].reference_count = 0;
      node_buffer_sync[i].consumed_count = 0;
    }

    for (size_t i = 0; i < coordinator_node.size(); i++) {
      coordinator_node[i] = new std::atomic<node_id_t<C>>();
    }

    parent_buffer_stats.init("parent_buffer", parent_buffer.capacity());
    node_buffer_stats.init("node_buffer", node_buffer.capacity());
    read_fifo_stats.init("read_fifo", parent_read_fifo.capacity());
    write_fifo_stats.init("write_fifo", node_write_fifo.capacity());
    head_minus_hashed_stats.init("head_minus_hashed", 0);
    hashed_minus_written_stats.init("hashed_minus_written", 0);
    written_minus_tail_stats.init("written_minus_tail", 0);

    return 0;
  }

  void clear_stats() {
#ifdef STATS
    parent_buffer_stats.clear();
    node_buffer_stats.clear();
    read_fifo_stats.clear();
    write_fifo_stats.clear();
    head_minus_hashed_stats.clear();
    hashed_minus_written_stats.clear();
    written_minus_tail_stats.clear();
    hasher_clear_stats(hashers[0]);
    if (parent_reader != nullptr) {
      rw_clear_stats(parent_reader);
    }
#endif
  }
  void record_stats() {
#ifdef STATS
    parent_buffer_stats.record(parent_buffer.size());
    node_buffer_stats.record(node_buffer.size());
    read_fifo_stats.record(parent_read_fifo.size());
    write_fifo_stats.record(node_write_fifo.size());
#endif
  }

  void print_periodic_stats() {
#ifdef STATS
    parent_buffer_stats.snapshot();
    node_buffer_stats.snapshot();
    read_fifo_stats.snapshot();
    write_fifo_stats.snapshot();
    head_minus_hashed_stats.snapshot();
    hashed_minus_written_stats.snapshot();
    written_minus_tail_stats.snapshot();
    hasher_snapshot(hashers[0]);
    if (parent_reader != nullptr) {
      rw_snapshot(parent_reader);
    }
    parent_buffer_full_stats.snapshot();
    node_buffer_full_stats.snapshot();
    parent_read_fifo_full_stats.snapshot();

    parent_buffer_stats.print();
    node_buffer_stats.print();
    read_fifo_stats.print();
    write_fifo_stats.print();
    head_minus_hashed_stats.print();
    hashed_minus_written_stats.print();
    written_minus_tail_stats.print();
    hasher_print(hashers[0]);
    if (parent_reader != nullptr) {
      rw_print(parent_reader);
    }
    parent_buffer_full_stats.print();
    node_buffer_full_stats.print();
    parent_read_fifo_full_stats.print();
#endif
  }
};

#endif
