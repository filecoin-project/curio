// Copyright Supranational LLC

#ifndef __SEQUENTIAL_IO_T_HPP__
#define __SEQUENTIAL_IO_T_HPP__

// Simple interface to read sequential data from an NVME drive

class sequential_io_t {
  nvme_controller_t& controller;

  struct cb_t {
    nvme_io_tracker_t io;
    sequential_io_t*  me;
  };
  pool_t<cb_t>       cb_pool;
  
  static int completion_cb(void *arg) {
    cb_t* cb = (cb_t *)arg;
    SPDK_ERROR(cb->me->cb_pool.enqueue(cb));
    return 0;
  }

public:
  sequential_io_t(nvme_controller_t& _controller):
    controller(_controller) {
    // Allocate io trackers
    SPDK_ASSERT(cb_pool.create(nvme_controller_t::queue_size));
    SPDK_ASSERT(cb_pool.fill());
    for (size_t i = 0; i < cb_pool.size(); i++) {
      cb_pool[i].me = this;
    }
  }

  // buf must be pages sized and pinned
  int rw(bool read, size_t pages, uint8_t *buf, size_t offset = 0) {
    size_t page = 0;
    while (page < pages) {
      // Initiate ops
      while (page < pages) {
        cb_t *cb = cb_pool.dequeue();
        if (cb == nullptr) {
          break;
        }
        cb->io.buf = &(buf[page * PAGE_SIZE]);
        if (read){
          SPDK_ERROR(controller.read(&cb->io, 0, 0, page + offset,
                                     completion_cb, cb));
        } else {
          SPDK_ERROR(controller.write(&cb->io, 0, 0, page + offset,
                                      completion_cb, cb));
        }
        page++;
      }
      // Complete reads
      controller.process_completions(0);
    }
    controller.process_all_completions(0);
    return 0;
  }
};

#endif
