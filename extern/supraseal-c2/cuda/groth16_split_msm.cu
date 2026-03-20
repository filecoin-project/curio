// Copyright Supranational LLC

#include <msm/batch_addition.cuh>

template __global__
void batch_addition<bucket_t>(bucket_t::mem_t ret_[],
                              const affine_t::mem_t points_[], uint32_t npoints,
                              const uint32_t bitmap[], bool accumulate,
                              uint32_t sid);

template __global__
void batch_addition<bucket_fp2_t>(bucket_fp2_t::mem_t ret_[],
                                  const affine_fp2_t::mem_t points_[],
                                  uint32_t npoints, const uint32_t bitmap[],
                                  bool accumulate, uint32_t sid);

struct batch_add_results {
    std::vector<point_t> l;
    std::vector<point_t> a;
    std::vector<point_t> b_g1;
    std::vector<point_fp2_t> b_g2;

    batch_add_results(size_t num_circuits) : l(num_circuits),
                                             a(num_circuits),
                                             b_g1(num_circuits),
                                             b_g2(num_circuits) { }
};

template<typename T> class uninit {
    T val;
public:
    uninit()            { } // don't zero std::vector<uninit<T>>
    uninit(T v)         { val = v; }
    operator T() const  { return val; }
};

using mask_t = uninit<uint64_t>;

const size_t CHUNK_BITS = sizeof(mask_t) * 8; // 64 bits

#define NUM_BATCHES 8
#define GPU_DIV (32*WARP_SZ)

class split_vectors {
public:
    std::vector<std::vector<mask_t>> bit_vector;
    std::vector<std::vector<fr_t>>   tail_msm_scalars;
    size_t batch_size, bit_vector_size;

    split_vectors(size_t num_circuits, size_t num_points)
        :   bit_vector{num_circuits},
            tail_msm_scalars{num_circuits}
    {
        batch_size = (num_points + GPU_DIV - 1) / GPU_DIV;
        batch_size = (batch_size + NUM_BATCHES - 1) / NUM_BATCHES;
        batch_size *= GPU_DIV;

        bit_vector_size = (num_points + CHUNK_BITS - 1) / CHUNK_BITS;

        for (size_t c = 0; c < num_circuits; c++) {
            bit_vector[c].resize(bit_vector_size);
        }
    }

    void tail_msms_resize(size_t num_sig_scalars) {
        size_t num_circuits = tail_msm_scalars.size();
        for (size_t c = 0; c < num_circuits; c++) {
            tail_msm_scalars[c].resize(num_sig_scalars);
        }
    }
};

template<class bucket_t,
         class point_t,
         class bucket_h = class bucket_t::mem_t,
         class affine_t = class bucket_t::affine_t,
         class affine_h = class bucket_t::affine_t::mem_t>
void execute_batch_addition(const gpu_t& gpu,
                            size_t circuit0, size_t num_circuits,
                            slice_t<affine_t> points,
                            const split_vectors& split_vector,
                            point_t batch_add_res[])
{
    int sm_count = gpu.sm_count();

    uint32_t nbuckets = sm_count * BATCH_ADD_BLOCK_SIZE / WARP_SZ;

    uint32_t bit_vector_size = (split_vector.bit_vector_size + WARP_SZ - 1) & (0u - WARP_SZ);
    size_t batch_size = split_vector.batch_size;

    assert(batch_size == (uint32_t)batch_size);

    size_t d_points_size = batch_size * 2 * sizeof(affine_h);
    size_t d_buckets_size = num_circuits * nbuckets * sizeof(bucket_h);

    dev_ptr_t<uint8_t> d_temp{d_points_size + d_buckets_size +
                              num_circuits * bit_vector_size * sizeof(mask_t)};

    vec2d_t<affine_h> d_points{&d_temp[0], (uint32_t)batch_size};
    vec2d_t<bucket_h> d_buckets{&d_temp[d_points_size], nbuckets};
    vec2d_t<mask_t>   d_bit_vectors{&d_temp[d_points_size + d_buckets_size],
                                    bit_vector_size};

    uint32_t sid = 0;

    for (size_t c = 0; c < num_circuits; c++)
        gpu[sid].HtoD(d_bit_vectors[c], split_vector.bit_vector[circuit0 + c]);

    size_t npoints = points.size();
    for (uint32_t batch = 0; npoints > 0; batch++, sid ^= 1) {
        uint32_t amount = std::min(npoints, batch_size);
        size_t cursor = batch * batch_size;

        gpu[sid].HtoD(d_points[sid], &points[cursor], amount);

        for (size_t c = 0; c < num_circuits; c++)
            gpu[sid].launch_coop(batch_addition<bucket_t>,
                {sm_count, BATCH_ADD_BLOCK_SIZE},
                d_buckets[c], (const affine_h*)d_points[sid], amount,
                (const uint32_t*)&d_bit_vectors[c][cursor / CHUNK_BITS],
                batch > 0, sid);

        npoints -= amount;
    }
    sid ^= 1;

    vec2d_t<bucket_t> buckets{nbuckets, num_circuits};
    gpu[sid].DtoH(buckets[0], d_buckets[0], num_circuits * nbuckets);
    gpu[sid].sync();

    gpu.par_map(num_circuits, 1, [&, batch_add_res, nbuckets](size_t c) {
        batch_add_res[c] = sum_up(buckets[c], nbuckets);
    });
}
