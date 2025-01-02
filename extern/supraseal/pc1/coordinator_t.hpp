// Copyright Supranational LLC

#ifndef __COORDINATOR_T_HPP__
#define __COORDINATOR_T_HPP__

// Use a mutex to synchronize when data is not ready for hashing (as
// opposed to a spin lock).
#define MUTEX_THREAD_SYNC

#include "../sha/sha_functions.hpp"
#include "../util/util.hpp"

template<class C>
class coordinator_t {
  typedef typename system_buffers_t<C>::parent_ptr_batch_t parent_ptr_batch_t;
  typedef typename system_buffers_t<C>::parent_ptr_sync_batch_t parent_ptr_sync_batch_t;

  std::atomic<bool> &terminator;
  system_buffers_t<C>& system;

  // Coodinator id
  size_t id;
  // First sector for this coordinator
  size_t start_sector;

  // Node range to hash
  node_id_t<C> node_start;
  node_id_t<C> node_stop;

  queue_stat_t   num_available_nodes_stats;
  counter_stat_t hasher_data_not_ready_stats;
#ifdef HASHER_TSC
  uint64_t cycles_hashing;
  uint64_t cycles_other;
  uint64_t tsc_start_cycle;
#endif

  // Replica IDs per hashing thread
  struct per_thread_t {
    replica_id_buffer_t replica_id_buffer;
    uint32_t*           replica_id_ptrs[2];
    uint32_t*           pad_0_ptr;
  };
  std::vector<per_thread_t>  thr_data;

  // Members for the coordinator function
  thread_pool_t*   pool; // Contains the pool of threads
  size_t           num_threads;
  std::atomic<uint64_t> coord_next_valid; // Next node ID to hash

  // Local node buffer to store the data copied from the parent
  // ponters. Each batch includes all 14 parents.
  struct coord_node_t {
    uint32_t sectors[MAX_HASHERS_PER_COORD * NODES_PER_HASHER][NODE_WORDS];
  };
  struct coord_batch_t {
    coord_node_t parents[COORD_BATCH_SIZE * PARENT_COUNT];
  };
  typedef ring_buffer_t<coord_batch_t, 1, COORD_BATCH_COUNT> coord_ring_t;
  coord_ring_t   coord_ring;
  spdk_ptr_t<coord_batch_t> coord_ring_storage;
  // Used to count threads that have consumed each batch
  std::atomic<size_t> coord_done_count[COORD_BATCH_COUNT];
#ifdef MUTEX_THREAD_SYNC
  std::mutex          coord_ready_mtx[COORD_BATCH_COUNT];
#endif

  // For near parents in the current layer the data will not be ready yet
  // for the coordinator to copy it. In this case coord_ring_offsets will
  // store the offset from the node to be hashed for where to get the data
  // in the node buffer. Otherwise it will contain zero.
  parallel_node_t<C>* coord_ring_ptrs[COORD_BATCH_COUNT][COORD_BATCH_SIZE * PARENT_COUNT];

public:
  coordinator_t(std::atomic<bool>&          _terminator,
                system_buffers_t<C>&     _system,
                size_t                      _id, // Coordinator number
                size_t                      _start_sector,
                topology_t::coordinator_t   topology,
                node_id_t<C>                _node_start,
                node_id_t<C>                _node_stop,
                replica_id_buffer_t*        replica_id_buffers // One per sector
                ) :
    terminator(_terminator),
    system(_system),
    coord_next_valid(_node_start)
  {
    id = _id;
    start_sector = _start_sector;
    node_start = _node_start;
    node_stop = _node_stop;

    // printf("Constructing coordinator %ld, sector %ld, num_hashers %ld\n",
    //        id, start_sector, topology.num_hashers);

    thr_data.resize(topology.num_hashers);
    for (size_t i = 0; i < topology.num_hashers; i++) {
      thr_data[i].replica_id_buffer  = replica_id_buffers[i];
      thr_data[i].replica_id_ptrs[0] = thr_data[i].replica_id_buffer.ids[0];
      thr_data[i].replica_id_ptrs[1] = thr_data[i].replica_id_buffer.cur_loc[0];
      thr_data[i].pad_0_ptr          = thr_data[i].replica_id_buffer.pad_0[0];
    }

#ifdef HASHER_TSC
    cycles_hashing = 0;
    cycles_other = 0;
#endif

    // Set up the ring buffer for local storage
    coord_ring_storage.alloc(COORD_BATCH_COUNT);
    SPDK_ASSERT(coord_ring.create(coord_ring_storage));
    for (size_t i = 0; i < COORD_BATCH_COUNT; i++) {
      coord_done_count[i] = 0;
    }

#ifdef MUTEX_THREAD_SYNC
    for (size_t i = 0; i < COORD_BATCH_COUNT; i++) {
      coord_ready_mtx[i].lock();
    }
#endif

    // Set up hashing threads.
    num_threads = topology.num_hashers;
    // printf("Coord %ld start_sector %ld sectors %ld num_threads %ld\n",
    //        id, start_sector, topology.num_sectors(), num_threads);

    pool = new thread_pool_t(num_threads);
    size_t sector = start_sector;
    for (size_t i = 0; i < num_threads; i++) {
      int thr_core = topology.get_hasher_core(i);
      pool->spawn([&, i, thr_core, sector]() {
        // printf("Setting affinity for hasher %ld thread %ld to core %d. sector %ld\n",
        //        id, i, thr_core, sector);
        set_core_affinity(thr_core);
        run_thread(i, sector);
      });
      sector += NODES_PER_HASHER;
    }

    num_available_nodes_stats.init("hasher available", 0);
    hasher_data_not_ready_stats.init("hasher_data_not_ready");
  }

  ~coordinator_t() {}

  int init() {
    return 0;
  }

  void clear_stats() {
    num_available_nodes_stats.clear();
    hasher_data_not_ready_stats.clear();
  }
  void snapshot() {
    num_available_nodes_stats.snapshot();
    hasher_data_not_ready_stats.snapshot();
  }
  void print() {
    num_available_nodes_stats.print();
    hasher_data_not_ready_stats.print();
  }

  // Hash one node
  void hash(node_id_t<C> node, coord_node_t* parent_nodes,
            parallel_node_t<C>** local_ptrs, parallel_node_t<C>* hash_out,
            size_t offset_id, size_t thread_id, per_thread_t* replicaId
#ifdef VERIFY_HASH_RESULT
            , uint32_t* sealed_data
#endif
            ) {
    // Update the location in replica ID
    uint32_t cur_layer = node.layer() + 1;
    replicaId->replica_id_buffer.cur_loc[0][0]     = cur_layer;
    replicaId->replica_id_buffer.cur_loc[0 + 1][0] = cur_layer;
    replicaId->replica_id_buffer.cur_loc[0][2]     = node.node();
    replicaId->replica_id_buffer.cur_loc[0 + 1][2] = node.node();

    // Create parent pointers for the hasher
    parent_ptr_batch_t ptr_batch;
    assert (PARENT_PTR_BATCH_SIZE == PARENT_COUNT);
    for (size_t i = 0; i < PARENT_COUNT; i++) {
      if (local_ptrs[i] != nullptr) {
        ptr_batch.batch[i].ptr = (parallel_node_t<C>*)&local_ptrs[i]->sectors[offset_id];
      } else {
        ptr_batch.batch[i].ptr = (parallel_node_t<C>*)&parent_nodes[i].sectors[thread_id * NODES_PER_HASHER];
      }
    }

    parallel_node_t<C>** cur_parent_ptr  = &ptr_batch.batch[0].ptr;
    uint32_t**  cur_data_buf;
    size_t      blocks;
    size_t      repeat;
    if (node.node() == 0) {
      cur_data_buf = &replicaId->pad_0_ptr;
      blocks       = NODE_0_BLOCKS;
      repeat       = NODE_0_REPEAT;
    } else {
      cur_data_buf = (uint32_t**)cur_parent_ptr;
      blocks       = NODE_GT_0_BLOCKS;
      if (cur_layer == 1)
        repeat     = LAYER_1_REPEAT;
      else
        repeat     = LAYERS_GT_1_REPEAT;
    }

    // // {
    // if (node.node() == 0) {
    //     unique_lock<mutex> lck(print_mtx);
    //     printf("blocks %ld, repeat %ld\n", blocks, repeat);
    //     for (size_t j = 0; j < PARENT_PTR_BATCH_SIZE; j++) {
    //       //printf("Printing parent %ld\n", j);
    //       if (ptr_batch.batch[j].ptr == nullptr) {
    //         printf("H %ld T %ld N %8lx.%2x P %02ld: nullptr\n",
    //                offset_id, thread_id, node.id(), node.node(), j);
    //       } else {
    //         char prefix[64];
    //         snprintf(prefix, 64, "H %ld N %ld %8lx.%2d P %02ld ",
    //                  offset_id, thread_id, node.id(), node.node(), j);
    //         //size_t node_idx = batch->batch[j].node % C::NODES_PER_PAGE;
    //         print_node(ptr_batch.batch[j].ptr, 2, prefix);
    //       }
    //       //printf("Received data for buf %d\n", io->buf_id);
    //     }
    // }

    for (size_t j = 0; j < PARENT_PTR_BATCH_SIZE; j++) {
      __builtin_prefetch(&ptr_batch.batch[j].ptr->sectors[0].limbs[0], 0, 3);
      __builtin_prefetch(&ptr_batch.batch[j].ptr->sectors[0 + 1].limbs[0], 0, 3);
    }

#ifdef HASHER_TSC
    uint64_t tsc = get_tsc();
    cycles_other += (tsc - tsc_start_cycle);
    tsc_start_cycle = tsc;
#endif

    // Hash the node
    sha_ext_mbx2(&hash_out->sectors[offset_id].limbs[0],
                 &(replicaId->replica_id_ptrs[0]), cur_data_buf,
                 0, blocks, repeat);

#ifdef HASHER_TSC
    tsc = get_tsc();
    cycles_hashing += (tsc - tsc_start_cycle);
    tsc_start_cycle = tsc;
#endif

    // // Periodically print the hash result
    // if (offset_id == 0 && thread_id == 0 &&
    //     (node.node() & ((NODE_COUNT / 4) - 1)) == 0) {
    //   unique_lock<mutex> lck(print_mtx);
    //   printf("H %3ld T %3ld node %08lx hasher out %p: ",
    //          offset_id, thread_id, node.id(), &hash_out->sectors[offset_id].limbs[0]);
    //   print_digest_reorder(&hash_out->sectors[offset_id].limbs[0]);
    // }

#ifdef VERIFY_HASH_RESULT
    if (offset_id == 18 &&
        (htonl(hash_out->sectors[offset_id][0]) != sealed_data[node.node() * NODE_WORDS] ||
         htonl(hash_out->sectors[offset_id + 1][0]) != sealed_data[node.node() * NODE_WORDS])) {
      unique_lock<mutex> lck(print_mtx);

      printf("\nMISMATCH: Hasher %ld thr %ld Node %x layer %d id %lx thr_offset_id %ld hash_out->sectors %p %p\n",
             id, thread_id, node.node(), node.layer() + 1, node.id(),
             offset_id,
             hash_out->sectors, hash_out->sectors[offset_id]);
      print_digest_reorder(hash_out->sectors[offset_id]);
      print_digest_reorder(hash_out->sectors[offset_id + 1]);
      printf("Expected:\n");
      print_digest_reorder(&sealed_data[node.node() * NODE_WORDS]);

      printf("blocks %ld, repeat %ld\n", blocks, repeat);
      for (size_t j = 0; j < PARENT_PTR_BATCH_SIZE; j++) {
        //printf("Printing parent %ld\n", j);
        if (ptr_batch.batch[j].ptr == nullptr) {
          printf("H %ld T %ld N %8lx.%2x P %02ld: nullptr\n",
                 offset_id, thread_id, node.id(), node.node(), j);
        } else {
          char prefix[64];
          snprintf(prefix, 64, "H %ld N %ld %8lx.%2d P %02ld ",
                   offset_id, thread_id, node.id(), node.node(), j);
          //size_t node_idx = batch->batch[j].node % C::NODES_PER_PAGE;
          //print_node((parallel_node_t<C>*)ptr_batch.batch[j].ptr->sectors[0], 2, prefix);
          printf("%s %p: ", prefix, ptr_batch.batch[j].ptr->sectors[0]);
          print_digest_reorder(ptr_batch.batch[j].ptr->sectors[0]);
          printf("%s %p: ", prefix, ptr_batch.batch[j].ptr->sectors[1]);
          print_digest_reorder(ptr_batch.batch[j].ptr->sectors[1]);

        }
        //printf("Received data for buf %d\n", io->buf_id);
      }
      printf("H %ld T %ld node %08lx hasher out %p: ",
             offset_id, thread_id, node.id(), hash_out->sectors[offset_id]);
      print_digest_reorder(hash_out->sectors[offset_id]);


      sleep(5);
      abort();
    }
#endif
  }

  // Run a hashing thread
  //   thr_count - thread count within this coordinator
  //   sector    - sector to hash
  __attribute__ ((noinline)) void run_thread(size_t thr_count, size_t sector) {
    // Absolute node count
    node_id_t<C> thr_node = node_start;
    // Index into local coordinator ring buffer
    size_t coord_idx = 0;
    // Index into node buffer
    size_t node_idx = 0;
    // The sector offset within buffers
    size_t offset_id = sector;
    // Count the number of times data is not ready
    size_t data_not_ready = 0;
    // Mechanism to reset counters after starting to clear startup noise
    bool data_reset = false;

#ifdef VERIFY_HASH_RESULT
    // The CWD should contain a symlink to a cached benchy run to check results
    const char* sealed_file_template = "../cache_benchy_run_32G/sc-02-data-layer-%d.dat";
    int sealed_data_fd = 0;
    uint32_t* sealed_data = nullptr;
#endif

#ifdef HASHER_TSC
    tsc_start_cycle = get_tsc();
#endif

    per_thread_t* replicaId = &thr_data[thr_count];

    // {
    //   unique_lock<mutex> lck(print_mtx);

    //   printf("Starting hasher %ld thread %ld offset_id %ld\n", id, thr_count, offset_id);
    //   print_digest(&(replicaId->replica_id_buffer.ids[0][0]));
    //   print_digest(&(replicaId->replica_id_buffer.cur_loc[0][0]));
    //   print_digest(&(replicaId->replica_id_buffer.pad_0[0][0]));
    //   print_digest(&(replicaId->replica_id_buffer.pad_1[0][0]));
    //   print_digest(&(replicaId->replica_id_buffer.padding[0][0]));
    //   printf("\n");
    // }

    while (thr_node < node_stop) {
      // Wait for the next node to be ready
      uint64_t valid_nodes;
      valid_nodes = coord_next_valid.load(DEFAULT_MEMORY_ORDER);
      if (valid_nodes <= thr_node.id()) {
        data_not_ready++;

#ifdef MUTEX_THREAD_SYNC
        coord_ready_mtx[coord_idx].lock();
        coord_ready_mtx[coord_idx].unlock();
#endif

        while ((valid_nodes = coord_next_valid.load(DEFAULT_MEMORY_ORDER)) <= thr_node.id()) {}
      }

      // Clear counters after startup
      if (valid_nodes > (node_start + C::GetNumNodes() / 128) && !data_reset) {
        data_not_ready = 0;
#ifdef HASHER_TSC
        cycles_other = 0;
        cycles_hashing = 0;
#endif
        data_reset = true;
      }

      while (thr_node.id() < valid_nodes) {
        coord_node_t* parent_batch = coord_ring.get_entry(coord_idx)->parents;
        parallel_node_t<C>** local_ptr_batch = coord_ring_ptrs[coord_idx];

#ifdef VERIFY_HASH_RESULT
        if (thr_node.node() == 0) {
          if (sealed_data_fd != 0) {
            munmap(sealed_data, C::GetSectorSize());
            close(sealed_data_fd);
          }
          char sealed_file_name[256];
          uint32_t cur_layer = thr_node.layer() + 1;
          sprintf(sealed_file_name, sealed_file_template, cur_layer);
          sealed_data_fd = open(sealed_file_name, O_RDONLY);
          assert (sealed_data_fd != -1);
          sealed_data = (uint32_t*)mmap(NULL, C::GetSectorSize(), PROT_READ,
                                        MAP_PRIVATE, sealed_data_fd, 0);
          if (sealed_data == MAP_FAILED) {
            perror("mmap");
            exit(1);
          }
          assert (sealed_data != MAP_FAILED);
        }
#endif

        for (size_t i = 0; i < COORD_BATCH_SIZE; i++) {
          size_t node_in_page = thr_node.node() % C::NODES_PER_PAGE;
          coord_node_t* parent_nodes = &parent_batch[i * PARENT_COUNT];
          parallel_node_t<C>* hash_out = &system.node_buffer.get_entry(node_idx)->
            batch[0].parallel_nodes[node_in_page];
          hash(thr_node, parent_nodes,
               &local_ptr_batch[i * PARENT_COUNT],
               hash_out, offset_id, thr_count, replicaId
#ifdef VERIFY_HASH_RESULT
               , sealed_data
#endif
               );

          thr_node++;
          if (thr_node.node() % C::NODES_PER_PAGE == 0) {
            node_idx = system.node_buffer.incr(node_idx);
          }
        }

        // Indicate that we're done
        coord_done_count[coord_idx].fetch_add(1, DEFAULT_MEMORY_ORDER);
        coord_idx = coord_ring.incr(coord_idx);
      }
    }
// #ifdef HASHER_TSC
//     printf("Hasher %ld thr %ld: data_not_ready %ld, cycles_hashing %lu, cycles_other %lu\n",
//            id, thr_count, data_not_ready, cycles_hashing, cycles_other);
// #else
//     printf("Hasher %ld thr %ld: data_not_ready %ld\n",
//            id, thr_count, data_not_ready);
// #endif
  }

  // Perform the coordinator functions
  int run() {
    // Absolute node count
    node_id_t<C> node(node_start);
    // Completed node count
    node_id_t<C> completed_node(node_start);

    // Index into parent_buffer
    size_t idx = 0;
    // Index into node buffer
    size_t node_idx = 0;
    // The offset into buffers
    size_t offset_id = start_sector;

    // Number of batches between updates back to the storage core
    const size_t NODE_COUNT_UPDATE_INTERVAL = 4;
    size_t node_update_batch_count = 0;

    size_t data_not_ready = 0;

    //printf("Starting coord %ld thread %d offset_id %ld\n", id, 0, offset_id);

    timestamp_t layer_start = std::chrono::high_resolution_clock::now();

#ifdef PRINT_STALLS
    size_t no_action_run_length = 0;
#endif

    // Node count within the coordinator local data batch
    size_t coord_batch_count = 0;
    // Index in the local data ring buffer
    size_t coord_batch_idx = 0;
    coord_batch_t* current_batch = coord_ring.reserve(coord_batch_idx);
    parallel_node_t<C>** coord_ring_ptrs_batch = coord_ring_ptrs[coord_batch_idx];

    while(node < node_stop) {
      bool advanced = false;
      bool done = false;

      bool cur_valid = system.parent_buffer.is_valid(idx);
      while (!done && cur_valid) {

#ifdef STATS
        // Periodically count the number of available nodes in the parent buffer
        if (id == 0 && (node.id() & STATS_MASK) == 0) {
          size_t count = 0;
          size_t count_idx = idx;
          size_t cur_head = system.parent_buffer.get_head();
          while (system.parent_buffer.is_valid(count_idx) && count_idx != cur_head) {
            count_idx = system.parent_buffer.incr(count_idx);
            count++;
          }
          num_available_nodes_stats.record(count);
        }
#endif
        // Precompute next indices where the instructions can be hidden
        size_t next_idx = system.parent_buffer.incr(idx);
        cur_valid = system.parent_buffer.is_valid(next_idx);
        size_t next_node_idx = system.node_buffer.incr(node_idx);
        done = node.id() + 1 == node_stop.id();

        if (node.node() == 0 && node.layer() > 0) {
          if (id == 0) {
            timestamp_t stop = std::chrono::high_resolution_clock::now();
            uint64_t secs = std::chrono::duration_cast<
              std::chrono::seconds>(stop - layer_start).count();
            printf("Layer took %ld seconds\n", secs);
            layer_start = stop;
          }
        }

        // Copy the parent pointer data into the local buffer
        parent_ptr_batch_t* ptr_batch = &system.parent_ptrs[idx];
        parent_ptr_sync_batch_t* ptr_sync_batch = &system.parent_ptr_syncs[idx];
        for (size_t j = 0; j < PARENT_COUNT; j++) {
          coord_node_t* dst = &current_batch->parents[coord_batch_count * PARENT_COUNT + j];
          parallel_node_t<C>* src = ptr_batch->batch[j].ptr;

          if (src == nullptr) {
            // Do nothing
            coord_ring_ptrs_batch[coord_batch_count * PARENT_COUNT + j] = nullptr;
          } else if (ptr_sync_batch->batch[j].node_buffer_idx == parent_ptr_sync_t::LOCAL_NODE) {
            // Send the offset down to the hashers
            coord_ring_ptrs_batch[coord_batch_count * PARENT_COUNT + j] = src;
          } else {
            memcpy(dst, &src->sectors[offset_id], num_threads * NODES_PER_HASHER * NODE_SIZE);
            coord_ring_ptrs_batch[coord_batch_count * PARENT_COUNT + j] = nullptr;
          }
        }

    //     // Print the input data
        // if (id == 1 && node.id() == 0x115f9a05) {
        //   unique_lock<mutex> lck(print_mtx);
        //   parent_ptr_sync_batch_t* ptr_sync_batch = &system.parent_ptr_syncs[idx];
        //   printf("\nCoordinator printing parents\n");
        //   for (size_t j = 0; j < PARENT_PTR_BATCH_SIZE; j++) {
        //     if (ptr_batch->batch[j].ptr == nullptr) {
        //       printf("C N %8lx.%2x P %02ld: nullptr\n", node.id(), node.node(), j);
        //     } else if (ptr_sync_batch->batch[j].node_buffer_idx == parent_ptr_sync_t::LOCAL_NODE) {
        //       // Send the offset down to the hashers
        //       printf("C N %8lx.%2x P %02ld: local_node\n", node.id(), node.node(), j);
        //     } else {
        //       char prefix[64];
        //       snprintf(prefix, 64, "C N %8lx.%2d P %02ld $ Nidx %8x %p: ", node.id(), node.node(), j,
        //                ptr_sync_batch->batch[j].node_buffer_idx, ptr_batch->batch[j].ptr->sectors[18]);
        //       //size_t node_idx = batch->batch[j].node % C::NODES_PER_PAGE;
        //       //print_node(ptr_batch->batch[j].ptr, 2, prefix);
        //       printf("%s\n", prefix);
        //       print_digest_reorder(ptr_batch->batch[j].ptr->sectors[18]);
        //       print_digest_reorder(ptr_batch->batch[j].ptr->sectors[19]);
        //     }
        //     //printf("Received data for buf %d\n", io->buf_id);
        //   }
        // }

        // Once the batch is completed make it available to the hashers
        coord_batch_count++;
        if (coord_batch_count == COORD_BATCH_SIZE) {
#ifdef MUTEX_THREAD_SYNC
          coord_ready_mtx[coord_batch_idx].unlock();
#endif
          coord_next_valid += COORD_BATCH_SIZE;
          // Recover a buffer if needed
          while (coord_ring.is_full()) {
            if (coord_done_count[coord_ring.get_tail()] == num_threads) {
              coord_done_count[coord_ring.get_tail()] = 0;
#ifdef MUTEX_THREAD_SYNC
              coord_ready_mtx[coord_ring.get_tail()].lock();
#endif
              coord_ring.release();
              completed_node += COORD_BATCH_SIZE;
              break;
            }
          }
          current_batch = coord_ring.reserve(coord_batch_idx);
          assert (current_batch != nullptr);
          coord_ring_ptrs_batch = coord_ring_ptrs[coord_batch_idx];

          coord_batch_count = 0;
          node_update_batch_count++;
        }

        // Advance to the next node
        advanced = true;
        node++;
        idx = next_idx;
        if (node.node() % C::NODES_PER_PAGE == 0) {
          node_idx = next_node_idx;
        }
        if (node_update_batch_count == NODE_COUNT_UPDATE_INTERVAL) {
          // Batch update - this is expensive.
          system.coordinator_node[id]->store(completed_node);
          node_update_batch_count = 0;
        }
      }
      if (advanced) {
#ifdef PRINT_STALLS
        no_action_run_length = 0;
#endif
      } else {
        assert (cur_valid == false);
        hasher_data_not_ready_stats.record();
        data_not_ready++;
#ifdef PRINT_STALLS
        no_action_run_length++;
        if (no_action_run_length == 1024) {
          printf("Hasher detected stall node %lu, node_idx %lu, parent_buf_valid %d %p\n", node.id(), idx,
                 system.parent_buffer.is_valid(idx), system.parent_buffer.get_valid_ptr(idx));
        }
#endif
      }
    }
    // Wait for the remaining batches to finish
    while(completed_node < node_stop) {
      assert (coord_ring.size() > 0);
      while (coord_done_count[coord_ring.get_tail()] < num_threads) {}
      coord_done_count[coord_ring.get_tail()] = 0;
      coord_ring.release();
      completed_node += COORD_BATCH_SIZE;
    }

    // printf("Coordinator %ld: data_not_ready %ld\n", id, data_not_ready);
    system.coordinator_node[id]->store(completed_node);

    if (id == 0) {
      timestamp_t stop = std::chrono::high_resolution_clock::now();
      uint64_t secs = std::chrono::duration_cast<
        std::chrono::seconds>(stop - layer_start).count();
      printf("Layer took %ld seconds\n", secs);
    }

    return 0;
  }
};

template<class C>
inline void coordinator_clear_stats(coordinator_t<C>* coordinator) {
  coordinator->clear_stats();
}
template<class C>
inline void coordinator_snapshot(coordinator_t<C>* coordinator) {
  coordinator->snapshot();
}
template<class C>
inline void coordinator_print(coordinator_t<C>* coordinator) {
  coordinator->print();
}

#endif
