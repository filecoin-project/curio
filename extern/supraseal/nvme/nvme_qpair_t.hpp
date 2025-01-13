// Copyright Supranational LLC

#ifndef __NVME_QPAIR_T_HPP__
#define __NVME_QPAIR_T_HPP__

class nvme_qpair_t {
  struct spdk_nvme_qpair* qpair;
  size_t outstanding_io_ops;
  
public:
  nvme_qpair_t(struct spdk_nvme_qpair* _qpair) {
    qpair = _qpair;
    outstanding_io_ops = 0;
  }
  ~nvme_qpair_t() {}

  struct spdk_nvme_qpair* get_qpair() {
    return qpair;
  }
  void incr_ops() {
    outstanding_io_ops++;
  }
  size_t get_outstanding_io_ops() {
    return outstanding_io_ops;
  }

  void cleanup() {
    // Free the I/O qpair.  This typically is done when an application exits.
    // But SPDK does support freeing and then reallocating qpairs during
    // operation.  It is the responsibility of the caller to ensure all
    // pending I/O are completed before trying to free the qpair.
    spdk_nvme_ctrlr_free_io_qpair(qpair);
  }

  int process_completions() {
    // Poll for completions.  0 here means process all available completions.
    // In certain usage models, the caller may specify a positive integer
    // instead of 0 to signify the maximum number of completions it should
    // process.  This function will never block - if there are no
    // completions pending on the specified qpair, it will return immediately.
     
    // When the write I/O completes, write_complete() will submit a new I/O
    // to read LBA 0 into a separate buffer, specifying read_complete() as its
    // completion routine.  When the read I/O completes, read_complete() will
    // print the buffer contents and set sequence.is_completed = 1.  That will
    // break this loop and then exit the program.
    if (outstanding_io_ops > 0) {
      // Returns the number of completions process, -ERRNO for error
      int completions = spdk_nvme_qpair_process_completions(qpair, 0);
      assert(completions >= 0);
      outstanding_io_ops -= completions;
      return completions;
    }
    return 0;
  }
};

#endif
