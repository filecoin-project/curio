// Copyright Supranational LLC

// Poseidon for Filecoin
// Primary usage is in regenerating truncated portions of tree r

#ifndef __POSEIDON_HPP__
#define __POSEIDON_HPP__

#include <cstdint>
#include <blst_t.hpp>
#include <ff/bls12-381.hpp>

class Poseidon {
 public:
  static constexpr vec256 BLS12_381_r = {
      TO_LIMB_T(0xffffffff00000001), TO_LIMB_T(0x53bda402fffe5bfe),
      TO_LIMB_T(0x3339d80809a1d805), TO_LIMB_T(0x73eda753299d7d48)
  };
  static constexpr vec256 BLS12_381_rRR = {
      TO_LIMB_T(0xc999e990f3f29c6d), TO_LIMB_T(0x2b6cedcb87925c23),
      TO_LIMB_T(0x05d314967254398f), TO_LIMB_T(0x0748d9d99f59ff11)
  };
  static constexpr vec256 BLS12_381_rONE = {
      TO_LIMB_T(0x00000001fffffffe), TO_LIMB_T(0x5884b7fa00034802),
      TO_LIMB_T(0x998c4fefecbc4ff5), TO_LIMB_T(0x1824b159acc5056f)
  };
  // This conflicts with sppark fr_t
  // typedef blst_256_t<BLS12_381_r, 0xfffffffeffffffff,
  //                    BLS12_381_rRR, BLS12_381_rONE> fr_t;

  Poseidon(const int arity);
  ~Poseidon();
  void Hash(uint8_t* out, const uint8_t* in);

 protected:
  void QuinticSBox(fr_t& element, const fr_t& round_constant);
  void MatrixMul(fr_t* elements, const fr_t* matrix);
  void SparseMatrixMul(fr_t* elements, const fr_t* sparse_matrix);
  void RoundMatrixMul(fr_t* elements, const int current_round);
  void FullRound(fr_t* elements, int& rk_offset, int& current_round);
  void LastFullRound(fr_t* elements, const fr_t* mds_matrix);
  void PartialRound(fr_t* elements, int& rk_offset, int& current_round);

  void AssignPointers(fr_t* constants_file,
                      fr_t** round_constants, fr_t** mds_matrix,
                      fr_t** pre_sparse_matrix, fr_t** sparse_matrices);

  int arity_;
  int partial_rounds_;
  int half_full_rounds_;
  int t_;

  fr_t  domain_tag_;

  fr_t*  constants_file_;
  size_t constants_size_;
  fr_t*  round_constants_;
  fr_t*  mds_matrix_;
  fr_t*  pre_sparse_matrix_;
  fr_t*  sparse_matrices_;
};
#endif // __POSEIDON_HPP__
