// Copyright Supranational LLC

#ifndef __SUPRA_SEAL_H__
#define __SUPRA_SEAL_H__

#ifdef __cplusplus
extern "C" {
#endif

int supra_version();

// Optional init function.
// config_file - topology config file. Defaults to supra_config.cfg
void supra_seal_init(size_t sector_size, const char* config_file);

// Build tree-r from a last-layer file (optionally with a staged data file) and write outputs to output_dir.
// CUDA-only path; does not require SPDK. Returns 0 on success.
int tree_r_file(const char* last_layer_filename, const char* data_filename, const char* output_dir,
                size_t sector_size);

// Perform pc1, storing the sealed layers starting at block_offset.
int pc1(uint64_t block_offset, size_t num_sectors,
        const uint8_t* replica_ids, const char* parents_filename,
        size_t sector_size);

// Perform pc2 for layers stored starting at block_offset.
// 'data_filenames' may be NULL for all CC sectors, or point to
// an array of length 'num_sectors' of pointers to filenames.
// Any element in the array may be NULL for CC or contain a path
// to a data file.
int pc2(size_t block_offset, size_t num_sectors, const char* output_dir,
        const char** data_filenames, size_t sector_size);

// Delete files associated with pc2
int pc2_cleanup(size_t num_sectors, const char* output_dir, size_t sector_size);

int c1(size_t block_offset,size_t num_sectors, size_t sector_slot,
       const uint8_t* replica_id, const uint8_t* seed,
       const uint8_t* ticket, const char* cache_path,
       const char* parents_filename, const char* replica_path,
       size_t sector_size);

// Returns the highest available block offset, which is the minimum block
// count across all attached NVMe drives, plus one. I.e., the usable blocks
// are [0 .. max).
size_t get_max_block_offset(size_t sector_size);

// Returns the size in blocks required to for the given num_sectors.
size_t get_slot_size(size_t num_sectors, size_t sector_size);

// 32 bytes of space for the comm_ values should be preallocated prior to call

// Returns comm_c after calculating from tree file(s)
bool get_comm_c_from_tree(uint8_t* comm_c, const char* cache_path, size_t sector_size);

// Returns comm_c from p_aux file
bool get_comm_c(uint8_t* comm_c, const char* cache_path);

// Sets comm_c in the p_aux file
bool set_comm_c(uint8_t* comm_c, const char* cache_path);

// Returns comm_r_last after calculating from tree file(s)
bool get_comm_r_last_from_tree(uint8_t* comm_r_last, const char* cache_path, size_t sector_size);

// Returns comm_r_last from p_aux file
bool get_comm_r_last(uint8_t* comm_r_last, const char* cache_path);

// Sets comm_r_last in the p_aux file
bool set_comm_r_last(uint8_t* comm_r_last, const char* cache_path);

// Returns comm_r after calculating from p_aux file
bool get_comm_r(uint8_t* comm_r, const char* cache_path);

// Returns comm_d from tree_d file
bool get_comm_d(uint8_t* comm_d, const char* cache_path);

// Returns comm_d for a cc sector
bool get_cc_comm_d(uint8_t* comm_d, size_t sector_size);

#ifdef __cplusplus
}
#endif

#endif
