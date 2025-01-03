// Copyright Supranational LLC

#ifndef __NVME_IO_TRACKER_T_HPP__
#define __NVME_IO_TRACKER_T_HPP__

// Track NVME IO operations
class nvme_io_tracker_t;
typedef int (*completion_cb_t)(void *arg);

class nvme_controller_t;
class nvme_namespace_t;
class nvme_qpair_t;

class nvme_io_tracker_t {
public:
  friend class nvme_controller_t;

  // Must be set by caller of read/write
  uint8_t*           buf;

  // Will bet set internally
  nvme_controller_t* controller;
  nvme_namespace_t*  ns;
  nvme_qpair_t*      qpair;
    
  // Completion callback
  void*              completion_arg;
  completion_cb_t    completion_cb;
    
  nvme_io_tracker_t() {
    controller    = nullptr;
    ns            = nullptr;
    qpair         = nullptr;
    buf           = nullptr;
    completion_cb = nullptr;
  }

  size_t len() {
    return PAGE_SIZE;
  }
};

#endif
