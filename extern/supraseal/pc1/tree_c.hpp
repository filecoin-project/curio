// Copyright Supranational LLC

#include "tree_builder.hpp"

// Tree-c builder for Filecoin sealing
template<class P>
class TreeC {
public:
  node_t BuildTreeC(node_t* leaves, std::string output_path,
                    thread_pool_t& pool) {
    size_t sector_size = P::GetSectorSize();
    size_t cur_nodes = sector_size * P::GetNumLayers() / sizeof(node_t);

    TreeBuilder tree_c(P::GetNumLayers(), P::GetNumTreeRCArity(), P::GetNumTreeRDiscardRows());
    size_t elmts = tree_c.size(cur_nodes / P::GetNumTreeRCFiles(), true);

    node_t final_row[P::GetNumTreeRCFiles()];

    printf("Building tree-c...\n");
    timestamp_t start = std::chrono::high_resolution_clock::now();
    if (!output_path.empty()) {
      const size_t MAX = 256;
      char fname[MAX];
      if (P::GetNumTreeRCFiles() > 1) {
        const char *tree_r_filename_template = "%s/sc-02-data-tree-c-%ld.dat";
        size_t sub_tree_size = cur_nodes / P::GetNumTreeRCFiles();
        for (size_t i = 0; i < P::GetNumTreeRCFiles(); i++) {
          snprintf(fname, MAX, tree_r_filename_template, output_path.c_str(), i);
          mmap_t<node_t> out_file;
          out_file.mmap_write(fname, elmts * sizeof(node_t), true);
          if (P::GetSectorSizeLg() > 15)
            final_row[i] = tree_c.BuildTree(sub_tree_size, &out_file[0], &leaves[i * sub_tree_size], pool, true);
          else
            final_row[i] = tree_c.BuildTree(sub_tree_size, &out_file[0], &leaves[i * sub_tree_size], true);
        }
      } else {
        const char *tree_r_filename_template = "%s/sc-02-data-tree-c.dat";
        snprintf(fname, MAX, tree_r_filename_template, output_path.c_str());
        mmap_t<node_t> out_file;
        out_file.mmap_write(fname, elmts * sizeof(node_t), true);
        if (P::GetSectorSizeLg() > 15)
          final_row[0] = tree_c.BuildTree(cur_nodes, &out_file[0], leaves, pool, true);
        else
          final_row[0] = tree_c.BuildTree(cur_nodes, &out_file[0], leaves, true);
      }
    } else {
      node_t* store = new node_t[elmts];
      if (P::GetSectorSizeLg() > 15)
        (void)tree_c.BuildTree(cur_nodes, store, leaves, pool, true);
      else
        (void)tree_c.BuildTree(cur_nodes, store, leaves, true);
      delete [] store;
    }
    timestamp_t stop = std::chrono::high_resolution_clock::now();
    uint64_t secs = std::chrono::duration_cast<
      std::chrono::seconds>(stop - start).count();
    printf("Tree-c took %ld seconds\n", secs);

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

  node_t BuildTreeC(std::string layers_cache, std::string output_path,
                    int num_threads = 0) {
    thread_pool_t pool(num_threads);

    mmap_t<node_t> layer;
    layer.mmap_read(layers_cache + std::string("/sc-02-data-layer-1.dat"),
                    (size_t)-1);
    size_t sector_size = layer.get_size();
    size_t num_layers = P::GetNumLayers();
    size_t cur_nodes = num_layers * sector_size / sizeof(node_t);
    std::vector<node_t> merged_layers(cur_nodes);

    for (size_t i = 0; i < cur_nodes / num_layers; i++) {
        merged_layers[i * num_layers] = layer[i];
    }

    for (size_t layer_idx = 1; layer_idx < num_layers; layer_idx++) {
        std::string layer_filename = std::string("/sc-02-data-layer-") +
                                     std::to_string(layer_idx + 1) +
                                     std::string(".dat");
        mmap_t<node_t> layer2;
        layer2.mmap_read(layers_cache + layer_filename, (size_t)-1);

        for (size_t i = 0; i < cur_nodes / num_layers; i++) {
            merged_layers[i * num_layers + layer_idx] = layer2[i];
        }
    }

    printf("Building tree-c for sector size %ld\n", P::GetSectorSize());

    node_t* leaves = &merged_layers[0];

    return BuildTreeC(leaves, output_path, pool);
  }
};
