// Copyright Supranational LLC
#ifndef __PC2_CUH__
#define __PC2_CUH__

#include "../../nvme/ring_t.hpp"
#include "../pc2_internal.hpp"
#include "file_writer_t.hpp"

//#define DISABLE_FILE_READS
//#define DISABLE_FILE_WRITES

// Class to compute the offset of serialized nodes in a tree.
template<class P>
class tree_address_t {
  size_t node_count;
  size_t arity;
  size_t node_size;
  std::vector<size_t> layer_offsets;
public:
  tree_address_t(size_t _node_count, size_t _arity, size_t _node_size, size_t layer_skips)
    : node_count(_node_count), arity(_arity), node_size(_node_size) {
    size_t layer = 0;
    size_t offset = 0;
    size_t arity = P::GetNumTreeRCArity();
      
    for (size_t i = 0; i < layer_skips; i++) {
      node_count /= arity;
    }
    while (node_count > 1) {
      layer_offsets.push_back(offset);
      layer++;
      offset += node_count * node_size;
      node_count /= arity;
    }
    layer_offsets.push_back(offset);
  }

  size_t address(node_id_t<P>& node) {
    size_t base = layer_offsets[node.layer()];
    return base + (size_t)node.node() * node_size;
  }

  // Total tree size
  size_t data_size() {
    return layer_offsets.back() + node_size;
  }

  void print() {
    size_t layer = 0;
    for (auto i : layer_offsets) {
      printf("layer %2ld, offset 0x%08lx %ld\n", layer, i, i);
      layer++;
    }
  }
};

enum class ResourceState {
  IDLE,
  DATA_READ,
  DATA_WAIT,
  HASH_COLUMN,
  HASH_COLUMN_WRITE,
  HASH_COLUMN_LEAVES,
  HASH_LEAF,
  HASH_WAIT,
  DONE
};

typedef host_ptr_t<fr_t> host_buffer_t;

template<class C>
struct gpu_resource_t {
  size_t id;

  // GPU id
  const gpu_t& gpu;

  // GPU stream
  stream_t stream;

  // Storage for column input data
  size_t batch_elements;
  // Host side column (layer) data
  fr_t* column_data;
  // Device side column (layer) data
  dev_ptr_t<fr_t> column_data_d;
  // Host side column (layer) data
  host_ptr_t<fr_t> replica_data;
  // Starting node for the column data
  size_t start_node;
  // Valid count from page reader
  std::atomic<uint64_t> valid;
  // Expected valid count for all pages
  size_t valid_count;

  // Hashed node buffers
  buffers_t<gpu_buffer_t> buffers;

  // Aux buffer
  dev_ptr_t<fr_t> aux_d;

  // Schedulers for tree-c and tree-r. They will follow identical paths
  // but this is a clean way to track input/output buffers through the tree.
  scheduler_t<gpu_buffer_t, C> scheduler_c;
  scheduler_t<gpu_buffer_t, C> scheduler_r;

  // Current work item
  work_item_t<gpu_buffer_t, C> work_c;
  work_item_t<gpu_buffer_t, C> work_r;
  // Flag set by Cuda when a hashing job is complete
  std::atomic<bool> async_done;

  ResourceState state;

  // Last hash is in progress
  bool last;

  gpu_resource_t(size_t _id,
                 const gpu_t& _gpu,
                 size_t _nodes_to_read,
                 size_t _batch_size)
    : id(_id),
      gpu(_gpu),
      stream(gpu.id()),
      // TODO: could allocate 1 layer when only doing tree_r
      batch_elements(C::PARALLEL_SECTORS * C::GetNumLayers() * _batch_size),
      column_data_d(batch_elements),
      replica_data(C::PARALLEL_SECTORS * _batch_size),
      buffers(_batch_size * C::PARALLEL_SECTORS),
      // Size aux to hold the larger of the tree and column hash data
      aux_d(max(// column aux size
                _batch_size * C::PARALLEL_SECTORS * (C::GetNumLayers() + 1),
                // tree aux size - expand to hold domain tag
                _batch_size * C::PARALLEL_SECTORS / C::GetNumTreeRCArity() *
                                                    C::GetNumTreeRCArityDT())),
      scheduler_c(_nodes_to_read / _batch_size, C::GetNumTreeRCArity(), buffers),
      scheduler_r(_nodes_to_read / _batch_size, C::GetNumTreeRCArity(), buffers),
      async_done(true),
      state(ResourceState::IDLE),
      last(false)
  {}
  void reset() {
    state = ResourceState::IDLE;
    last = false;
    async_done = true;
    scheduler_c.reset();
    scheduler_r.reset();
  }
};

template<class C>
struct buf_to_disk_t {
  // Block of data from the device (pointed into by src)
  fr_t* data;
  // Destination address (mmapped file)
  file_writer_t<fr_t>* dst[C::PARALLEL_SECTORS];
  size_t offset;

  // Source address
  fr_t* src[C::PARALLEL_SECTORS];
  // Size of each write, in field elements
  size_t size;
  // Stride for subsequent field elements
  size_t stride;
  // Whether bytes should be reversed
  bool reverse;
};

template<class C>
class pc2_t {
private:
  topology_t& topology;
  bool tree_r_only;
  streaming_node_reader_t<C>& reader;
  size_t nodes_to_read;
  size_t batch_size;
  tree_address_t<C> tree_c_address;
  tree_address_t<C> tree_r_address;
  size_t stream_count;
  size_t nodes_per_stream;

  union PoseidonCudaOption {
      PoseidonCuda<3>* arity_2;
      PoseidonCuda<12>* arity_11;
  };

  // Array of vectors of mapped files
  std::vector<file_writer_t<fr_t>*> tree_c_files[C::PARALLEL_SECTORS];
  std::vector<file_writer_t<fr_t>*> tree_r_files[C::PARALLEL_SECTORS];
  // Files that store the data being sealed
  mmap_t<fr_t> data_files[C::PARALLEL_SECTORS];
  // Files that store the sealed data
  file_writer_t<fr_t> sealed_files[C::PARALLEL_SECTORS];

  // Store the partition roots
  std::vector<fr_t> tree_c_partition_roots;
  std::vector<fr_t> tree_r_partition_roots;

  // Storage to transfer results from GPU to CPU for tree-c and tree-r
  std::mutex gpu_results_in_use;
  host_ptr_t<fr_t> gpu_results_c;
  host_ptr_t<fr_t> gpu_results_r;

  // Final offset for GPU data in tree-c and tree-rfiles
  size_t final_gpu_offset_c;
  size_t final_gpu_offset_r;

  // Used to compute the actual node id for the various streams
  std::vector<size_t> layer_offsets_c;
  std::vector<size_t> layer_offsets_r;

  // GPU resources
  std::vector<PoseidonCudaOption> poseidon_columns;
  std::vector<PoseidonCuda<C::GetNumTreeRCArityDT()>*> poseidon_trees;
  std::vector<gpu_resource_t<C>*> resources;

  // Buffer to store pages loaded from drives
  uint8_t* page_buffer;

  // Buffer pool for data coming back from GPU
  // The number of buffers should be large enough to hide disk IO delays.
  //
  static const size_t num_host_bufs = 1<<13;
  static const size_t disk_io_batch_size = 64;
  // static const size_t num_host_bufs = 64;
  // static const size_t disk_io_batch_size = 4;
  static const size_t num_host_batches = num_host_bufs / disk_io_batch_size;
  // Should be a minimum of gpu resources / disk_io_batch_size
  static const size_t num_host_empty_batches = 8;
public:
  typedef batch_t<buf_to_disk_t<C>*, disk_io_batch_size> buf_to_disk_batch_t;
private:

  // Memory space for the host side buffers
  host_ptr_t<fr_t> host_buf_storage;
  // Store the host buffer batch objects
  // Each batch contains disk_io_batch_size buffers, each of which contains
  // batch_size * C::PARALLEL_SECTORS field elements.
  std::vector<buf_to_disk_t<C>> host_bufs;
  std::vector<buf_to_disk_batch_t> host_batches;
  // Queue to write to disk
  mtx_fifo_t<buf_to_disk_batch_t> host_buf_to_disk;
  // Pool of available full batches
  mtx_fifo_t<buf_to_disk_batch_t> host_buf_pool_full;
  // Pool of available empty batches
  mtx_fifo_t<buf_to_disk_batch_t> host_buf_pool_empty;

  // p_aux filenames
  std::vector<std::string> p_aux_filenames;

  // When performing data encoding, the source data files. `data_filenames`
  // or any individual pointer may be null, in which case CC is assumed.
  const char** data_filenames;
  // Record the presence of CC/non-CC to simplify coding logic
  bool has_cc_sectors;
  bool has_non_cc_sectors;

  // The output directory for files we will write
  const char* output_dir;

public:
  static void get_filenames(const char* output_dir,
                            std::vector<std::string>& directories,
                            std::vector<std::string>& p_aux_filenames,
                            std::vector<std::vector<std::string>>& tree_c_filenames,
                            std::vector<std::vector<std::string>>& tree_r_filenames,
                            std::vector<std::string>& sealed_filenames);
private:
  void open_files();

  void hash_gpu(size_t partition);
  void hash_cpu(fr_t* roots, size_t partition, fr_t* input,
                std::vector<file_writer_t<fr_t>*>* tree_files,
                size_t file_offset);
  void write_roots(fr_t* roots_c, fr_t* roots_r);
  void process_writes(int core, size_t max_write_size,
                      mtx_fifo_t<buf_to_disk_batch_t>& to_disk,
                      mtx_fifo_t<buf_to_disk_batch_t>& pool,
                      std::atomic<bool>& terminate,
                      std::atomic<int>& disk_writer_done);

  static void parse_custom_paths(const char* custom_paths,
                          std::vector<std::string>& directories,
                          std::vector<std::string>& p_aux_filenames,
                          std::vector<std::vector<std::string>>& tree_c_filenames,
                          std::vector<std::vector<std::string>>& tree_r_filenames,
                          std::vector<std::string>& sealed_filenames);

  static void generate_default_paths(const char* output_dir,
                              const std::string& pc2_replica_output_dir,
                              std::vector<std::string>& directories,
                              std::vector<std::string>& p_aux_filenames,
                              std::vector<std::vector<std::string>>& tree_c_filenames,
                              std::vector<std::vector<std::string>>& tree_r_filenames,
                              std::vector<std::string>& sealed_filenames);

  static void add_paths_for_sector(const char* output_dir,
                            size_t sector,
                            const std::string& pc2_replica_output_dir,
                            std::vector<std::string>& directories,
                            std::vector<std::string>& p_aux_filenames,
                            std::vector<std::vector<std::string>>& tree_c_filenames,
                            std::vector<std::vector<std::string>>& tree_r_filenames,
                            std::vector<std::string>& sealed_filenames);

public:
  pc2_t(topology_t& _topology,
        bool _tree_r_only, streaming_node_reader_t<C>& _reader,
        size_t _nodes_to_read, size_t _batch_size, size_t _stream_count,
        const char** data_filenames, const char* output_dir);
  ~pc2_t();

  void hash();
};

#endif
