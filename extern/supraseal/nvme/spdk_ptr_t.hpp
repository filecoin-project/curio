// Copyright Supranational LLC

#ifndef __SPDK_PTR_T_HPP__
#define __SPDK_PTR_T_HPP__

// Allocator for spdk data
template<typename T>
class spdk_ptr_t {
  T*     ptr;
  size_t count;
  
public:
  spdk_ptr_t() : ptr(nullptr), count(0) {}
  
  spdk_ptr_t(size_t nelems) : ptr(nullptr) {
    alloc(nelems);
  }
  ~spdk_ptr_t() {
    if (ptr) {
	    spdk_free(ptr);
    }
  }

  void alloc(size_t nelems) {
    free();
    if (nelems) {
      size_t bytes = nelems * sizeof(T);
      ptr = (T*)spdk_dma_zmalloc(bytes, PAGE_SIZE, NULL);
      assert (ptr != nullptr);
      count = nelems;
    }
  }

  void free() {
    if (ptr) {
	    spdk_free(ptr);
      ptr = nullptr;
      count = 0;
    }
  }

  size_t size()                               { return count; }
  
  inline operator const T*() const            { return ptr; }
  inline operator T*() const                  { return ptr; }
  inline operator void*() const               { return (void*)ptr; }
  inline const T& operator[](size_t i) const  { return ptr[i]; }
  inline T& operator[](size_t i)              { return ptr[i]; }
};

#endif
