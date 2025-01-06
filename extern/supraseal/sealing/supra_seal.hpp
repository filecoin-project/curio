// Copyright Supranational LLC

#ifndef __SUPRA_SEAL_HPP__
#define __SUPRA_SEAL_HPP__

#include <mutex>
#include "../util/stats.hpp"
#include "../sealing/constants.hpp"
#include "../nvme/nvme.hpp"
#include "../sealing/data_structures.hpp"

// Forward declarations
template<class C> class coordinator_t;
template<class C, class B> class node_rw_t;
template<class C> class orchestrator_t;

const size_t STATS_PERIOD = 1<<22;
const size_t STATS_MASK   = STATS_PERIOD - 1;

extern std::mutex print_mtx;

#include "../util/debug_helpers.hpp"
#include "topology_t.hpp"
#include "../pc1/system_buffers_t.hpp"
#include "../pc1/parent_iter_t.hpp"
#include "../pc1/orchestrator_t.hpp"
#include "../pc1/node_rw_t.hpp"
#include "../pc1/coordinator_t.hpp"
#include "supra_seal.h"

int node_read(size_t sector_size, size_t num_sectors, uint64_t node_to_read);

#endif
