// Copyright Supranational LLC

#include <cstdint>           // uint*
#include <iostream>          // printing
#include <iomanip>           // printing
#include <x86intrin.h>       // x86 intrinsics
#include <algorithm>
#include <arpa/inet.h>       // htons
#include "debug_helpers.hpp" // header

template<class C>
void print_parameters() {
  std::cout << "Sealing Parameters" << std::endl;
  std::cout << "SECTOR_SIZE           "<< C::GetSectorSize() << std::endl;
  std::cout << "SECTOR_SIZE_LG        "<< C::GetSectorSizeLg() << std::endl;
  std::cout << "NODE_SIZE             "<< NODE_SIZE << std::endl;
  std::cout << "NODE_WORDS            "<< NODE_WORDS << std::endl;
  std::cout << "NODE_COUNT            "<< C::GetNumNodes() << std::endl;
  std::cout << "PARENT_COUNT_BASE     "<< PARENT_COUNT_BASE << std::endl;
  std::cout << "PARENT_COUNT_EXP      "<< PARENT_COUNT_EXP << std::endl;
  std::cout << "PARENT_COUNT          "<< PARENT_COUNT << std::endl;
  std::cout << "PARENT_SIZE           "<< PARENT_SIZE << std::endl;
  std::cout << "LAYER_COUNT           "<< C::GetNumLayers() << std::endl;
  std::cout << "NODES_PER_HASHER      "<< NODES_PER_HASHER << std::endl;
  std::cout << "PARENT_BUFFER_NODES   "<< PARENT_BUFFER_NODES << std::endl;
  std::cout << "NODE_BUFFER_NODES     "<< NODE_BUFFER_NODES << std::endl;
  std::cout << std::endl;
}

void print_digest(uint32_t* digest) {
  for (int i = 0; i < 8; ++i) {
    std::cout << std::hex << std::setfill('0') << std::setw(8)
              << digest[i] << " ";
  }
  std::cout << std::endl;
}

void print_buffer(uint8_t* buf, size_t bytes) {
  for (size_t i = 0; i < bytes; ++i) {
    std::cout << std::hex << std::setfill('0') << std::setw(2)
              << (uint32_t) buf[i] << " ";
  }
  std::cout << std::endl;
}

void print_buffer_dec(uint8_t* buf, size_t bytes) {
  for (size_t i = 0; i < bytes; ++i) {
    std::cout << std::dec << std::setw(3)
              << (uint32_t) buf[i] << " ";
  }
  std::cout << std::endl;
}

template<class T> void Log256(const __m256i & value) {
  const size_t n = sizeof(__m256i) / sizeof(T);
  T buffer[n];
  _mm256_storeu_si256((__m256i*)buffer, value);
  for (size_t i = 0; i < n; i++)
    if (sizeof(T) == 1) {
      std::cout << std::setw(sizeof(T)*2) << std::setfill('0')
                << std::hex << (uint32_t) buffer[i] << " ";
    } else {
      std::cout << std::setw(sizeof(T)*2) << std::setfill('0')
                << std::hex << buffer[i] << " ";
    }
  std::cout << std::endl;
}

template<class T> void Log128(const __m128i & value) {
  const size_t n = sizeof(__m128i) / sizeof(T);
  T buffer[n];
  _mm_storeu_si128((__m128i*)buffer, value);
  for (size_t i = 0; i < n; i++)
    std::cout << std::setw(8) << std::setfill('0')
              << std::hex << buffer[i] << " ";
  //std::cout << std::endl;
}

void print_digest_reorder(uint32_t* digest) {
  static const uint32_t BYTE_SHUFFLE_MASK[8] = {
    0x00010203, 0x04050607, 0x08090a0b, 0x0c0d0e0f,
    0x00010203, 0x04050607, 0x08090a0b, 0x0c0d0e0f
  };

  __m128i mask = _mm_loadu_si128((__m128i*)&(BYTE_SHUFFLE_MASK[0]));
  __m128i d0   = _mm_loadu_si128((__m128i*)&(digest[0])); // ABEF
  __m128i d1   = _mm_loadu_si128((__m128i*)&(digest[4])); // CDGH

  d0 = _mm_shuffle_epi8(d0, mask);           // Change endianess
  d1 = _mm_shuffle_epi8(d1, mask);           // Change endianess

  Log128<uint32_t>(d0);
  Log128<uint32_t>(d1);
  std::cout << std::endl;
}

void print_single_node(uint64_t* n, const char *prefix, bool reverse) {
  if (reverse) {
    uint16_t *n16 = (uint16_t*)n;
    printf("%s", prefix == NULL ? "" : prefix);
    for (size_t i = 0; i < 16; i++) {
      printf("%04x ", htons(n16[i ^ 1]));
    }
  } else {
    printf("%s%016lx %016lx %016lx %016lx",
           prefix == NULL ? "" : prefix,
           n[0], n[1], n[2], n[3]);
  }
}

void _print_buf(uint8_t *buf, size_t lines, const char *prefix,
                bool reverse, size_t words_per_page) {
  if (lines == 0) {
    for (unsigned node = 0; node < words_per_page; node++) {
      uint64_t *p = (uint64_t *)(buf + node * NODE_SIZE);
      print_single_node(p, prefix, reverse);
      printf("\n");
    }
  } else {
    // First 'lines' nodes...
    for (size_t node = 0; node < std::min(words_per_page, lines); node++) {
      uint64_t *p = (uint64_t *)(buf + node * NODE_SIZE);
      print_single_node(p, prefix, reverse);
      if (node == std::min(words_per_page, lines) - 1) {
        printf(" ... %p\n", buf);
      } else {
        printf("\n");
      }
    }
    // last 'lines' nodes...
    unsigned start;
    if (lines > words_per_page) {
      start = 0;
    } else {
      start = words_per_page - lines;
    }
    for (size_t node = start; node < words_per_page; node++) {
      uint64_t *p = (uint64_t *)(buf + node * NODE_SIZE);
      print_single_node(p, prefix, reverse);
      printf("\n");
    }
  }
}

template<class C>
void print_node(parallel_node_t<C> *buf, size_t lines, const char *prefix, bool reverse) {
  _print_buf((uint8_t*)buf, lines, prefix, reverse, C::PARALLEL_SECTORS);
}

void print_buf(uint8_t *buf, size_t lines, const char *prefix) {
  _print_buf(buf, lines, prefix, false, PAGE_SIZE / NODE_SIZE);
}

void print_parents_graph(uint32_t* parents) {
  const size_t count = 128;
  for (size_t i = 0; i < count; i++) {
    printf("Node %2ld: ", i);
    for (size_t j = 0; j < PARENT_COUNT; j++) {
      printf("%08x ", parents[i * PARENT_COUNT + j]);
    }
    printf("\n");
  }
}
