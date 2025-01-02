// Copyright Supranational LLC

// Runtime sector parameters
// Mostly derived from filecoin-proofs/src/constants.rs

#ifndef __SECTOR_PARAMETERS_HPP__
#define __SECTOR_PARAMETERS_HPP__

#include <string>
#include <iostream>
#include <assert.h>

template<size_t sector_size_>
class SectorParameters {
 public:
  static constexpr size_t ONE_KB = 1024;
  static constexpr size_t ONE_MB = ONE_KB * 1024;
  static constexpr size_t ONE_GB = ONE_MB * 1024;

  static constexpr size_t GetSectorSizeLg()        { return sector_size_lg_; }
  static constexpr size_t GetSectorSize()          { return sector_size_; }
  static constexpr size_t GetNodeBits()            { return node_bits_; }
  static constexpr size_t GetNodeMask()            { return node_mask_; }
  static constexpr size_t GetNumChallenges()       { return num_challenges_; }
  static constexpr size_t GetNumPartitions()       { return num_partitions_; }
  static constexpr size_t GetNumLayers()           { return num_layers_; }
  static constexpr size_t GetNumNodes()            { return num_leaves_; }
  static constexpr size_t GetNumLeaves()           { return num_leaves_; }
  static constexpr size_t GetNumTreeDArity()       { return tree_d_arity_; }
  static constexpr size_t GetNumTreeDLevels()      { return tree_d_levels_; }
  static constexpr size_t GetNumTreeRLabels()      { return tree_r_labels_; }
  static constexpr size_t GetChallengeStartMask()  { return ~(tree_r_labels_ - 1); }
  static constexpr size_t GetNumTreeRCFiles()      { return tree_rc_files_; }
  static constexpr size_t GetNumTreeRCLgArity()    { return tree_rc_lg_base_arity_; }
  static constexpr size_t GetNumTreeRCArity()      { return tree_rc_base_arity_; }
  static constexpr size_t GetNumTreeRCArityDT()    { return tree_rc_base_arity_ + 1; }
  static constexpr size_t GetNumTreeRCConfig()     { return tree_rc_config_; }
  static constexpr size_t GetNumTreeRDiscardRows() { return tree_r_discard_rows_; }
  static constexpr size_t GetNumTreeRCLevels()     {
    // FIXME - this won't work for non-uniform trees
    size_t tree_rc_levels = log2(num_leaves_) / log2(tree_rc_base_arity_);
    assert(tree_rc_top_arity_ == 0);
    assert(tree_rc_sub_arity_ != 2);
    
    return tree_rc_levels;
  }

 private:
  static const size_t sector_size_lg_        = 63 - __builtin_clzll((uint64_t)sector_size_);
  static const size_t node_bits_             = sector_size_lg_ - NODE_SIZE_LG;
  static const size_t node_mask_             = (1UL << node_bits_) - 1;
  static const size_t num_challenges_        = sector_size_ <= ONE_GB ? 2 : 180;
  static const size_t num_partitions_        = sector_size_ <= ONE_GB ? 1 : 10;
  static const size_t num_layers_            = sector_size_ <= ONE_GB ? 2 : 11;
  static const size_t num_leaves_            = sector_size_ / sizeof(node_t);
  static const size_t tree_d_arity_          = 2;
  static const size_t tree_d_levels_         = log2(num_leaves_);
  static const size_t tree_rc_config_        = ((sector_size_== 2 * ONE_KB || sector_size_== 8 * ONE_MB || sector_size_== 512 * ONE_MB) ? 0 :
                                                (sector_size_== 32 * ONE_KB || sector_size_== 64 * ONE_GB) ? 2 :
                                                1);
  static const size_t tree_rc_lg_base_arity_ = 3;
  static const size_t tree_rc_base_arity_    = 1 << tree_rc_lg_base_arity_;
  static const size_t tree_rc_sub_arity_     = ((sector_size_== 2 * ONE_KB || sector_size_== 8 * ONE_MB || sector_size_== 512 * ONE_MB) ? 0 :
                                                (sector_size_== 4 * ONE_KB || sector_size_== 16 * ONE_MB || sector_size_== 1 * ONE_GB) ? 2 :
                                                8);
  static const size_t tree_rc_top_arity_     = ((sector_size_== 32 * ONE_KB || sector_size_== 64 * ONE_GB) ? 2 : 0);
  static const size_t tree_r_discard_rows_   = sector_size_ <= (32 * ONE_KB) ? 1 : 2;
  static const size_t tree_rc_files_         = ((tree_rc_top_arity_ == 0) && (tree_rc_sub_arity_ == 0) ? 1 :
                                                tree_rc_top_arity_ > 0 ? tree_rc_top_arity_ * tree_rc_sub_arity_ :
                                                tree_rc_sub_arity_);
  static const size_t tree_r_labels_         = pow(tree_rc_base_arity_, tree_r_discard_rows_ + 1);
};

#ifdef RUNTIME_SECTOR_SIZE

template class SectorParameters<1UL << Sector2KB>;
template class SectorParameters<1UL << Sector4KB>;
template class SectorParameters<1UL << Sector16KB>;
template class SectorParameters<1UL << Sector32KB>;

template class SectorParameters<1UL << Sector8MB>;
template class SectorParameters<1UL << Sector16MB>;

template class SectorParameters<1UL << Sector1GB>;
template class SectorParameters<1UL << Sector64GB>;

#endif

template class SectorParameters<1UL << Sector512MB>;
template class SectorParameters<1UL << Sector32GB>;

#ifdef RUNTIME_SECTOR_SIZE

typedef SectorParameters<1UL << Sector2KB>   sector_parameters2KB;
typedef SectorParameters<1UL << Sector4KB>   sector_parameters4KB;
typedef SectorParameters<1UL << Sector16KB>  sector_parameters16KB;
typedef SectorParameters<1UL << Sector32KB>  sector_parameters32KB;

typedef SectorParameters<1UL << Sector8MB>   sector_parameters8MB;
typedef SectorParameters<1UL << Sector16MB>  sector_parameters16MB;

typedef SectorParameters<1UL << Sector1GB>   sector_parameters1GB;
typedef SectorParameters<1UL << Sector64GB>  sector_parameters64GB;

#endif

typedef SectorParameters<1UL << Sector512MB> sector_parameters512MB;
typedef SectorParameters<1UL << Sector32GB>  sector_parameters32GB;

#endif // __SECTOR_PARAMETERS_HPP__
