// Copyright Supranational LLC

#include <assert.h>


#ifdef __CUDA_ARCH__

extern __shared__ fr_t scratchpad[];

__device__ __forceinline__
fr_t pow_5(const fr_t& element) {
    fr_t tmp = sqr(element);
    tmp = sqr(tmp);
    return element * tmp;
}

__device__ __forceinline__
void quintic_s_box(fr_t& element, const fr_t& round_constant) {

    element = pow_5(element);
    element += round_constant;
}

__device__ __forceinline__
void partial_quintic_s_box(fr_t& element) {

    element = pow_5(element);
}

__device__ __forceinline__
void add_full_round_constants(fr_t& element, const fr_t& round_constant) {

    element += round_constant;
}

__device__ __forceinline__
void matrix_mul(fr_t& element, const fr_t* matrix, const int t,
                const int thread_pos, const int shared_pos) {

    scratchpad[threadIdx.x] = element;
    __syncthreads();

    element = fr_t::dot_product(&scratchpad[shared_pos], &matrix[thread_pos], t, t);
    __syncthreads();
}

__device__ __forceinline__
fr_t last_matrix_mul(const fr_t* elements, const fr_t* matrix, const int t) {

    return fr_t::dot_product(elements, &matrix[1], t, t);
}

__device__ __forceinline__
void scalar_product(fr_t* elements, const fr_t* sparse_matrix,
                    const int t) {

    elements[0] *= sparse_matrix[0];
    elements[0] += fr_t::dot_product(&elements[1], &sparse_matrix[1], t-1);
}

__device__ __forceinline__
void sparse_matrix_mul(fr_t* elements, const fr_t* sparse_matrix,
                       const int t) {

    fr_t element0 = elements[0];

    scalar_product(elements, sparse_matrix, t);

    #pragma unroll
    for (int i = 1; i < t; i++) {
        elements[i] += element0 * sparse_matrix[t + i - 1];
    }
}

__device__ __forceinline__
void round_matrix_mul(fr_t& element, const kernel_params_t constants,
                      const int current_round, const int thread_pos,
                      const int shared_pos) {

    if (current_round == constants.half_full_rounds - 1) {
        matrix_mul(element, constants.pre_sparse_matrix, constants.t,
                   thread_pos, shared_pos);
    }
    else {
        matrix_mul(element, constants.mds_matrix, constants.t, thread_pos,
                   shared_pos);
    }
}

__device__ __forceinline__
void full_round(fr_t& element, const kernel_params_t constants,
                int& rk_offset, int& current_round, const int thread_pos,
                const int shared_pos) {


    quintic_s_box(element, constants.round_constants[rk_offset]);
    rk_offset += constants.t;

    round_matrix_mul(element, constants, current_round, thread_pos, shared_pos);
    current_round++;
}

__device__ __forceinline__
void partial_round(fr_t* elements, const int t,
                   const kernel_params_t constants,
                   int& rk_offset, int& current_round) {

    quintic_s_box(elements[0], constants.round_constants[rk_offset]);
    rk_offset += 1;

    sparse_matrix_mul(elements, constants.sparse_matrices +
                      (t * 2 - 1) *
                      (current_round - constants.half_full_rounds), t);
    current_round++;
}

__device__ __forceinline__
uint32_t bswap(uint32_t a)
{
  uint32_t ret;
  asm("prmt.b32 %0, %1, %1, 0x0123;" : "=r"(ret) : "r"(a));
  return ret;
}

__device__ __forceinline__
void bswap(fr_t& a)
{
  for (int i = 0; i < a.len(); i++) {
    a[i] = bswap(a[i]);
  }
}

#endif

// Perform first 4 full rounds
//   in_ptrs       - input data
//   aux_ptr       - aux buffer to store results
//   constants     - constants related to configuration & application
//   mont          - if true convert field elements to montgomery form
//   first         - if true this is the first operation on data
//   multi_in_ptr  - if true multiple input pointers are used
// Launch parameters
//   One thread per element (including domain tag)
// in_ptr layout
//   Contains input field elements with one empty element for the
//   domain tag before each 'arity' set of inputs.
//   dt0 fr0 fr1 fr2 fr3 fr4 fr5 fr6 fr7 dt1 etc
//   If multi_in_ptr == false then all field elements are in a contiguous buffer.
//   If multi_in_ptr == true then ARITY pointers are provided, one per branch with data
//   layed out as:
//     in_ptr[0] = s0n0 s1n0 s2n0 s3n0 ...
//     in_ptr[1] = s0n1 s1n1 s2n1 s3n1 ...
//     in_ptr[2] = s0n2 s1n2 s2n2 s3n2 ...
//       ...
//     in_ptr[7] = s0n7 s1n7 s2n7 s3n7 ...
//
// aux_ptr
//   Will contain the hashed outputs in the same layout as in_ptr and should contain
//   space for num_hashes * ARITY_DT elements.
//
// num_hashes - number of inputs to hash
// stride     - number of elements between subsequent inputs to a hash
template<int ARITY_DT> __global__ 
void poseidon_hash_1_0(in_ptrs_d<ARITY_DT - 1> in_ptrs, fr_t* aux_ptr, const fr_t domain_tag,
                       const kernel_params_t constants,
                       const int num_hashes, const int stride,
                       const bool to_mont, const bool do_bswap,
                       const bool first_tree_c, const bool first_tree_r,
                       const bool multi_in_ptr) {
#ifdef __CUDA_ARCH__
  const int ARITY   = ARITY_DT - 1;
  int current_round = 0;
  int rk_offset     = 0;
  
  int tid = threadIdx.x + blockIdx.x * blockDim.x;

  if (tid >= num_hashes * ARITY_DT) {
    return;
  }

  // Position in shared memory
  int shared_pos = (threadIdx.x / ARITY_DT) * ARITY_DT;
  // Index into set of t elements
  //int idx = blockIdx.x * (blockDim.x / ARITY_DT) + threadIdx.x / ARITY_DT;
  //int idx = tid / ARITY_DT;
  
  // For PC2, traversal is
  // thr0 - dt0
  // thr1 - s0n0l0
  // thr2 - s0n0l1
  // thr3 - s0n0l2
  // ...
  // thr11 - s0n0l10
  // thr12 - s0n1l0
  // thr13 - s0n1l2

  int num_batches = (num_hashes + stride - 1) / stride;
  int hash_num = tid / ARITY_DT;
  // Position within set of t elements
  int hash_input = tid % ARITY_DT;

  int node = hash_num % num_batches;
  int sector = hash_num / num_batches;
  fr_t element;

  if (hash_input == 0) {
    element = domain_tag;
  }
  else {
    if (multi_in_ptr) {
      // This is a bit complicated due to the pattern of the data. When num_hashes
      // is equal to batch size then the pattern is:
      //     in_ptr[0] = s0n0 s1n0 s2n0 s3n0 ...
      //     in_ptr[1] = s0n1 s1n1 s2n1 s3n1 ...
      //     in_ptr[2] = s0n2 s1n2 s2n2 s3n2 ...
      //     in_ptr[7] = s0n7 s1n7 s2n7 s3n7 ...
      // When num_hashes is a multiple of stride, say 2x:
      //     in_ptr[0] = s0n0 s0n1
      //     in_ptr[1] = s0n2 s0n3
      //     in_ptr[2] = s0n4 s0n5
      //     in_ptr[3] = s0n6 s0n7
      //     in_ptr[4] = s1n0 s1n1
      //     in_ptr[7] = s1n6 s1n7
      // ie, two consecutive elements at a time. This pattern is repeated many times
      // based on batch size.

      int elements_per_hash_per_ptr = num_hashes / stride;
      int element_idx = hash_num * (ARITY_DT - 1) + (hash_input - 1);
      int element_batch_idx = element_idx / elements_per_hash_per_ptr;
      int element_batch_off = element_idx % elements_per_hash_per_ptr;
      int ptr_num = element_batch_idx % ARITY;
      int ptr_idx = element_idx / (ARITY * elements_per_hash_per_ptr) * elements_per_hash_per_ptr + element_batch_off;
      element = in_ptrs.ptrs[ptr_num][ptr_idx];
    } else {
      fr_t* in_ptr = in_ptrs.ptrs[0];
      if (first_tree_r || first_tree_c) {
        int first_element;
        int element_index;
        if (first_tree_c) {
          first_element = node * stride + sector;
          element_index = first_element + (hash_input - 1) * stride * num_batches;
        } else {
          first_element = node * ARITY * stride + sector;
          element_index = first_element + (hash_input - 1) * stride;
        }
        element = in_ptr[element_index];
        if (do_bswap) {
          bswap(element);
        }
      } else {
        // Access element from a packed array (no domain tag)
        element = in_ptr[hash_num * (ARITY_DT - 1) + (hash_input - 1)];
      }
    }
    if (to_mont) {
      element.to();
    }
  }

  rk_offset += hash_input;

  add_full_round_constants(element, constants.round_constants[rk_offset]);
  rk_offset += ARITY_DT;

  for (int i = 0; i < constants.half_full_rounds; i++) {
    full_round(element, constants, rk_offset, current_round, hash_input,
               shared_pos);
  }

  // When first is true this unstrides the sectors from pc1, leading to
  // s0n0 s0n1 ... s1n0 s1n1 ... s2n0 s2n1 ...
  
  __syncthreads();
  aux_ptr[hash_num * ARITY_DT + hash_input] = element;
#endif
}

// Perform partial rounds
// Data is in aux_ptr from poseidon_hash_1
// rk_offset     - 5 * t
// current_round - 4
// Launch params
//   One thread per t elements
template<int ARITY_DT> __global__ 
void poseidon_hash_2(fr_t* aux_ptr, const kernel_params_t constants,
                     int rk_offset, int current_round, const int batch_size) {

#ifdef __CUDA_ARCH__
  int idx = threadIdx.x + blockIdx.x * blockDim.x;

  if (idx >= batch_size) {
    return;
  }
  
  aux_ptr += idx * ARITY_DT;

  fr_t elements[ARITY_DT];

  for (int i = 0; i < ARITY_DT; i++) {
    elements[i] = aux_ptr[i];
  }

  for (int i = 0; i < constants.partial_rounds; i++) {
    partial_round(elements, ARITY_DT, constants, rk_offset, current_round);
  }

  for (int i = 0; i < ARITY_DT; i++) {
    aux_ptr[i] = elements[i];
  }
#endif
}

// Perform 3 of the final 4 full rounds
// rk_offset - 5 * t + number of partial rounds for this config from partial_rounds_map
// current_round - 4 + number of partial rounds for this config from partial_rounds_map
// Launch parameters
//   One thread per element (including domain tag)
template<int ARITY_DT> __global__ 
void poseidon_hash_3(fr_t* aux_ptr, const kernel_params_t constants,
                     int rk_offset, int current_round, const int batch_size) {
#ifdef __CUDA_ARCH__
  int idx = threadIdx.x + blockIdx.x * blockDim.x;

  if (idx >= batch_size) {
    return;
  }

  int thread_pos = threadIdx.x % ARITY_DT;
  int shared_pos = (threadIdx.x / ARITY_DT) * ARITY_DT;
  idx = blockIdx.x * (blockDim.x / ARITY_DT) + threadIdx.x / ARITY_DT;

  rk_offset += thread_pos;

  fr_t element = aux_ptr[idx * ARITY_DT + thread_pos];

  for (int i = 0; i < constants.half_full_rounds - 1; i++) {
    full_round(element, constants, rk_offset, current_round, thread_pos,
               shared_pos);
  }

  partial_quintic_s_box(element);

  aux_ptr[idx * ARITY_DT + thread_pos] = element;
#endif
}

// Perform last of the final 4 full rounds
// Data is in aux_ptr from poseidon_hash_1
// Output is written to out_ptr
// Launch params
//   One thread per t elements
template<int ARITY_DT> __global__ 
void poseidon_hash_4(const fr_t* aux_ptr, fr_t* out_ptr, const fr_t* mds_matrix,
                     const int batch_size, const bool from_mont) {
#ifdef __CUDA_ARCH__
  int idx = threadIdx.x + blockIdx.x * blockDim.x;

  if (idx >= batch_size) {
    return;
  }

  aux_ptr += idx * ARITY_DT;
  // fr_t elements[t];

  // for (int i = 0; i < t; i++) {
  //   elements[i] = aux_ptr[i];
  // }

  // This writes state[1] into out_ptr
  // No dt slots
  fr_t out = last_matrix_mul(aux_ptr, mds_matrix, ARITY_DT);
  if (from_mont) {
    out.from();
  }
  out_ptr[idx] = out;
#endif
}
