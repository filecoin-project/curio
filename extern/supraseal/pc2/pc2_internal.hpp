// Copyright Supranational LLC
#ifndef __PC2_INTERNAL_HPP__
#define __PC2_INTERNAL_HPP__

#include "../sealing/constants.hpp"
#include "../sealing/data_structures.hpp"
#include "../sealing/topology_t.hpp"
#ifdef STREAMING_NODE_READER_FILES
#include "../c1/streaming_node_reader_files.hpp"
#else
#include "../nvme/streaming_node_reader_nvme.hpp"
#endif

template<class C>
void pc2_hash(topology_t& topology,
              bool tree_r_only,
              streaming_node_reader_t<C>& _reader,
              size_t _nodes_to_read, size_t _batch_size,
              size_t _stream_count,
              const char** data_filenames, const char* output_dir);

template<class C>
void do_pc2_cleanup(const char* output_dir);

#endif
