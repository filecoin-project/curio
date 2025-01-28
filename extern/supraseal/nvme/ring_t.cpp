// Copyright Supranational LLC

#include "ring_t.hpp"
#include "nvme.hpp"

void ring_spdk_free(void *ptr) {
  spdk_free(ptr);
}

void* ring_spdk_alloc(size_t bytes) {
 return spdk_dma_zmalloc(bytes, PAGE_SIZE, NULL);
}
