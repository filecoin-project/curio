// Copyright Supranational LLC

#ifndef __TREE_PROOF_HPP__
#define __TREE_PROOF_HPP__

#include "tree_d_cc_nodes.h"

class TreeProof {
 public:
  TreeProof(size_t arity, size_t levels,
            node_t** tree_bufs, size_t num_tree_bufs,
            size_t discard_rows);
  virtual ~TreeProof();

  void   SetRoot(node_t* root) { root_ = root; }
  void   SetLeaf(node_t* leaf) { leaf_ = leaf; }
  size_t WriteProof(uint8_t* file_ptr, size_t buf_index, uint32_t proof_type);
  static size_t ProofSize(size_t arity, size_t levels, uint32_t proof_type);

  virtual void GenInclusionPath(uint64_t challenge,
                                node_t* first_level = nullptr);
 protected:
  bool PerformFirstLevels(uint64_t challenge, node_t* first_level,
                          size_t* indices);

  // The index array will be filled with which side of the input the
  //  challenge node to prove is located on all the way up the tree.
  void GetTreePaths(size_t* indices, uint64_t challenge);

  size_t      arity_;
  size_t      levels_;
  node_t**    tree_bufs_;
  size_t      tree_bufs_len_;
  size_t      discard_rows_;
  node_t*     root_;
  node_t*     leaf_;
  node_t*     path_buf_; // Used for rebuilding trees if needed
  std::vector<PathElement*> path_; // levels number of PathElements
};

TreeProof::TreeProof(size_t arity, size_t levels,
                     node_t** tree_bufs, size_t tree_bufs_len = 1,
                     size_t discard_rows = 0) :
  arity_(arity),
  levels_(levels),
  tree_bufs_(tree_bufs),
  tree_bufs_len_(tree_bufs_len),
  discard_rows_(discard_rows)
{
  path_.reserve(levels);

  if (discard_rows > 0) {
    path_buf_ = new node_t[discard_rows * (arity - 1)];
  } else {
    path_buf_ = nullptr;
  }
}

TreeProof::~TreeProof() {
  if (path_buf_ != nullptr) {
    delete path_buf_;
  }

  for (size_t l = 0; l < levels_; ++l) {
    delete path_[l];
  }
}

size_t TreeProof::ProofSize(size_t arity, size_t levels, uint32_t proof_type) {
  size_t proof_size = 4;  // proof type u32
  proof_size += sizeof(node_t);       // root
  proof_size += sizeof(node_t);       // leaf

  proof_size += 8;        // base size u64 
  proof_size += (((sizeof(node_t) * (arity - 1)) + 8 + 8) * levels);  // path

  if (proof_type == 1) {
    proof_size += 8;    // sub size u64 
  }

  return proof_size;
}

size_t TreeProof::WriteProof(uint8_t* file_ptr, size_t buf_index,
                             uint32_t proof_type) {
  std::memcpy(file_ptr + buf_index, &proof_type, sizeof(uint32_t));
  buf_index += sizeof(uint32_t);

  if (proof_type == 0) {
    // Root
    std::memcpy(file_ptr + buf_index, root_, sizeof(node_t));
    buf_index += sizeof(node_t);

    // Leaf
    std::memcpy(file_ptr + buf_index, leaf_, sizeof(node_t));
    buf_index += sizeof(node_t);

    // Proof size
    std::memcpy(file_ptr + buf_index, &levels_, sizeof(uint64_t));
    buf_index += sizeof(uint64_t);

    // Proofs
    for (size_t i = 0; i < levels_; ++i) {
      buf_index = path_[i]->Write(file_ptr, buf_index);
    }
  } else if (proof_type == 1) {
    // Only supports specific tree of single level sub (e.g. 32G case)

    // Base proof size
    uint64_t base_proof_vec_len = levels_ - 1;
    std::memcpy(file_ptr + buf_index, &base_proof_vec_len, sizeof(uint64_t));
    buf_index += sizeof(uint64_t);

    // Base proofs
    for (size_t i = 0; i < base_proof_vec_len; ++i) {
      buf_index = path_[i]->Write(file_ptr, buf_index);
    }

    // Sub proof size
    uint64_t sub_proof_vec_len = 1;
    std::memcpy(file_ptr + buf_index, &sub_proof_vec_len, sizeof(uint64_t));
    buf_index += sizeof(uint64_t);

    // Sub proof
    buf_index = path_[base_proof_vec_len]->Write(file_ptr, buf_index);

    // Root
    std::memcpy(file_ptr + buf_index, root_, sizeof(node_t));
    buf_index += sizeof(node_t);

    // Leaf
    std::memcpy(file_ptr + buf_index, leaf_, sizeof(node_t));
    buf_index += sizeof(node_t);
  }

  return buf_index;
}

/*
  Rebuilding discarded tree r rows
  Gather enough nodes around the challenge to build subtree
  First level inclusion path is nodes
  Second level inclusion path requires hashing the 7 adjacent nodes
  Third level inclusion path requires hashing two levels to get 7 adjacent
  Fourth level and above are in the tree r files

                                       O
                                  ____/|\____
                                 /    ...    \
                                O             O
           ____________________/|\__       __/|\_____________________
          /                 |                      |                 \
         O                  O                      O                  O
  / / / / \ \ \ \    / / / / \ \ \ \  ...   / / / / \ \ \ \    / / / / \ \ \ \
 O O O O   O O O O  O O O O   O O O O      O O O O   O O O O  O O O O   O O O O
 0 1 2 3   4 5 6 7  8 9 A B   C D E F ... 1F0            1F7 1F8             1FF
*/
bool TreeProof::PerformFirstLevels(uint64_t challenge,
                                   node_t* first_level,
                                   size_t* indices) {
  const size_t arity_mask = ~(arity_ - 1);
  const size_t labels     = pow(arity_, discard_rows_ + 1);
  const size_t index_mask = labels - 1;
  const size_t sec_mask   = ~((arity_ * arity_) - 1);

  size_t leaf_start     = (challenge & arity_mask) & index_mask;
  size_t leaf_idx       = indices[0];
  size_t hash_idx       = 0;

  // Set leaf from first level
  SetLeaf((node_t*)(first_level + leaf_start + leaf_idx));

  // First level labels are separate from tree buffer files
  path_.push_back(new PathElement(arity_, (uint64_t) indices[0]));
  for (size_t a = 0; a < arity_; ++a) {
    if (a != leaf_idx) {
      path_[0]->SetHash(hash_idx++, (node_t*)(first_level + leaf_start + a));
    }
  }

  // Second level needs to hash adjacent labels
  leaf_idx       = indices[1];
  path_.push_back(new PathElement(arity_, (uint64_t) indices[1]));

  Poseidon p(arity_);

  hash_idx       = 0;
  leaf_start    &= sec_mask;
  for (size_t a = 0; a < arity_; ++a) {
    if (a != leaf_idx) {
      p.Hash((uint8_t*)&(path_buf_[hash_idx]),
             (uint8_t*)&(first_level[leaf_start + (a * arity_)]));
      path_[1]->SetHash(hash_idx, &(path_buf_[hash_idx]));
      hash_idx++;
    }
  }

  if (levels_ == 2) { // 2K case
    return true;
  }

  // Third level needs to hash adjacent labels for two levels
  uint8_t p_hash_buf[arity_][sizeof(node_t)];
  path_.push_back(new PathElement(arity_, (uint64_t) indices[2]));
  hash_idx       = 0;
  leaf_start   >>= (size_t) log2(arity_ * arity_);
  for (size_t a_o = 0; a_o < arity_; ++a_o) {
    // leaf_start is the node to skip
    if (a_o != leaf_start) {
      for (size_t a_i = 0; a_i < arity_; ++a_i) {
        p.Hash(p_hash_buf[a_i], (uint8_t*)&(first_level[(a_o * arity_ * arity_)+
                                                        (a_i * arity_)]));
      }
      p.Hash((uint8_t*)&(path_buf_[hash_idx + arity_ - 1]), p_hash_buf[0]);
      path_[2]->SetHash(hash_idx, &(path_buf_[hash_idx + arity_ - 1]));
      hash_idx++;
    }
  }

  if (levels_ == 3) {
    return true;
  }

  return false;
}

void TreeProof::GenInclusionPath(uint64_t challenge,
                                 node_t* first_level) {
  // Get the challenge index for each level of the tree
  size_t indices[levels_];
  GetTreePaths(indices, challenge);

  size_t starting_level = 0;

  if (first_level != nullptr) {
    bool done = PerformFirstLevels(challenge, first_level, indices);
    if (done) return;
    starting_level = 3;
  }

  size_t finish_level   = levels_ - 1;
  if (tree_bufs_len_ == 1) {
    finish_level   = levels_;
  }

  const size_t arity_mask = ~(arity_ - 1);
  const size_t arity_lg = (size_t) log2(arity_);
  const size_t leaves = pow(2, levels_ * arity_lg);
  const size_t file_leaves = (size_t) (leaves / tree_bufs_len_);
  const size_t file_shift = (size_t) log2(file_leaves);
  const size_t tree_idx_mask = file_leaves - 1;
  size_t start_level_size  = file_leaves;

  if (first_level != nullptr) {
    size_t act_file_leaves = pow(2, (levels_ - (discard_rows_ + 1)) * arity_lg);
    start_level_size  = (size_t) (act_file_leaves / tree_bufs_len_);
  }

  const size_t buf_idx = challenge >> file_shift;
  size_t cur_level_size = start_level_size;
  size_t add_level_size = 0;
  size_t leaf_idx;
  size_t hash_idx;
  size_t leaf_start;

  for (size_t l = starting_level; l < finish_level; ++l) {
    leaf_idx         = indices[l];
    leaf_start       = challenge & tree_idx_mask;
    leaf_start     >>= (l * arity_lg);
    leaf_start      &= arity_mask;
    leaf_start      += add_level_size;
    add_level_size  += cur_level_size;
    cur_level_size >>= arity_lg;

    if (l == 0) {
      SetLeaf((node_t*)(tree_bufs_[buf_idx] + leaf_start + leaf_idx));
    }

    path_.push_back(new PathElement(arity_, (uint64_t)leaf_idx));
    hash_idx       = 0;
    for (size_t a = 0; a < arity_; ++a) {
      if (a != leaf_idx) {
        path_[l]->SetHash(hash_idx++,
                          (node_t*)(tree_bufs_[buf_idx] + leaf_start + a));
      }
    }
  }

  if (tree_bufs_len_ == 1) {
    return;
  }

  leaf_idx         = indices[levels_ - 1];
  path_.push_back(new PathElement(arity_, (uint64_t)leaf_idx));
  hash_idx       = 0;
  for (size_t a = 0; a < arity_; ++a) {
    if (a != leaf_idx) {
      path_[levels_ - 1]->SetHash(hash_idx++,
                                  (node_t*)(tree_bufs_[a] + add_level_size));
    }
  }
}

void TreeProof::GetTreePaths(size_t* indices, uint64_t challenge) {
  size_t arity_lg   = log2(arity_);
  size_t arity_mask = arity_ - 1;

  for (size_t i = 0; i < levels_; ++i) {
    indices[i] = challenge & arity_mask;
    challenge >>= arity_lg;
  }
}

class TreeDCCProof : public TreeProof {
 public:
  TreeDCCProof(size_t arity, size_t levels,
               node_t** tree_bufs, size_t num_tree_bufs,
               size_t discard_rows) :
    TreeProof(arity, levels, tree_bufs, num_tree_bufs, discard_rows) {
      // TODO: for 64GB would need to access the next layer. CC_TREE_D_NODE_VALUES
      // would need to be filled in.
      assert (levels <= 31);
    
      SetRoot((node_t*)(CC_TREE_D_NODE_VALUES[levels]));
      SetLeaf((node_t*)(CC_TREE_D_NODE_VALUES[0]));
    }

  void GenInclusionPath(size_t challenge, node_t* first_level);
};

void TreeDCCProof::GenInclusionPath(uint64_t challenge,
                                    node_t* first_level) {
  size_t comm_d_indices[levels_];
  GetTreePaths(comm_d_indices, challenge);

  for (size_t l = 0; l < levels_; ++l) {
    path_.push_back(new PathElement(arity_, (uint64_t) comm_d_indices[l]));
    path_[l]->SetHash(0, (node_t*)(first_level + l));
  }
}

#endif // __TREE_PROOF_HPP__
