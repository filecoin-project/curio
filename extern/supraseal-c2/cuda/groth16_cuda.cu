// Copyright Supranational LLC

#include <iostream>
#include <thread>
#include <vector>
#include <mutex>
#include <algorithm>
#include <sys/time.h>

#if defined(FEATURE_BLS12_381)
# include <ff/bls12-381-fp2.hpp>
#else
# error "only FEATURE_BLS12_381 is supported"
#endif

#include <ec/jacobian_t.hpp>
#include <ec/xyzz_t.hpp>

typedef jacobian_t<fp_t> point_t;
typedef xyzz_t<fp_t> bucket_t;
typedef bucket_t::affine_t affine_t;

typedef jacobian_t<fp2_t> point_fp2_t;
typedef xyzz_t<fp2_t> bucket_fp2_t;
typedef bucket_fp2_t::affine_t affine_fp2_t;

typedef fr_t scalar_t;

#define SPPARK_DONT_INSTANTIATE_TEMPLATES
#include <msm/pippenger.cuh>
#include <msm/pippenger.hpp>

template<class Scalar>
struct Assignment {
    // Density of queries
    const uint64_t* a_aux_density;
    size_t a_aux_bit_len;
    size_t a_aux_popcount;

    const uint64_t* b_inp_density;
    size_t b_inp_bit_len;
    size_t b_inp_popcount;

    const uint64_t* b_aux_density;
    size_t b_aux_bit_len;
    size_t b_aux_popcount;

    // Evaluations of A, B, C polynomials
    const Scalar* a;
    const Scalar* b;
    const Scalar* c;
    size_t abc_size;

    // Assignments of variables
    const Scalar* inp_assignment_data;
    size_t inp_assignment_size;

    const Scalar* aux_assignment_data;
    size_t aux_assignment_size;
};

#include "groth16_ntt_h.cu"
#include "groth16_split_msm.cu"

template<class point_t, class affine_t>
static void mult(point_t& ret, const affine_t point, const scalar_t& fr,
                 size_t top = scalar_t::nbits)
{
#ifndef __CUDA_ARCH__
    scalar_t::pow_t scalar;
    fr.to_scalar(scalar);

    mult(ret, point, scalar, top);
#endif
}

// GPU-side CPU thread pool for preprocessing + b_g2_msm.
// Lazily initialized on first use so that the Rust caller has time to
// set CUZK_GPU_THREADS before the pool is created.
//
// CUZK_GPU_THREADS env var controls pool size (0 or unset = all CPUs).
// When running parallel synthesis alongside GPU proving, limit this to
// avoid CPU contention (e.g. 32 threads on a 96-core machine).
static thread_pool_t* groth16_pool_ptr = nullptr;
static std::once_flag  groth16_pool_init_flag;

static thread_pool_t& get_groth16_pool() {
    std::call_once(groth16_pool_init_flag, []() {
        unsigned int num_threads = 0;
        const char* env = getenv("CUZK_GPU_THREADS");
        if (env && env[0]) {
            unsigned int n = (unsigned int)atoi(env);
            if (n > 0) num_threads = n;
        }
        groth16_pool_ptr = new thread_pool_t(num_threads);
    });
    return *groth16_pool_ptr;
}

struct msm_results {
    std::vector<point_t> h;
    std::vector<point_t> l;
    std::vector<point_t> a;
    std::vector<point_t> b_g1;
    std::vector<point_fp2_t> b_g2;

    msm_results(size_t num_circuits) : h(num_circuits),
                                       l(num_circuits),
                                       a(num_circuits),
                                       b_g1(num_circuits),
                                       b_g2(num_circuits) {}
};

struct groth16_proof {
    point_t::affine_t a;
    point_fp2_t::affine_t b;
    point_t::affine_t c;
};

#include "groth16_srs.cuh"

#if defined(_MSC_VER) && !defined(__clang__) && !defined(__builtin_popcountll)
#define __builtin_popcountll(x) __popcnt64(x)
#endif

// Phase 8: Allocate/free a std::mutex on the C++ heap, returned as an
// opaque pointer for Rust to hold. This ensures correct C++ ABI alignment
// and constructor/destructor semantics.
extern "C" void* create_gpu_mutex() {
    return static_cast<void*>(new std::mutex());
}

extern "C" void destroy_gpu_mutex(void* mtx) {
    delete static_cast<std::mutex*>(mtx);
}

// Phase 12: Pending proof handle for split (async) API.
// Holds everything needed to finalize a proof after the GPU worker returns.
// The b_g2_msm thread runs in the background; finalize() joins it and
// assembles the final proof points.
struct groth16_pending_proof {
    std::thread prep_msm_thread;  // b_g2_msm still running
    msm_results results;
    batch_add_results batch_add_res;
    const verifying_key* vk;
    bool l_split_msm, a_split_msm, b_split_msm;
    size_t num_circuits;
    std::vector<fr_t> r_s_owned;  // copy of randomness
    std::vector<fr_t> s_s_owned;
    std::atomic<bool> caught_exception;
    struct timeval tv_func_entry;

    // Owned copy of the Assignment structs. The prep_msm_thread reads
    // prover fields (inp/aux_assignment_data, density bitmaps) after the
    // C function returns, so we must keep a stable copy in the handle.
    // The Assignment struct only contains pointers+sizes — the underlying
    // Rust data is kept alive by the Rust PendingProofHandle.
    std::vector<Assignment<fr_t>> provers_owned;

    // Dealloc data — moved here so we can free them in the finalizer
    split_vectors sv_l, sv_a, sv_b;
    std::vector<affine_t> tail_l, tail_a, tail_b_g1;
    std::vector<affine_fp2_t> tail_b_g2;

    groth16_pending_proof(size_t nc)
        : results(nc), batch_add_res(nc), vk(nullptr),
          l_split_msm(false), a_split_msm(false), b_split_msm(false),
          num_circuits(nc), caught_exception(false),
          sv_l(0, 0), sv_a(0, 0), sv_b(0, 0) {}
};

// Phase 12: Finalize a pending proof — join b_g2_msm, run epilogue, write proofs.
extern "C"
RustError::by_value finalize_groth16_proof_c(void* handle, groth16_proof proofs[]) {
    auto* pp = static_cast<groth16_pending_proof*>(handle);

    // Wait for b_g2_msm to complete
    pp->prep_msm_thread.join();

    if (pp->caught_exception.load()) {
        // Clean up via dealloc thread, same as normal path
        static std::mutex dealloc_mtx;
        std::thread([
            sv_l = std::move(pp->sv_l),
            sv_a = std::move(pp->sv_a),
            sv_b = std::move(pp->sv_b),
            tl = std::move(pp->tail_l),
            ta = std::move(pp->tail_a),
            tb = std::move(pp->tail_b_g1),
            tb2 = std::move(pp->tail_b_g2)
        ]() mutable {
            std::lock_guard<std::mutex> lk(dealloc_mtx);
            { auto tmp = std::move(sv_l); }
            { auto tmp = std::move(sv_a); }
            { auto tmp = std::move(sv_b); }
            { auto tmp = std::move(tl); }
            { auto tmp = std::move(ta); }
            { auto tmp = std::move(tb); }
            { auto tmp = std::move(tb2); }
        }).detach();
        delete pp;
        return RustError{1, "b_g2_msm caught exception"};
    }

    // Epilogue — assemble final proof points
    for (size_t circuit = 0; circuit < pp->num_circuits; circuit++) {
        if (pp->l_split_msm)
            pp->results.l[circuit].add(pp->batch_add_res.l[circuit]);
        if (pp->a_split_msm)
            pp->results.a[circuit].add(pp->batch_add_res.a[circuit]);
        if (pp->b_split_msm) {
            pp->results.b_g1[circuit].add(pp->batch_add_res.b_g1[circuit]);
            pp->results.b_g2[circuit].add(pp->batch_add_res.b_g2[circuit]);
        }

        fr_t r = pp->r_s_owned[circuit], s = pp->s_s_owned[circuit];
        fr_t rs = r * s;

        point_t g_a, g_c, a_answer, b1_answer, vk_delta_g1_rs, vk_alpha_g1_s,
                vk_beta_g1_r;
        point_fp2_t g_b;

        mult(vk_delta_g1_rs, pp->vk->delta_g1, rs);
        mult(vk_alpha_g1_s, pp->vk->alpha_g1, s);
        mult(vk_beta_g1_r, pp->vk->beta_g1, r);

        mult(b1_answer, pp->results.b_g1[circuit], r);

        // A
        mult(g_a, pp->vk->delta_g1, r);
        g_a.add(pp->vk->alpha_g1);
        g_a.add(pp->results.a[circuit]);

        // B
        mult(g_b, pp->vk->delta_g2, s);
        g_b.add(pp->vk->beta_g2);
        g_b.add(pp->results.b_g2[circuit]);

        // C
        mult(g_c, pp->results.a[circuit], s);
        g_c.add(b1_answer);
        g_c.add(vk_delta_g1_rs);
        g_c.add(vk_alpha_g1_s);
        g_c.add(vk_beta_g1_r);
        g_c.add(pp->results.h[circuit]);
        g_c.add(pp->results.l[circuit]);

        // to affine
        proofs[circuit].a = g_a;
        proofs[circuit].b = g_b;
        proofs[circuit].c = g_c;
    }

    // Async dealloc of large buffers
    static std::mutex dealloc_mtx;
    std::thread([
        sv_l = std::move(pp->sv_l),
        sv_a = std::move(pp->sv_a),
        sv_b = std::move(pp->sv_b),
        tl = std::move(pp->tail_l),
        ta = std::move(pp->tail_a),
        tb = std::move(pp->tail_b_g1),
        tb2 = std::move(pp->tail_b_g2)
    ]() mutable {
        std::lock_guard<std::mutex> lk(dealloc_mtx);
        struct timeval tv_start, tv_end;
        gettimeofday(&tv_start, NULL);
        { auto tmp = std::move(sv_l); }
        { auto tmp = std::move(sv_a); }
        { auto tmp = std::move(sv_b); }
        { auto tmp = std::move(tl); }
        { auto tmp = std::move(ta); }
        { auto tmp = std::move(tb); }
        { auto tmp = std::move(tb2); }
        gettimeofday(&tv_end, NULL);
        long destr_ms = (tv_end.tv_sec - tv_start.tv_sec) * 1000
                      + (tv_end.tv_usec - tv_start.tv_usec) / 1000;
        fprintf(stderr, "CUZK_TIMING: async_dealloc_ms=%ld\n", destr_ms);
    }).detach();

    delete pp;
    return RustError{cudaSuccess};
}

// Phase 12: Destroy a pending proof handle without finalizing (cleanup on error).
extern "C"
void destroy_pending_proof(void* handle) {
    auto* pp = static_cast<groth16_pending_proof*>(handle);
    if (pp->prep_msm_thread.joinable())
        pp->prep_msm_thread.join();
    delete pp;
}

// Phase 11 Intervention 3: Declared in Rust (cuzk-pce/src/eval.rs).
// Sets the memory-bandwidth throttle flag checked by synthesis SpMV threads.
extern "C" void set_membw_throttle(int value);

// Phase 12: Async entry point — returns a pending proof handle instead of
// blocking on b_g2_msm + epilogue. The GPU worker can loop immediately.
// Call finalize_groth16_proof_c(handle, proofs) later to complete.
extern "C"
RustError::by_value generate_groth16_proofs_start_c(
    const Assignment<fr_t> provers[],
    size_t num_circuits,
    const fr_t r_s[], const fr_t s_s[],
    SRS& srs, std::mutex* gpu_mtx,
    void** pending_out);

// Main sync entry point — delegates to the core with pending_out=nullptr.
extern "C"
RustError::by_value generate_groth16_proofs_c(const Assignment<fr_t> provers[],
                                              size_t num_circuits,
                                              const fr_t r_s[], const fr_t s_s[],
                                              groth16_proof proofs[], SRS& srs,
                                              std::mutex* gpu_mtx)
{
    // Sync path: run everything including b_g2_msm + epilogue.
    // We call start then finish inline.
    void* pending = nullptr;
    auto err = generate_groth16_proofs_start_c(provers, num_circuits, r_s, s_s,
                                               srs, gpu_mtx, &pending);
    if (err.code != 0)
        return err;
    return finalize_groth16_proof_c(pending, proofs);
}

extern "C"
RustError::by_value generate_groth16_proofs_start_c(
    const Assignment<fr_t> provers[],
    size_t num_circuits,
    const fr_t r_s[], const fr_t s_s[],
    SRS& srs, std::mutex* gpu_mtx,
    void** pending_out)
{
    // Phase 8: The caller passes a per-GPU mutex pointer. The lock is acquired
    // at a narrow scope around the CUDA kernel region only (see below), allowing
    // CPU preprocessing and b_g2_msm to overlap with another worker's GPU work.
    // If gpu_mtx is nullptr, fall back to a function-local static mutex for
    // backward compatibility (non-engine callers).
    static std::mutex fallback_mtx;
    std::mutex* mtx_ptr = gpu_mtx ? gpu_mtx : &fallback_mtx;

    auto t_entry = std::chrono::steady_clock::now();

    struct timeval tv_func_entry;
    gettimeofday(&tv_func_entry, NULL);

    if (!ngpus()) {
        return RustError{ENODEV, "No CUDA devices available"};
    }

    const verifying_key* vk = &srs.get_vk();

    auto points_h = srs.get_h_slice();
    auto points_l = srs.get_l_slice();
    auto points_a = srs.get_a_slice();
    auto points_b_g1 = srs.get_b_g1_slice();
    auto points_b_g2 = srs.get_b_g2_slice();

    for (size_t c = 0; c < num_circuits; c++) {
        auto& p = provers[c];

        assert(points_l.size() == p.aux_assignment_size);
        assert(points_a.size() == p.inp_assignment_size + p.a_aux_popcount);
        assert(points_b_g1.size() == p.b_inp_popcount + p.b_aux_popcount);
        assert(p.a_aux_bit_len == p.aux_assignment_size);
        assert(p.b_aux_bit_len == p.aux_assignment_size);
        assert(p.b_inp_bit_len == p.inp_assignment_size);
    }

    // Phase 12: Allocate the pending proof handle early and construct
    // all shared state directly in it, so threads can reference the
    // handle's fields by address (stable, heap-allocated).
    auto* pp = new groth16_pending_proof(num_circuits);
    pp->vk = vk;
    pp->tv_func_entry = tv_func_entry;

    // Copy the Assignment structs into the handle so the prep_msm_thread
    // and post-unlock code can safely read them after this function returns.
    // Assignment is a trivial POD (pointers+sizes) — the underlying Rust
    // data is kept alive by PendingProofHandle on the Rust side.
    pp->provers_owned.assign(provers, provers + num_circuits);

    // Split MSM flags — set during prep_msm, read by GPU threads + epilogue.
    pp->l_split_msm = true;
    pp->a_split_msm = true;
    pp->b_split_msm = true;
    bool& l_split_msm = pp->l_split_msm;
    bool& a_split_msm = pp->a_split_msm;
    bool& b_split_msm = pp->b_split_msm;
    size_t l_popcount = 0, a_popcount = 0, b_popcount = 0;

    struct timeval tv_split_start, tv_split_end;
    gettimeofday(&tv_split_start, NULL);
    pp->sv_l = split_vectors{num_circuits, points_l.size()};
    pp->sv_a = split_vectors{num_circuits, points_a.size()};
    pp->sv_b = split_vectors{num_circuits, points_b_g1.size()};
    gettimeofday(&tv_split_end, NULL);
    {
        long split_ms = (tv_split_end.tv_sec - tv_split_start.tv_sec) * 1000
                      + (tv_split_end.tv_usec - tv_split_start.tv_usec) / 1000;
        long setup_ms = (tv_split_end.tv_sec - tv_func_entry.tv_sec) * 1000
                      + (tv_split_end.tv_usec - tv_func_entry.tv_usec) / 1000;
        fprintf(stderr, "CUZK_TIMING: split_vectors_ms=%ld setup_to_split_ms=%ld\n",
                split_ms, setup_ms);
    }

    // Aliases for readability — these point into the heap-allocated handle.
    // All threads capture these references; the handle outlives all threads.
    auto& split_vectors_l = pp->sv_l;
    auto& split_vectors_a = pp->sv_a;
    auto& split_vectors_b = pp->sv_b;
    auto& tail_msm_l_bases = pp->tail_l;
    auto& tail_msm_a_bases = pp->tail_a;
    auto& tail_msm_b_g1_bases = pp->tail_b_g1;
    auto& tail_msm_b_g2_bases = pp->tail_b_g2;
    auto& results = pp->results;

    semaphore_t barrier;
    auto& caught_exception = pp->caught_exception;
    size_t n_gpus = std::min(ngpus(), num_circuits);

    // Alias for the heap-owned provers copy. The prep_msm_thread captures
    // this reference (which resolves to pp->provers_owned, heap-allocated)
    // instead of the function parameter (which is on the stack).
    auto& provers_safe = pp->provers_owned;

    std::thread prep_msm_thread([&, num_circuits]
    {
        auto t_prep_start = std::chrono::steady_clock::now();
        // pre-processing step
        // mark inp and significant scalars in aux assignments
        get_groth16_pool().par_map(num_circuits, [&](size_t c) {
            auto& prover = provers_safe[c];
            auto& l_bit_vector = split_vectors_l.bit_vector[c];
            auto& a_bit_vector = split_vectors_a.bit_vector[c];
            auto& b_bit_vector = split_vectors_b.bit_vector[c];

            size_t a_bits_cursor = 0, b_bits_cursor = 0;
            uint64_t a_bits = 0, b_bits = 0;
            uint32_t a_bit_off = 0, b_bit_off = 0;

            size_t inp_size = prover.inp_assignment_size;

            for (size_t i = 0; i < inp_size; i += CHUNK_BITS) {
                uint64_t b_map = prover.b_inp_density[i / CHUNK_BITS];
                uint64_t map_mask = 1;
                size_t chunk_bits = std::min(CHUNK_BITS, inp_size - i);

                for (size_t j = 0; j < chunk_bits; j++, map_mask <<= 1) {
                    a_bits |= map_mask;

                    if (b_map & map_mask) {
                        b_bits |= (uint64_t)1 << b_bit_off;
                        if (++b_bit_off == CHUNK_BITS) {
                            b_bit_off = 0;
                            b_bit_vector[b_bits_cursor++] = b_bits;
                            b_bits = 0;
                        }
                    }
                }

                a_bit_vector[i / CHUNK_BITS] = a_bits;
                if (chunk_bits == CHUNK_BITS)
                    a_bits = 0;
            }

            a_bits_cursor = inp_size / CHUNK_BITS;
            a_bit_off = inp_size % CHUNK_BITS;

            auto* aux_assignment = prover.aux_assignment_data;
            size_t aux_size = prover.aux_assignment_size;

            for (size_t i = 0; i < aux_size; i += CHUNK_BITS) {
                uint64_t a_map = prover.a_aux_density[i / CHUNK_BITS];
                uint64_t b_map = prover.b_aux_density[i / CHUNK_BITS];
                uint64_t l_bits = 0;
                uint64_t map_mask = 1;
                size_t chunk_bits = std::min(CHUNK_BITS, aux_size - i);

                for (size_t j = 0; j < chunk_bits; j++, map_mask <<= 1) {
                    const fr_t& scalar = aux_assignment[i + j];

                    bool is_one = scalar.is_one();
                    bool is_zero = scalar.is_zero();

                    if (!is_zero && !is_one)
                        l_bits |= map_mask;

                    if (a_map & map_mask) {
                        if (!is_zero && !is_one) {
                            a_bits |= ((uint64_t)1 << a_bit_off);
                        }

                        if (++a_bit_off == CHUNK_BITS) {
                            a_bit_off = 0;
                            a_bit_vector[a_bits_cursor++] = a_bits;
                            a_bits = 0;
                        }
                    }

                    if (b_map & map_mask) {
                        if (!is_zero && !is_one) {
                            b_bits |= ((uint64_t)1 << b_bit_off);
                        }

                        if (++b_bit_off == CHUNK_BITS) {
                            b_bit_off = 0;
                            b_bit_vector[b_bits_cursor++] = b_bits;
                            b_bits = 0;
                        }
                    }
                }

                l_bit_vector[i / CHUNK_BITS] = l_bits;
            }

            if (a_bit_off)
                a_bit_vector[a_bits_cursor] = a_bits;

            if (b_bit_off)
                b_bit_vector[b_bits_cursor] = b_bits;
        });

        if (caught_exception)
            return;

        // merge all the masks from aux_assignments and count set bits
        std::vector<mask_t> tail_msm_l_mask(split_vectors_l.bit_vector_size);
        std::vector<mask_t> tail_msm_a_mask(split_vectors_a.bit_vector_size);
        std::vector<mask_t> tail_msm_b_mask(split_vectors_b.bit_vector_size);

        for (size_t i = 0; i < tail_msm_l_mask.size(); i++) {
            uint64_t mask = split_vectors_l.bit_vector[0][i];
            for (size_t c = 1; c < num_circuits; c++)
                mask |= split_vectors_l.bit_vector[c][i];
            tail_msm_l_mask[i] = mask;
            l_popcount += __builtin_popcountll(mask);
        }

        for (size_t i = 0; i < tail_msm_a_mask.size(); i++) {
            uint64_t mask = split_vectors_a.bit_vector[0][i];
            for (size_t c = 1; c < num_circuits; c++)
                mask |= split_vectors_a.bit_vector[c][i];
            tail_msm_a_mask[i] = mask;
            a_popcount += __builtin_popcountll(mask);
        }

        for (size_t i = 0; i < tail_msm_b_mask.size(); i++) {
            uint64_t mask = split_vectors_b.bit_vector[0][i];
            for (size_t c = 1; c < num_circuits; c++)
                mask |= split_vectors_b.bit_vector[c][i];
            tail_msm_b_mask[i] = mask;
            b_popcount += __builtin_popcountll(mask);
        }

        if (caught_exception)
            return;

        if (l_split_msm = (l_popcount <= points_l.size() / 2)) {
            split_vectors_l.tail_msms_resize(l_popcount);
            tail_msm_l_bases.resize(l_popcount);
        }

        if (a_split_msm = (a_popcount <= points_a.size() / 2)) {
            split_vectors_a.tail_msms_resize(a_popcount);
            tail_msm_a_bases.resize(a_popcount);
        } else {
            split_vectors_a.tail_msms_resize(points_a.size());
        }

        if (b_split_msm = (b_popcount <= points_b_g1.size() / 2)) {
            split_vectors_b.tail_msms_resize(b_popcount);
            tail_msm_b_g1_bases.resize(b_popcount);
            tail_msm_b_g2_bases.resize(b_popcount);
        } else {
            split_vectors_b.tail_msms_resize(points_b_g1.size());
        }

        // populate bitmaps for batch additions, bases and scalars for tail msms
        get_groth16_pool().par_map(num_circuits, [&](size_t c) {
            auto& prover = provers_safe[c];
            auto& l_bit_vector = split_vectors_l.bit_vector[c];
            auto& a_bit_vector = split_vectors_a.bit_vector[c];
            auto& b_bit_vector = split_vectors_b.bit_vector[c];
            auto& tail_msm_l_scalars = split_vectors_l.tail_msm_scalars[c];
            auto& tail_msm_a_scalars = split_vectors_a.tail_msm_scalars[c];
            auto& tail_msm_b_scalars = split_vectors_b.tail_msm_scalars[c];

            size_t a_cursor = 0, b_cursor = 0;

            uint32_t a_bit_off = 0, b_bit_off = 0;
            size_t a_bits_cursor = 0, b_bits_cursor = 0;

            auto* inp_assignment = prover.inp_assignment_data;
            size_t inp_size = prover.inp_assignment_size;

            for (size_t i = 0; i < inp_size; i += CHUNK_BITS) {
                uint64_t b_map = prover.b_inp_density[i / CHUNK_BITS];
                size_t chunk_bits = std::min(CHUNK_BITS, inp_size - i);

                for (size_t j = 0; j < chunk_bits; j++, b_map >>= 1) {
                    const fr_t& scalar = inp_assignment[i + j];

                    if (b_map & 1) {
                        if (c == 0 && b_split_msm) {
                            tail_msm_b_g1_bases[b_cursor] = points_b_g1[b_cursor];
                            tail_msm_b_g2_bases[b_cursor] = points_b_g2[b_cursor];
                        }
                        tail_msm_b_scalars[b_cursor] = scalar;
                        b_cursor++;

                        if (++b_bit_off == CHUNK_BITS) {
                            b_bit_off = 0;
                            b_bit_vector[b_bits_cursor++] = 0;
                        }
                    }

                    if (c == 0 && a_split_msm)
                        tail_msm_a_bases[a_cursor] = points_a[a_cursor];
                    tail_msm_a_scalars[a_cursor] = scalar;
                    a_cursor++;
                }

                a_bit_vector[i / CHUNK_BITS] = 0;
            }

            assert(b_cursor == prover.b_inp_popcount);

            a_bits_cursor = inp_size / CHUNK_BITS;
            a_bit_off = inp_size % CHUNK_BITS;

            uint64_t a_mask = tail_msm_a_mask[a_bits_cursor], a_bits = 0;
            uint64_t b_mask = tail_msm_b_mask[b_bits_cursor], b_bits = 0;

            size_t points_a_cursor = a_cursor,
                   points_b_cursor = b_cursor,
                   l_cursor = 0;

            auto* aux_assignment = prover.aux_assignment_data;
            size_t aux_size = prover.aux_assignment_size;

            for (size_t i = 0; i < aux_size; i += CHUNK_BITS) {
                uint64_t a_map = prover.a_aux_density[i / CHUNK_BITS];
                uint64_t b_map = prover.b_aux_density[i / CHUNK_BITS];
                uint64_t l_map = tail_msm_l_mask[i / CHUNK_BITS], l_bits = 0;
                uint64_t map_mask = 1;

                size_t chunk_bits = std::min(CHUNK_BITS, aux_size - i);
                for (size_t j = 0; j < chunk_bits; j++, map_mask <<= 1) {
                    const fr_t& scalar = aux_assignment[i + j];
                    bool is_one = scalar.is_one();

                    if (l_split_msm) {
                        if (is_one)
                            l_bits |= map_mask;

                        if (l_map & map_mask) {
                            if (c == 0)
                                tail_msm_l_bases[l_cursor] = points_l[i+j];
                            tail_msm_l_scalars[l_cursor] = czero(scalar, is_one);
                            l_cursor++;
                        }
                    }

                    if (a_split_msm) {
                        if (a_map & map_mask) {
                            uint64_t mask = (uint64_t)1 << a_bit_off;

                            if (a_mask & mask) {
                                if (c == 0)
                                    tail_msm_a_bases[a_cursor] = points_a[points_a_cursor];
                                tail_msm_a_scalars[a_cursor] = czero(scalar, is_one);
                                a_cursor++;
                            }

                            points_a_cursor++;

                            if (is_one)
                                a_bits |= mask;

                            if (++a_bit_off == CHUNK_BITS) {
                                a_bit_off = 0;
                                a_bit_vector[a_bits_cursor++] = a_bits;
                                a_bits = 0;
                                a_mask = tail_msm_a_mask[a_bits_cursor];
                            }
                        }
                    } else {
                        if (a_map & map_mask) {
                            tail_msm_a_scalars[a_cursor] = scalar;
                            a_cursor++;
                        }
                    }

                    if (b_split_msm) {
                        if (b_map & map_mask) {
                            uint64_t mask = (uint64_t)1 << b_bit_off;

                            if (b_mask & mask) {
                                if (c == 0) {
                                    tail_msm_b_g1_bases[b_cursor] =
                                        points_b_g1[points_b_cursor];
                                    tail_msm_b_g2_bases[b_cursor] =
                                        points_b_g2[points_b_cursor];
                                }
                                tail_msm_b_scalars[b_cursor] = czero(scalar,
                                                                     is_one);
                                b_cursor++;
                            }

                            points_b_cursor++;

                            if (is_one)
                                b_bits |= mask;

                            if (++b_bit_off == CHUNK_BITS) {
                                b_bit_off = 0;
                                b_bit_vector[b_bits_cursor++] = b_bits;
                                b_bits = 0;
                                b_mask = tail_msm_b_mask[b_bits_cursor];
                            }
                        }
                    } else {
                        if (b_map & map_mask) {
                            tail_msm_b_scalars[b_cursor] = scalar;
                            b_cursor++;
                        }
                    }
                }

                l_bit_vector[i / CHUNK_BITS] = l_bits;
            }

            if (a_bit_off)
                a_bit_vector[a_bits_cursor] = a_bits;

            if (b_bit_off)
                b_bit_vector[b_bits_cursor] = b_bits;

            if (l_split_msm)
                assert(l_cursor == l_popcount);

            if (a_split_msm) {
                assert(points_a_cursor == points_a.size());
                assert(a_cursor == a_popcount);
            } else {
                assert(a_cursor == points_a.size());
            }

            if (b_split_msm) {
                assert(points_b_cursor == points_b_g1.size());
                assert(b_cursor == b_popcount);
            } else {
                assert(b_cursor == points_b_g1.size());
            }

        });
        // end of pre-processing step

        auto t_prep_end = std::chrono::steady_clock::now();
        auto prep_ms = std::chrono::duration_cast<std::chrono::milliseconds>(t_prep_end - t_prep_start).count();
        fprintf(stderr, "CUZK_TIMING: prep_msm_ms=%ld\n", prep_ms);

        for (size_t i = 0; i < n_gpus; i++)
            barrier.notify();

        if (caught_exception)
            return;

        // tail MSM b_g2 - on CPU, parallelized across circuits
        // With batch_size=N, running N single-threaded Pippengers in parallel
        // is much faster than running N sequential thread-pooled Pippengers.
        auto t_bg2_start = std::chrono::steady_clock::now();

        // Phase 11 Intervention 3: Signal synthesis SpMV threads to yield
        // during b_g2_msm's memory-intensive Pippenger computation.
        // This reduces L3 cache contention between Pippenger bucket arrays
        // and SpMV's scattered CSR matrix accesses.
        set_membw_throttle(1);

#ifndef __CUDA_ARCH__
        if (num_circuits > 1) {
            get_groth16_pool().par_map(num_circuits, [&](size_t c) {
                if (caught_exception)
                    return;
                const affine_fp2_t* bg2_bases = b_split_msm
                    ? tail_msm_b_g2_bases.data() : points_b_g2.data();
                mult_pippenger<bucket_fp2_t>(results.b_g2[c],
                    bg2_bases,
                    split_vectors_b.tail_msm_scalars[c].size(),
                    split_vectors_b.tail_msm_scalars[c].data(),
                    true, nullptr);  // nullptr = single-threaded per circuit
                });
        } else {
            const affine_fp2_t* bg2_bases = b_split_msm
                ? tail_msm_b_g2_bases.data() : points_b_g2.data();
            mult_pippenger<bucket_fp2_t>(results.b_g2[0],
                bg2_bases,
                split_vectors_b.tail_msm_scalars[0].size(),
                split_vectors_b.tail_msm_scalars[0].data(),
                true, &get_groth16_pool());  // single circuit: use full thread pool
        }
#endif
        set_membw_throttle(0);

        auto t_bg2_end = std::chrono::steady_clock::now();
        auto bg2_ms = std::chrono::duration_cast<std::chrono::milliseconds>(t_bg2_end - t_bg2_start).count();
        fprintf(stderr, "CUZK_TIMING: b_g2_msm_ms=%ld num_circuits=%zu\n", bg2_ms, num_circuits);
    });

    auto& batch_add_res = pp->batch_add_res;
    std::vector<std::thread> per_gpu;
    RustError ret{cudaSuccess};

    // Phase 9: Pre-stage state — set up after acquiring the GPU mutex.
    // The pre-staging allocates device buffers and issues async HtoD
    // transfers from pinned host memory. Even though the uploads happen
    // while we hold the mutex, the key gains are:
    // 1. cudaHostRegister enables DMA (full PCIe bandwidth, no bounce buffer)
    // 2. cudaMemcpyAsync from pinned memory is truly async — CPU thread
    //    isn't blocked, NTT compute starts as soon as each upload finishes
    //    (via CUDA events), overlapping upload and compute on the same GPU.
    //
    // Host page pinning is done BEFORE the mutex (it's a CPU-only operation
    // that doesn't touch the GPU) so it overlaps with the other worker's
    // CUDA kernels.

    bool prestage_ok = false;
    cudaStream_t upload_stream = nullptr;
    cudaEvent_t ev_a = nullptr, ev_b = nullptr, ev_c = nullptr;
    fr_t* d_a_prestaged = nullptr;
    fr_t* d_bc_prestaged = nullptr;
    size_t prestage_lg_domain = 0;
    size_t prestage_domain_size = 0;
    bool prestage_lot_of_memory = false;
    bool host_a_registered = false, host_b_registered = false, host_c_registered = false;

    // Phase 9: Pin host pages BEFORE the mutex — this is a CPU-only
    // operation (mlock() syscall) that doesn't touch the GPU. Pinning
    // takes ~1-5ms per 2 GiB buffer and enables DMA without bounce buffer.
    if (num_circuits == 1) {
        size_t actual_size = provers[0].abc_size;
        size_t abc_bytes = actual_size * sizeof(fr_t);
        cudaError_t err;

        err = cudaHostRegister((void*)provers[0].a, abc_bytes, cudaHostRegisterDefault);
        if (err == cudaSuccess) {
            host_a_registered = true;
            err = cudaHostRegister((void*)provers[0].b, abc_bytes, cudaHostRegisterDefault);
        }
        if (err == cudaSuccess) {
            host_b_registered = true;
            err = cudaHostRegister((void*)provers[0].c, abc_bytes, cudaHostRegisterDefault);
        }
        if (err == cudaSuccess) {
            host_c_registered = true;
        }
        if (err != cudaSuccess) {
            fprintf(stderr, "CUZK_TIMING: host_register=fallback err=%d\n", (int)err);
            if (host_c_registered) { cudaHostUnregister((void*)provers[0].c); host_c_registered = false; }
            if (host_b_registered) { cudaHostUnregister((void*)provers[0].b); host_b_registered = false; }
            if (host_a_registered) { cudaHostUnregister((void*)provers[0].a); host_a_registered = false; }
        }
    }

    // Phase 8: Acquire GPU mutex — serializes CUDA kernel region only.
    // CPU preprocessing (prep_msm_thread) is already running concurrently.
    // Another worker's CPU work can overlap with our GPU kernels.
    std::unique_lock<std::mutex> gpu_lock(*mtx_ptr);

    // Phase 9: Now that we hold the mutex (no other worker on this GPU),
    // allocate device buffers and issue async uploads from pinned memory.
    //
    // Memory-aware allocation: query actual free VRAM and only pre-stage
    // what fits with a 512 MiB safety margin. The rest of the proving
    // pipeline (MSM, batch_add, tail MSMs) also needs device memory, so
    // we must not consume everything.
    if (num_circuits == 1 && (host_a_registered && host_b_registered && host_c_registered)) {
        size_t npoints = points_h.size();
        prestage_lg_domain = lg2(npoints - 1) + 1;
        prestage_domain_size = (size_t)1 << prestage_lg_domain;
        size_t actual_size = provers[0].abc_size;

        const gpu_t& gpu0 = select_gpu(0);
        prestage_lot_of_memory = 3 * prestage_domain_size * sizeof(fr_t) <
                                 gpu0.props().totalGlobalMem - ((size_t)1 << 30);

        // Drain CUDA async memory pools so cudaMemGetInfo reports accurate
        // free memory. Previous partitions may have freed via cudaFreeAsync
        // (used by gpu_t::Dfree/stream_t::Dfree), leaving memory cached in
        // the async pool — invisible to cudaMalloc.
        cudaDeviceSynchronize();
        {
            cudaMemPool_t pool = nullptr;
            if (cudaDeviceGetDefaultMemPool(&pool, gpu0.cid()) == cudaSuccess && pool) {
                cudaMemPoolTrimTo(pool, 0);
            }
        }

        size_t free_bytes = 0, total_bytes = 0;
        cudaMemGetInfo(&free_bytes, &total_bytes);

        const size_t SAFETY_MARGIN = (size_t)512 << 20;  // 512 MiB
        size_t usable = free_bytes > SAFETY_MARGIN ? free_bytes - SAFETY_MARGIN : 0;

        size_t d_a_bytes = prestage_domain_size * sizeof(fr_t);
        size_t d_bc_elems = prestage_domain_size * (prestage_lot_of_memory + 1);
        size_t d_bc_bytes = d_bc_elems * sizeof(fr_t);
        size_t total_needed = d_a_bytes + d_bc_bytes;

        fprintf(stderr, "CUZK_TIMING: prestage_vram free_mib=%zu usable_mib=%zu "
                "need_da_mib=%zu need_dbc_mib=%zu total_mib=%zu\n",
                free_bytes >> 20, usable >> 20,
                d_a_bytes >> 20, d_bc_bytes >> 20, total_needed >> 20);

        cudaError_t err = cudaSuccess;

        if (usable >= total_needed) {
            // Enough VRAM: pre-stage d_a + d_bc
            err = cudaMalloc(&d_a_prestaged, d_a_bytes);
            if (err == cudaSuccess)
                err = cudaMalloc(&d_bc_prestaged, d_bc_bytes);
        } else {
            // Not enough: skip pre-staging entirely
            fprintf(stderr, "CUZK_TIMING: prestage_setup=skip_vram\n");
            err = cudaErrorMemoryAllocation;  // force fallback
        }

        // Create upload stream and events
        if (err == cudaSuccess)
            err = cudaStreamCreateWithFlags(&upload_stream, cudaStreamNonBlocking);
        if (err == cudaSuccess)
            err = cudaEventCreateWithFlags(&ev_a, cudaEventDisableTiming);
        if (err == cudaSuccess)
            err = cudaEventCreateWithFlags(&ev_b, cudaEventDisableTiming);
        if (err == cudaSuccess)
            err = cudaEventCreateWithFlags(&ev_c, cudaEventDisableTiming);

        // Issue async uploads + zero-pad + record events
        if (err == cudaSuccess) {
            size_t pad_elems = prestage_domain_size - actual_size;
            fr_t* d_b = d_bc_prestaged;
            fr_t* d_c = &d_bc_prestaged[prestage_domain_size * prestage_lot_of_memory];

            err = cudaMemcpyAsync(d_a_prestaged, provers[0].a,
                                  actual_size * sizeof(fr_t),
                                  cudaMemcpyHostToDevice, upload_stream);
            if (err == cudaSuccess && pad_elems > 0)
                err = cudaMemsetAsync(&d_a_prestaged[actual_size], 0,
                                      pad_elems * sizeof(fr_t), upload_stream);
            if (err == cudaSuccess)
                err = cudaEventRecord(ev_a, upload_stream);

            if (err == cudaSuccess)
                err = cudaMemcpyAsync(d_b, provers[0].b,
                                      actual_size * sizeof(fr_t),
                                      cudaMemcpyHostToDevice, upload_stream);
            if (err == cudaSuccess && pad_elems > 0)
                err = cudaMemsetAsync(&d_b[actual_size], 0,
                                      pad_elems * sizeof(fr_t), upload_stream);
            if (err == cudaSuccess)
                err = cudaEventRecord(ev_b, upload_stream);

            if (err == cudaSuccess)
                err = cudaMemcpyAsync(d_c, provers[0].c,
                                      actual_size * sizeof(fr_t),
                                      cudaMemcpyHostToDevice, upload_stream);
            if (err == cudaSuccess && pad_elems > 0)
                err = cudaMemsetAsync(&d_c[actual_size], 0,
                                      pad_elems * sizeof(fr_t), upload_stream);
            if (err == cudaSuccess)
                err = cudaEventRecord(ev_c, upload_stream);
        }

        if (err == cudaSuccess) {
            prestage_ok = true;
            fprintf(stderr, "CUZK_TIMING: prestage_setup=ok domain=%zu lot_of_memory=%d\n",
                    prestage_domain_size, (int)prestage_lot_of_memory);
        } else {
            // Cleanup partial state and fall back to original path
            if (err != cudaErrorMemoryAllocation)
                fprintf(stderr, "CUZK_TIMING: prestage_setup=fallback err=%d\n", (int)err);
            if (ev_c) { cudaEventDestroy(ev_c); ev_c = nullptr; }
            if (ev_b) { cudaEventDestroy(ev_b); ev_b = nullptr; }
            if (ev_a) { cudaEventDestroy(ev_a); ev_a = nullptr; }
            if (upload_stream) { cudaStreamDestroy(upload_stream); upload_stream = nullptr; }
            if (d_bc_prestaged) { cudaFree(d_bc_prestaged); d_bc_prestaged = nullptr; }
            if (d_a_prestaged) { cudaFree(d_a_prestaged); d_a_prestaged = nullptr; }
        }
    }

    for (size_t tid = 0; tid < n_gpus; tid++) {
        per_gpu.emplace_back(std::thread([&, tid, n_gpus, prestage_ok](size_t num_circuits)
        {
            const gpu_t& gpu = select_gpu(tid);

            size_t rem = num_circuits % n_gpus;
            num_circuits /= n_gpus;
            num_circuits += tid < rem;
            size_t circuit0 = tid * num_circuits;
            if (tid >= rem)
                circuit0 += rem;

            try {
                auto t_gpu_start = std::chrono::steady_clock::now();
                {
                    if (prestage_ok) {
                        // Phase 9: Use pre-staged device buffers — no HtoD inside mutex
                        gpu_ptr_t<fr_t> d_a{d_a_prestaged};

                        for (size_t c = circuit0; c < circuit0 + num_circuits; c++) {
#ifndef __CUDA_ARCH__
                            ntt_msm_h::execute_ntt_msm_h_prestaged(
                                gpu, d_a, d_bc_prestaged,
                                prestage_lg_domain, prestage_domain_size,
                                prestage_lot_of_memory,
                                ev_a, ev_b, ev_c,
                                points_h, results.h[c]);
#endif
                            if (caught_exception)
                                return;
                        }
                        // Phase 9: Free d_bc immediately — NTT phase is done,
                        // the b/c data was consumed by coeff_wise_mult and
                        // sub_mult_with_constant. Freeing 8 GiB here is
                        // critical for VRAM headroom during batch_add + tail MSM.
                        if (d_bc_prestaged) {
                            cudaFree(d_bc_prestaged);
                            d_bc_prestaged = nullptr;
                        }
                        // d_a ownership transferred to gpu_ptr_t — freed
                        // by destructor after H MSM when scope exits below.
                    } else {
                        // Original path (fallback or multi-circuit)
                        size_t d_a_sz = sizeof(fr_t) << (lg2(points_h.size() - 1) + 1);
                        gpu_ptr_t<fr_t> d_a{(scalar_t*)gpu.Dmalloc(d_a_sz)};

                        for (size_t c = circuit0; c < circuit0 + num_circuits; c++) {
#ifndef __CUDA_ARCH__
                            ntt_msm_h::execute_ntt_msm_h(gpu, d_a, provers[c],
                                                         points_h,
                                                         results.h[c]);
#endif
                            if (caught_exception)
                                return;
                        }
                    }
                }
                auto t_ntt_h_end = std::chrono::steady_clock::now();
                auto ntt_h_ms = std::chrono::duration_cast<std::chrono::milliseconds>(t_ntt_h_end - t_gpu_start).count();
                fprintf(stderr, "CUZK_TIMING: gpu_tid=%zu ntt_msm_h_ms=%ld\n", tid, ntt_h_ms);

                barrier.wait();

                if (caught_exception)
                    return;

                auto t_batch_add_start = std::chrono::steady_clock::now();
                if (l_split_msm) {
                    // batch addition L - on GPU
                    execute_batch_addition<bucket_t>(gpu, circuit0, num_circuits,
                        points_l, split_vectors_l,
                        &batch_add_res.l[circuit0]);

                    if (caught_exception)
                        return;
                }

                if (a_split_msm) {
                    // batch addition a - on GPU
                    execute_batch_addition<bucket_t>(gpu, circuit0, num_circuits,
                        points_a, split_vectors_a,
                        &batch_add_res.a[circuit0]);

                    if (caught_exception)
                        return;
                }

                if (b_split_msm) {
                    // batch addition b_g1 - on GPU
                    execute_batch_addition<bucket_t>(gpu, circuit0, num_circuits,
                        points_b_g1, split_vectors_b,
                        &batch_add_res.b_g1[circuit0]);

                    if (caught_exception)
                        return;

                    // batch addition b_g2 - on GPU
                    execute_batch_addition<bucket_fp2_t>(gpu, circuit0,
                        num_circuits, points_b_g2,
                        split_vectors_b, &batch_add_res.b_g2[circuit0]);

                    if (caught_exception)
                        return;
                }

                auto t_batch_add_end = std::chrono::steady_clock::now();
                auto batch_add_ms = std::chrono::duration_cast<std::chrono::milliseconds>(t_batch_add_end - t_batch_add_start).count();
                fprintf(stderr, "CUZK_TIMING: gpu_tid=%zu batch_add_ms=%ld\n", tid, batch_add_ms);

                {
                    auto t_tail_msm_start = std::chrono::steady_clock::now();
                    // D4: Per-MSM window size tuning — separate msm_t objects
                    // for each MSM type, so bucket count is tuned to each size.
                    size_t l_msm_size = l_split_msm ? l_popcount : points_l.size();
                    size_t a_msm_size = a_split_msm ? a_popcount : points_a.size();
                    size_t b_msm_size = b_split_msm ? b_popcount : points_b_g1.size();
                    msm_t<bucket_t, point_t, affine_t, scalar_t> msm_l{nullptr, l_msm_size};
                    msm_t<bucket_t, point_t, affine_t, scalar_t> msm_a{nullptr, a_msm_size};
                    msm_t<bucket_t, point_t, affine_t, scalar_t> msm_b{nullptr, b_msm_size};

                    for (size_t c = circuit0; c < circuit0+num_circuits; c++) {
                        // tail MSM l - on GPU
                        if (l_split_msm)
                            msm_l.invoke(results.l[c], tail_msm_l_bases,
                                split_vectors_l.tail_msm_scalars[c], true);
                        else
                            msm_l.invoke(results.l[c], points_l,
                                provers[c].aux_assignment_data, true);

                        if (caught_exception)
                            return;

                        // tail MSM a - on GPU
                        if (a_split_msm)
                            msm_a.invoke(results.a[c], tail_msm_a_bases,
                                split_vectors_a.tail_msm_scalars[c], true);
                        else
                            msm_a.invoke(results.a[c], points_a,
                                split_vectors_a.tail_msm_scalars[c], true);

                        if (caught_exception)
                            return;

                        // tail MSM b_g1 - on GPU
                        if (b_split_msm)
                            msm_b.invoke(results.b_g1[c], tail_msm_b_g1_bases,
                                split_vectors_b.tail_msm_scalars[c], true);
                        else
                            msm_b.invoke(results.b_g1[c], points_b_g1,
                                split_vectors_b.tail_msm_scalars[c], true);

                        if (caught_exception)
                            return;
                    }
                    auto t_tail_msm_end = std::chrono::steady_clock::now();
                    auto tail_msm_ms = std::chrono::duration_cast<std::chrono::milliseconds>(t_tail_msm_end - t_tail_msm_start).count();
                    auto gpu_total_ms = std::chrono::duration_cast<std::chrono::milliseconds>(t_tail_msm_end - t_gpu_start).count();
                    fprintf(stderr, "CUZK_TIMING: gpu_tid=%zu tail_msm_ms=%ld gpu_total_ms=%ld\n", tid, tail_msm_ms, gpu_total_ms);
                }
            } catch (const cuda_error& e) {
                bool already = caught_exception.exchange(true);
                if (!already) {
                    for (size_t i = 1; i < n_gpus; i++)
                        barrier.notify();
#ifdef TAKE_RESPONSIBILITY_FOR_ERROR_MESSAGE
                    ret = RustError{e.code(), e.what()};
#else
                    ret = RustError{e.code()};
#endif
                }
                gpu.sync();
            }
        }, num_circuits));
    }

    // Phase 8: Wait for GPU threads first (CUDA kernel region ends here),
    // then release the GPU lock so another worker can start its kernels.
    // prep_msm_thread's b_g2_msm (CPU-only) may still be running — that's
    // fine, it doesn't need the GPU.
    for (auto& tid : per_gpu)
        tid.join();

    // Phase 9: Free GPU resources BEFORE releasing the mutex, so the next
    // worker doesn't OOM when trying to pre-stage its own buffers.
    // d_a was handed to gpu_ptr_t (already freed when per_gpu scope exited).
    // d_bc, events, and stream must be freed while we still hold the lock.
    if (prestage_ok) {
        if (d_bc_prestaged) { cudaFree(d_bc_prestaged); d_bc_prestaged = nullptr; }
        if (ev_c) { cudaEventDestroy(ev_c); ev_c = nullptr; }
        if (ev_b) { cudaEventDestroy(ev_b); ev_b = nullptr; }
        if (ev_a) { cudaEventDestroy(ev_a); ev_a = nullptr; }
        if (upload_stream) { cudaStreamDestroy(upload_stream); upload_stream = nullptr; }
    }

    // Release GPU lock — another worker can now launch CUDA kernels
    // while we finish b_g2_msm + epilogue on CPU.
    gpu_lock.unlock();

    // Phase 9: Unregister host pages AFTER releasing the lock — this is a
    // CPU-only operation (munlock syscall) and doesn't need GPU exclusivity.
    if (host_c_registered) { cudaHostUnregister((void*)provers[0].c); host_c_registered = false; }
    if (host_b_registered) { cudaHostUnregister((void*)provers[0].b); host_b_registered = false; }
    if (host_a_registered) { cudaHostUnregister((void*)provers[0].a); host_a_registered = false; }

    // Phase 12: All data is already in the handle (results, batch_add_res,
    // split_vectors, tail_msm_bases are aliases to pp->fields).
    // Just store the thread and copy randomness.
    pp->prep_msm_thread = std::move(prep_msm_thread);

    // Copy randomness — the caller's r_s/s_s may be freed before finalize
    pp->r_s_owned.assign(r_s, r_s + num_circuits);
    pp->s_s_owned.assign(s_s, s_s + num_circuits);

    *pending_out = static_cast<void*>(pp);

    return ret;
}
