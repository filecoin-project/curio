// Copyright Supranational LLC

#ifndef __C1CHALLENGE_HPP__
#define __C1CHALLENGE_HPP__

template<class C>
class C1Challenge {
 public:
  C1Challenge(uint64_t challenge, 
              node_t* tree_r_root_, node_t* tree_c_root_, node_t* tree_d_root);
  ~C1Challenge();

  void GetParents(const uint32_t* parents_buf);
  void GetNodes(streaming_node_reader_t<C>& reader, size_t sector_slot);
  void GetTreeRNodes(node_t* replica_buf);
  size_t WriteTreeProof(uint8_t* file_ptr, size_t buf_index,
                        node_t** tree_r_bufs, node_t* tree_d_buf);
  size_t WriteNodeProof(uint8_t* file_ptr, size_t buf_index,
                        node_t** tree_c_bufs);
  size_t WriteProof(uint8_t* file_ptr, size_t buf_index,
                    node_t** tree_r_bufs, node_t** tree_c_bufs,
                    node_t* tree_d_buf);

 private:
  uint64_t          challenge_;
  uint32_t          drg_parents_[PARENT_COUNT_BASE];
  uint32_t          exp_parents_[PARENT_COUNT_EXP];
  node_t*           nodes_;        // This node and its parents for each layer
  node_t*           tree_r_nodes_; // Replica nodes to rebuild discarded rows
  node_t*           tree_r_root_;
  node_t*           tree_c_root_;
  node_t*           tree_d_root_;
};

template<class C>
C1Challenge<C>::C1Challenge(uint64_t challenge,
                            node_t* tree_r_root, node_t* tree_c_root,
                            node_t* tree_d_root) :
  challenge_(challenge),
  tree_r_root_(tree_r_root),
  tree_c_root_(tree_c_root),
  tree_d_root_(tree_d_root) {

  nodes_        = new node_t[C::GetNumLayers() * (PARENT_COUNT + 1)];
  tree_r_nodes_ = new node_t[C::GetNumTreeRLabels()];
}

template<class C>
C1Challenge<C>::~C1Challenge() {
  if (nodes_        != nullptr) delete[] nodes_;
  if (tree_r_nodes_ != nullptr) delete[] tree_r_nodes_;
}

template<class C>
void C1Challenge<C>::GetParents(const uint32_t* parents_buf) {
  size_t p_idx = challenge_ * PARENT_COUNT;
  for (size_t k = 0; k < PARENT_COUNT_BASE; ++k) {
    drg_parents_[k] = parents_buf[p_idx];
    p_idx++;
  }
  for (size_t k = 0; k < PARENT_COUNT_EXP; ++k) {
    exp_parents_[k] = parents_buf[p_idx];
    p_idx++;
  }
}

template<class C>
void C1Challenge<C>::GetNodes(streaming_node_reader_t<C>& reader,
                              size_t sector_slot) {
  std::vector<std::pair<size_t, size_t>> nodes;

  size_t layer_count = C::GetNumLayers();
  for (size_t l = 0; l < layer_count; ++l) {
    nodes.push_back(std::pair(l, challenge_));

    // Get all base parents
    for (size_t k = 0; k < PARENT_COUNT_BASE; ++k) {
      nodes.push_back(std::pair(l, drg_parents_[k]));
    }

    // Get all exp parents
    for (size_t k = 0; k < PARENT_COUNT_EXP; ++k) {
      nodes.push_back(std::pair(l, exp_parents_[k]));
    }
  }
  reader.alloc_slots(1, nodes.size(), false);
  reader.load_nodes(0, nodes);
  for (size_t i = 0; i < nodes.size(); i++) {
    node_t n = reader.get_node(0, nodes, i, sector_slot);
    nodes_[i] = n;
  }
  reader.free_slots();
}

template<class C>
void C1Challenge<C>::GetTreeRNodes(node_t* replica_buf) {
  size_t tree_r_label_idx  = challenge_ & C::GetChallengeStartMask();
  for (size_t k = 0; k < C::GetNumTreeRLabels(); ++k) {
    std::memcpy(tree_r_nodes_ + k, &(replica_buf[tree_r_label_idx]),
                sizeof(node_t));
    tree_r_label_idx++;
  }
}

template<class C>
size_t C1Challenge<C>::WriteTreeProof(uint8_t* file_ptr, size_t buf_index,
                                      node_t** tree_r_bufs, node_t* tree_d_buf) {
  ///////////////////////////////////////
  // Build Tree D inclusion proof
  ///////////////////////////////////////
  if (tree_d_buf == nullptr) {
    TreeDCCProof tree_d(C::GetNumTreeDArity(),
                        C::GetNumTreeDLevels(), nullptr, 0, 0);
    tree_d.GenInclusionPath(challenge_, (node_t*) CC_TREE_D_NODE_VALUES);
    buf_index = tree_d.WriteProof(file_ptr, buf_index, SINGLE_PROOF_DATA);
  } else {
    TreeProof tree_d(C::GetNumTreeDArity(),
                     C::GetNumTreeDLevels(), &tree_d_buf, 1, 0);
    tree_d.SetRoot(tree_d_root_);
    tree_d.GenInclusionPath(challenge_, nullptr);
    buf_index = tree_d.WriteProof(file_ptr, buf_index, SINGLE_PROOF_DATA);
  }

  ///////////////////////////////////////
  // Build Tree R inclusion proof
  ///////////////////////////////////////
  TreeProof tree_r(C::GetNumTreeRCArity(),
                   C::GetNumTreeRCLevels(), tree_r_bufs,
                   C::GetNumTreeRCFiles(),
                   C::GetNumTreeRDiscardRows());
  tree_r.SetRoot(tree_r_root_);
  tree_r.GenInclusionPath(challenge_, tree_r_nodes_);
  buf_index = tree_r.WriteProof(file_ptr, buf_index,
                                C::GetNumTreeRCConfig());

  return buf_index;
}

template<class C>
size_t C1Challenge<C>::WriteNodeProof(uint8_t* file_ptr, size_t buf_index,
                                      node_t** tree_c_bufs) {
  ///////////////////////////////////////
  // Column proofs
  ///////////////////////////////////////
  ColumnProof c_x = ColumnProof<C>(challenge_,
                                   nodes_, 0, (PARENT_COUNT + 1),
                                   tree_c_bufs, tree_c_root_);
  buf_index = c_x.WriteProof(file_ptr, buf_index,
                             C::GetNumTreeRCConfig());

  ///////////////////////////////////////
  // DRG Parents
  ///////////////////////////////////////
  std::memcpy(file_ptr + buf_index, &PARENT_COUNT_BASE, sizeof(uint64_t));
  buf_index += sizeof(uint64_t);

  for (size_t k = 0; k < PARENT_COUNT_BASE; ++k) {
    ColumnProof drg = ColumnProof<C>(drg_parents_[k],
                                     nodes_, k + 1, (PARENT_COUNT + 1),
                                     tree_c_bufs, tree_c_root_);
    buf_index = drg.WriteProof(file_ptr, buf_index,
                               C::GetNumTreeRCConfig());
  }

  ///////////////////////////////////////
  // Expander Parents
  ///////////////////////////////////////
  std::memcpy(file_ptr + buf_index, &PARENT_COUNT_EXP, sizeof(uint64_t));
  buf_index += sizeof(uint64_t);

  for (size_t k = 0; k < PARENT_COUNT_EXP; ++k) {
    ColumnProof exp = ColumnProof<C>(exp_parents_[k], nodes_,
                                     k + 1 + PARENT_COUNT_BASE,
                                     (PARENT_COUNT + 1),
                                     tree_c_bufs, tree_c_root_);
    buf_index = exp.WriteProof(file_ptr, buf_index,
                               C::GetNumTreeRCConfig());
  }

  ///////////////////////////////////////
  // Labeling Proofs
  ///////////////////////////////////////
  size_t layer_count = C::GetNumLayers();
  LabelProof label_proof(challenge_, layer_count, nodes_, (PARENT_COUNT + 1));
  buf_index = label_proof.WriteProof(file_ptr, buf_index);

  ///////////////////////////////////////
  // Encoding Proof
  ///////////////////////////////////////
  LabelProof enc_proof(challenge_, layer_count, nodes_, (PARENT_COUNT + 1));
  buf_index = enc_proof.WriteProof(file_ptr, buf_index, true);

  return buf_index;
}

template<class C>
size_t C1Challenge<C>::WriteProof(uint8_t* file_ptr, size_t buf_index,
                                  node_t** tree_r_bufs, node_t** tree_c_bufs,
                                  node_t* tree_d_buf) {
  buf_index = WriteTreeProof(file_ptr, buf_index, tree_r_bufs, tree_d_buf);
  buf_index = WriteNodeProof(file_ptr, buf_index, tree_c_bufs);

  return buf_index;
}
#endif // __C1CHALLENGE_HPP__
