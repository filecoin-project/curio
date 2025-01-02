// Copyright Supranational LLC

#ifndef __ORCHESTRATOR_T_HPP__
#define __ORCHESTRATOR_T_HPP__

template<class C>
class orchestrator_t {
  typedef typename system_buffers_t<C>::node_io_batch_t        node_io_batch_t;
  typedef typename system_buffers_t<C>::node_batch_t           node_batch_t;
  typedef typename system_buffers_t<C>::node_buffer_iterator_t node_buffer_iterator_t;
  typedef typename system_buffers_t<C>::page_batch_ptr_t       page_batch_ptr_t;
  typedef typename system_buffers_t<C>::node_batch_ptr_t       node_batch_ptr_t;

  // Nodes to process
  node_id_t<C> node_start;
  node_id_t<C> node_stop;

  // Terminator to shut down threads
  std::atomic<bool> &terminator;

  // Reference to the system class
  system_buffers_t<C>& system;

  // Record the previously hashed node for setting up parent pointers
  parallel_node_t<C>* prev_hashed_node;

  // Absolute count of node buffer tail node
  node_id_t<C> tail_node;
  // Tail node of the parent node buffer
  node_id_t<C> tail_node_parents;
  // The oldest node fully written to disk
  node_buffer_iterator_t tail_node_write;
  // The next node to write to disk
  node_buffer_iterator_t head_node_write;
  // The oldest node being hashed by the hashers
  node_id_t<C> min_node_hash;

  // Stat counters
  uint64_t cached_parents;
  uint64_t reads_issued;
  uint64_t writes_issued;
  uint64_t parent_read_fifo_full;
  uint64_t io_req_pool_empty;
  uint64_t parent_buffer_full;
  uint64_t node_buffer_full;
  uint64_t from_hashers_empty;
  uint64_t no_action;
  uint64_t node_write_fifo_full;

  // TSC
#ifdef TSC
  uint64_t tsc_start;
  uint64_t loop_cycles;
  uint64_t parent_cycles;
  uint64_t min_node_cycles;
  uint64_t read_batch_cycles;
  uint64_t write_cycles;
  uint64_t release_parent_cycles;
  uint64_t advance_written_cycles;
  uint64_t advance_tail_cycles;
  uint64_t noaction_cycles;
  uint64_t rb_check_buf_cycles;
  uint64_t rb_reserve_buf_cycles;
  uint64_t rb_special_case_cycles;
  uint64_t rb_cache_params_cycles;
  uint64_t rb_parents_cycles;
  uint64_t rb_send_cycles;
#endif

  int parents_fd;
  uint32_t* parents_buf;
  parent_iter_t<C> parent_iter;

public:
  orchestrator_t(std::atomic<bool> &_terminator,
                 system_buffers_t<C>& _system,
                 node_id_t<C> _node_start,
                 node_id_t<C> _node_stop,
                 const char* parents_file) :
    terminator(_terminator),
    system(_system),
    tail_node(_node_start),
    tail_node_parents(_node_start),
    tail_node_write(_node_start),
    head_node_write(_node_start),
    parent_iter(_node_start)
  {
    prev_hashed_node = nullptr;
    node_start = _node_start;
    node_stop = _node_stop;

    printf("Opening parents file %s\n", parents_file);
    parents_fd = open(parents_file, O_RDONLY);
    if (parents_fd == -1) {
      printf("Could not open parents file %s\n", parents_file);
      exit(1);
    }
    struct stat statbuf;
    fstat(parents_fd, &statbuf);
    if ((size_t)statbuf.st_size != parent_iter_t<C>::bytes(C::GetNumNodes())) {
      printf("Found size %ld bytes for parents file %s. Expected %ld bytes.\n",
             statbuf.st_size, parents_file, parent_iter_t<C>::bytes(C::GetNumNodes()));
      exit(1);
    }

    parents_buf = (uint32_t*)mmap(NULL, parent_iter_t<C>::bytes(C::GetNumNodes()),
                                  PROT_READ, MAP_PRIVATE, parents_fd, 0);
    if (parents_buf == MAP_FAILED) {
      perror("mmap failed for parents file");
      exit(1);
    }
    if (((uintptr_t)parents_buf & 0xFFF) != 0) {
      printf("Error: parents buffer is not page aligned\n");
      exit(1);
    }
    parent_iter.set_buf(parents_buf);

#ifdef TSC
    loop_cycles = 0;
    parent_cycles = 0;
    min_node_cycles = 0;
    read_batch_cycles = 0;
    write_cycles = 0;
    release_parent_cycles = 0;
    advance_written_cycles = 0;
    advance_tail_cycles = 0;
    noaction_cycles = 0;
    rb_check_buf_cycles = 0;
    rb_reserve_buf_cycles = 0;
    rb_special_case_cycles = 0;
    rb_cache_params_cycles = 0;
    rb_parents_cycles = 0;
    rb_send_cycles = 0;
#endif
  }

  ~orchestrator_t() {
    munmap(parents_buf, parent_iter_t<C>::bytes(C::GetNumNodes()));
    close(parents_fd);
  }

  int init() {
    return 0;
  }

  void print_state() {
    printf("System tail_node %lu, tail_node_parents %lu, tail_node_write %lu, "
           "head_node_write %lu, min_node_hash %lu, head_node %lu\n",
           tail_node.id(), tail_node_parents.id(), tail_node_write.abs().id(),
           head_node_write.abs().id(), min_node_hash.id(), parent_iter.get_node().id());
  }

  // Issue a batch of reads. A batch in this case is PAGE_BATCH_SIZE sized
  // and consumes one node and all parents, which are sent as a batch to the IO.
  __attribute__ ((noinline))
  size_t read_batch(node_id_t<C> min_hash_node, node_buffer_iterator_t& tail_node_write,
                    bool &advanced, size_t remaining_nodes) {
    // Maximum number of reads we will do
    const size_t MAX_READ_BATCH = C::NODES_PER_PAGE * 2;
    // To simplify the logic always read on page boundaries. This way we don't
    // have to manage pages with some nodes hashed and some not.
    const size_t BATCH_INCREMENT = C::NODES_PER_PAGE;

#ifdef TSC
    uint64_t tsc;
    uint64_t tsc_start = get_tsc();
#endif

    advanced = false;

    ////////////////////////////////////////////////////////////
    // Determine how many nodes to read
    ////////////////////////////////////////////////////////////

    size_t parent_read_fifo_free = system.parent_read_fifo.free_count();
    size_t node_buffer_free = system.node_buffer.free_count();
    size_t parent_buffer_free = system.parent_buffer.free_count();

    // Update event counters
    if (parent_read_fifo_free < MAX_READ_BATCH) {
      parent_read_fifo_full++;
      system.parent_read_fifo_full_stats.record();
    }
    if (node_buffer_free * C::NODES_PER_PAGE < MAX_READ_BATCH) {
      node_buffer_full++;
      system.node_buffer_full_stats.record();
    }
    if (parent_buffer_free < MAX_READ_BATCH) {
      parent_buffer_full++;
      system.parent_buffer_full_stats.record();
    }

    // Determine the number of batches to read
    size_t min_free = std::min(parent_read_fifo_free, node_buffer_free * C::NODES_PER_PAGE);
    min_free = std::min(min_free, parent_buffer_free);
    size_t max_batch = std::min(min_free, remaining_nodes);
    max_batch = std::min(max_batch, MAX_READ_BATCH);
    // Round down to the increment size
    max_batch &= ~(BATCH_INCREMENT - 1);

    if (max_batch == 0) {
      return 0;
    }

#ifdef TSC
    tsc = get_tsc();
    rb_check_buf_cycles += tsc - tsc_start;
    tsc_start = tsc;
#endif

    ////////////////////////////////////////////////////////////
    // Reserve buffer entries
    ////////////////////////////////////////////////////////////

    // Reserve all parent buffers
    size_t parent_buffer_id;
    page_batch_ptr_t page_batch_ptrs[max_batch];
    system.parent_buffer.reserve_batch_nocheck(max_batch, parent_buffer_id, page_batch_ptrs);

    // Reserve node buffer entries
    size_t num_node_buffers = max_batch / C::NODES_PER_PAGE;
    node_batch_ptr_t node_batch_ptrs[num_node_buffers];
    size_t cur_node_buffer_id;
    system.node_buffer.reserve_batch_nocheck(num_node_buffers, cur_node_buffer_id, node_batch_ptrs);

#ifdef TSC
    tsc = get_tsc();
    rb_reserve_buf_cycles += tsc - tsc_start;
    tsc_start = tsc;
#endif

    ////////////////////////////////////////////////////////////
    // Determine caching parameters
    ////////////////////////////////////////////////////////////

    // To determine if we can reference cached data we need to know if the parent
    // is in the node buffer.
    // Don't cache nodes less than the parent buffer cache size from the tail or it can
    // prevent forward progress with reading nodes.
    size_t cache_skid_size = PARENT_BUFFER_BATCHES;

    // Number of nodes in the buffer
    node_id_t<C> node = parent_iter.get_node(); // node we're reading parents for
    size_t node_buffer_count = system.node_buffer.size() * C::NODES_PER_PAGE - max_batch;
    node_id_t<C> cache_min_node = node - node_buffer_count;
    node_id_t<C> cache_min_cacheable_node =
      std::min(node_id_t<C>(cache_min_node + cache_skid_size), // Keep space from the tail
          // Always reference the cache for nodes not yet written to disk
          tail_node_write.abs());

    // printf("Reading parents for node %lx: node_buffer_count %ld cache_min_node %08lx "
    //        "cache_min_cacheable_node %08lx, head_node_write %lx\n",
    //        node.id(), node_buffer_count, cache_min_node.id(), cache_min_cacheable_node.id(),
    //        tail_node_write.abs().id());

#ifdef TSC
    tsc = get_tsc();
    rb_cache_params_cycles += tsc - tsc_start;
    tsc_start = tsc;
#endif

    ////////////////////////////////////////////////////////////
    // Process the nodes
    ////////////////////////////////////////////////////////////
    size_t total_reads_issued = 0;
    for (size_t i = 0; i < max_batch;
         i++, parent_buffer_id = system.parent_buffer.incr(parent_buffer_id)) {

      node_id_t<C> node = parent_iter.get_node(); // node we're reading parents for
      size_t node_in_node_buffer_page = node.node() % C::NODES_PER_PAGE;
      parallel_node_t<C>* cur_node_buffer =
        &node_batch_ptrs[i / C::NODES_PER_PAGE]->batch[0].parallel_nodes[node_in_node_buffer_page];
      if (i > 0 && node_in_node_buffer_page == 0) {
        cur_node_buffer_id = system.node_buffer.incr(cur_node_buffer_id);
      }

      // Get the parent pointers for this batch
      typename system_buffers_t<C>::page_batch_t* page_batch =
        page_batch_ptrs[i];
      // Batch of IO requests going to disks
      typename system_buffers_t<C>::page_io_batch_t* io_batch =
        &system.parent_buffer_io[parent_buffer_id];
      // Batch of parent pointers - aligns with parent buffers
      typename system_buffers_t<C>::parent_ptr_batch_t* ptr_batch =
        &system.parent_ptrs[parent_buffer_id];
      typename system_buffers_t<C>::parent_ptr_sync_batch_t* ptr_sync_batch =
        &system.parent_ptr_syncs[parent_buffer_id];

#ifdef TSC
      tsc = get_tsc();
      rb_reserve_buf_cycles += tsc - tsc_start;
      tsc_start = tsc;
#endif

      size_t parent_start_idx = 0;
      // Special case: For the first node the first layer there are no parents
      if (node.id() == 0) {
        ptr_batch->batch[0].ptr = nullptr;
        ptr_sync_batch->batch[0].node_buffer_idx = parent_ptr_sync_t::NOT_NODE_BUFFER;
        parent_iter++;
        for (size_t j = 0; j < PAGE_BATCH_SIZE; j++) {
          ptr_batch->batch[j + 1].ptr = nullptr;
          ptr_sync_batch->batch[j + 1].node_buffer_idx = parent_ptr_sync_t::NOT_NODE_BUFFER;
          parent_iter++;
        }
        // Increment the valid counter for the batch
        system.parent_buffer.incr_valid(parent_buffer_id, system.parent_buffer.VALID_THRESHOLD);
        prev_hashed_node = cur_node_buffer;
        continue;
      }
      else if (node.node() == 0) {
        // Special case: For the first node any layer there are no base parents
        ptr_batch->batch[0].ptr = nullptr;
        ptr_sync_batch->batch[0].node_buffer_idx = parent_ptr_sync_t::NOT_NODE_BUFFER;
        parent_iter++;
        for (size_t j = 0; j < PARENT_COUNT_BASE; j++) {
          ptr_batch->batch[j + 1].ptr = nullptr;
          ptr_sync_batch->batch[j + 1].node_buffer_idx = parent_ptr_sync_t::NOT_NODE_BUFFER;
          parent_iter++;
        }
        parent_start_idx = PARENT_COUNT_BASE;
      } else {
        // For the first parent pointer we point to the previous hashed node
        assert (parent_iter.get_parent() == 0);

        ptr_batch->batch[0].ptr = prev_hashed_node;
        assert((system_buffers_t<C>::node_batch_t::BATCH_SIZE == 1));
        ptr_sync_batch->batch[0].node_buffer_idx = parent_ptr_sync_t::LOCAL_NODE;
        // Since this is within COORD_BATCH_NODE_COUNT we don't set a reference count

        // Increment the parent pointer
        parent_iter++;
      }

#ifdef TSC
      tsc = get_tsc();
      rb_special_case_cycles += tsc - tsc_start;
      tsc_start = tsc;
#endif

      ////////////////////////////////////////////////////////////
      // Process the parents for a node
      ////////////////////////////////////////////////////////////

      node_id_t<C> parent_node;
      size_t node_in_page;
      size_t reads_issued = 0;
      for (size_t j = parent_start_idx; j < PAGE_BATCH_SIZE; j++) {
        assert (node == parent_iter.get_node());

        // printf("Reading node %ld parent %ld prev_node %p\n",
        //        node, parent_iter.get_parent(), prev_node);
        node_io_t *io = &io_batch->batch[j];

        if (node.layer() == 0 && parent_iter.is_prev_layer()) {
          // Special case: There are no expander parents for the first layer so just map them
          // to the previous node.
          parent_node = node;
          parent_node--;
        } else {
          parent_node = *parent_iter;
        }

        // Use the node buffer for cached parents
        // In this case we set the IO type to NOP and update the synchronization structures.
        bool use_cache = parent_node >= cache_min_cacheable_node;

        // Parent nodes that are near to the node being hashed will be treated differently.
        // When the coordinator is creating the contiguous data buffer it will need to defer
        // copying very close parents since they are not yet hashed.
        bool is_local = (node - parent_node) < COORD_BATCH_NODE_COUNT;

        if (use_cache) {
          // Compute the entry in the node buffer where the data will be
          node_id_t<C> parent_node_cache_offset = parent_node - cache_min_node;
          size_t node_buffer_entry =
            system.node_buffer.add(system.node_buffer.get_tail(),
                                   parent_node_cache_offset.id() / C::NODES_PER_PAGE);
          if (parent_node_cache_offset.id() >= node_buffer_count + i) {
            printf("Using cache for node %lx parent %lx parent_cache_offset %08lx "
                   "entry %ld subentry %ld %p\n",
                   node.id(), parent_node.id(), parent_node_cache_offset.id(), node_buffer_entry,
                   parent_node.node() % C::NODES_PER_PAGE, ptr_batch->batch[j + 1].ptr);
          }
          assert(parent_node_cache_offset.id() < node_buffer_count + i);
          io->type = node_io_t::type_e::NOP;

          // Address correct node in the page
          node_in_page = parent_node.node() % C::NODES_PER_PAGE;
          assert(node_batch_t::BATCH_SIZE == 1);

          // Store in j + 1 in the batch since the ptr batch size is one larger than page batch
          // and the zeroeth entry references the previous hashed node.

          // It's a bit expensive to access the entry through the ring buffer since it stores
          // pointers and requires a dereference. Instead we can compute the node buffer pointer.
          node_batch_t* node_buffer_ptr = system.node_buffer_store;
          node_batch_t* tmp_batch = node_buffer_ptr + node_buffer_entry;
          parallel_node_t<C>* tmp_node = &tmp_batch->batch[0].parallel_nodes[node_in_page];
          ptr_batch->batch[j + 1].ptr = tmp_node;

          if (is_local) {
            ptr_sync_batch->batch[j + 1].node_buffer_idx = parent_ptr_sync_t::LOCAL_NODE;
          } else {
            ptr_sync_batch->batch[j + 1].node_buffer_idx = node_buffer_entry;

            // This is a bit expensive since we need to increment a random node's reference count
            system.node_buffer_sync[NODE_IDX_TO_SYNC_IDX(node_buffer_entry)].reference_count++;
          }

          // printf("Using cache for node %lx parent %ld parent_cache_offset %08lx "
          //        "entry %ld subentry %ld %p\n",
          //        node.id(), parent_node.id(), parent_node_cache_offset.id(), node_buffer_entry,
          //        parent_node.node() % C::NODES_PER_PAGE, ptr_batch->batch[j + 1].ptr);
        } else {
          io->node = parent_node.id();
          io->type = node_io_t::type_e::READ;

          // Address correct node in the page
          node_in_page = parent_node.node() % C::NODES_PER_PAGE;
          // Store in j + 1 since the ptr batch size is one larger than page batch
          // and the zeroeth entry references the previous hashed node.
          ptr_batch->batch[j + 1].ptr = &page_batch->batch[j].parallel_nodes[node_in_page];
          ptr_sync_batch->batch[j + 1].node_buffer_idx = parent_ptr_sync_t::NOT_NODE_BUFFER;

#ifdef NO_DISK_READS
          io->type = node_io_t::type_e::NOP;
          io->valid->fetch_add(1, DEFAULT_MEMORY_ORDER);
#endif

          //   printf("Reading N %lx P# %ld: P %2d.%08x (%lx) read_idx %lx read ptr %p\n",
          //          node.id(), parent_iter.get_parent(), parent_node.layer(), parent_node.node(),
          //          parent_node.id(), cur_node_buffer_id, ptr_batch->batch[j + 1].ptr);
          reads_issued++;
        }

        // Advance the node/parent counters
        parent_iter++;
      }
      total_reads_issued += reads_issued;

#ifdef TSC
      tsc = get_tsc();
      rb_parents_cycles += tsc - tsc_start;
      tsc_start = tsc;
#endif

      // Increment the valid point to cover the cached entries
      size_t num_cached_in_batch = system.parent_buffer.VALID_THRESHOLD - reads_issued;
      system.parent_buffer.incr_valid(parent_buffer_id, num_cached_in_batch);
      cached_parents += num_cached_in_batch;

#ifdef NO_DISK_READS
      assert (system.parent_buffer.is_valid(parent_buffer_id));
#endif

      prev_hashed_node = cur_node_buffer;

      // Send
#ifndef NO_DISK_READS
      SPDK_ERROR(system.parent_read_fifo.enqueue_nocheck(io_batch));
#endif
    }
    advanced = true;

#ifdef TSC
    tsc = get_tsc();
    rb_send_cycles += tsc - tsc_start;
    tsc_start = tsc;
#endif

    return total_reads_issued;
  }

  // Write nodes to disk. Currently writes a single page at a time.
  //   head_node_write     - The next node to write to disk
  //   min_node_hash       - The next node to hash
  //   head_node_write_idx - The page index in the node buffer of head_node_write
  size_t write_nodes(node_buffer_iterator_t& head_node_write, node_id_t<C> min_node_hash) {
    size_t head_page_write = head_node_write.abs().id() / C::NODES_PER_PAGE;
    size_t min_page_hash = min_node_hash.id() / C::NODES_PER_PAGE;
    if (!(head_page_write < min_page_hash)) {
      // No nodes to write
      return 0;
    }

    if (system.node_write_fifo.is_full()) {
      node_write_fifo_full++;
      return 0;
    }

    // Batch of IO requests going to disks
    node_io_batch_t* io_batch = &system.node_buffer_io[head_node_write.idx()];
    assert(node_batch_t::BATCH_SIZE == 1);
    node_io_t *io = &io_batch->batch[0];
    // Use the full node ID here since we need to write out all layers
    io->node = head_node_write.abs().id();
    assert(system.node_buffer.get_valid(head_node_write.idx()) == 0);

    // printf("Write node %ld, node idx %ld to block %ld\n",
    //        head_node_write.abs().id(), head_node_write.idx(), head_node_write.abs().id());
    // char prefix[32];
    // snprintf(prefix, 32, "Write %8lx: ", head_node_write.abs().id());
    // parallel_node_t<C> *node = &system.node_buffer.get_entry(head_node_write.idx())->batch[0].nodes[0];
    // for (size_t i = 0; i < C::NODES_PER_PAGE; i++) {
    //   print_node(&node[i], 2, prefix);
    // }

    //io->valid->fetch_add(1, DEFAULT_MEMORY_ORDER); // TODO: perform the actual write
    // Send
    SPDK_ERROR(system.node_write_fifo.enqueue(io_batch));

    return C::NODES_PER_PAGE;
  }

  __attribute__ ((noinline)) int process(bool print_timing = true) {
    timestamp_t start = std::chrono::high_resolution_clock::now();

    cached_parents = 0;
    reads_issued = 0;
    writes_issued = 0;
    parent_read_fifo_full = 0;
    io_req_pool_empty = 0;
    parent_buffer_full = 0;
    node_buffer_full = 0;
    from_hashers_empty = 0;
    no_action = 0;
    node_write_fifo_full = 0;

    size_t outstanding_writes = 0;

    // Limit the time spent on any one activity to keep queues balanced
    size_t ACTION_BATCH = 8;
    size_t actions_remaining;

    node_id_t<C> node = parent_iter.get_node();

    // Keep track of actions taken last iteration
#ifdef PRINT_STALLS
    bool last_iter_read_nodes = false;
    bool last_iter_wrote_nodes = false;
    bool last_iter_released_parents = false;
    bool last_iter_advanced_written = false;
    bool last_iter_advanced_tail_node = false;

    size_t no_action_run_length = 0;
#endif

#ifdef TSC
    uint64_t tsc;
    tsc_start = get_tsc();
    uint64_t loop_cycles_update = 0;
    uint64_t parent_cycles_update = 0;
    uint64_t min_node_cycles_update = 0;
    uint64_t read_batch_cycles_update = 0;
    uint64_t write_cycles_update = 0;
    uint64_t release_parent_cycles_update = 0;
    uint64_t advance_written_cycles_update = 0;
    uint64_t advance_tail_cycles_update = 0;
#endif


    while (node < node_stop
           || tail_node_write.abs() < node_stop
           || outstanding_writes > 0) {

#ifdef TSC
      tsc = get_tsc();
      loop_cycles_update = tsc - tsc_start;
      tsc_start = tsc;
#endif

      node_id_t<C> next_node = parent_iter.get_node();

#ifdef TSC
      tsc = get_tsc();
      parent_cycles_update = tsc - tsc_start;
      tsc_start = tsc;
#endif

#ifdef STATS
      system.record_stats();
      // if (parent_iter.get_node().node() > 0 &&
      //     (parent_iter.get_node().id() & STATS_MASK) == 0 &&
      //     parent_iter.get_node() < node_stop) {
      //   system.print_periodic_stats();
      // }
#endif

      //////////////////////////////////////////////////////////////////////
      // Parent reading
      //////////////////////////////////////////////////////////////////////

      // Determine the oldest node that has been hashed
      min_node_hash = system.coordinator_node[0]->load(DEFAULT_MEMORY_ORDER);
      for (size_t i = 1; i < system.coordinators.size(); i++) {
        node_id_t<C> n = system.coordinator_node[i]->load(DEFAULT_MEMORY_ORDER);
        min_node_hash = std::min(min_node_hash, n);
      }

      // And factoring in writing the nodes to disk, the oldest node we need
      // to keep in the node buffer.
      node_id_t<C> min_node = std::min(min_node_hash, tail_node_write.abs());

      system.head_minus_hashed_stats.record(parent_iter.get_node() - min_node_hash);
      system.hashed_minus_written_stats.record(min_node_hash - tail_node_write.abs());
      system.written_minus_tail_stats.record(tail_node_write.abs() - tail_node);

#ifdef PRINT_STALLS
      last_iter_read_nodes = false;
      last_iter_wrote_nodes = false;
      last_iter_released_parents = false;
      last_iter_advanced_written = false;
      last_iter_advanced_tail_node = false;
#endif
#ifdef TSC
      tsc = get_tsc();
      min_node_cycles_update = tsc - tsc_start;
      tsc_start = tsc;
#endif

      // Initiate new reads of parent nodes
      bool advanced = false;
      size_t reads_issued_now = 0;
      if (parent_iter.get_node() < node_stop) {
        reads_issued_now = read_batch(min_node_hash, tail_node_write,
                                      advanced, node_stop.id() - parent_iter.get_node().id());
#ifdef PRINT_STALLS
        last_iter_read_nodes = advanced;
#endif
        if (reads_issued_now > 0) {
          reads_issued += reads_issued_now;
        }
      }
#ifdef TSC
      tsc = get_tsc();
      read_batch_cycles_update = advanced ? tsc - tsc_start : 0;
      tsc_start = tsc;
#endif

      // Initiate a write of hashed nodes to disk
      bool initiated_writes = false;
      actions_remaining = ACTION_BATCH;
      while (actions_remaining-- > 0 && head_node_write.abs() < min_node_hash) {
        size_t writes_issued = write_nodes(head_node_write, min_node_hash);
        head_node_write += writes_issued;
        outstanding_writes += writes_issued / C::NODES_PER_PAGE;
        writes_issued += writes_issued / C::NODES_PER_PAGE;
        if (writes_issued > 0) {
#ifdef PRINT_STALLS
          last_iter_wrote_nodes = true;
#endif
          initiated_writes = true;
        }
      }
#ifdef TSC
      tsc = get_tsc();
      write_cycles_update = tsc - tsc_start;
      tsc_start = tsc;
#endif
      // Release parent buffers as soon as they are no longer needed by hashers
      actions_remaining = ACTION_BATCH;
      while (actions_remaining-- > 0 && tail_node_parents < min_node_hash) {
        typename system_buffers_t<C>::parent_ptr_sync_batch_t* ptr_sync_batch =
          &system.parent_ptr_syncs[system.parent_buffer.get_tail()];
        for (size_t j = 0; j < PARENT_PTR_BATCH_SIZE; j++) {
          if (ptr_sync_batch->batch[j].is_node_buffer()) {
            system.node_buffer_sync[NODE_IDX_TO_SYNC_IDX(ptr_sync_batch->batch[j].node_buffer_idx)]
              .consumed_count++;
          }
        }
        system.parent_buffer.release();
        tail_node_parents++;
#ifdef PRINT_STALLS
        last_iter_released_parents = true;
#endif
      }
#ifdef TSC
      tsc = get_tsc();
      release_parent_cycles_update = tsc - tsc_start;
      tsc_start = tsc;
#endif

      // Advance the written node tail as nodes are written to disk
      actions_remaining = ACTION_BATCH;
      while (actions_remaining-- > 0 &&
             tail_node_write.idx() != head_node_write.idx() &&
             system.node_buffer.get_valid(tail_node_write.idx())) {
        outstanding_writes--;
        tail_node_write += C::NODES_PER_PAGE;
#ifdef PRINT_STALLS
        last_iter_advanced_written = true;
#endif
      }

#ifdef TSC
      tsc = get_tsc();
      advance_written_cycles_update = tsc - tsc_start;
      tsc_start = tsc;
#endif

      // Release the node buffer entries only as needed (when the node buffer
      // is full) to maximize caching.
      actions_remaining = ACTION_BATCH;
      size_t free_count = system.node_buffer.free_count();
      while (actions_remaining-- > 0 &&
             tail_node < min_node &&
             tail_node < tail_node_write.abs() &&
             free_count < nvme_controller_t::queue_size * 512) {
        if ((system.node_buffer.get_tail() & NODE_BUFFER_SYNC_BATCH_MASK) == 0) {
          // Crossing into a new sync batch so need to check counters
          size_t sync_idx = NODE_IDX_TO_SYNC_IDX(system.node_buffer.get_tail());
          if (system.node_buffer_sync[sync_idx].reference_count ==
              system.node_buffer_sync[sync_idx].consumed_count) {
            system.node_buffer_sync[sync_idx].reference_count = 0;
            system.node_buffer_sync[sync_idx].consumed_count = 0;
          } else {
            // This batch is not finished yet
            break;
          }
        }

        // Release buffers once all nodes are used in the page
        if ((tail_node.node() % C::NODES_PER_PAGE) == C::NODES_PER_PAGE - 1) {
          system.node_buffer.release();
        }
        tail_node++;
#ifdef PRINT_STALLS
        last_iter_advanced_tail_node = true;
#endif
      }

#ifdef TSC
      tsc = get_tsc();
      advance_tail_cycles_update = tsc - tsc_start;
      tsc_start = tsc;
#endif
      node = next_node;

      if (parent_iter.get_node().id() - tail_node.id() >=
          NODE_BUFFER_BATCHES * C::NODES_PER_PAGE) {
        printf("Unexpected tail/parent node values: %lu - %lu = %lu > %lu\n",
               parent_iter.get_node().id(), tail_node.id(),
               parent_iter.get_node().id() - tail_node.id(),
               NODE_BUFFER_BATCHES * C::NODES_PER_PAGE);
        exit(1);
      }
      if (!initiated_writes) {
        from_hashers_empty++;
      }

      if (reads_issued_now == 0) {
        no_action++;
#ifdef PRINT_STALLS
        no_action_run_length++;
        if (no_action_run_length == 10000) {
          printf("Detected stall ");
          print_state();
          //system.print_periodic_stats();
          printf("  parent_tail %ld, valid %d %d %p\n",
                 system.parent_buffer.get_tail(), system.parent_buffer.is_tail_valid(),
                 system.parent_buffer.is_valid(system.parent_buffer.get_tail()),
                 system.parent_buffer.get_valid_ptr(system.parent_buffer.get_tail()));
        }
#endif

#ifdef TSC
        noaction_cycles += loop_cycles_update;
        noaction_cycles += parent_cycles_update;
        noaction_cycles += min_node_cycles_update;
        noaction_cycles += read_batch_cycles_update;
        noaction_cycles += write_cycles_update;
        noaction_cycles += release_parent_cycles_update;
        noaction_cycles += advance_written_cycles_update;
        noaction_cycles += advance_tail_cycles_update;
#endif
      } else {
#ifdef PRINT_STALLS
        no_action_run_length = 0;
#endif

#ifdef TSC
        loop_cycles += loop_cycles_update;
        parent_cycles += parent_cycles_update;
        min_node_cycles += min_node_cycles_update;
        read_batch_cycles += read_batch_cycles_update;
        write_cycles += write_cycles_update;
        release_parent_cycles += release_parent_cycles_update;
        advance_written_cycles += advance_written_cycles_update;
        advance_tail_cycles += advance_tail_cycles_update;
#endif
      }
    }

    timestamp_t stop = std::chrono::high_resolution_clock::now();

    uint64_t nodes_rw = reads_issued + writes_issued;
    if (print_timing) {
      uint64_t ms = std::chrono::duration_cast<
        std::chrono::milliseconds>(stop - start).count();
      uint64_t mbytes = (nodes_rw * BLOCK_SIZE) >> 20;
      printf("Reading/writing took %ld ms for %ld pages / %ld MB (%0.2lf MB/s), "
             "%0.2lf IOPs\n",
             ms, nodes_rw, mbytes,
             (double)mbytes / ((double)ms / 1000.0),
             (double)nodes_rw / ((double)ms / 1000.0));
    }
//     printf("orchestrator_t outstanding_writes       %ld\n", outstanding_writes);
// #ifdef TSC
//     printf("orchestrator_t loop_cycles              %lu\n", loop_cycles);
//     printf("orchestrator_t parent_cycles            %lu\n", parent_cycles);
//     printf("orchestrator_t min_node_cycles          %lu\n", min_node_cycles);
//     printf("orchestrator_t read_batch_cycles        %lu\n", read_batch_cycles);
//     printf("orchestrator_t   rb_check_buf_cycles    %lu\n", rb_check_buf_cycles);
//     printf("orchestrator_t   rb_reserve_buf_cycles  %lu\n", rb_reserve_buf_cycles);
//     printf("orchestrator_t   rb_special_case_cycles %lu\n", rb_special_case_cycles);
//     printf("orchestrator_t   rb_cache_params_cycles %lu\n", rb_cache_params_cycles);
//     printf("orchestrator_t   rb_parents_cycles      %lu\n", rb_parents_cycles);
//     printf("orchestrator_t   rb_send_cycles         %lu\n", rb_send_cycles);
//     printf("orchestrator_t write_cycles             %lu\n", write_cycles);
//     printf("orchestrator_t release_parent_cycles    %lu\n", release_parent_cycles);
//     printf("orchestrator_t advance_written_cycles   %lu\n", advance_written_cycles);
//     printf("orchestrator_t advance_tail_cycles      %lu\n", advance_tail_cycles);
//     printf("orchestrator_t noaction_cycles          %lu\n", noaction_cycles);
// #endif

//     print_stats();
    return 0;
  }

  void print_stats() {
    printf("orchestrator_t reads_issued %ld writes_issued %ld cached_parents %ld\n",
           reads_issued, writes_issued, cached_parents);
    printf("orchestrator_t parent_read_fifo_full %ld, io_req_pool_empty %ld, "
           "parent_buffer_full %ld, node_buffer_full %ld, from_hashers_empty %ld, "
           "no_action %ld, node_write_fifo_full %ld\n",
           parent_read_fifo_full, io_req_pool_empty,
           parent_buffer_full, node_buffer_full, from_hashers_empty,
           no_action, node_write_fifo_full);

    printf("parent_buffer "); system.parent_buffer.print();
  }
};

#endif
