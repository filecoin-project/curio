// Copyright Supranational LLC
#ifndef __PC2_INTERNAL_HPP__
#define __PC2_INTERNAL_HPP__

#include "../sealing/constants.hpp"
#include "../sealing/data_structures.hpp"
#include "../sealing/topology_t.hpp"

// Include NVMe reader implementation (default)
#include "../nvme/streaming_node_reader_nvme.hpp"

// Include file reader implementation with renamed class to avoid conflict
#define streaming_node_reader_t streaming_node_reader_files_t
#include "../c1/streaming_node_reader_files.hpp"
#undef streaming_node_reader_t

// Unified pc2_hash template that works with either reader type
// Both implementations satisfy the same interface
template<class C, class Reader>
void pc2_hash_impl(topology_t& topology,
                   bool tree_r_only,
                   Reader& _reader,
                   size_t _nodes_to_read, size_t _batch_size,
                   size_t _stream_count,
                   const char** data_filenames, const char* output_dir);

// Convenience wrappers for backward compatibility
template<class C>
void pc2_hash(topology_t& topology,
              bool tree_r_only,
              streaming_node_reader_t<C>& _reader,
              size_t _nodes_to_read, size_t _batch_size,
              size_t _stream_count,
              const char** data_filenames, const char* output_dir) {
  pc2_hash_impl<C, streaming_node_reader_t<C>>(
    topology, tree_r_only, _reader, _nodes_to_read, _batch_size,
    _stream_count, data_filenames, output_dir);
}

template<class C>
void pc2_hash_files(topology_t& topology,
                    bool tree_r_only,
                    streaming_node_reader_files_t<C>& _reader,
                    size_t _nodes_to_read, size_t _batch_size,
                    size_t _stream_count,
                    const char** data_filenames, const char* output_dir) {
  pc2_hash_impl<C, streaming_node_reader_files_t<C>>(
    topology, tree_r_only, _reader, _nodes_to_read, _batch_size,
    _stream_count, data_filenames, output_dir);
}

template<class C>
void do_pc2_cleanup(const char* output_dir);

#endif
