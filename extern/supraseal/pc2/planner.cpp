// Copyright Supranational LLC

#include <vector>
#include <map>
#include <stdint.h>
#include <stdio.h>
#include <functional>
#include "pc2_internal.hpp"

//using namespace std;

typedef dev_ptr_t<fr_t> gpu_buffer_t;

template<class buffer_t, class P>
struct work_item_t {
  // Index of the arity elements to be hashed
  node_id_t<P> idx;
  size_t       dependencies_ready;
  bool         is_leaf;
  buffer_t*    buf;
  buffer_t*    inputs[P::GetNumTreeRCArity()];

  work_item_t() {
    idx = (uint64_t)-1;
    dependencies_ready = 0;
    is_leaf = false;
    buf = nullptr;
    for (size_t i = 0; i < P::GetNumTreeRCArity(); i++) {
      inputs[i] = nullptr;
    }
  }

  work_item_t(node_id_t<P> _idx, bool _is_leaf) {
    idx = _idx;
    dependencies_ready = 0;
    is_leaf = _is_leaf;
    buf = nullptr;
    for (size_t i = 0; i < P::GetNumTreeRCArity(); i++) {
      inputs[i] = nullptr;
    }
  }

  size_t leaf_num() {
    return idx.node() & (P::GetNumTreeRCArity() - 1);
  }

  void print() {
    printf("layer %2d, node %08x, deps ready %ld, buf %p",
           idx.layer(), idx.node(), dependencies_ready, buf);
  }
};

template<class buffer_t>
class buffers_t {

  std::vector<buffer_t*> buffers;
  size_t num_elements;
  size_t num_buffers;

public:
  buffers_t(size_t _num_elements) {
    num_buffers = 0;
    num_elements = _num_elements;
  }

  ~buffers_t() {
    while (buffers.size() != 0) {
      delete buffers.back();
      buffers.pop_back();
    }
  }

  buffer_t* get() {
    buffer_t* buf;
    if (buffers.size() == 0) {
      buf = new buffer_t(num_elements);
      num_buffers++;
    } else {
      buf = buffers.back();
      buffers.pop_back();
    }
    return buf;
  }

  void put(buffer_t* buf) {
    buffers.push_back(buf);
  }

  size_t size() {
    return buffers.size();
  }
};

void indent(size_t levels) {
  for (size_t i = 0; i < levels; i++) {
    printf("  ");
  }
}

// This doesn't need to consider striding of sectors within a page except
// for the storage used.
template<class buffer_t, class P>
class scheduler_t {
public:
  typedef std::vector<work_item_t<buffer_t, P>> work_stack_t;
  typedef std::map<uint64_t, work_item_t<buffer_t, P>> work_map_t;
  typedef std::function<void(work_item_t<buffer_t, P>&)> hash_cb_t;

protected:
  size_t initial_nodes;
  work_stack_t stack;
  work_map_t wip;
  buffers_t<buffer_t>& bufs;
  size_t hash_count;
  size_t arity;

public:
  scheduler_t(size_t _initial_nodes, size_t _arity,
              buffers_t<buffer_t>& _bufs)
    : initial_nodes(_initial_nodes), bufs(_bufs)
  {
    arity = _arity;

    reset();
  }

  ~scheduler_t() {
  }

  void reset() {
    hash_count = 0;

    wip.clear();
    stack.clear();

    // Insert all of the initial work for the bottom layer of the tree
    for (size_t i = 0; i < initial_nodes; i++) {
      // Start with layer one since the hash represents the resulting layer,
      // not the input layer
      stack.emplace_back(node_id_t<P>(1, initial_nodes - i - 1), true);
    }
  }

  // Returns true if there is more work to be done
  bool next(hash_cb_t hash_cb, work_item_t<buffer_t, P>* work) {
    if (stack.size() == 0) {
      return false;
    }

    // Pop the element
    work_item_t<buffer_t, P> w = stack.back();
    stack.pop_back();

    // We need a buffer whether it's a leaf or an internal node
    w.buf = bufs.get();
    if (work) {
      *work = w;
    } else {
      // Perform a hash
      hash_cb(w);
    }

    hash_count++;

    // Return the input buffers to the pool
    if (!w.is_leaf) {
      for (size_t i = 0; i < P::GetNumTreeRCArity(); i++) {
        bufs.put(w.inputs[i]);
      }
    }

    // Compute the location of the parent node
    // Map them to the output node in the next layer
    node_id_t<P> next_layer_id(w.idx.layer() + 1, w.idx.node() / arity);

    // Record the result
    if (wip.find(next_layer_id) == wip.end()) {
      wip.emplace(next_layer_id, work_item_t<buffer_t, P>(next_layer_id, false));
    }
    work_item_t<buffer_t, P>& dependant_work = wip.at(next_layer_id);
    dependant_work.inputs[w.leaf_num()] = w.buf;

    dependant_work.dependencies_ready++;
    if (dependant_work.dependencies_ready == arity) {
      // Move from wip into the queue
      stack.push_back(dependant_work);
      wip.erase(next_layer_id);
    }

    return stack.size() != 0;
  }

  bool is_done() {
    if (wip.size() != 1) {
      printf("ERROR planner.cpp: expected wip.size() to be 1\n");
      assert(false);
    }
    work_item_t<buffer_t, P>& work = wip.begin()->second;
    if (work.inputs[0] == nullptr) {
      printf("ERROR planner.cpp: expected work.inputs[0] != nullptr\n");
      assert(false);
    }
    if (work.inputs[1] != nullptr) {
      printf("ERROR planner.cpp: expected work.inputs[1] == nullptr\n");
      assert(false);
    }
    return true;
  }

  buffer_t* run(hash_cb_t hash_cb) {
    while(next(hash_cb, nullptr)) {}
    work_item_t<buffer_t, P>& work = wip.begin()->second;
    return work.inputs[0];
  }
};
