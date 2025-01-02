// Copyright Supranational LLC

#include <vector>
#include <deque>
#include <fstream>       // file read
#include <iostream>      // printing
#include <cstring>
#include <arpa/inet.h> // htonl

// Enable profiling
//#define PROFILE

// Enable data collection in the orchestrator using the timestamp counter
//#define TSC

// Enable data collection in the hasher using the timestamp counter
//#define HASHER_TSC

// Enable more general statistics collection
//#define STATS

// Disable reading parents from disk (will not produce the correct result)
//#define NO_DISK_READS

// Print a message if the orchestrator is stalled for too long
//#define PRINT_STALLS

// Verify that hashed result matches a known good sealing
//#define VERIFY_HASH_RESULT

#include "pc1.hpp"
#include "../util/util.hpp"

#include "../util/stats.hpp"
#include "../sealing/constants.hpp"
#include "../nvme/nvme.hpp"
#include "../sealing/data_structures.hpp"

// Forward declarations
template<class C> class coordinator_t;
template<class C, class B> class node_rw_t;
template<class C> class orchestrator_t;

const size_t STATS_PERIOD = 1<<22;
const size_t STATS_MASK   = STATS_PERIOD - 1;

extern std::mutex print_mtx;

#include "../util/debug_helpers.hpp"
#include "system_buffers_t.hpp"
#include "parent_iter_t.hpp"
#include "orchestrator_t.hpp"
#include "node_rw_t.hpp"
#include "coordinator_t.hpp"

template<class C>
int do_pc1(nvme_controllers_t* controllers,
           topology_t& topology,
           uint64_t block_offset,
           const uint32_t* replica_ids,
           const char* parents_filename) {
  topology_t::sector_config_t* sector_config =
    topology.get_sector_config(C::PARALLEL_SECTORS);
  if (sector_config == nullptr) {
    printf("No configuration provided for %ld sectors\n", C::PARALLEL_SECTORS);
    exit(1);
  }

  size_t layer_count = C::GetNumLayers();
  size_t node_count = C::GetSectorSize() / NODE_SIZE;

  thread_pool_t pool(3 + sector_config->num_coordinators());
  std::atomic<bool> terminator(false);

  node_id_t<C> node_start = node_count * 0;
  //node_id_t node_stop(node_count * 0 + node_count / 32);
  node_id_t<C> node_stop(node_count * layer_count);

  system_buffers_t<C> system(*sector_config);
  SPDK_ERROR(system.init(controllers->size()));

  // Parent reader
  node_rw_t<C, typename system_buffers_t<C>::page_io_batch_t> parent_reader
    (terminator, *controllers, system.parent_read_fifo,
     topology.pc1_qpair_reader, block_offset);
  SPDK_ERROR(parent_reader.init());
  system.parent_reader = &parent_reader;

  // Node writer
  node_rw_t<C, typename system_buffers_t<C>::node_io_batch_t> node_writer
    (terminator, *controllers, system.node_write_fifo,
     topology.pc1_qpair_writer, block_offset);
  SPDK_ERROR(node_writer.init());

  // Orchestrator
  orchestrator_t<C> orchestrator
    (terminator, system, node_start, node_stop, parents_filename);
  SPDK_ERROR(orchestrator.init());
  system.orchestrator = &orchestrator;

  // Replica ID hashing buffers for all sectors
  replica_id_buffer_t replica_id_bufs[C::PARALLEL_SECTORS] __attribute__ ((aligned (4096)));
  std::memset(replica_id_bufs, 0, sizeof(replica_id_buffer_t) * C::PARALLEL_SECTORS);

  for (size_t i = 0; i < sector_config->num_hashers(); ++i) {
    for (size_t j = 0; j < NODES_PER_HASHER; ++j) {
      for (size_t k = 0; k < NODE_WORDS; k++) {
        size_t idx = (i * NODES_PER_HASHER * NODE_WORDS +
                      j * NODE_WORDS);
        replica_id_bufs[i].ids[j][k] = htonl(replica_ids[idx + k]);
      }
      replica_id_bufs[i].pad_0[j][0]  = 0x80000000; // byte 67
      replica_id_bufs[i].pad_1[j][7]  = 0x00000200; // byte 125
      replica_id_bufs[i].padding[j][0]  = 0x80000000; // byte 67
      replica_id_bufs[i].padding[j][7]  = 0x00002700; // byte 125
    }
  }

  channel_t<size_t> ch;
  pool.spawn([&]() {
    size_t core_num = topology.pc1_reader;
    set_core_affinity(core_num);
    assert(parent_reader.process(topology.pc1_reader_sleep_time) == 0);
    ch.send(0);
  });
  pool.spawn([&]() {
    size_t core_num = topology.pc1_writer;
    set_core_affinity(core_num);
    assert(node_writer.process(topology.pc1_writer_sleep_time, 10) == 0);
    ch.send(0);
  });

  size_t sector = 0;
  size_t hasher_count = 0;
  for (size_t coord_id = 0; coord_id < sector_config->num_coordinators(); coord_id++) {
    size_t core_num = sector_config->get_coordinator_core(coord_id);
    pool.spawn([&, sector_config, coord_id, core_num, sector, hasher_count]() {
      set_core_affinity(core_num);
      coordinator_t coordinator(terminator, system,
                                coord_id, sector,
                                sector_config->coordinators[coord_id],
                                node_start, node_stop,
                                &replica_id_bufs[hasher_count]);
      system.coordinators[coord_id] = &coordinator;
      assert(coordinator.run() == 0);
      ch.send(0);
    });
    sector += sector_config->coordinators[coord_id].num_sectors();
    hasher_count += sector_config->coordinators[coord_id].num_hashers;
  }

  timestamp_t start = std::chrono::high_resolution_clock::now();
  size_t core_num = topology.pc1_orchestrator;
  set_core_affinity(core_num);

  orchestrator.process(true);

  // Wait for completions
  for (size_t i = 0; i < sector_config->num_coordinators(); i++) {
    ch.recv(); // each coordinator
  }
  terminator = true;
  ch.recv(); // rw handler
  ch.recv(); // node_writer handler

  timestamp_t stop = std::chrono::high_resolution_clock::now();
  uint64_t secs = std::chrono::duration_cast<
    std::chrono::seconds>(stop - start).count();
  printf("Sealing took %ld seconds\n", secs);

  return 0;
}

#ifdef RUNTIME_SECTOR_SIZE
template int do_pc1<sealing_config_128_2KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_128_4KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_128_16KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_128_32KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_128_8MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_128_16MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_128_1GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_128_64GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_64_2KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_64_4KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_64_16KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_64_32KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_64_8MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_64_16MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_64_1GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_64_64GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_32_2KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_32_4KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_32_16KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_32_32KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_32_8MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_32_16MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_32_1GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_32_64GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_16_2KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_16_4KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_16_16KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_16_32KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_16_8MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_16_16MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_16_1GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_16_64GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_8_2KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_8_4KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_8_16KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_8_32KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_8_8MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_8_16MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_8_1GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_8_64GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_4_2KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_4_4KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_4_16KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_4_32KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_4_8MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_4_16MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_4_1GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_4_64GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_2_2KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_2_4KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_2_16KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_2_32KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_2_8MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_2_16MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_2_1GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_2_64GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_1_2KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_1_4KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_1_16KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_1_32KB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_1_8MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_1_16MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_1_1GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_1_64GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
#endif
template int do_pc1<sealing_config_128_512MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_128_32GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_64_512MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_64_32GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_32_512MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_32_32GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_16_512MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_16_32GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_8_512MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_8_32GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_4_512MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_4_32GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_2_512MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_2_32GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_1_512MB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
template int do_pc1<sealing_config_1_32GB_t> (nvme_controllers_t*, topology_t&, uint64_t, const uint32_t*, const char*);
