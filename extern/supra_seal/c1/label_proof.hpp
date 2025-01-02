// Copyright Supranational LLC
    
#ifndef __LABEL_PROOF_HPP__
#define __LABEL_PROOF_HPP__

#include "../sealing/data_structures.hpp"

class LabelProof {
 public:
  LabelProof(uint64_t challenge, uint64_t layers,
             node_t* labels, size_t label_inc);
  ~LabelProof() { }

  size_t WriteProof(uint8_t* file_ptr, size_t buf_index, bool enc = false);
  static size_t ProofSize(size_t layers, bool enc);

 private:
  uint64_t   challenge_;
  uint64_t   layers_;
  node_t*  labels_;
  size_t     label_inc_;
};

LabelProof::LabelProof(uint64_t challenge, uint64_t layers,
                       node_t* labels, size_t label_inc) :
  challenge_(challenge),
  layers_(layers),
  labels_(labels),
  label_inc_(label_inc) { }

size_t LabelProof::ProofSize(size_t layers, bool enc) {
  size_t proof_size = 8;

  if ((enc == false) || (layers == 1)) {
    if (enc == false) 
      proof_size += (layers * 8);
    proof_size += sizeof(node_t) * LAYER_ONE_REPEAT_SEQ * PARENT_COUNT_BASE;
    proof_size += sizeof(node_t) * LAYER_ONE_FINAL_SEQ;
    proof_size += 4;
    proof_size += 8;
    layers--;
  }

  if ((enc == true) && (layers > 1)) {
    layers = 1;
  }

  proof_size += (layers * sizeof(node_t) * LAYER_N_REPEAT_SEQ *
                 PARENT_COUNT_BASE);
  proof_size += (layers * sizeof(node_t) * LAYER_N_REPEAT_SEQ *
                 PARENT_COUNT_EXP);
  proof_size += (layers * sizeof(node_t) * LAYER_N_FINAL_SEQ);
  proof_size += (layers * 4);
  proof_size += (layers * 8);

  return proof_size;
}

size_t LabelProof::WriteProof(uint8_t* file_ptr, size_t buf_index,
                              bool enc) {
  uint32_t l = 1;

  if (enc == true) { // Encoding, only last layer
    l = layers_;
  } else {
    // Write vector length of proofs
    std::memcpy(file_ptr + buf_index, &layers_, sizeof(uint64_t));
    buf_index += sizeof(uint64_t);
  }

  while (l <= layers_) {
    // Number of parents in label calculation
    std::memcpy(file_ptr + buf_index, &LABEL_PARENTS, sizeof(uint64_t));
    buf_index += sizeof(uint64_t);

    if (l == 1) {
      for (size_t k = 0; k < LAYER_ONE_REPEAT_SEQ; ++k) {
        for (size_t c = 0; c < PARENT_COUNT_BASE; ++c) {
          std::memcpy(file_ptr + buf_index,
            labels_ + c + 1 + ((l - 1) * label_inc_), sizeof(node_t));
          buf_index += sizeof(node_t);
        }
      }

      for (size_t c = 0; c < LAYER_ONE_FINAL_SEQ; ++c) {
        std::memcpy(file_ptr + buf_index,
                    labels_ + c + 1 + ((l - 1) * label_inc_), sizeof(node_t));
        buf_index += sizeof(node_t);
      }
    } else {
      for (size_t k = 0; k < LAYER_N_REPEAT_SEQ; ++k) {
        for (size_t c = 0; c < PARENT_COUNT_BASE; ++c) {
          std::memcpy(file_ptr + buf_index,
            labels_ + c + 1 + ((l - 1) * label_inc_), sizeof(node_t));
          buf_index += sizeof(node_t);
        }

        for (size_t c = 0; c < PARENT_COUNT_EXP; ++c) {
          std::memcpy(file_ptr + buf_index,
            labels_ + c + 1 + PARENT_COUNT_BASE + ((l - 2) * label_inc_),
            sizeof(node_t));
          buf_index += sizeof(node_t);
        }
      }

      for (size_t c = 0; c < LAYER_N_FINAL_SEQ; ++c) {
        if (c < PARENT_COUNT_BASE) {
          std::memcpy(file_ptr + buf_index,
            labels_ + c + 1 + ((l - 1) * label_inc_), sizeof(node_t));
        } else {
          std::memcpy(file_ptr + buf_index,
            labels_ + c + 1 + ((l - 2) * label_inc_),
            sizeof(node_t));
        }
        buf_index += sizeof(node_t);
      }
    }

    // Layer index
    std::memcpy(file_ptr + buf_index, &l, sizeof(uint32_t));
    buf_index += sizeof(uint32_t);

    // Node - challenge
    std::memcpy(file_ptr + buf_index, &challenge_, sizeof(uint64_t));
    buf_index += sizeof(uint64_t);

    l++;
  }

  return buf_index;
}

#endif // __LABEL_PROOF_HPP__
