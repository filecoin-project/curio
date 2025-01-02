// Copyright Supranational LLC

#ifndef __PATH_ELEMENT_HPP__
#define __PATH_ELEMENT_HPP__

class PathElement {
 public:
  PathElement(size_t arity, uint64_t index);
  ~PathElement();
  void SetHash(size_t index, node_t* hash) { hashes_[index] = hash; }
  size_t Write(uint8_t* file_ptr, size_t buf_index);

 private:
  size_t     arity_;
  uint64_t   index_;
  node_t** hashes_; // arity - 1 hashes
};

PathElement::PathElement(size_t arity, uint64_t index) :
  arity_(arity),
  index_(index) {
  hashes_ = new node_t*[arity - 1]{ nullptr };
}

PathElement::~PathElement() {
  delete hashes_;
}

size_t PathElement::Write(uint8_t* file_ptr, size_t buf_index) {
  uint64_t len = (uint64_t)arity_ - 1;
  std::memcpy(file_ptr + buf_index, &len, sizeof(uint64_t));
  buf_index += sizeof(uint64_t);

  for(uint64_t i = 0; i < len; ++i) {
    std::memcpy(file_ptr + buf_index, hashes_[i], sizeof(node_t));
    buf_index += sizeof(node_t);
  }

  std::memcpy(file_ptr + buf_index, &index_, sizeof(uint64_t));
  buf_index += sizeof(uint64_t);

  return buf_index;
}
#endif // __PATH_ELEMENT_HPP__
