// Copyright Supranational LLC

#ifndef __RING_T_HPP__
#define __RING_T_HPP__

#include <stdint.h>
#include <stdio.h>
#include <vector>
#include <atomic>
#include <mutex>
#include <assert.h>

void ring_spdk_free(void *);
void* ring_spdk_alloc(size_t bytes);

// Single producer / Single consumer lock free fifo
template <class T>
class mt_fifo_t {
  std::vector<T*> store;

  // If we allocated the contents store the pointer
  T *contents;

  // 'head' tracks the next unused element.
  std::atomic<size_t> head;
  // 'tail' tracks the last used element
  std::atomic<size_t> tail;
  
public:
  mt_fifo_t() : head(0), tail(0) {
    contents = nullptr;
  }
  ~mt_fifo_t() {
#ifndef NO_SPDK
    ring_spdk_free(contents);
#endif
  }

  int create(const char *name, size_t count) {
    return create(count);
  }
  
  int create(size_t _count) {
    // Create a pool to hold the desired size
    store.resize(_count + 1);
    return 0;
  }

  size_t capacity() {
    return store.size() - 1;
  }

  int enqueue_nocheck(T *obj) {
    //assert (!is_full());
    size_t h = head;
    store[h] = obj;
    if (h == store.size() - 1) {
      head = 0;
    } else {
      head = h + 1;
    }
    return 0;
  }

  int enqueue(T *obj) {
    assert (!is_full());
    size_t h = head;
    store[h] = obj;
    if (h == store.size() - 1) {
      head = 0;
    } else {
      head = h + 1;
    }
    return 0;
  }

  T* dequeue() {
    if (size() != 0) {
      size_t t = tail;
      T *obj = store[t];
      if (t == store.size() - 1) {
        tail = 0;
      } else {
        tail = t + 1;
      }
      return obj;
    }
    return nullptr;
  }

  // Fill the pool
  // One element of store is left empty since the pool can hold
  // size() - 1 of usable data.
#ifndef NO_SPDK
  int fill() {
    contents = (T*)ring_spdk_alloc(sizeof(T) * (store.size() - 1));
    if (contents == nullptr) {
      return 1;
    }
    for (size_t i = 0; i < store.size() - 1; i++) {
      store[i] = &contents[i];
    }
    head = store.size() - 1;
    
    return 0;
  }
#endif
  
  // Number of used entries in the ring
  size_t size() {
    // Load values so we can perform a consistent calculation
    size_t h = head;
    size_t t = tail;
    if (h >= t) {
      return h - t;
    } else {
      return (store.size() + h) - t;
    }
  }

  // Get entry at index
  T& operator[](size_t i) {
    return *store[i];
  }

  bool is_full() {
    return size() == store.size() - 1;
  }

  inline size_t free_count() {
    return capacity() - size();
  }

  void print() {
    size_t h = head;
    size_t t = tail;
    printf("mt_ring_t: tail %ld, head %ld\n", t, h);
  }
};

// Non-multithread safe pool using a ring buffer
template <class T>
class pool_t {
  std::vector<T*> store;
  
  // If we allocated the contents store the pointer
  T *contents;

  // 'head' tracks the next unused element.
  size_t head;
  // 'tail' tracks the last used element
  size_t tail;
  
public:
  pool_t() {
    contents = nullptr;
    head = 0;
    tail = 0;
  }
  ~pool_t() {
#ifndef NO_SPDK
    ring_spdk_free(contents);
#endif
  }

  int create(size_t _count) {
    // Create a pool to hold the desired size
    store.resize(_count + 1);
    return 0;
  }

  int enqueue(T *obj) {
    assert (!is_full());
    store[head++] = obj;
    if (head >= store.size()) {
      head = 0;
    }
    return 0;
  }

  T* dequeue() {
    if (size() != 0) {
      T *obj = store[tail++];
      if (tail >= store.size()) {
        tail = 0;
      }
      return obj;
    }
    return nullptr;
  }

  // Dequeue a block of contiguous elements
  // Returns null if not enough elements or elements are non-contiguous
  T** dequeue_bulk(size_t count) {
    if (size() >= count && tail + count <= store.size()) {
      T **obj = &store[tail];
      tail += count;
      if (tail >= store.size()) {
        tail -= store.size();
      }
      return obj;
    }
    return nullptr;
  }

  // Fill the pool
  // One element of store is left empty since the pool can hold
  // size() - 1 of usable data.
#ifndef NO_SPDK
  int fill() {
    contents = (T*)ring_spdk_alloc(sizeof(T) * (store.size() - 1));
    if (contents == nullptr) {
      return 1;
    }
    for (size_t i = 0; i < store.size() - 1; i++) {
      store[i] = &contents[i];
    }
    head = store.size() - 1;
    
    return 0;
  }
#endif

  // Number of used entries in the ring
  size_t size() {
    if (head >= tail) {
      return head - tail;
    } else {
      return (store.size() + head) - tail;
    }
  }

  // Get entry at index
  T& operator[](size_t i) {
    return *store[i];
  }

  bool is_full() {
    return size() == store.size() - 1;
  }

  size_t capacity() {
    return store.size() - 1;
  }

  inline size_t free_count() {
    return capacity() - size();
  }
};

template <class T>
class mtx_fifo_t {
  pool_t<T> pool;
  std::mutex mtx;
  
public:
  int create(size_t _count) {
    std::unique_lock<std::mutex> lock(mtx);
    return pool.create(_count);
  }

  int enqueue(T *obj) {
    std::unique_lock<std::mutex> lock(mtx);
    return pool.enqueue(obj);
  }

  T* dequeue() {
    std::unique_lock<std::mutex> lock(mtx);
    return pool.dequeue();
  }

  // Number of used entries in the ring
  size_t size() {
    std::unique_lock<std::mutex> lock(mtx);
    return pool.size();
  }

  // Get entry at index
  T& operator[](size_t i) {
    std::unique_lock<std::mutex> lock(mtx);
    return pool[i];
  }

  bool is_full() {
    std::unique_lock<std::mutex> lock(mtx);
    return pool.is_full();
  }

  size_t capacity() {
    std::unique_lock<std::mutex> lock(mtx);
    return pool.capacity();
  }

  inline size_t free_count() {
    std::unique_lock<std::mutex> lock(mtx);
    return pool.free_count();
  }
};

// Ring buffer for data from disk
// Safe for single producer / single consumer.
// On the producer side, the flow is:
// - Advance the head to reserve an element. The element is marked as invalid.
// - Initiate a disk read for the data
// - Sometime later the disk DMA completes and we are notified. At this point
//   the data is expected to be in memory.
// - Mark the entry as valid, indicating it may be read
// On the consumer side
// - Data is consumed from the tail. Once consumed tail is advanced
// - Data is only consumed when marked as valid.
typedef std::atomic<uint64_t> ring_buffer_valid_t;

template <class T, unsigned int _VALID_THRESHOLD, int SIZE>
class ring_buffer_t {
public:
  typedef T* T_ptr;

  const unsigned int VALID_THRESHOLD = _VALID_THRESHOLD;
  // Number of entries
  static const size_t count = SIZE;

private:
  // Array of entries
  T_ptr    entries; // 4k pages - 1536 pages to saturate drives
  // Store which entries are valid
  ring_buffer_valid_t* valid;

  //                       pointers move right ----->
  //      tail                                  head_valid       head
  //        |                                       |              |
  // -----------------------------------------------------------------------
  // |    |    |    |    |    |    |    |    |    |    |    |    |    |    |
  // -----------------------------------------------------------------------
  //   0    1    1    1    1    1    1    1    1    1    0    1    0    0
  //  valid

  // 'head' tracks the next unused element.
  size_t head;
  // 'head_valid' tracks the point at which all previous elements are valid
  size_t head_valid;
  // 'tail' tracks the last used element
  size_t tail;
  // track size
  size_t cur_size;
  
public:
  ring_buffer_t() {
    entries = nullptr;
    valid = nullptr;
    head = 0;
    head_valid = 0;
    tail = 0;
    cur_size = 0;
  }
  
  ~ring_buffer_t() {
    delete [] valid;
    valid = nullptr;
  }

  // Usable size will be count - 1
  // 'entries' is an array of count entries
  int create(T *_entries) {
    entries = _entries;

    valid = new ring_buffer_valid_t[count];
    for (size_t i = 0; i < count; i++) {
      valid[i] = 0;
    }
    return 0;
  }

  // Number of elements of storage
  inline size_t storage() {
    return count;
  }

  inline size_t capacity() {
    return count - 1;
  }

public:
  inline size_t size() {
    //return sub(head, tail);
    return cur_size;
  }

  inline size_t free_count() {
    //return capacity() - size();
    return capacity() - cur_size;
  }

  inline bool is_full() {
    return cur_size == capacity();
  }

  inline T *get_entry(size_t idx) {
    return &entries[idx];
  }

  // Reserve the next free element for use, advance head
  // Do not perform the safety check for a full buffer
  inline T *reserve_nocheck(size_t &idx) {
    // Store index
    idx = head;
    // Advance head
    size_t next_head = incr(head);
    valid[next_head] = 0;
    head = next_head;
    cur_size++;
    return &entries[idx];
  }
  
  // Reserve the next free element for use, advance head
  inline T *reserve(size_t &idx) {
    if (is_full()) {
      return nullptr;
    }
    // Store index
    idx = head;
    // Advance head
    size_t next_head = incr(head);
    valid[next_head] = 0;
    head = next_head;
    cur_size++;
    return &entries[idx];
  }

  // Reserve the next free element for use, advance head
  // Do not perform the safety check for a full buffer
  inline void reserve_batch_nocheck(size_t count, size_t &idx, T** batch) {
    // Store index
    idx = head;
    for (size_t i = 0; i < count; i++) {
      batch[i] = &entries[head];
      // Advance head
      size_t next_head = incr(head);
      valid[next_head] = 0;
      head = next_head;
    }
    cur_size += count;
  }
  
  // Mark element as valid
  inline void incr_valid(size_t idx, uint64_t amount = 1) {
    valid[idx].fetch_add(amount);
  }

  inline ring_buffer_valid_t get_valid(size_t idx) {
    return valid[idx].load();
  }
  inline bool is_valid(size_t idx) {
    return valid[idx].load() >= VALID_THRESHOLD;
  }

  inline ring_buffer_valid_t* get_valid_ptr(size_t idx) {
    return &valid[idx];
  }

  inline size_t get_head() {
    return head;
  }

  inline size_t get_tail() {
    return tail;
  }

  inline bool is_tail_valid() {
    return valid[tail].load() >= VALID_THRESHOLD;
  }

  inline size_t incr(size_t idx) {
    idx = (idx == count - 1) ? 0 : idx + 1;
    return idx;
  }

  inline size_t decr(size_t idx) {
    return (idx == 0) ? count - 1 : idx - 1;
  }

  // Returns a - b, taking into account wraparound
  inline size_t sub(size_t a, size_t b) {
    if (a >= b) {
      return a - b;
    }
    return (count + a) - b;
  }

  // Returns a + b, taking into account wraparound
  inline size_t add(size_t a, size_t b) {
    size_t res = a + b;
    if (res >= count) {
      res -= count;
    }
    return res;
  }

  // Advance the head_valid pointer
  inline size_t advance_valid() {
    size_t cnt = 0;
    while (valid[head_valid].load() >= VALID_THRESHOLD) {
      cnt++;
      head_valid = incr(head_valid);
    }
    return cnt;
  }

  // Release the tail element to unused state
  inline void release() {
    // Advance tail
    //assert (size() > 0);
    tail = incr(tail);
    cur_size--;
  }

  // Release valid tail elements to unused state
  inline size_t release_valid() {
    size_t count = 0;
    // Advance tail
    while (valid[tail].load() >= VALID_THRESHOLD) {
      valid[tail] = 0;
      count++;
      tail = incr(tail);
      cur_size--;
    }
    return count;
  }

  // Print debug information
  void print() {
    printf("count %ld, tail %ld, head_valid %ld, head %ld, size %ld, full %d, free_count %ld\n",
           count, tail, head_valid, head, size(), is_full(), free_count());
  }
};

template<typename ABS_TYPE, int RING_SIZE, int RING_DIVISOR>
class ring_counter_t {
private:
  ABS_TYPE _abs;
  size_t   _idx;

public:
  ring_counter_t(ABS_TYPE& abs) : _abs(abs) {
    _idx = 0;
  }

  ABS_TYPE& abs() {
    return _abs;
  }
  size_t idx() {
    return _idx / RING_DIVISOR;
  }
  size_t offset() {
    return _idx % RING_DIVISOR;
  }
  void operator ++(int) {
    _abs++;
    _idx++;
    if (_idx == RING_SIZE) {
      _idx = 0;
    }
  }
  ring_counter_t& operator+=(const size_t rhs) {
    _abs += rhs;
    _idx += rhs;
    if (_idx >= RING_SIZE) {
      _idx -= RING_SIZE;
    }
    return *this;
  }
};

#endif

