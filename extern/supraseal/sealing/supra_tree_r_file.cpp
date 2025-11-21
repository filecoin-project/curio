// Copyright Curio Storage, Inc.

#include <vector>
#include <string>

#include "constants.hpp"
#include "topology_t.hpp"
#include "../util/sector_util.hpp"
#include "../util/util.hpp"
#include "../pc2/pc2_internal.hpp"

// CUDA-based tree-r from last-layer file(s) using the file-streaming reader
// Always uses P::PARALLEL_SECTORS == 1
template<class P>
static int tree_r_file_impl(const char* last_layer_filename,
                            const char* data_filename,
                            const char* output_dir) {
  topology_t topology("supra_seal.cfg");
  set_core_affinity(topology.pc2_hasher);

  size_t stream_count = P::GetSectorSizeLg() <= 24 ? 8 : 64;
  size_t batch_size   = P::GetSectorSizeLg() <= 24 ? 64 * 8 : 64 * 64;
  size_t nodes_to_read = P::GetNumNodes() / P::GetNumTreeRCFiles();

  std::vector<std::string> layer_filenames;
  layer_filenames.push_back(std::string(last_layer_filename));
  streaming_node_reader_files_t<sealing_config_t<1, P>> node_reader(P::GetSectorSize(), layer_filenames);

  node_reader.alloc_slots(stream_count * 2, P::GetNumLayers() * batch_size, true);

  const char* data_filenames[1];
  if (data_filename != nullptr && data_filename[0] != '\0') {
    data_filenames[0] = data_filename;
  } else {
    data_filenames[0] = nullptr;
  }

  bool tree_r_only = true;
  pc2_hash_files<sealing_config_t<1, P>>(topology, tree_r_only, node_reader,
                                   nodes_to_read, batch_size, stream_count,
                                   data_filenames, output_dir);
  return 0;
}

extern "C" int tree_r_file(const char* last_layer_filename,
                            const char* data_filename,
                            const char* output_dir,
                            size_t sector_size) {
  SECTOR_PARAMS_TABLE(return tree_r_file_impl<decltype(params)>(last_layer_filename, data_filename, output_dir));
}


