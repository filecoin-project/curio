// Copyright Supranational LLC

#ifndef __STATS_HPP__
#define __STATS_HPP__

struct queue_stat_t {
  const char* name;
  size_t capacity;

  struct stats_t {
    size_t last_size;
    size_t total;
    size_t samples;
    stats_t() {
      clear();
    }
    void clear() {
      total = 0;
      samples = 0;
    }
  };
  stats_t cur;
  stats_t snap;
  
  queue_stat_t(const char* _name, size_t _capacity) :
    name(_name), capacity(_capacity)
  {}
  queue_stat_t() {
    name = "null";
    capacity = 0;
  }
  void init(const char* _name, size_t _capacity) {
    name = _name;
    capacity = _capacity;
  }
  void clear() {
    cur.clear();
  }
  void record(size_t cur_size) {
#ifdef STATS
    cur.last_size = cur_size;
    cur.samples++;
    cur.total += cur_size;
#endif
  }
  void snapshot() {
    snap = cur;
  }
  void print() {
    size_t avg_size = snap.samples == 0 ? 0 : snap.total / snap.samples;
    printf("%30s: capacity %10ld, cur %10ld, avg_size %10ld\n",
           name, capacity,
           snap.last_size, avg_size);
  }
};

struct counter_stat_t {
  const char* name;

  struct stats_t {
    size_t count;
  };
  stats_t cur;
  stats_t snap;
  
  counter_stat_t(const char *_name) {
    name = _name;
    cur.count = 0;
  }
  counter_stat_t() {
    name = "null";
  }
  void init(const char* _name) {
    name = _name;
  }
  void clear() {
    cur.count = 0;
  }
  void record() {
#ifdef STATS
    cur.count++;
#endif
  }
  void snapshot() {
    snap = cur;
  }
  void print() {
    printf("%30s: count %ld\n",
           name, snap.count);
  }
};


#endif
