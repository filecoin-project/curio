// Copyright Supranational LLC

#ifndef __NODE_RW_T_HPP__
#define __NODE_RW_T_HPP__

#include "../util/stats.hpp"

///////////////////////////////////////////////////////////////////////////
//
// On disk node storage and addressing
//
//       |        block      |
//       | n0 | n1 | n2 | n3 |
// ctrl0 | 00 | 01 | 02 | 03 | 16 | 17 | 18 | 19 |
// ctrl1 | 04 | 05 | 06 | 07 |      ...
// ctrl2 | 08 | 09 | 10 | 11 |
// ctrl3 | 12 | 13 | 14 | 15 |
//
// block = node / NODES_PER_PAGE
// ctrl  = block % NUM_CTRLS
//
///////////////////////////////////////////////////////////////////////////

template<class C>
inline void nvme_node_indexes(size_t num_controllers, size_t node,
                              size_t &ctrl_id, size_t &block_on_controller) {
  size_t block = node / C::NODES_PER_PAGE;
  block_on_controller = block / num_controllers;
  ctrl_id = block - block_on_controller * num_controllers;
}

// Process read/write IO requests
// Templated with a config and batch_t specialization
template<class C, class B>
class node_rw_t {
  std::atomic<bool>& terminator;
  nvme_controllers_t& controllers;

private:
  // FIFO of requests
  mt_fifo_t<B>& parent_read_fifo;
  // Qpair to use for reads/writes
  size_t        qpair_id;
  // Block offset for all reads/writes
  size_t        block_offset;

  // Stats counters
  uint64_t ios_issued;
  uint64_t ios_completed;
  uint64_t parent_read_fifo_empty;
  uint64_t disk_queues_full;
  
  queue_stat_t parent_read_fifo_avail_stats;
  queue_stat_t min_free_stats;
  queue_stat_t num_issue_stats;
  
public:
  node_rw_t(std::atomic<bool>& _terminator,
            nvme_controllers_t& _controllers,
            mt_fifo_t<B>& _parent_read_fifo,
            size_t _qpair_id,
            size_t _block_offset
            ):
    terminator(_terminator),
    controllers(_controllers),
    parent_read_fifo(_parent_read_fifo)
  {
    qpair_id = _qpair_id;
    block_offset = _block_offset;
  }

  void cleanup() {
  }
  
  int init() {
    parent_read_fifo_avail_stats.init("r:rw_fifo_avail", 0);
    min_free_stats.init("r:min_free", 0);
    num_issue_stats.init("r:num_issue", 0);
    return 0;
  }
  void clear_stats() {
    parent_read_fifo_avail_stats.clear();
    min_free_stats.clear();
    num_issue_stats.clear();
  }
  void snapshot() {
    parent_read_fifo_avail_stats.snapshot();
    min_free_stats.snapshot();
    num_issue_stats.snapshot();
  }
  void print() {
    parent_read_fifo_avail_stats.print();
    min_free_stats.print();
    num_issue_stats.print();
  }

private:
  static int completion_cb(void *arg) {
    node_io_t* io = (node_io_t *)arg;
    // Set the valid bit
    io->valid->fetch_add(1, DEFAULT_MEMORY_ORDER);
    return 0;
  }

public:
  // Process IO requests
  int process(size_t idle_sleep = 0, size_t duty_cycle = 0) {
    // Reset stats
    ios_issued = 0;
    ios_completed = 0;
    parent_read_fifo_empty = 0;
    disk_queues_full = 0;

    // Data collection
    size_t outstanding_counters[controllers.size()];
    size_t total_counters[controllers.size()];
    size_t samples = 0;
    const size_t interval = 250;
    size_t delay_count = 0;
    for (size_t ctrl_id = 0; ctrl_id < controllers.size(); ctrl_id++) {
      outstanding_counters[ctrl_id] = 0;
      total_counters[ctrl_id] = 0;
    }

    size_t iter_count = 0;
      
    // Run
    while (!terminator) {
      // Track whether we are able to do any work
      bool dequed_any = false;

      // Determine open disk io slots so we know we can dispatch 
      size_t min_free = nvme_controller_t::queue_size -
        controllers[0].get_outstanding_io_ops(qpair_id);
      for (size_t ctrl_id = 1; ctrl_id < controllers.size(); ctrl_id++) {
        size_t free_slots = nvme_controller_t::queue_size -
          controllers[ctrl_id].get_outstanding_io_ops(qpair_id);
        min_free = std::min(min_free, free_slots);
      }
      // min_free = best_disk_free;
      if (delay_count == interval) {
        for (size_t ctrl_id = 0; ctrl_id < controllers.size(); ctrl_id++) {
          outstanding_counters[ctrl_id] +=
            controllers[ctrl_id].get_outstanding_io_ops(qpair_id);
        }
        samples++;
        delay_count = 0;
      }
      delay_count++;


      // Determine the number of batches to process
      size_t available = parent_read_fifo.size();
      size_t num_batches = std::min(min_free / B::BATCH_SIZE, available);
      
      parent_read_fifo_avail_stats.record(available);
      min_free_stats.record(min_free);
      num_issue_stats.record(num_batches);

      for (size_t i = 0; i < num_batches; i++) {
        B* req_batch = parent_read_fifo.dequeue();
        assert (req_batch != nullptr);
        dequed_any = true;

        for (size_t j = 0; j < B::BATCH_SIZE; j++) {
          node_io_t* req = &req_batch->batch[j];
          nvme_io_tracker_t* io = &req->tracker;

          if (req->type == node_io_t::type_e::NOP) {
            // do nothing
            continue;
          }
          
          // Compute the strided index on disk
          size_t ctrl_id, strided_block;
          nvme_node_indexes<C>(controllers.size(), req->node, ctrl_id, strided_block);
          
          // Initiate the IO
          // printf("%s node %lx strided_block %lx ctrl %ld qpair %ld\n",
          //        req->type == node_io_t::type_e::READ ? "Reading" : "Writing",
          //        req->node, strided_block, ctrl_id, qpair_id);
          if (req->type == node_io_t::type_e::READ) {
            SPDK_ERROR(controllers[ctrl_id].read(io, 0, qpair_id, strided_block + block_offset,
                                                 completion_cb, req));
          } else {
            SPDK_ERROR(controllers[ctrl_id].write(io, 0, qpair_id, strided_block + block_offset,
                                                  completion_cb, req));
          }
          ios_issued++;
          total_counters[ctrl_id]++;
        }
      }
      if (min_free < B::BATCH_SIZE) {
        disk_queues_full++;
      } else if (!dequed_any) {
        parent_read_fifo_empty++;
      }

      // Process completions
      size_t ios_completed_now = 0;
      for (size_t ctrl_id = 0; ctrl_id < controllers.size(); ctrl_id++) {
        ios_completed_now += controllers[ctrl_id].process_completions(qpair_id);
      }
      ios_completed += ios_completed_now;

      if ((!dequed_any && ios_completed_now == 0 && idle_sleep > 0) ||
          iter_count == duty_cycle) {
        usleep(idle_sleep);
        iter_count = 0;
      }
      iter_count++;
    }
    for (size_t ctrl_id = 0; ctrl_id < controllers.size(); ctrl_id++) {
      ios_completed += controllers[ctrl_id].process_all_completions(qpair_id);
    }

    // {
    //   unique_lock<mutex> lck(print_mtx);
      
    //   print_stats();

    //   printf("Average outstanding ops %ld samples\n", samples);
    //   for (size_t ctrl_id = 0; ctrl_id < controllers.size(); ctrl_id++) {
    //     printf("  %ld: %0.2lf\n", ctrl_id,
    //            (double)outstanding_counters[ctrl_id] / (double)samples);
    //   }
    // }
    // printf("Total  ops\n");
    // for (size_t ctrl_id = 0; ctrl_id < controllers.size(); ctrl_id++) {
    //   printf("  %ld: %ld\n", ctrl_id, total_counters[ctrl_id]);
    // }

    return 0;
  }
  void print_stats() {
    printf("node_rw_t   ios issued %ld completed %ld parent_read_fifo_empty %ld disk_queues_full %ld\n",
           ios_issued, ios_completed, parent_read_fifo_empty, disk_queues_full);
  }
};

template<class C, class B>
inline void rw_clear_stats(node_rw_t<C, B> *rw) {
  rw->clear_stats();
}
template<class C, class B>
inline void rw_snapshot(node_rw_t<C, B> *rw) {
  rw->snapshot();
}
template<class C, class B>
inline void rw_print(node_rw_t<C, B> *rw) {
  rw->print();
}

#endif
