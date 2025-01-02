// Copyright Supranational LLC

#ifndef __NVME_HPP__
#define __NVME_HPP__

extern "C" {
#include "spdk/stdinc.h"
#include "spdk/nvme.h"
#include "spdk/vmd.h"
#include "spdk/nvme_zns.h"
#include "spdk/env.h"
#include "spdk/string.h"
#include "spdk/log.h"
}
#include <set>
#include <string>
#include <chrono>
#include <algorithm>
#include <mutex>
#include "ring_t.hpp"

//using namespace std;

const static size_t BLOCK_SIZE = PAGE_SIZE;

extern int g_spdk_error;
extern std::mutex print_mtx;

typedef std::chrono::high_resolution_clock::time_point timestamp_t;

#define SPDK_ERROR(op) \
  { int rc; \
    if ((rc = (op)) != 0) {                     \
      g_spdk_error = rc; \
      printf("SPDK error encountered: %d at %s:%d\n", rc, __FILE__, __LINE__); \
      return rc; \
    } \
  }
#define SPDK_ASSERT(op) \
  { int rc; \
    if ((rc = (op)) != 0) {                     \
      printf("SPDK error encountered: %d at %s:%d\n", rc, __FILE__, __LINE__); \
      assert (rc == 0);                                                 \
    } \
  }

class nvme_controllers_t;
class nvme_controller_t;
class nvme_namespace_t;
class nvme_qpair_t;

#include "nvme_namespace_t.hpp"
#include "nvme_qpair_t.hpp"
#include "nvme_io_tracker_t.hpp"
#include "nvme_controller_t.hpp"
#include "sequential_io_t.hpp"
#include "spdk_ptr_t.hpp"

#endif
