// Copyright Supranational LLC

#include "tree_builder.hpp"

// Tree-r builder for Filecoin sealing with optional data encoding
template<class P>
class TreeR {
public:
  void ElementAdd(uint8_t* out, uint8_t* a, uint8_t* b) {
    fr_t a_mont;
    a_mont.to(a, 32, true);

    fr_t* out_ptr = (fr_t*)out;
    out_ptr->to(b, 32, true);

    *out_ptr += a_mont;

    out_ptr->to_scalar(*((fr_t::pow_t*)out));
  }

  // // TODO: This is no faster - presumably disk IO limited
  // void ElementAdd(node_t* out, node_t a, node_t b) {
  //   a.reverse_l();
  //   b.reverse_l();
  //   fr_t* fra = (fr_t*)&a;
  //   fr_t* frb = (fr_t*)&b;
  //   fr_t* frout = (fr_t*)out;
  //   *frout = *fra + *frb;
  //   out->reverse_l();
  // }

  node_t BuildTreeR(node_t* leaves, std::string output_path,
                    thread_pool_t& pool) {

    size_t last_layer_size = P::GetSectorSize();
    size_t sector_size = last_layer_size / sizeof(node_t);

    TreeBuilder tree_r(2, P::GetNumTreeRCArity(), P::GetNumTreeRDiscardRows());
    size_t elmts = tree_r.size(sector_size / P::GetNumTreeRCFiles(), false);

    node_t final_row[P::GetNumTreeRCFiles()];

    printf("Building tree-r...\n");
    timestamp_t start = std::chrono::high_resolution_clock::now();
    if (!output_path.empty()) {
      const size_t MAX = 256;
      char fname[MAX];
      if (P::GetNumTreeRCFiles() > 1) {
        const char *tree_r_filename_template = "%s/sc-02-data-tree-r-last-%ld.dat";
        size_t sub_tree_size = sector_size / P::GetNumTreeRCFiles();
        for (size_t i = 0; i < P::GetNumTreeRCFiles(); i++) {
          snprintf(fname, MAX, tree_r_filename_template, output_path.c_str(), i);
          mmap_t<node_t> out_file;
          out_file.mmap_write(fname, elmts * sizeof(node_t), true);
          if (P::GetSectorSizeLg() > 15) {
              final_row[i] = tree_r.BuildTree(sub_tree_size, &out_file[0], &leaves[i * sub_tree_size], pool, false);
          }
          else {
              final_row[i] = tree_r.BuildTree(sub_tree_size, &out_file[0], &leaves[i * sub_tree_size], false);
          }
        }
      } else {
        const char *tree_r_filename_template = "%s/sc-02-data-tree-r-last.dat";
        snprintf(fname, MAX, tree_r_filename_template, output_path.c_str());
        mmap_t<node_t> out_file;
        out_file.mmap_write(fname, elmts * sizeof(node_t), true);
        if (P::GetSectorSizeLg() > 15) {
            final_row[0] = tree_r.BuildTree(sector_size, &out_file[0], leaves, pool, false);
        }
        else {
            final_row[0] = tree_r.BuildTree(sector_size, &out_file[0], leaves, false);
        }
      }
    } else {
      node_t* store = new node_t[elmts];
      if (P::GetSectorSizeLg() > 15)
        (void)tree_r.BuildTree(sector_size, store, leaves, pool, false);
      else
        (void)tree_r.BuildTree(sector_size, store, leaves, false);
      delete [] store;
    }
    timestamp_t stop = std::chrono::high_resolution_clock::now();
    uint64_t secs = std::chrono::duration_cast<
      std::chrono::seconds>(stop - start).count();
    printf("Tree-r took %ld seconds\n", secs);

    if (P::GetNumTreeRCFiles() == 16) {
        node_t n2[2], root;
        TreeBuilder last(2, 8, 0);

        last.HashNode(&n2[0], &final_row[0]);
        last.HashNode(&n2[1], &final_row[8]);

        last.ColHashNode(&root, n2);
        return root;
    } else if (P::GetNumTreeRCFiles() > 1) {
        TreeBuilder last(2, P::GetNumTreeRCFiles(), 0);
        node_t root;
        last.HashNode(&root, final_row);
        return root;
    } else {
        return final_row[0];
    }
  }

  // TODO: templatize this too and don't determine sector size in a roundabout way
  // TODO: also do the same for tree C
  node_t BuildTreeR(std::string last_layer_filename, std::string data_filename,
                    std::string output_path, int num_threads = 0) {
    thread_pool_t pool(num_threads);

    mmap_t<node_t> last_layer;
    last_layer.mmap_read(last_layer_filename, (size_t)-1);
    size_t last_layer_size = last_layer.get_size();

    printf("Building tree-r for sector size %ld\n", P::GetSectorSize());

    size_t sector_size = last_layer_size / sizeof(node_t);

    node_t* leaves = &last_layer[0];

    // Encode the data, if provided
    mmap_t<node_t> sealed_file;
    node_t* encoded_leaves = nullptr;
    if (!data_filename.empty()) {
      size_t num_chunks = pool.size() * 4;
      size_t chunk_size = (sector_size + num_chunks - 1) / num_chunks;

      std::string sealed_filename = output_path + "/sealed-file";
      sealed_file.mmap_write(sealed_filename, last_layer_size, true);
      encoded_leaves = &sealed_file[0];

      mmap_t<node_t> data_file;
      data_file.mmap_read(data_filename, last_layer_size);

      printf("Encoding data...\n");
      timestamp_t start = std::chrono::high_resolution_clock::now();
      pool.par_map(num_chunks, [&](size_t chunk) {
        size_t start = chunk * chunk_size;
        size_t stop = std::min(start + chunk_size, sector_size);
        for (size_t i = start; i < stop; ++i) {
          ElementAdd((uint8_t*)&(encoded_leaves[i]),
                     (uint8_t*)&(data_file[i]),
                     (uint8_t*)&(last_layer[i]));
        }
      });
      timestamp_t stop = std::chrono::high_resolution_clock::now();
      uint64_t secs = std::chrono::duration_cast<
        std::chrono::seconds>(stop - start).count();

      // Use the encoded data for tree building
      leaves = &encoded_leaves[0];
      printf("Encoding took %ld seconds\n", secs);
    }

    return BuildTreeR(leaves, output_path, pool);
  }
};
