// Copyright Supranational LLC

#include <ntt/ntt.cuh>

__launch_bounds__(1024)
__global__ void coeff_wise_mult(fr_t* a, const fr_t* b, uint32_t lg_domain_size)
{
    uint32_t idx0 = threadIdx.x + blockIdx.x * blockDim.x;
    size_t limit = (size_t)1 << lg_domain_size;

    for (size_t idx = idx0; idx < limit; idx += blockDim.x * gridDim.x)
        a[idx] *= b[idx];
}

__launch_bounds__(1024)
__global__ void sub_mult_with_constant(fr_t* a, const fr_t* c, fr_t z,
                                       uint32_t lg_domain_size)
{
    uint32_t idx0 = threadIdx.x + blockIdx.x * blockDim.x;
    size_t limit = (size_t)1 << lg_domain_size;

    for (size_t idx = idx0; idx < limit; idx += blockDim.x * gridDim.x) {
        fr_t r = a[idx] - c[idx];
        a[idx] = r * z;
    }
}

#ifndef __CUDA_ARCH__

const size_t gib = (size_t)1 << 30;

class ntt_msm_h : public NTT {
private:
    static fr_t calculate_z_inv(size_t lg_domain_size) {
        fr_t gen_pow = group_gen;
        while (lg_domain_size--)
            gen_pow ^= 2;
        return (gen_pow - fr_t::one()).reciprocal();
    }

    static void execute_ntts_single(fr_t* d_inout, const fr_t* in,
                                    size_t lg_domain_size, size_t actual_size,
                                    stream_t& stream)
    {
        size_t domain_size = (size_t)1 << lg_domain_size;

        assert(actual_size <= domain_size);

        stream.HtoD(&d_inout[0], in, actual_size);

        if (actual_size < domain_size) {
            cudaMemsetAsync(&d_inout[actual_size], 0,
                (domain_size - actual_size) * sizeof(fr_t), stream);
        }

        NTT_internal(&d_inout[0], lg_domain_size,
            NTT::InputOutputOrder::NR, NTT::Direction::inverse,
            NTT::Type::standard, stream);
        NTT_internal(&d_inout[0], lg_domain_size,
            NTT::InputOutputOrder::RN, NTT::Direction::forward,
            NTT::Type::coset, stream);
    }

    static int lg2(size_t n)
    {   int ret = 0; while (n >>= 1) ret++; return ret;   }

public:

    // a, b, c = coset_ntt(intt(a, b, c))
    // a *= b
    // a -= c
    // a[i] /= (multiplicative_gen^domain_size) - 1
    // a = coset_intt(a)
    // a is the result vector
    static void execute_ntt_msm_h(const gpu_t& gpu, gpu_ptr_t<fr_t> d_a,
                                  const Assignment<fr_t>& input,
                                  slice_t<affine_t> points_h,
                                  point_t& result_h)
    {
        size_t actual_size = input.abc_size;
        size_t npoints = points_h.size();
        size_t lg_domain_size = lg2(npoints - 1) + 1;
        size_t domain_size = (size_t)1 << lg_domain_size;

        fr_t z_inv = calculate_z_inv(lg_domain_size);

        int sm_count = gpu.props().multiProcessorCount;

        bool lot_of_memory = 3 * domain_size * sizeof(fr_t) <
                             gpu.props().totalGlobalMem - gib;
        {
            dev_ptr_t<fr_t> d_b(domain_size * (lot_of_memory + 1));
            fr_t* d_c = &d_b[domain_size * lot_of_memory];

            event_t sync_event;

            execute_ntts_single(&d_a[0], input.a, lg_domain_size,
                                actual_size, gpu[0]);
            sync_event.record(gpu[0]);

            execute_ntts_single(&d_b[0], input.b, lg_domain_size,
                                actual_size, gpu[1]);

            sync_event.wait(gpu[1]);
            coeff_wise_mult<<<sm_count, 1024, 0, gpu[1]>>>
                (&d_a[0], &d_b[0], (index_t)lg_domain_size);
            sync_event.record(gpu[1]);

            execute_ntts_single(&d_c[0], input.c, lg_domain_size,
                                actual_size, gpu[1 + lot_of_memory]);

            sync_event.wait(gpu[1 + lot_of_memory]);
            sub_mult_with_constant<<<sm_count, 1024, 0, gpu[1 + lot_of_memory]>>>
                (&d_a[0], &d_c[0], z_inv, (index_t)lg_domain_size);
        }

        NTT_internal(&d_a[0], lg_domain_size, NTT::InputOutputOrder::NN,
            NTT::Direction::inverse, NTT::Type::coset, gpu[1 + lot_of_memory]);

        gpu[1 + lot_of_memory].sync();

        msm_t<bucket_t, point_t, affine_t, fr_t> msm(nullptr, npoints);
        msm.invoke(result_h, points_h, d_a, true);
    }
};

#endif
