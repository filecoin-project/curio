// Copyright Supranational LLC

#ifndef __FILE_WRITER_T_HPP__
#define __FILE_WRITER_T_HPP__

#include "../../util/mmap_t.hpp"
#include "../../util/file_t.hpp"

template<class T>
class file_writer_t {
  file_t<T> file_writer;
  mmap_t<T> mmap_writer;
  bool use_mmap;

public:
  file_writer_t() {}

  int open(std::string _fname, size_t _size,
           bool remove_first = false, bool _use_mmap = true) {
    use_mmap = _use_mmap;
    if (use_mmap) {
      return mmap_writer.mmap_write(_fname, _size, remove_first);
    } else {
      return file_writer.file_write(_fname, _size, remove_first);
    }
  }

  void write_data(size_t offset, T* buf, size_t size) {
    if (use_mmap) {
      mmap_writer.write_data(offset, buf, size);
    } else {
      file_writer.write_data(offset, buf, size);
    }
  }

  void advise_random() {
    if (use_mmap) {
      mmap_writer.advise_random();
    } else {
      file_writer.advise_random();
    }
  }
};

#endif
