// Copyright Supranational LLC
#ifndef __PC2_HPP__
#define __PC2_HPP__

#include <filesystem>
#include "pc2_internal.hpp"
#include "../util/util.hpp"
#include "../pc1/tree_c.hpp"
#include "../pc1/tree_r.hpp"

template<class C>
void do_pc2_cpu(topology_t& topology,
                nvme_controllers_t& controllers,
                streaming_node_reader_t<C>& node_reader, size_t block_offset,
                const char** data_filenames, const char* output_dir) {

  node_reader.alloc_slots(1, C::GetNumNodes() * C::GetNumLayers(), true);
  std::atomic<uint64_t> valid;
  size_t valid_count;
  fr_t* data = (fr_t*)node_reader.load_layers(0, 0, 0,
    C::GetNumNodes(), C::GetNumLayers(), &valid, &valid_count);

  thread_pool_t pool;

  while (valid.load() != valid_count) {
    usleep(100);
  }

  for (size_t i = 0; i < C::PARALLEL_SECTORS; i++) {
    std::vector<fr_t> tree_c_input(C::GetNumNodes() * C::GetNumLayers());
    std::vector<fr_t> tree_r_input(C::GetNumNodes());
    node_t tree_c_root, tree_r_root;

    for (size_t j = 0; j < C::GetNumLayers(); j++) {
      for (size_t k = 0; k < C::GetNumNodes(); k++) {
        fr_t val = data[j * C::GetNumNodes() * C::PARALLEL_SECTORS +
                        k * C::PARALLEL_SECTORS + i];
        tree_c_input[k * C::GetNumLayers() + j] = val;
        if (j == C::GetNumLayers() - 1) {
          tree_r_input[k] = val;
        }
      }
    }

    std::string pc2_replica_output_dir = output_dir;
    pc2_replica_output_dir += "/replicas";
    if (!std::filesystem::exists(pc2_replica_output_dir.c_str())) {
      pc2_replica_output_dir = output_dir;
    }

    const size_t MAX = 256;
    char fname[MAX];
    if (C::PARALLEL_SECTORS > 1) {
      snprintf(fname, MAX, "%s/%03ld", pc2_replica_output_dir.c_str(), i);
    } else {
      snprintf(fname, MAX, "%s", pc2_replica_output_dir.c_str());
    }

    std::string sub_output_dir(fname);
    if (!std::filesystem::exists(sub_output_dir)) {
      std::filesystem::create_directory(sub_output_dir);
    }

    std::string p_aux_path = sub_output_dir + std::string("/p_aux");
    mmap_t<node_t> p_aux_file;
    p_aux_file.mmap_write(p_aux_path.c_str(), 2 * sizeof(node_t), true);
    TreeC<C> tree_c;
    tree_c_root = tree_c.BuildTreeC((node_t*)&tree_c_input[0],
                                    sub_output_dir, pool);
    TreeR<C> tree_r;
    tree_r_root = tree_r.BuildTreeR((node_t*)&tree_r_input[0],
                                    sub_output_dir, pool);

    p_aux_file[0] = tree_c_root;
    p_aux_file[1] = tree_r_root;
  }
}

template<class C>
int do_pc2(topology_t& topology,
           nvme_controllers_t& controllers, size_t block_offset,
           const char** data_filenames, const char* output_dir) {
  set_core_affinity(topology.pc2_hasher);

  streaming_node_reader_t<C> node_reader(&controllers, topology.pc2_qpair,
                                         block_offset, topology.pc2_reader,
                                         (size_t)topology.pc2_sleep_time);

  // Do PC2 on the CPU if the sector size is <= 32KiB
  if (C::GetSectorSizeLg() <= 15) {
    do_pc2_cpu(topology, controllers, node_reader, block_offset,
               data_filenames, output_dir);

    return 0;
  } else {
    // Total number of streams across all GPUs
    // Use less streams if sector size is <= 16MiB
    size_t stream_count = C::GetSectorSizeLg() < 24 ? 8 : 64;

    // Batch size in nodes. Each node includes all parallel sectors
    // Reduce batch size if sector size is <= 16MiB
    // TODO: try larger batch size for 32GB
    size_t batch_size = C::GetSectorSizeLg() < 24 ? 64 * 8 : 64;
    assert (batch_size % C::GetNumTreeRCArity() == 0);

    // Nodes to read per partition
    size_t nodes_to_read = C::GetNumNodes() / C::GetNumTreeRCFiles();

    assert (batch_size % C::NODES_PER_PAGE == 0);

    // Allocate storage for 2x the streams to support tree-c and tree-r
    // PC2 assumes that nodes for subsequent layers are contiguous, so each
    // layer's nodes should fill some number of pages.
    node_reader.alloc_slots(stream_count * 2, C::GetNumLayers() * batch_size, true);

    bool tree_r_only = false;
    pc2_hash<C>(topology, tree_r_only, node_reader,
                nodes_to_read, batch_size, stream_count,
                data_filenames, output_dir);
    return 0;
  }
}

#endif
