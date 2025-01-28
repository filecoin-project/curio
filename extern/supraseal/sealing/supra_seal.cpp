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

#include "../sealing/constants.hpp"
#include "../nvme/streaming_node_reader_nvme.hpp"

#include "../c1/c1.hpp"
#include "../pc1/pc1.hpp"
#include "../pc2/pc2.hpp"
#include "../util/util.hpp"
#include "../util/sector_util.cpp"

#include "../util/debug_helpers.cpp"

// Simplify calling the various functions for different
// sector configurations
#define COMMA ,
#define SECTOR_CALL_TABLE(FUNC) \
  switch (num_sectors) {        \
  case 128:                     \
    FUNC(sealing_config_t<128 COMMA decltype(params)>);  \
    break;                      \
  case 64:                      \
    FUNC(sealing_config_t<64 COMMA decltype(params)>);   \
    break;                      \
  case 32:                      \
    FUNC(sealing_config_t<32 COMMA decltype(params)>); \
    break;                      \
  case 16:                      \
    FUNC(sealing_config_t<16 COMMA decltype(params)>); \
    break;                      \
  case 8:                       \
    FUNC(sealing_config_t<8 COMMA decltype(params)>);  \
    break;                      \
  case 4:                       \
    FUNC(sealing_config_t<4 COMMA decltype(params)>);  \
    break;                      \
  case 2:                       \
    FUNC(sealing_config_t<2 COMMA decltype(params)>);  \
    break;                      \
  }

class sealing_ctx_t {
public:
  nvme_controllers_t* controllers;
  topology_t* topology;

  template<class P>
  sealing_ctx_t(P& params, const char* filename) {
    init<P>(filename);
  }

  template<class P>
  void init(const char* filename) {
    printf("Initializing spdk using config %s\n", filename);

    topology = new topology_t(filename);

    // Initialize SPDK
    struct spdk_env_opts opts;
    spdk_env_opts_init(&opts);
    opts.name = "nvme";
    int rc = spdk_env_init(&opts);
    if (rc < 0) {
      fprintf(stderr, "Unable to initialize SPDK env\n");
      exit(1);
    }

    controllers = new nvme_controllers_t(topology->get_allowed_nvme());
    controllers->init(4); // qpairs
    controllers->sort();

    if (controllers->size() < topology->get_allowed_nvme().size()) {
      printf("Unable to attached to all specified NVMe. Ensure spdk scripts/setup.sh"
             " was run and drive list is up-to-date in .cfg\n");
      exit(1);
    }
    
    print_parameters<P>();
    //print_temps();
  }

  void print_temps() {
    std::thread t = std::thread([this]() {
      while (true) {
        controllers->print_temperatures();
        sleep(5 * 60);
      }
    });
    t.detach();
  }
};

static sealing_ctx_t* sealing_ctx = nullptr;

static std::mutex ctx_mtx;

static void init_ctx(size_t sector_size) {
  std::unique_lock<std::mutex> lck(ctx_mtx);
  if (sealing_ctx == nullptr) {
    SECTOR_PARAMS_TABLE(sealing_ctx = new sealing_ctx_t(params, "supra_seal.cfg"));
  }
}

extern "C"
int supra_version() {
    return 0x100001;
}

extern "C"
void supra_seal_init(size_t sector_size, const char* config_file) {
  printf("INIT called %s\n", config_file);
  std::unique_lock<std::mutex> lck(ctx_mtx);
  if (sealing_ctx == nullptr) {
    SECTOR_PARAMS_TABLE(sealing_ctx = new sealing_ctx_t(params, config_file));
  }
}

extern "C"
size_t get_nvme_health_info(nvme_health_info* health_infos, size_t max_controllers, size_t sector_size) {
    init_ctx(sector_size);
    
    if (!sealing_ctx || !sealing_ctx->controllers) {
        return 0;
    }

    auto health_data = (*sealing_ctx->controllers).get_health_info();
    size_t count = std::min(health_data.size(), max_controllers);
    
    // Copy the health info into the provided array
    for (size_t i = 0; i < count; i++) {
        health_infos[i] = health_data[i];
    }
    
    return count;
}

extern "C"
int pc1(uint64_t block_offset, size_t num_sectors,
        const uint8_t* replica_ids, const char* parents_filename,
        size_t sector_size) {
  init_ctx(sector_size);
#ifndef __CUDA_ARCH__
#define CALL_PC1(C)                                                     \
  do_pc1<C>(sealing_ctx->controllers,                 \
                              *sealing_ctx->topology,                   \
                              block_offset,                             \
                              (const uint32_t*)replica_ids,             \
                              parents_filename);
  SECTOR_PARAMS_TABLE(SECTOR_CALL_TABLE(CALL_PC1));
#undef CALL_PC1
#endif
  return 0;
}

extern "C"
int pc2_cleanup(size_t num_sectors, const char* output_dir,
                size_t sector_size) {
#define CALL_PC2_CLEANUP(C)                     \
  do_pc2_cleanup<C>(output_dir);

  SECTOR_PARAMS_TABLE(SECTOR_CALL_TABLE(CALL_PC2_CLEANUP));
#undef CALL_PC2_CLEANUP
  return 0;
}

extern "C"
int pc2(size_t block_offset, size_t num_sectors, const char* output_dir,
        const char** data_filenames, size_t sector_size) {
  init_ctx(sector_size);

#define CALL_PC2(C)                                   \
  do_pc2<C>(*sealing_ctx->topology, \
            *sealing_ctx->controllers,                \
            block_offset,                             \
            data_filenames, output_dir);
  SECTOR_PARAMS_TABLE(SECTOR_CALL_TABLE(CALL_PC2));
#undef CALL_PC2
  return 0;
}

extern "C"
int c1(size_t block_offset, size_t num_sectors, size_t sector_slot,
       const uint8_t* replica_id, const uint8_t* seed,
       const uint8_t* ticket, const char* cache_path,
       const char* parents_filename, const char* replica_path,
       size_t sector_size) {
  try {
    size_t qpair = sealing_ctx->topology->c1_qpair;
    int node_reader_core = sealing_ctx->topology->c1_reader;
    const char* output_dir = cache_path;

    init_ctx(sector_size);

    #define CALL_C1(C) \
    { \
      streaming_node_reader_t<C> reader(sealing_ctx->controllers, qpair, \
                                        block_offset, node_reader_core,   \
                                        sealing_ctx->topology->c1_sleep_time); \
      return do_c1<C>(reader,                                             \
                      num_sectors, sector_slot,                           \
                      replica_id, seed,                                   \
                      ticket, cache_path,                                 \
                      parents_filename, replica_path,                     \
                      output_dir);                                        \
    }

    SECTOR_PARAMS_TABLE(SECTOR_CALL_TABLE(CALL_C1));
    #undef CALL_C1

    return 0;
  } catch (const std::exception& e) {
    fprintf(stderr, "Exception in c1: %s\n", e.what());
    std::terminate();
  } catch (...) {
    fprintf(stderr, "Unknown exception in c1\n");
    std::terminate();
  }
}

template<class C>
int do_node_read(size_t sector_size, uint64_t node_to_read) {
  // Read and print a hashed node
  size_t pages_to_read = 1;

  init_ctx(sector_size);

  page_t<C> *pages = (page_t<C> *)
    spdk_dma_zmalloc(sizeof(page_t<C>) * pages_to_read, PAGE_SIZE, NULL);
  assert (pages != nullptr);

  size_t ctrl_id;
  size_t block_on_controller;
  nvme_node_indexes<C>(sealing_ctx->controllers->size(),
                       node_to_read, ctrl_id, block_on_controller);

  printf("Reading block %ld on controller %ld\n", block_on_controller, ctrl_id);

  sequential_io_t sio((*sealing_ctx->controllers)[ctrl_id]);
  SPDK_ERROR(sio.rw(true, pages_to_read, (uint8_t *)&pages[0], block_on_controller));

  size_t node_in_page = node_to_read % C::NODES_PER_PAGE;
  printf("Node %8lx, ctrl %ld, block %ld, node_in_page %ld\n",
         node_to_read, ctrl_id, block_on_controller, node_in_page);

  char prefix[32];
  snprintf(prefix, 32, "Node %8lx: ", node_to_read);
  print_node<C>(&pages[0].parallel_nodes[node_in_page], 0, prefix, true);
  return 0;
}

int node_read(size_t sector_size, size_t num_sectors, uint64_t node_to_read) {
#define CALL_NR(C)                                   \
  do_node_read<C>(sector_size, node_to_read);
  SECTOR_PARAMS_TABLE(SECTOR_CALL_TABLE(CALL_NR));
#undef CALL_NR
  return 0;
}

extern "C"
size_t get_max_block_offset(size_t sector_size) {
  init_ctx(sector_size);

  if (sealing_ctx->controllers[0].size() == 0) {
    return 0;
  }
  size_t min_block = (*sealing_ctx->controllers)[0].get_page_count(0);
  for (size_t i = 1; i < (*sealing_ctx->controllers).size(); i++) {
    size_t blocks = (*sealing_ctx->controllers)[i].get_page_count(0);
    if (min_block > blocks) {
      min_block = blocks;
    }
  }
  return min_block;
}

extern "C"
size_t get_slot_size(size_t num_sectors, size_t sector_size) {
  size_t num_layers;
  SECTOR_PARAMS_TABLE(num_layers = params.GetNumLayers());

  // Number of nodes stored per page (packed)
  size_t nodes_per_page  = PAGE_SIZE / (num_sectors * NODE_SIZE);
  // Number of pages per layer
  size_t pages_per_layer = sector_size / NODE_SIZE / nodes_per_page;

  // We want the number of sectors to be a power of two and 128 at maximum
  if (!((num_sectors & (num_sectors - 1)) == 0 && num_sectors > 0 && num_sectors <= 128)) {
    printf("Unsupported number of sectors %ld\n", num_sectors);
    exit(1);
  }

  size_t num_controllers = sealing_ctx->controllers[0].size();
  size_t pages_per_layer_per_controller =
    ((pages_per_layer + num_controllers - 1) / num_controllers);
  return pages_per_layer_per_controller * num_layers;
}

node_t* p_aux_open_read(const char* cache) {
  node_t* p_aux_buf = nullptr;

  const char* p_aux_template = "%s/p_aux";
  const size_t MAX = 256;
  char fname[MAX];
  snprintf(fname, MAX, p_aux_template, cache);

  int p_aux_fd = open(fname, O_RDONLY);
  if (p_aux_fd == -1) {
    printf("p_aux_open_read failed, unable to open %s\n", fname);
    return nullptr;
  }

  assert (p_aux_fd != -1);
  p_aux_buf = (node_t*)mmap(NULL, sizeof(node_t) * 2, PROT_READ,
                            MAP_SHARED, p_aux_fd, 0);
  close(p_aux_fd);

  if (p_aux_buf == MAP_FAILED) {
    perror("mmap failed for p_aux file");
    return nullptr;
  }

  return p_aux_buf;
}

void p_aux_close(node_t* p_aux_buf) {
  munmap(p_aux_buf, sizeof(node_t) * 2);
}

bool p_aux_write(int index, size_t nodes, uint8_t* value, const char* cache) {
  assert((index == 0) || (index == 1));
  assert((index == 0) && ((nodes == 1) || (nodes == 2)));
  assert((index == 1) && (nodes == 1));

  const char* p_aux_template = "%s/p_aux";
  const size_t MAX = 256;
  char fname[MAX];
  snprintf(fname, MAX, p_aux_template, cache);

  int p_aux_fd = open(fname, O_RDWR);
  if (p_aux_fd == -1) {
    printf("p_aux_write failed, unable to open %s\n", fname);
    return false;
  }

  assert (p_aux_fd != -1);
  node_t* p_aux_buf = (node_t*)mmap(NULL, sizeof(node_t) * 2, PROT_WRITE,
                                    MAP_SHARED, p_aux_fd, 0);

  if (p_aux_buf == MAP_FAILED) {
    perror("mmap failed for p_aux file");
    return false;
  }

  std::memcpy(&(p_aux_buf[index]), value, nodes * sizeof(node_t));

  munmap(p_aux_buf, sizeof(node_t) * 2);
  close(p_aux_fd);
  return true;
}

template<size_t sector_size>
bool get_comm_from_tree(SectorParameters<sector_size> &params, uint8_t* comm, const char* cache,
                        size_t num_files, const char* prefix) {
  uint8_t* bufs[num_files];

  size_t file_size =0;

  for (size_t l = 0; l < num_files; ++l) {
    const size_t MAX = 256;
    char fname[MAX];
    if (num_files == 1) {
      snprintf(fname, MAX, prefix, cache);
    } else {
      snprintf(fname, MAX, prefix, cache, l);
    }

    int tree_fd = open(fname, O_RDONLY);
    if (tree_fd == -1) {
      printf("Failed to open tree file %s\n", fname);
      return false;
    }

    assert (tree_fd != -1);
    struct stat buf;
    fstat(tree_fd, &buf);
    bufs[l] = (uint8_t*)mmap(NULL, buf.st_size, PROT_READ, MAP_SHARED,
                             tree_fd, 0);
    file_size = buf.st_size;
    if (bufs[l] == MAP_FAILED) {
      perror("mmap failed for tree file");
      return false;
    }
    close(tree_fd);
  }

  if (num_files == 1) {
    uint8_t* comm_addr = bufs[0] + (file_size - sizeof(node_t));
    std::memcpy(comm, comm_addr, sizeof(node_t));
  } else {
    // Since files > 1, assume poseidon tree
    size_t arity = params.GetNumTreeRCArity();
    node_t nodes[arity];

    for (size_t l = 0; l < num_files; ++l) {
      uint8_t* last_addr = bufs[l] + (file_size - sizeof(node_t));
      std::memcpy((uint8_t*)&(nodes[l]), last_addr, sizeof(node_t));
    }

    Poseidon poseidon_comm(arity);
    poseidon_comm.Hash(comm, (uint8_t*)&(nodes[0]));
  }

  for (size_t l = 0; l < num_files; ++l) {
    munmap(bufs[l], file_size);
  }

  return true ;
}

extern "C"
bool get_comm_c_from_tree(uint8_t* comm_c, const char* cache_path,
                          size_t sector_size) {
  SECTOR_PARAMS_TABLE(if (params.GetNumTreeRCFiles() == 1) {                     \
                        return get_comm_from_tree(params, comm_c, cache_path,    \
                                                  params.GetNumTreeRCFiles(),    \
                                                  "%s/sc-02-data-tree-c.dat");   \
                      }                                                          \
                      return get_comm_from_tree(params, comm_c, cache_path,      \
                                                params.GetNumTreeRCFiles(),      \
                                                "%s/sc-02-data-tree-c-%ld.dat"); \
    );
}

extern "C"
bool get_comm_c(uint8_t* comm_c, const char* cache_path) {
  node_t* p_aux_buf = p_aux_open_read(cache_path);
  if (p_aux_buf == nullptr) return false;

  std::memcpy(comm_c, &(p_aux_buf[0]), sizeof(node_t));

  p_aux_close(p_aux_buf);
  return true;
}

extern "C"
bool set_comm_c(uint8_t* comm_c, const char* cache_path) {
  return p_aux_write(0, 1, comm_c, cache_path);
}

extern "C"
bool get_comm_r_last_from_tree(uint8_t* comm_r_last, const char* cache_path,
                               size_t sector_size) {
  SECTOR_PARAMS_TABLE(if (params.GetNumTreeRCFiles() == 1) {                          \
                        return get_comm_from_tree(params, comm_r_last, cache_path,    \
                                                  params.GetNumTreeRCFiles(),         \
                                                  "%s/sc-02-data-tree-r-last.dat");   \
                      }                                                               \
                      return get_comm_from_tree(params, comm_r_last, cache_path,      \
                                                params.GetNumTreeRCFiles(),           \
                                                "%s/sc-02-data-tree-r-last-%ld.dat"); \
    );
}

extern "C"
bool get_comm_r_last(uint8_t* comm_r_last, const char* cache_path) {
  node_t* p_aux_buf = p_aux_open_read(cache_path);
  if (p_aux_buf == nullptr) return false;

  std::memcpy(comm_r_last, &(p_aux_buf[1]), sizeof(node_t));

  p_aux_close(p_aux_buf);
  return true;
}

extern "C"
bool set_comm_r_last(uint8_t* comm_r_last, const char* cache_path) {
  return p_aux_write(1, 1, comm_r_last, cache_path);
}

extern "C"
bool get_comm_r(uint8_t* comm_r, const char* cache_path) {
  node_t* p_aux_buf = p_aux_open_read(cache_path);
  if (p_aux_buf == nullptr) return false;

  Poseidon poseidon_comm_r(2);
  poseidon_comm_r.Hash(comm_r, (uint8_t*)p_aux_buf);

  p_aux_close(p_aux_buf);

  return true;
}

extern "C"
bool get_comm_d(uint8_t* comm_d, const char* cache_path, size_t sector_size) {
  SECTOR_PARAMS_TABLE(return get_comm_from_tree(params, comm_d, cache_path, 1, "%s/sc-02-data-tree-d.dat"));
}

extern "C"
bool get_cc_comm_d(uint8_t* comm_d, size_t sector_size) {
  SECTOR_PARAMS_TABLE(std::memcpy(comm_d, CC_TREE_D_NODE_VALUES[params.GetNumTreeDLevels()], \
                                  sizeof(node_t)));

  return true;
}

#undef SECTOR_PARAMS_TABLE
#undef SECTOR_CALL_TABLE
#undef COMMA
