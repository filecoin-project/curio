// Copyright Supranational LLC

// Filecoin Sealing Commit 1 (C1) operation

#include <cstdint>             // uint*
#include <cstring>             // memcpy
#include <fstream>             // file read
#include <fcntl.h>             // file open
#include <unistd.h>            // file close
#include <sys/mman.h>          // mapping
#include <sys/stat.h>          // file stats
#include <iostream>            // printing
#include <iomanip>             // printing
#include <cassert>             // assertions
#include <cmath>               // log2
#include <gmp.h>               // gmp for challenge modulo operation
#include <vector>              //

#include <ff/bls12-381.hpp>
#include "tree_d_cc_nodes.h"
#include "../poseidon/poseidon.hpp"
#include "../sha/sha_functions.hpp"
#include "../util/mmap_t.hpp"

#include "path_element.hpp"
#include "tree_proof.hpp"
#include "column_proof.hpp"
#include "label_proof.hpp"
#include "challenge.hpp"

template<class C>
class C1 {
 public:
  C1(streaming_node_reader_t<C>& reader, size_t sector_slot);
  ~C1();

  void SetReplicaID(const node_t* replica_id) { replica_id_ = replica_id; }
  void SetTicket(const node_t* ticket) { ticket_ = ticket; }

  void DeriveChallenges(const uint8_t* seed);

  void SetTreeRBufs(const char* tree_r_cache, const char* file_prefix,
                    bool include_slot = false) {
    size_t num_files = C::GetNumTreeRCFiles();
    tree_r_bufs_.resize(num_files);
    SetTreeBufs(&tree_r_bufs_[0], tree_r_cache,
                file_prefix, num_files, include_slot);
  }

  void SetTreeCBufs(const char* tree_c_cache, const char* file_prefix,
                    bool include_slot = false) {
    size_t num_files = C::GetNumTreeRCFiles();
    tree_c_bufs_.resize(num_files);
    SetTreeBufs(&tree_c_bufs_[0], tree_c_cache,
                file_prefix, num_files, include_slot);
  }

  void SetTreeDBuf(const char* tree_d_cache, const char* file_prefix,
                   bool include_slot = false) {
    SetTreeBufs(&tree_d_buf_, tree_d_cache,
                file_prefix, 1, include_slot);
    if (tree_d_buf_.is_open() == 0) {
      printf("No tree d file, assuming CC sector\n");
      // TODO: for 64GB would need to access the next layer. CC_TREE_D_NODE_VALUES
      // would need to be filled in.
      assert (C::GetNumTreeDLevels() <= 31);
      comm_d_ = (node_t*) CC_TREE_D_NODE_VALUES[C::GetNumTreeDLevels()];
    } else {
      uint8_t* comm_d_addr = (uint8_t*)&tree_d_buf_[0] +
                              (tree_d_buf_.get_size() - sizeof(node_t));
      comm_d_ = (node_t*)comm_d_addr;
    }
  }

  void GetRoots(const char* cache);
  void SetParentsBuf(const char* filename);
  void SetReplicaBuf(const char* cache);

  void WriteProofs(const char* filename, bool do_tree, bool do_node);

  size_t ProofSize(bool do_tree, bool do_node);

  void CombineProofs(const char* filename,
                     const char* tree_filename,
                     const char* node_filename);
 private:
  void WriteTreeDProof(uint64_t challenge);
  void SetTreeBufs(mmap_t<node_t>* bufs, const char* cache,
                   const char* prefix, size_t num_files, bool include_slot);

  streaming_node_reader_t<C>& reader_;
  size_t                      sector_slot_;
  const node_t*               replica_id_;
  uint64_t*                   challenges_;
  size_t                      challenges_count_;
  mmap_t<node_t>              replica_buf_;
  std::vector<mmap_t<node_t>> tree_r_bufs_;
  std::vector<mmap_t<node_t>> tree_c_bufs_;
  mmap_t<node_t>              tree_d_buf_;
  node_t                      tree_c_root_;
  node_t                      tree_r_root_;
  node_t                      comm_r_;
  node_t*                     comm_d_;
  const node_t*               seed_;
  const node_t*               ticket_;
  mmap_t<uint32_t>            parents_buf_;
};

template<class C>
C1<C>::C1(streaming_node_reader_t<C>& reader, size_t sector_slot) :
  reader_(reader), sector_slot_(sector_slot) {

  challenges_count_ = C::GetNumChallenges() / C::GetNumPartitions();

  challenges_  = nullptr;
}

template<class C>
C1<C>::~C1() {
  if (challenges_  != nullptr) delete challenges_;
}

// https://spec.filecoin.io/#section-algorithms.sdr.porep-challenges
template<class C>
void C1<C>::DeriveChallenges(const uint8_t* seed) {
  seed_   = (node_t*) seed;

  uint32_t hash[8] __attribute__ ((aligned (32)));
  size_t leaves = C::GetNumLeaves();
  challenges_   = new uint64_t[C::GetNumChallenges()];

  for (uint8_t k = 0; k < C::GetNumPartitions(); ++k) {
    uint8_t buf[128] __attribute__ ((aligned (32))) = {0};
    std::memcpy(buf, replica_id_, 32);
    std::memcpy(buf + 32, seed, 32);
    buf[68] = 0x80;  // padding
    // 544 bits -> 0x220
    buf[126] = 0x02; // padding length
    buf[127] = 0x20; // padding length

    mpz_t gmp_challenge;
    mpz_init(gmp_challenge);

    for (size_t i = 0; i < challenges_count_; ++i) {
      uint32_t j = (uint32_t)((challenges_count_ * k) + i);
      buf[64] = (uint8_t)(j & 0xFF);
      buf[65] = (uint8_t)((j >> 8) & 0xFF);

      std::memcpy(hash, SHA256_INITIAL_DIGEST, NODE_SIZE);
      blst_sha256_block(hash, buf, 2);
      blst_sha256_emit((uint8_t*)hash, hash);

      mpz_import(gmp_challenge, 8, -1, 4, 0, 0, hash);

      // Resulting challenge must be a leaf index and not the first leaf
      // Use gmp to perform modulo operation
      challenges_[i + (k * challenges_count_)] =
        mpz_mod_ui(gmp_challenge, gmp_challenge, leaves - 1) + 1;
    }
    mpz_clear(gmp_challenge);

  }
}

template<class C>
void C1<C>::SetParentsBuf(const char* filename) {
  assert (parents_buf_.mmap_read(filename) == 0);
}

template<class C>
void C1<C>::SetReplicaBuf(const char* cache) {
  const char* rep_template = "%s";;
  const size_t MAX = 256;
  char fname[MAX];
  snprintf(fname, MAX, rep_template, cache);
  assert (replica_buf_.mmap_read(fname) == 0);
}

template<class C>
void C1<C>::SetTreeBufs(mmap_t<node_t>* bufs, const char* cache,
                        const char* prefix, size_t num_files,
                        bool include_slot) {
  for (size_t l = 0; l < num_files; ++l) {
    const size_t MAX = 256;
    char fname[MAX];
    if (include_slot) {
      snprintf(fname, MAX, prefix, cache, sector_slot_, l);
    } else {
      if (num_files == 1) {
        snprintf(fname, MAX, prefix, cache);
      } else {
        snprintf(fname, MAX, prefix, cache, l);
      }
    }

    int tree_fd = open(fname, O_RDONLY);
    if (tree_fd == -1) {
      printf("Failed to open tree file %s\n", fname);
      break;
    }
    close(tree_fd);

    assert (bufs[l].mmap_read(fname) == 0);
  }
}

template<class C>
void C1<C>::GetRoots(const char* cache) {
  // Get tree_c_root and tree_r_last_root from p_aux file
  const char* p_aux_template = "%s/p_aux";
  const size_t MAX = 256;
  char fname[MAX];
  snprintf(fname, MAX, p_aux_template, cache);

  mmap_t<node_t> p_aux_buf;
  p_aux_buf.mmap_read(fname);

  std::memcpy(&tree_c_root_, &(p_aux_buf[0]), sizeof(node_t));
  std::memcpy(&tree_r_root_, &(p_aux_buf[1]), sizeof(node_t));

  // Calculate comm r
  Poseidon poseidon_comm_r(2);
  poseidon_comm_r.Hash((uint8_t*)&comm_r_, (uint8_t*)&p_aux_buf[0]);
}

template<class C>
size_t C1<C>::ProofSize(bool do_tree, bool do_node) {
  uint64_t num_partitions = C::GetNumPartitions();
  uint64_t num_challenges = C::GetNumChallenges();

  size_t tree_d_proof_size =  TreeProof::ProofSize(C::GetNumTreeDArity(),
    C::GetNumTreeDLevels(), SINGLE_PROOF_DATA);
  size_t tree_rc_proof_size = TreeProof::ProofSize(C::GetNumTreeRCArity(),
    C::GetNumTreeRCLevels(), C::GetNumTreeRCConfig());
  size_t tree_proof_size = tree_d_proof_size + tree_rc_proof_size;

  size_t label_proof_size =  LabelProof::ProofSize(C::GetNumLayers(),
                                                   false);
  size_t enc_proof_size =  LabelProof::ProofSize(C::GetNumLayers(),
                                                 true);
  size_t col_proof_size = ((1 + PARENT_COUNT_BASE + PARENT_COUNT_EXP) *
                           ColumnProof<C>::ProofSize()) +
                           (2 * sizeof(uint64_t));

  size_t node_proof_size = label_proof_size + enc_proof_size + col_proof_size;

  size_t proof_size = (num_partitions * sizeof(uint64_t)) + sizeof(uint64_t);

  if (do_tree == true) {
    proof_size += (tree_proof_size * num_challenges);
    proof_size += 2 * sizeof(node_t);
  }

  if (do_node == true) {
    proof_size += (node_proof_size * num_challenges);
    proof_size += 3 * sizeof(node_t);
  }

  return proof_size;
}

template<class C>
void C1<C>::WriteProofs(const char* filename, bool do_tree, bool do_node) {
  remove(filename);
  mmap_t<uint8_t> file_ptr;
  size_t expected_file_size = ProofSize(do_tree, do_node);
  file_ptr.mmap_write(filename, expected_file_size);

  size_t buf_index = 0;

  // Need to put together  pub vanilla_proofs: Vec<Vec<VanillaSealProof<Tree>>>,
  uint64_t vp_outer_length = C::GetNumPartitions();
  uint64_t vp_inner_length = challenges_count_;

  std::memcpy(&file_ptr[0] + buf_index, &vp_outer_length, sizeof(uint64_t));
  buf_index += sizeof(uint64_t);

  // Gather the output buffers into a contiguous array to keep challenge agnostic
  // about file IO
  size_t num_files = C::GetNumTreeRCFiles();
  std::vector<node_t*> tree_r(num_files);
  std::vector<node_t*> tree_c(num_files);
  for (size_t i = 0; i < num_files; i++) {
    tree_r[i] = &tree_r_bufs_[i][0];
    tree_c[i] = &tree_c_bufs_[i][0];
  }
  node_t* tree_d = tree_d_buf_.is_open() ? &tree_d_buf_[0] : nullptr;

  for (uint64_t i = 0; i < vp_outer_length; ++i) {
    std::memcpy(&file_ptr[0] + buf_index, &vp_inner_length, sizeof(uint64_t));
    buf_index += sizeof(uint64_t);

    for (uint64_t j = 0; j < vp_inner_length; ++j) {
      C1Challenge<C> challenge(challenges_[j + (i * challenges_count_)],
                                  &tree_r_root_, &tree_c_root_, comm_d_);


      if (do_node == true) {
        challenge.GetParents(&parents_buf_[0]);
        challenge.GetNodes(reader_, sector_slot_);
        if (do_tree == true) {
          challenge.GetTreeRNodes(replica_buf_);
          buf_index = challenge.WriteProof(&file_ptr[0], buf_index, &tree_r[0],
                                           &tree_c[0], tree_d);
        } else {
          buf_index = challenge.WriteNodeProof(&file_ptr[0], buf_index,
                                               &tree_c[0]);
        }
      } else {
        challenge.GetTreeRNodes(replica_buf_);
        buf_index = challenge.WriteTreeProof(&file_ptr[0], buf_index,
                                             &tree_r[0], tree_d);
      }
    }
  }

  if (do_tree == true) {
    // Comm R
    std::memcpy(&file_ptr[0] + buf_index, &comm_r_, sizeof(node_t));
    buf_index += sizeof(node_t);

    // Comm D
    std::memcpy(&file_ptr[0] + buf_index, comm_d_, sizeof(node_t));
    buf_index += sizeof(node_t);
  }

  if (do_node == true) {
    // Replica ID
    std::memcpy(&file_ptr[0] + buf_index, replica_id_, sizeof(node_t));
    buf_index += sizeof(node_t);

    // Seed
    std::memcpy(&file_ptr[0] + buf_index, seed_, sizeof(node_t));
    buf_index += sizeof(node_t);

    // Ticket
    std::memcpy(&file_ptr[0] + buf_index, ticket_, sizeof(node_t));
    buf_index += sizeof(node_t);
  }

  //printf("WriteProofs buf_index %ld\n", buf_index);
  assert(buf_index == expected_file_size);
}

template<class C>
void C1<C>::CombineProofs(const char* filename,
                             const char* tree_filename,
                             const char* node_filename) {
  remove(filename);
  mmap_t<uint8_t> file_ptr;
  size_t expected_file_size = ProofSize(true, true);
  file_ptr.mmap_write(filename, expected_file_size);

  mmap_t<uint8_t> tree_ptr;
  size_t exp_tree_buf_size = ProofSize(true, false);
  tree_ptr.mmap_read(tree_filename);

  mmap_t<uint8_t> node_ptr;
  node_ptr.mmap_read(node_filename);

  size_t buf_index = 0;
  size_t tree_buf_index = 0;
  size_t node_buf_index = 0;

  size_t tree_d_proof_size =  TreeProof::ProofSize(C::GetNumTreeDArity(),
    C::GetNumTreeDLevels(), SINGLE_PROOF_DATA);
  size_t tree_rc_proof_size = TreeProof::ProofSize(C::GetNumTreeRCArity(),
    C::GetNumTreeRCLevels(), C::GetNumTreeRCConfig());
  size_t tree_proof_size = tree_d_proof_size + tree_rc_proof_size;

  size_t label_proof_size =  LabelProof::ProofSize(C::GetNumLayers(),
                                                   false);
  size_t enc_proof_size =  LabelProof::ProofSize(C::GetNumLayers(),
                                                 true);
  size_t col_proof_size = ((1 + PARENT_COUNT_BASE + PARENT_COUNT_EXP) *
                           ColumnProof<C>::ProofSize()) +
                           (2 * sizeof(uint64_t));
  size_t node_proof_size = label_proof_size + enc_proof_size + col_proof_size;

  // Need to put together  pub vanilla_proofs: Vec<Vec<VanillaSealProof<Tree>>>,
  uint64_t vp_outer_length = C::GetNumPartitions();
  uint64_t vp_inner_length = challenges_count_;

  std::memcpy(&file_ptr[0] + buf_index, &vp_outer_length, sizeof(uint64_t));
  buf_index += sizeof(uint64_t);
  tree_buf_index += sizeof(uint64_t);
  node_buf_index += sizeof(uint64_t);

  for (uint64_t i = 0; i < vp_outer_length; ++i) {
    std::memcpy(&file_ptr[0] + buf_index, &vp_inner_length, sizeof(uint64_t));
    buf_index += sizeof(uint64_t);
    tree_buf_index += sizeof(uint64_t);
    node_buf_index += sizeof(uint64_t);

    for (uint64_t j = 0; j < vp_inner_length; ++j) {
      std::memcpy(&file_ptr[0] + buf_index, &tree_ptr[0] + tree_buf_index, tree_proof_size);
      buf_index += tree_proof_size;
      tree_buf_index += tree_proof_size;

      std::memcpy(&file_ptr[0] + buf_index, &node_ptr[0] + node_buf_index, node_proof_size);
      buf_index += node_proof_size;
      node_buf_index += node_proof_size;
    }
  }

  // Comm R
  std::memcpy(&file_ptr[0] + buf_index, &tree_ptr[0] + tree_buf_index, sizeof(node_t));
  buf_index += sizeof(node_t);
  tree_buf_index += sizeof(node_t);

  // Comm D
  std::memcpy(&file_ptr[0] + buf_index, &tree_ptr[0] + tree_buf_index, sizeof(node_t));
  buf_index += sizeof(node_t);
  tree_buf_index += sizeof(node_t);

  // Replica ID
  std::memcpy(&file_ptr[0] + buf_index, &node_ptr[0] + node_buf_index, sizeof(node_t));
  buf_index += sizeof(node_t);
  node_buf_index += sizeof(node_t);

  // Seed
  std::memcpy(&file_ptr[0] + buf_index, &node_ptr[0] + node_buf_index, sizeof(node_t));
  buf_index += sizeof(node_t);
  node_buf_index += sizeof(node_t);

  // Ticket
  std::memcpy(&file_ptr[0] + buf_index, &node_ptr[0] + node_buf_index, sizeof(node_t));
  buf_index += sizeof(node_t);
  node_buf_index += sizeof(node_t);

  assert(buf_index == expected_file_size);
  assert(tree_buf_index == exp_tree_buf_size);
  assert(node_buf_index == node_ptr.get_size());
}


template<class C>
int do_c1(streaming_node_reader_t<C>& reader,
          size_t num_sectors, size_t sector_slot,
          const uint8_t* replica_id, const uint8_t* seed,
          const uint8_t* ticket, const char* cache_path,
          const char* parents_filename, const char* replica_path,
          const char* output_dir) {
  C1<C> c1(reader, sector_slot);
  c1.SetReplicaID((node_t*)replica_id);
  c1.SetTicket((node_t*)ticket);

  c1.DeriveChallenges(seed);
  if (C::GetNumTreeRCFiles() == 1) {
    c1.SetTreeRBufs(cache_path, "%s/sc-02-data-tree-r-last.dat");
    c1.SetTreeCBufs(cache_path, "%s/sc-02-data-tree-c.dat");
  } else {
    c1.SetTreeRBufs(cache_path, "%s/sc-02-data-tree-r-last-%ld.dat");
    c1.SetTreeCBufs(cache_path, "%s/sc-02-data-tree-c-%ld.dat");
  }
  c1.SetTreeDBuf(cache_path, "%s/sc-02-data-tree-d.dat");

  c1.GetRoots(cache_path);
  c1.SetReplicaBuf(replica_path);
  c1.SetParentsBuf(parents_filename);

  const size_t MAX = 256;
  char fname[MAX];
  snprintf(fname, MAX, "%s/commit-phase1-output", output_dir);
  c1.WriteProofs(fname, true, true);

  return 0;
}

template<class C>
int do_c1_tree(streaming_node_reader_t<C>& reader,
               size_t num_sectors, size_t sector_slot,
               const uint8_t* replica_id, const uint8_t* seed,
               const uint8_t* ticket, const char* cache_path,
               const char* parents_filename, const char* replica_path,
               const char* output_dir) {
  C1<C> c1_tree(reader, sector_slot);
  c1_tree.SetReplicaID((node_t*)replica_id);
  c1_tree.DeriveChallenges(seed);
  if (C::GetNumTreeRCFiles() == 1) {
    c1_tree.SetTreeRBufs(cache_path, "%s/sc-02-data-tree-r-last.dat");
  } else {
    c1_tree.SetTreeRBufs(cache_path, "%s/sc-02-data-tree-r-last-%ld.dat");
  }
  c1_tree.SetTreeDBuf(cache_path, "%s/sc-02-data-tree-d.dat");
  c1_tree.GetRoots(cache_path);
  c1_tree.SetReplicaBuf(replica_path);

  const size_t MAX = 256;
  char fname_tree[MAX];
  snprintf(fname_tree, MAX, "%s/commit-phase1-output-tree", output_dir);
  c1_tree.WriteProofs(fname_tree, true, false);

  return 0;
}

template<class C>
int do_c1_node(streaming_node_reader_t<C>& reader,
               size_t num_sectors, size_t sector_slot,
               const uint8_t* replica_id, const uint8_t* seed,
               const uint8_t* ticket, const char* cache_path,
               const char* parents_filename, const char* replica_path,
               const char* output_dir) {
  C1<C> c1_node(reader, sector_slot);
  c1_node.SetReplicaID((node_t*)replica_id);
  c1_node.SetTicket((node_t*)ticket);
  c1_node.DeriveChallenges(seed);
  if (C::GetNumTreeRCFiles() == 1) {
    c1_node.SetTreeCBufs(cache_path, "%s/sc-02-data-tree-c.dat");
  } else {
    c1_node.SetTreeCBufs(cache_path, "%s/sc-02-data-tree-c-%ld.dat");
  }
  c1_node.SetParentsBuf(parents_filename);
  c1_node.GetRoots(cache_path);

  const size_t MAX = 256;
  char fname_node[MAX];
  snprintf(fname_node, MAX, "%s/commit-phase1-output-node", output_dir);
  c1_node.WriteProofs(fname_node, false, true);

  return 0;
}

template<class C>
int do_c1_comb(streaming_node_reader_t<C>& reader,
               size_t num_sectors, size_t sector_slot,
               const uint8_t* replica_id, const uint8_t* seed,
               const uint8_t* ticket, const char* cache_path,
               const char* parents_filename, const char* replica_path,
               const char* output_dir) {
  C1<C> c1_combine(reader, sector_slot);

  const size_t MAX = 256;
  char fname_tree[MAX];
  snprintf(fname_tree, MAX, "%s/commit-phase1-output-tree", output_dir);

  char fname_node[MAX];
  snprintf(fname_node, MAX, "%s/commit-phase1-output-node", output_dir);

  char fname_comb[MAX];
  snprintf(fname_comb, MAX, "%s/commit-phase1-output-comb", output_dir);

  c1_combine.CombineProofs(fname_comb, fname_tree, fname_node);

  return 0;
}
