// Copyright Supranational LLC

#ifndef __COLUMN_PROOF_HPP__
#define __COLUMN_PROOF_HPP__

template<class P>
class ColumnProof {
 public:
  ColumnProof(uint64_t challenge,
              node_t* labels, size_t label_idx, size_t label_inc,
              node_t** tree_bufs, node_t* root);
  ~ColumnProof();

  size_t WriteProof(uint8_t* file_ptr, size_t buf_index, uint32_t proof_type);
  static size_t ProofSize();

 private:
  uint64_t          challenge_;
  uint64_t          layers_;
  node_t*           labels_;
  size_t            label_idx_;
  size_t            label_inc_;
  TreeProof*        tree_;
};

template<class P>
ColumnProof<P>::ColumnProof(uint64_t challenge,
                            node_t* labels, size_t label_idx, size_t label_inc,
                            node_t** tree_bufs, node_t* root) :
  challenge_(challenge),
  layers_(P::GetNumLayers()),
  labels_(labels),
  label_idx_(label_idx),
  label_inc_(label_inc)
{
  tree_ = new TreeProof(P::GetNumTreeRCArity(),
                        P::GetNumTreeRCLevels(),
                        tree_bufs, P::GetNumTreeRCFiles());
  tree_->SetRoot(root);
  tree_->GenInclusionPath(challenge, nullptr);
}

template<class P>
ColumnProof<P>::~ColumnProof() {
  if (tree_ != nullptr) {
    delete tree_;
  }
}

template<class P>
size_t ColumnProof<P>::ProofSize() {
  size_t proof_size = 4;
  proof_size += 8;
  proof_size += (sizeof(node_t) * P::GetNumLayers());
  proof_size += TreeProof::ProofSize(P::GetNumTreeRCArity(),
                                     P::GetNumTreeRCLevels(),
                                     P::GetNumTreeRCConfig());
  return proof_size;
}

template<class P>
size_t ColumnProof<P>::WriteProof(uint8_t* file_ptr, size_t buf_index,
                                  uint32_t proof_type) {
  std::memcpy(file_ptr + buf_index, (uint32_t*)&challenge_, sizeof(uint32_t));
  buf_index += sizeof(uint32_t);

  std::memcpy(file_ptr + buf_index, &layers_, sizeof(uint64_t));
  buf_index += sizeof(uint64_t);

  for (uint64_t l = 0; l < layers_; ++l) {
    std::memcpy(file_ptr + buf_index, labels_ + label_idx_ + (l * label_inc_),
                sizeof(node_t));
    buf_index += sizeof(node_t);
  }

  buf_index = tree_->WriteProof(file_ptr, buf_index, proof_type);

  return buf_index;
}

#endif // __COLUMN_PROOF_HPP__
