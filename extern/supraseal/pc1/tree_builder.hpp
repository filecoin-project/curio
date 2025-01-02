// Copyright Supranational LLC

#ifndef __TREE_BUILDER_HPP__
#define __TREE_BUILDER_HPP__

#include "../sealing/constants.hpp"
#include "../poseidon/poseidon.hpp"
#include <util/thread_pool_t.hpp>
#include "../util/debug_helpers.hpp"
#include "../util/mmap_t.hpp"
#include <chrono>
typedef std::chrono::high_resolution_clock::time_point timestamp_t;

// Multi and single threaded tree builder class using Poseidon for tree-c.
// Options
//   arity        - tree arity
//   coL_arity    - column arity
class TreeBuilder {
private:
  size_t   col_arity_;
  size_t   arity_;
  size_t   discard_rows_;
  Poseidon col_hasher_, hasher_;

public:
  TreeBuilder(size_t col_arity, size_t arity, size_t discard_rows) :
    col_arity_(col_arity),
    arity_(arity),
    discard_rows_(discard_rows),
    col_hasher_(col_arity),
    hasher_(arity)
  {}

  // Compute the size to store the tree
  size_t size(size_t count, bool cols, bool no_discard = false) {
    assert (count % arity_ == 0);
    size_t cur_count = count;
    size_t total = 0;

    if (cols) {
        total += cur_count / col_arity_;
        cur_count /= col_arity_;
    }

    size_t discards = cols || no_discard ? 0 : discard_rows_;

    while (cur_count > 1) {
      if (discards > 0) {
          discards--;
      } else {
          total += cur_count / arity_;
      }
      cur_count /= arity_;
    }
    return total;
  }

  // out - hash result
  // in  - pointer to array of arity elements
  void HashNode(node_t* out, node_t* in) {
    hasher_.Hash((uint8_t*)out, (uint8_t*)in);
  }

  // out - hash result
  // in  - pointer to array of arity elements
  void ColHashNode(node_t* out, node_t* in) {
    col_hasher_.Hash((uint8_t*)out, (uint8_t*)in);
  }


  // Single threaded tree builder - uses the calling thread
  // out - storage for non-discarded tree levels
  // in  - tree leaves
  node_t BuildTree(size_t count, node_t* out, node_t* in, bool cols,
                   bool no_discard = false) {
    if (cols) {
      assert (count % col_arity_ == 0);
    }

    assert((cols ? count / col_arity_ : count) % arity_ == 0);

    // Pointer to hash output location
    node_t* cur_hash_out = out;
    node_t* cur_hash_in = in;

    size_t cur_count = count;

    if (cols) {
        node_t* tree_start = cur_hash_out;
        for (size_t i = 0; i < cur_count; i += col_arity_) {
          ColHashNode(cur_hash_out, cur_hash_in);
          cur_hash_in += col_arity_;
          cur_hash_out++;
        }
        cur_hash_in = tree_start;
        cur_count  /= col_arity_;
    }

    int discards = cols || no_discard ? -1 : (int)discard_rows_;
    node_t* discarded_rows = nullptr;
    if (discards > 0) {
        discarded_rows = new node_t[count / arity_];
        cur_hash_out = discarded_rows;
    }

    while (cur_count > 1) {
      node_t* tree_start = cur_hash_out;
      for (size_t i = 0; i < cur_count; i += arity_) {
        HashNode(cur_hash_out, cur_hash_in);
        cur_hash_in += arity_;
        cur_hash_out++;
      }
      cur_hash_in = tree_start;
      cur_count  /= arity_;
      discards--;
      if (discards == 1) {
          cur_hash_out = discarded_rows;
      } else if (discards == 0) {
          cur_hash_out = out;
      }
    }
    delete [] discarded_rows;
    cur_hash_out--;

    return cur_hash_out[0];
  }

  // Multi-threaded tree builder
  // out - storage for non-discarded tree levels
  // in  - tree leaves
  node_t BuildTree(size_t count, node_t* out, node_t* in, thread_pool_t& pool,
                   bool cols, bool no_discard = false) {
    if (cols) {
      assert (count % col_arity_ == 0);
    }

    assert((cols ? count / col_arity_ : count) % arity_ == 0);

    // The single-threaded tree builder should be called with sectors <= 32KiB
    assert (count > (cols ? 128 : 64));

    // For efficient multithreading, divide the tree into
    // a number of chunks that is significantly larger than
    // cores then use an atomic work counter to process them.
    size_t num_chunks = arity_;
    // Create enough chunks to provide good load balancing
    size_t min_chunks = pool.size() * 4;

    while (num_chunks < min_chunks) {
      num_chunks *= arity_;
    }
    size_t chunk_size = count / num_chunks;

    pool.par_map(num_chunks, [this, in, out, count, chunk_size, cols, no_discard](size_t chunk) {
      node_t* layer_out_start = out;
      size_t layer_size = count;

      node_t* cur_hash_out = &out[chunk * chunk_size / (cols ? col_arity_ : arity_)];
      node_t* cur_hash_in = &in[chunk * chunk_size];

      size_t cur_count = chunk_size;

      if (cols) {
          node_t* tree_start = cur_hash_out;
          for (size_t i = 0; i < cur_count; i += col_arity_) {
            ColHashNode(cur_hash_out, cur_hash_in);
            cur_hash_in += col_arity_;
            cur_hash_out++;
          }
          cur_hash_in = tree_start;
          cur_count  /= col_arity_;
          layer_size /= col_arity_;

          layer_out_start += layer_size;
          cur_hash_out = &layer_out_start[chunk * cur_count / arity_];
      }

      int discards = cols || no_discard ? -1 : (int)discard_rows_;
      node_t* discarded_rows = nullptr;
      if (discards > 0) {
          discarded_rows = new node_t[count / arity_];
          cur_hash_out = discarded_rows;
      }

      while (cur_count > 1) {
        node_t* tree_start = cur_hash_out;
        for (size_t i = 0; i < cur_count; i += arity_) {
          HashNode(cur_hash_out, cur_hash_in);
          cur_hash_in += arity_;
          cur_hash_out++;
        }
        cur_hash_in = tree_start;
        cur_count  /= arity_;

        layer_size /= arity_;

        discards--;
        if (discards == 1) {
            cur_hash_out = discarded_rows;
        } else if (discards == 0) {
            cur_hash_out = &out[chunk * cur_count / arity_];
        } else {
            layer_out_start += layer_size;
            cur_hash_out = &layer_out_start[chunk * cur_count / arity_];
        }
      }

      delete [] discarded_rows;
    });

    // Merge the chunks
    size_t top_tree_size = size(num_chunks * arity_, false, true);
    size_t top_tree_start_idx = size(count, cols) - top_tree_size;
    node_t* top_tree_start = &out[top_tree_start_idx];

    return BuildTree(num_chunks, &top_tree_start[num_chunks], top_tree_start, false, true);
  }
};

#endif /* __TREE_BUILDER_HPP__ */
