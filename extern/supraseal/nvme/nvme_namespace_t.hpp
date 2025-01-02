// Copyright Supranational LLC

#ifndef __NVME_NAMESPACE_T__
#define __NVME_NAMESPACE_T__

class nvme_namespace_t {
  struct   spdk_nvme_ns* ns;
  uint32_t sector_size;
  
public:
  nvme_namespace_t(struct spdk_nvme_ns* _ns) {
    ns = _ns;
    sector_size = spdk_nvme_ns_get_sector_size(ns);
  }

  struct spdk_nvme_ns* get_ns() {
    return ns;
  }

  size_t get_page_count() {
    return spdk_nvme_ns_get_size(ns) / PAGE_SIZE;
  }

  size_t get_sector_count() {
    return spdk_nvme_ns_get_size(ns) / get_sector_size();
  }
  
  uint32_t get_sector_size() {
    return sector_size;
  }
  
  void print() {
    printf("  Namespace ID: %d size: %juGB\n",
           spdk_nvme_ns_get_id(ns),
           spdk_nvme_ns_get_size(ns) / 1000000000);
  }
};

#endif
