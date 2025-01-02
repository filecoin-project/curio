// Copyright Supranational LLC

#ifndef __FILE_T_HPP__
#define __FILE_T_HPP__

#include <sys/types.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <unistd.h>
#include <assert.h>
#include <mutex>

template<class T>
class file_t {
private:
  std::string fname;
  size_t      size;
  int         fd;
  bool        is_write;
  std::mutex  mtx;
  
public:
  file_t(std::string _fname, size_t _size,
         bool _is_write = false, bool remove_first = false)
    : fd(-1)
  {
    open_file(_fname, _size, _is_write, remove_first);
  }
  file_t()
    : size(0), fd(-1)
  {}

  bool is_open() {
    return fd != -1;
  }

  size_t get_size() {
    return size;
  }

  int file_read(std::string _fname) {
    return file_read(_fname, (size_t)-1);
  }
  
  // Verify that the file size matches _size
  int file_read(std::string _fname, size_t _size) {
    return open_file(_fname, _size, false);
  }

  int file_write(std::string _fname, size_t _size, bool remove_first = false) {
    return open_file(_fname, _size, true, remove_first);
  }
  
  int open_file(std::string _fname, size_t _size,
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
      printf("ERROR: Could not open file %s for %s: %s\n",
             fname.c_str(), is_write ? "writing" : "reading",
             strerror(errno));
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
    return 0;
  }
  ~file_t() {
    if (fd != -1) {
      close(fd);
    }
  }
  void advise_random() {}

  void write_data(size_t offset, T* buf, size_t wr_size) {
    std::unique_lock<std::mutex> lock(mtx);
    assert (is_open());
    assert (lseek(fd, offset * sizeof(T), SEEK_SET) == offset * sizeof(T));
    if (write(fd, buf, wr_size * sizeof(T)) == -1) {
      printf("pc2 write failed errno %d: %s\n", errno, strerror(errno));
      exit(1);
    }
  }

  void read_data(size_t offset, T* buf, size_t size) {
    std::unique_lock<std::mutex> lock(mtx);
    assert (is_open());
    assert (lseek(fd, offset * sizeof(T), SEEK_SET) == offset * sizeof(T));
    if (read(fd, buf, size * sizeof(T)) == -1) {
      printf("file read failed errno %d: %s\n", errno, strerror(errno));
      exit(1);
    }
  }
};

#endif
