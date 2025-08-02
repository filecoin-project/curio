// Copyright Supranational LLC

#ifndef __TREE_D_HPP__
#define __TREE_D_HPP__

#include <chrono>

#include "tree_d.hpp"
#include "../sealing/constants.hpp"
#include "../sealing/sector_parameters.hpp"
#include "../util/sector_util.cpp"

// g++ -g -Wall -Wextra -Werror -DRUNTIME_SECTOR_SIZE -march=native -O3 -I../pc1 tree_d.cpp -L../deps/blst -lblst

int main(int argc, char* argv[]) {
  int  opt   = 0;
  bool copy  = true;
  std::string sector_size_string = "";

  std::string tree_d_filename = "./sc-02-data-tree-d.dat";
  std::string data_filename   = "";

  while ((opt = getopt(argc, argv, "t:d:s:ph")) != -1) {
    switch(opt) {
      case 't':
        std::cout << "tree_d_filename input " << optarg << std::endl;
        tree_d_filename = optarg;
        break;
      case 'd':
        std::cout << "data_filename input   " << optarg << std::endl;
        data_filename = optarg;
        break;
      case 'p':
        std::cout << "Copy flag is set" << std::endl;
        copy = false;
        break;
      case 's':
        std::cout << "sector_size input     " << optarg << std::endl;
        sector_size_string = optarg;
        break;
      case 'h':
      case ':':
      case '?':
        std::cout << "Sealing Client" << std::endl;
        std::cout << "If no staged data file, CC is assumed" << std::endl;
        std::cout << "Usage: " << argv[0] << " [OPTIONS]" << std::endl;
        std::cout << "-h        Print help message" << std::endl;
        std::cout << "-t <path> Tree D output file" << std::endl;
        std::cout << "-d <path> Staged data file" << std::endl;
        std::cout << "-s <size> Sector Size (2KiB, 32GiB, etc) " << std::endl;
        std::cout << "-p        Don't copy data into tree leaves" << std::endl;
        break;
    }
  }

  if (sector_size_string.empty()) {
    std::cout << "Please specify a sector size" << std::endl;
    exit(1);
  }

  size_t sector_size = get_sector_size_from_string(sector_size_string);

  node_t comm_d;

  auto start = std::chrono::high_resolution_clock::now();

  SECTOR_PARAMS_TABLE(                                           \
    TreeD tree_d(params, copy);                        \
                                                                 \
    if (!data_filename.empty()) {                                \
      tree_d.BuildTree(&comm_d, tree_d_filename, data_filename); \
    } else {                                                     \
      tree_d.BuildCCTree(&comm_d, tree_d_filename);              \
    }                                                            \
                                                                 \
    std::cout << std::endl << "comm_d ";                         \
    tree_d.print_digest_hex(&comm_d);                            \
  );

  auto cur = std::chrono::high_resolution_clock::now();
  auto duration =
    std::chrono::duration_cast<std::chrono::milliseconds>(cur - start).count();
  start = cur;
  std::cout << "Tree D generation took " << duration << "ms" << std::endl;

  return 0;
}
#endif
