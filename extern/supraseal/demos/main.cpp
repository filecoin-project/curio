// Copyright Supranational LLC

#include <vector>
#include <deque>
#include <fstream>       // file read
#include <iostream>      // printing
#include <cstring>
#include <arpa/inet.h> // htonl
#include <filesystem>

#include "../sealing/supra_seal.hpp"
#include "../util/sector_util.hpp"

uint8_t replica_id_buf_2K[] = { 24, 108, 245, 122, 161, 8, 61, 88, 51, 81, 141, 176, 97, 225, 25, 135, 218, 165, 249, 113, 195, 10, 255, 24, 6, 140, 145, 244, 253, 107, 8, 39 };
uint8_t replica_id_buf_4K[] = { 2, 239, 249, 237, 200, 74, 74, 118, 230, 239, 207, 194, 109, 161, 27, 24, 208, 63, 44, 254, 14, 250, 200, 138, 74, 35, 123, 115, 123, 86, 98, 2 };
uint8_t replica_id_buf_16K[] = { 240, 26, 25, 20, 201, 110, 242, 173, 62, 74, 255, 96, 37, 143, 120, 69, 91, 52, 81, 243, 134, 37, 112, 41, 27, 213, 208, 145, 107, 149, 76, 52 };
uint8_t replica_id_buf_32K[] = { 50, 213, 77, 230, 65, 212, 193, 39, 25, 125, 41, 233, 147, 28, 126, 201, 217, 162, 65, 39, 132, 252, 61, 245, 39, 34, 32, 38, 158, 149, 24, 24 };
uint8_t replica_id_buf_8M[] = { 23, 124, 26, 248, 237, 136, 178, 226, 193, 239, 173, 27, 131, 214, 147, 242, 18, 110, 7, 252, 4, 245, 118, 152, 94, 125, 73, 140, 25, 102, 152, 57 };
uint8_t replica_id_buf_16M[] = { 0, 104, 11, 183, 198, 151, 180, 179, 187, 46, 233, 221, 244, 44, 204, 221, 108, 14, 17, 49, 254, 229, 229, 252, 200, 102, 16, 240, 84, 175, 220, 52 };
uint8_t replica_id_buf_512M[] = { 37, 249, 121, 174, 70, 206, 91, 232, 165, 246, 66, 184, 198, 10, 232, 126, 215, 171, 221, 76, 26, 2, 117, 118, 201, 142, 116, 143, 25, 131, 167, 37 };
uint8_t replica_id_buf_1G[] = { 36, 67, 76, 192, 211, 223, 90, 159, 60, 141, 212, 178, 36, 120, 21, 93, 28, 92, 79, 231, 31, 100, 115, 240, 114, 152, 20, 78, 80, 158, 122, 34 };
uint8_t replica_id_buf_32G[] = { 121, 145, 135, 251, 187, 117, 51, 109, 88, 99, 80, 105, 79, 235, 85, 240, 147, 153, 120, 231, 144, 247, 244, 201, 42, 10, 149, 142, 203, 151, 188, 43 };
uint8_t replica_id_buf_64G[] = { 96, 159, 133, 62, 63, 177, 24, 234, 146, 31, 140, 109, 39, 48, 219, 3, 168, 169, 249, 98, 25, 210, 33, 210, 4, 217, 45, 216, 99, 90, 114, 4 };

// This ultimately comes from the sealing flows
const char* get_parent_filename(size_t sector_size_lg) {
  switch (sector_size_lg) {
  case SectorSizeLg::Sector2KB:
    // 2KB
    return "/var/tmp/filecoin-parents/v28-sdr-parent-652bae61e906c0732e9eb95b1217cfa6afcce221ff92a8aedf62fa778fa765bc.cache";
  case SectorSizeLg::Sector4KB:
    // 4KB
    return "/var/tmp/filecoin-parents/v28-sdr-parent-56d4865ec3476221fd1412409b5d9439182d71bf5e2078d0ecde76c0f7e33986.cache";
  case SectorSizeLg::Sector16KB:
    // 16KB
    return "/var/tmp/filecoin-parents/v28-sdr-parent-41059e359f8a8b479f9e29bdf20344fcd43d9c03ce4a7d01daf2c9a77909fd4f.cache";
  case SectorSizeLg::Sector32KB:
    // 32KB
    return "/var/tmp/filecoin-parents/v28-sdr-parent-81a0489b0dd6c7755cdce0917dd436288b6e82e17d596e5a23836e7a602ab9be.cache";
  case SectorSizeLg::Sector8MB:
    // 8MB
    return "/var/tmp/filecoin-parents/v28-sdr-parent-1139cb33af3e3c24eb644da64ee8bc43a8df0f29fc96b5337bee369345884cdc.cache";
  case SectorSizeLg::Sector16MB:
    // 16MB
    return "/var/tmp/filecoin-parents/v28-sdr-parent-7fa3ff8ffb57106211c4be413eb15ea072ebb363fa5a1316fe341ac8d7a03d51.cache";
  case SectorSizeLg::Sector512MB:
    // 512MB
    return "/var/tmp/filecoin-parents/v28-sdr-parent-7ba215a1d2345774ab90b8cb1158d296e409d6068819d7b8c7baf0b25d63dc34.cache";
  case SectorSizeLg::Sector1GB:
    // 1GB
    return "/var/tmp/filecoin-parents/v28-sdr-parent-637f021bceb5248f0d1dcf4dbf132fedc025d0b3b55d3e7ac171c02676a96ccb.cache";
  case SectorSizeLg::Sector32GB:
    // 32GB
    return "/var/tmp/filecoin-parents/v28-sdr-parent-21981246c370f9d76c7a77ab273d94bde0ceb4e938292334960bce05585dc117.cache";
  case SectorSizeLg::Sector64GB:
    // 64GB
    return "/var/tmp/filecoin-parents/v28-sdr-parent-767ee5400732ee77b8762b9d0dd118e88845d28bfa7aee875dc751269f7d0b87.cache";
  default:
    printf("ERROR: unknown sector size lg %ld\n", sector_size_lg);
    return nullptr;
  }
}

template<class P>
void demo_pipeline(size_t num_sectors, uint8_t* replica_ids) {
  size_t slot0 = 0;
  size_t slot1 = get_slot_size(num_sectors, P::GetSectorSize()) * 1;
  const char* parent_filename = get_parent_filename(P::GetSectorSizeLg());
  const char* output_dir0 = "/var/tmp/supra_seal/0";
  const char* output_dir1 = "/var/tmp/supra_seal/1";

  printf("slot0 %08lx\n", slot0);
  printf("slot1 %08lx\n", slot1);

  // Fill slot0 pc1
  printf("Starting slot0 pc1\n");
  pc1(slot0, num_sectors, replica_ids, parent_filename, P::GetSectorSize());

  // Slot0 PC2 + slot1 pc1
  std::thread j0([&]() {
    printf("Starting slot1 pc1\n");
    pc1(slot1, num_sectors, replica_ids, parent_filename, P::GetSectorSize());
  });
  std::thread j1([&]() {
    printf("Starting slot0 pc2\n");
    pc2(slot0, num_sectors, output_dir0, nullptr, P::GetSectorSize());
  });
  j0.join();
  j1.join();

  // slot1 pc2
  printf("Starting slot1 pc2\n");
  pc2(slot1, num_sectors, output_dir1, nullptr, P::GetSectorSize());
}

int main(int argc, char** argv) {
  uint64_t node_to_read = 0;
  uint64_t slot = 0;
  size_t   num_sectors = 64;
  std::string sector_size_string = "";
  const char* output_dir = "/var/tmp/supra_seal/0";

  enum { SEAL_MODE, READ_MODE, PARENTS_MODE, PIPELINE_MODE } mode = PIPELINE_MODE;
  bool perform_pc1 = false;
  bool perform_pc2 = false;
  bool perform_c1 = false;

  int opt;
  while ((opt = getopt(argc, argv, "123r:s:n:po:b:h")) != -1) {
    switch (opt) {
    case '1':
      mode = SEAL_MODE;
      perform_pc1 = true;
      break;
    case '2':
      mode = SEAL_MODE;
      perform_pc2 = true;
      break;
    case '3':
      mode = SEAL_MODE;
      perform_c1 = true;
      break;
    case 'r':
      mode = READ_MODE;
      node_to_read = strtol(optarg, NULL, 16);
      break;
    case 's':
      slot = strtol(optarg, NULL, 16);
      break;
    case 'o':
      output_dir = optarg;
      break;
    case 'n':
      num_sectors = strtol(optarg, NULL, 10);
      break;
    case 'p':
      mode = PIPELINE_MODE;
      break;
    case 'b':
      sector_size_string = optarg;
      break;
    case 'h':
      printf("Usage: sudo ./seal [options]\n");
      printf("  -1 - perform pc1\n");
      printf("  -2 - perform pc2\n");
      printf("  -3 - perform c1\n");
      printf("  -p - perform pc1, pc2, and c1 pipeline (default)\n");
      printf("  -n - number of parallel sectors (default 64)\n");
      printf("  -b - sector size e.g 32GiB\n");
      exit(0);
      break;
    }
  }

  if (sector_size_string == "") {
    printf("Please specify a sector size\n");
    exit(0);
  }

  size_t sector_size = get_sector_size_from_string(sector_size_string);
  size_t sector_size_lg;

  SECTOR_PARAMS_TABLE(sector_size_lg = params.GetSectorSizeLg());

  supra_seal_init(sector_size, "demos/rust/supra_seal.cfg");

  // // 512mb random data
  // uint8_t replica_id_buf_512M[] = {
  //   89, 186, 126, 238, 239, 37, 73, 20,
  //   148, 180, 147, 227, 154, 153, 224, 173,
  //   101, 206, 212, 202, 229, 49, 100, 20,
  //   19, 156, 251, 17, 68, 212, 238, 32
  // };

  uint8_t* replica_id_buf;
  switch (sector_size_lg) {
    case (size_t)SectorSizeLg::Sector2KB:
      replica_id_buf = replica_id_buf_2K;
      break;
    case (size_t)SectorSizeLg::Sector16KB:
      replica_id_buf = replica_id_buf_16K;
      break;
    case (size_t)SectorSizeLg::Sector8MB:
      replica_id_buf = replica_id_buf_8M;
      break;
    case (size_t)SectorSizeLg::Sector512MB:
      replica_id_buf = replica_id_buf_512M;
      break;
    case (size_t)SectorSizeLg::Sector32GB:
      replica_id_buf = replica_id_buf_32G;
      break;
    case (size_t)SectorSizeLg::Sector64GB:
      replica_id_buf = replica_id_buf_64G;
      break;
    default:
      replica_id_buf = replica_id_buf_2K;
      break;
  }
  uint8_t* replica_ids = new uint8_t[num_sectors * sizeof(replica_id_buf_2K)];
  assert (replica_ids != nullptr);
  for (size_t i = 0; i < num_sectors; i++) {
    memcpy(&replica_ids[sizeof(replica_id_buf_2K) * i],
           replica_id_buf, sizeof(replica_id_buf_2K));
  }

  if (mode == PIPELINE_MODE) {
    SECTOR_PARAMS_TABLE(demo_pipeline<decltype(params)>(num_sectors, replica_ids));
    exit(0);
  }

  printf("mode %d, node_to_read %lx, slot %lx, num_sectors %ld\n",
         mode, node_to_read, slot, num_sectors);

  size_t block_offset = get_slot_size(num_sectors, sector_size) * slot;
  node_to_read += block_offset;

  // Perform sealing
  if (mode == SEAL_MODE) {
    if (perform_pc1) {
      pc1(block_offset, num_sectors, replica_ids,
          get_parent_filename(sector_size_lg), sector_size);
    }

    if (perform_pc2) {
      pc2(block_offset, num_sectors, output_dir, nullptr, sector_size);
    }

    if (perform_c1) {
      std::string replica_cache_path = output_dir;
      replica_cache_path += "/replicas";
      if (!std::filesystem::exists(replica_cache_path.c_str())) {
        replica_cache_path = output_dir;
      }

      auto start = std::chrono::high_resolution_clock::now();
      for (size_t i = 0; i < num_sectors; i++) {
        const size_t MAX = 256;
        char sector_output_dir[MAX];
        snprintf(sector_output_dir, MAX, "%s/%03ld", output_dir, i);
        char sector_replica_dir[MAX];
        snprintf(sector_replica_dir, MAX, "%s/%03ld", replica_cache_path.c_str(), i);

        c1(block_offset, num_sectors, i, replica_ids, SEED,
           TICKET, sector_output_dir, get_parent_filename(sector_size_lg),
           sector_replica_dir, sector_size);
      }
      auto stop = std::chrono::high_resolution_clock::now();
      uint64_t secs = std::chrono::duration_cast<
        std::chrono::seconds>(stop - start).count();
      printf("c1 took %ld seconds\n", secs);
    }
  } else if (mode == READ_MODE) {
    node_read(sector_size, num_sectors, node_to_read);
  }

  exit(0);
}
