// Copyright Supranational LLC

#ifndef __MMAP_T_HPP__
#define __MMAP_T_HPP__

#include <sys/types.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <sys/mman.h>
#include <unistd.h>
#include <assert.h>

template<class T>
class mmap_t {
private:
  std::string fname;
  size_t      size;
  int         fd;
  T*          data;
  bool        is_write;
  
public:
  mmap_t(std::string _fname, size_t _size,
         bool _is_write = false, bool remove_first = false)
    : fd(-1), data(nullptr)
  {
    open_mmap(_fname, _size, _is_write, remove_first);
  }
  mmap_t()
    : size(0), fd(-1), data(nullptr)
  {}

  bool is_open() {
    return data != nullptr;
  }

  size_t get_size() {
    return size;
  }

  int mmap_read(std::string _fname) {
    return mmap_read(_fname, (size_t)-1);
  }
  
  // Verify that the file size matches _size
  int mmap_read(std::string _fname, size_t _size) {
    return open_mmap(_fname, _size, false);
  }

  int mmap_write(std::string _fname, size_t _size, bool remove_first = false) {
    return open_mmap(_fname, _size, true, remove_first);
  }
  
  int open_mmap(std::string _fname, size_t _size,
                bool _is_write = false, bool remove_first = false) {
    fname = _fname;
    size = _size;
    is_write = _is_write;
    
    if (is_write && remove_first) {
      remove(fname.c_str());
    }

    if (is_write) {
      fd = open(fname.c_str(), O_RDWR | O_CREAT, (mode_t)0664);
    } else {
      fd = open(fname.c_str(), O_RDONLY);
    }
    if (fd == -1) {
      printf("ERROR: Could not open file %s for %s\n",
             fname.c_str(), is_write ? "writing" : "reading");
      return 1;
    }
    
    if (is_write) {
      // lseek(fd, size - 1, SEEK_SET);
      // assert (write(fd, "", 1) != -1);
      posix_fallocate(fd, 0, size);
    } else {
      struct stat statbuf;
      fstat(fd, &statbuf);
      if (size == (size_t)-1) {
        size = (size_t)statbuf.st_size;
      } else if ((size_t)statbuf.st_size != size) {
        printf("ERROR: file %s is size %ld, expected %ld\n",
               fname.c_str(), (size_t)statbuf.st_size, size);
        return 1;
      }
    }
    if (is_write) {
      data = (T*)mmap(NULL, size, PROT_WRITE, MAP_SHARED, fd, 0);
    } else {
      data = (T*)mmap(NULL, size, PROT_READ, MAP_SHARED, fd, 0);
    }
    if (data == MAP_FAILED) {
      printf("mmap failed for file %s", fname.c_str());
      return 1;
    }
    return 0;
  }
  ~mmap_t() {
    if (data != nullptr && data != MAP_FAILED) {
      munmap(data, size);
    }
    if (fd != -1) {
      close(fd);
    }
  }
  void advise_random() {
    assert(madvise(data, size, MADV_RANDOM) == 0);
  }
  inline operator const T*() const            { return data; }
  inline operator T*() const                  { return data; }
  inline operator void*() const               { return (void*)data; }
  inline const T& operator[](size_t i) const  { return data[i]; }
  inline T& operator[](size_t i)              { return data[i]; }

  void write_data(size_t offset, T* buf, size_t size) {
    assert (is_open());
    memcpy(&data[offset], buf, size * sizeof(T));
  }

  void read_data(size_t offset, T* buf, size_t size) {
    assert (is_open());
    memcpy(buf, &data[offset], size * sizeof(T));
  }
};

#endif
