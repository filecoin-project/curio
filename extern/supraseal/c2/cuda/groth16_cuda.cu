// Copyright Supranational LLC

#include <iostream>
#include <thread>
#include <vector>
#include <mutex>
#include <algorithm>

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

static thread_pool_t groth16_pool;

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

extern "C"
RustError::by_value generate_groth16_proofs_c(const Assignment<fr_t> provers[],
                                              size_t num_circuits,
                                              const fr_t r_s[], const fr_t s_s[],
                                              groth16_proof proofs[], SRS& srs)
{
    // Mutex to serialize execution of this subroutine
    static std::mutex mtx;
    std::lock_guard<std::mutex> lock(mtx);

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

    bool l_split_msm = true, a_split_msm = true,
         b_split_msm = true;
    size_t l_popcount = 0, a_popcount = 0, b_popcount = 0;

    split_vectors split_vectors_l{num_circuits, points_l.size()};
    split_vectors split_vectors_a{num_circuits, points_a.size()};
    split_vectors split_vectors_b{num_circuits, points_b_g1.size()};

    std::vector<affine_t> tail_msm_l_bases,
                          tail_msm_a_bases,
                          tail_msm_b_g1_bases;
    std::vector<affine_fp2_t> tail_msm_b_g2_bases;

    msm_results results{num_circuits};

    semaphore_t barrier;
    std::atomic<bool> caught_exception{false};
    size_t n_gpus = std::min(ngpus(), num_circuits);

    std::thread prep_msm_thread([&, num_circuits]
    {
        // pre-processing step
        // mark inp and significant scalars in aux assignments
        groth16_pool.par_map(num_circuits, [&](size_t c) {
            auto& prover = provers[c];
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
        groth16_pool.par_map(num_circuits, [&](size_t c) {
            auto& prover = provers[c];
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

        for (size_t i = 0; i < n_gpus; i++)
            barrier.notify();

        if (caught_exception)
            return;

        // tail MSM b_g2 - on CPU
        for (size_t c = 0; c < num_circuits; c++) {
#ifndef __CUDA_ARCH__
            mult_pippenger<bucket_fp2_t>(results.b_g2[c],
                b_split_msm ? tail_msm_b_g2_bases.data() :
                              points_b_g2.data(),
                split_vectors_b.tail_msm_scalars[c].size(),
                split_vectors_b.tail_msm_scalars[c].data(),
                true, &groth16_pool);
#endif

            if (caught_exception)
                return;
        }
    });

    batch_add_results batch_add_res{num_circuits};
    std::vector<std::thread> per_gpu;
    RustError ret{cudaSuccess};

    for (size_t tid = 0; tid < n_gpus; tid++) {
        per_gpu.emplace_back(std::thread([&, tid, n_gpus](size_t num_circuits)
        {
            const gpu_t& gpu = select_gpu(tid);

            size_t rem = num_circuits % n_gpus;
            num_circuits /= n_gpus;
            num_circuits += tid < rem;
            size_t circuit0 = tid * num_circuits;
            if (tid >= rem)
                circuit0 += rem;

            try {
                {
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

                barrier.wait();

                if (caught_exception)
                    return;

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

                {
                    msm_t<bucket_t, point_t, affine_t, scalar_t> msm{nullptr,
                        (l_popcount + a_popcount + b_popcount) / 3};

                    for (size_t c = circuit0; c < circuit0+num_circuits; c++) {
                        // tail MSM l - on GPU
                        if (l_split_msm)
                            msm.invoke(results.l[c], tail_msm_l_bases,
                                split_vectors_l.tail_msm_scalars[c], true);
                        else
                            msm.invoke(results.l[c], points_l,
                                provers[c].aux_assignment_data, true);

                        if (caught_exception)
                            return;

                        // tail MSM a - on GPU
                        if (a_split_msm)
                            msm.invoke(results.a[c], tail_msm_a_bases,
                                split_vectors_a.tail_msm_scalars[c], true);
                        else
                            msm.invoke(results.a[c], points_a,
                                split_vectors_a.tail_msm_scalars[c], true);

                        if (caught_exception)
                            return;

                        // tail MSM b_g1 - on GPU
                        if (b_split_msm)
                            msm.invoke(results.b_g1[c], tail_msm_b_g1_bases,
                                split_vectors_b.tail_msm_scalars[c], true);
                        else
                            msm.invoke(results.b_g1[c], points_b_g1,
                                split_vectors_b.tail_msm_scalars[c], true);

                        if (caught_exception)
                            return;
                    }
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

    prep_msm_thread.join();
    for (auto& tid : per_gpu)
        tid.join();

    if (caught_exception)
        return ret;

    for (size_t circuit = 0; circuit < num_circuits; circuit++) {
        if (l_split_msm)
            results.l[circuit].add(batch_add_res.l[circuit]);
        if (a_split_msm)
            results.a[circuit].add(batch_add_res.a[circuit]);
        if (b_split_msm) {
            results.b_g1[circuit].add(batch_add_res.b_g1[circuit]);
            results.b_g2[circuit].add(batch_add_res.b_g2[circuit]);
        }

        fr_t r = r_s[circuit], s = s_s[circuit];
        fr_t rs = r * s;
        // we want the scalars to be in Montomery form when passing them to
        // "mult" routine

        point_t g_a, g_c, a_answer, b1_answer, vk_delta_g1_rs, vk_alpha_g1_s,
                vk_beta_g1_r;
        point_fp2_t g_b;

        mult(vk_delta_g1_rs, vk->delta_g1, rs);
        mult(vk_alpha_g1_s, vk->alpha_g1, s);
        mult(vk_beta_g1_r, vk->beta_g1, r);

        mult(b1_answer, results.b_g1[circuit], r);

        // A
        mult(g_a, vk->delta_g1, r);
        g_a.add(vk->alpha_g1);
        g_a.add(results.a[circuit]);

        // B
        mult(g_b, vk->delta_g2, s);
        g_b.add(vk->beta_g2);
        g_b.add(results.b_g2[circuit]);

        // C
        mult(g_c, results.a[circuit], s);
        g_c.add(b1_answer);
        g_c.add(vk_delta_g1_rs);
        g_c.add(vk_alpha_g1_s);
        g_c.add(vk_beta_g1_r);
        g_c.add(results.h[circuit]);
        g_c.add(results.l[circuit]);

        // to affine
        proofs[circuit].a = g_a;
        proofs[circuit].b = g_b;
        proofs[circuit].c = g_c;
    }

    return ret;
}
