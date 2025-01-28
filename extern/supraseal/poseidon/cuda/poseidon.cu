// Copyright Supranational LLC

#include <util/all_gpus.cpp>
#include <ff/bls12-381.hpp>

struct kernel_params_t {
  int   t;
  int   partial_rounds;
  int   half_full_rounds;
  fr_t* round_constants;
  fr_t* mds_matrix;
  fr_t* pre_sparse_matrix;
  fr_t* sparse_matrices;
};

#include "../poseidon.hpp"
#ifndef __CUDA_ARCH__
#include "../poseidon.cpp"
#endif

// CUDA doesn't seem to like templatized kernel arguments so encapsulate
// it in a struct.
template<size_t ARITY>
struct in_ptrs_d {
  fr_t* ptrs[ARITY];
};

#include "poseidon_kernels.cu"

template<size_t ARITY_DT>
struct PoseidonInternal {
  static const size_t DOMAIN_TAG = 1;
  static const size_t ARITY = ARITY_DT - DOMAIN_TAG;

  static void hash_batch_ptrs(kernel_params_t& params, fr_t& domain_tag,
                              //fr_t* out_d, fr_t* in_d[ARITY], fr_t* aux_d,
                              fr_t* out_d, in_ptrs_d<ARITY_DT - 1> in_d, fr_t* aux_d,
                              size_t num_hashes, size_t stride,
                              const cudaStream_t& stream,
                              const bool first_tree_c, const bool first_tree_r, 
                              const bool to_mont, const bool from_mont, const bool bswap,
                              const bool multi_in_ptr = true) {
    // block size for kernels 1 and 3 where we launch one thread per element
    const int block_size_13       = (256 / ARITY_DT) * ARITY_DT;
    const int hashes_per_block_13 = block_size_13 / ARITY_DT;
    // Block size for kernels 2 and 4 where we launch one thread per ARITY_DT elements
    const int block_size_24       = 128;

    int thread_count_13 = num_hashes * ARITY_DT;
    int block_count_13  = (thread_count_13 + block_size_13 - 1) / block_size_13;
    int block_count_24  = (num_hashes + block_size_24 - 1) / block_size_24;

    // printf("threads_13 %d, threads 24 %d\n",
    //        block_size_13 * block_count_13,
    //        block_size_24 * block_count_24);

    assert (aux_d != in_d.ptrs[0]);
    poseidon_hash_1_0<ARITY_DT><<<block_count_13, block_size_13,
      sizeof(fr_t) * hashes_per_block_13 * ARITY_DT, stream>>>
      (in_d, aux_d,
       domain_tag,
       params,
       num_hashes, stride,
       to_mont, bswap,
       first_tree_c, first_tree_r, multi_in_ptr);

    poseidon_hash_2<ARITY_DT><<<block_count_24, block_size_24, 0, stream>>>
      (aux_d,
       params,
       ARITY_DT * (params.half_full_rounds + 1),
       params.half_full_rounds,
       num_hashes);

    poseidon_hash_3<ARITY_DT><<<block_count_13, block_size_13,
      sizeof(fr_t) * hashes_per_block_13 * ARITY_DT, stream>>>
      (aux_d,
       params,
       ARITY_DT * (params.half_full_rounds + 1) + params.partial_rounds,
       params.half_full_rounds + params.partial_rounds,
       thread_count_13);

    poseidon_hash_4<ARITY_DT><<<block_count_24, block_size_24, 0, stream>>>
      (aux_d, out_d,
       params.mds_matrix,
       num_hashes, from_mont);
  }
};

template struct PoseidonInternal<12>;
template struct PoseidonInternal<9>;
template struct PoseidonInternal<3>;

#ifndef __CUDA_ARCH__
template<size_t ARITY_DT>
class PoseidonCuda : public Poseidon {
  static const size_t DOMAIN_TAG = 1;
  static const size_t ARITY = ARITY_DT - DOMAIN_TAG;
  
  gpu_ptr_t<fr_t> constants_d;
  kernel_params_t  kernel_params;
  const gpu_t& gpu;
  
public:
  PoseidonCuda(const gpu_t& _gpu) : Poseidon(ARITY), gpu(_gpu) {
    select_gpu(gpu);
    constants_d = gpu_ptr_t<fr_t>{(fr_t*)gpu.Dmalloc(constants_size_)};
    fr_t* constants_ptr = &constants_d[0];
    gpu.HtoD(constants_ptr, constants_file_, constants_size_ / sizeof(fr_t));
    gpu.sync();

    AssignPointers(constants_ptr,
                   &kernel_params.round_constants, &kernel_params.mds_matrix,
                   &kernel_params.pre_sparse_matrix, &kernel_params.sparse_matrices);
    kernel_params.t = t_;
    kernel_params.partial_rounds = partial_rounds_;
    kernel_params.half_full_rounds = half_full_rounds_;
  }
  
  void hash_batch(fr_t* out, fr_t* in,
                  size_t count, size_t stride,
                  const bool first_tree_c, const bool first_tree_r,
                  const bool to_mont, const bool from_mont, const bool bswap) {
    select_gpu(gpu);
    stream_t& stream = gpu[0];

    size_t batch_count = ((count + stride - 1) / stride);
    size_t elements_per_arity = batch_count * stride;
    size_t elements_to_xfer = ARITY_DT * elements_per_arity;

    dev_ptr_t<fr_t> in_d(ARITY * elements_per_arity);
    dev_ptr_t<fr_t> out_d(count);
    dev_ptr_t<fr_t> aux_d(ARITY_DT * elements_per_arity);

    // printf("elements_htod %ld element[0] %08x element[128] %08x\n",
    //        ARITY * elements_per_arity, ((uint32_t*)&in[0])[0], ((uint32_t*)&in[128])[0]);
    stream.HtoD(&in_d[0], in, ARITY * elements_per_arity);
    hash_batch_device(&out_d[0], &in_d[0], &aux_d[0],
                      count, stride,
                      stream, first_tree_c, first_tree_r,
                      to_mont, from_mont, bswap);

    stream.DtoH(out, &out_d[0], count);
    stream.sync();
  }

  void hash_batch_device(fr_t* out_d, fr_t* in_d,  fr_t* aux_d,
                         size_t count, size_t stride,
                         stream_t& stream, const bool first_tree_c, const bool first_tree_r,
                         const bool to_mont, const bool from_mont, const bool bswap) {
    select_gpu(gpu);
    in_ptrs_d<ARITY_DT - 1> in_ptrs_d;
    memset(&in_ptrs_d, 0, sizeof(in_ptrs_d));
    in_ptrs_d.ptrs[0] = in_d;
    hash_batch_device_ptrs(out_d, in_ptrs_d, aux_d,
                           count, stride,
                           stream, first_tree_c, first_tree_r,
                           to_mont, from_mont, bswap, false);
  }

  // count      - number of hash results to produce
  // The following are only used when first == true:
  // stride     - number of elements between subsequent inputs to a hash
  void hash_batch_device_ptrs(fr_t* out_d, in_ptrs_d<ARITY_DT - 1> in_d, fr_t* aux_d,
                              size_t count, size_t stride,
                              stream_t& stream, const bool first_tree_c, const bool first_tree_r,
                              const bool to_mont, const bool from_mont,
                              const bool bswap, const bool multi_in_ptrs = true) {
    select_gpu(gpu);
    assert(count % stride == 0);
    PoseidonInternal<ARITY_DT>::hash_batch_ptrs(kernel_params, domain_tag_,
                                                &out_d[0], in_d, &aux_d[0],
                                                count, stride,
                                                stream, first_tree_c, first_tree_r,
                                                to_mont, from_mont, bswap,
                                                multi_in_ptrs);
  }
};

#endif
