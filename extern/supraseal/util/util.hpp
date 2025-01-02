// Copyright Supranational LLC

#ifndef __UTIL_HPP__
#define __UTIL_HPP__

#include <stdio.h>
#include <stdint.h>

inline uint64_t get_tsc() {
  uint64_t count;

  // Read Time-Stamp Counter, Opcode - 0x0F 0x31, EDX:EAX <- TSC
  __asm__ volatile("lfence;             \
                        .byte 15; .byte 49; \
                        shlq  $32,  %%rdx;  \
                        orq  %%rdx, %%rax;  \
                        lfence;"
                   : "=a" (count)
                   :
                   : "%rdx"
                   );
  return count;
}

inline void set_core_affinity(size_t core_num) {
  pthread_t pid = pthread_self();
  cpu_set_t cpuset;
  CPU_ZERO(&cpuset);
  CPU_SET(core_num, &cpuset);
  pthread_setaffinity_np(pid, sizeof(cpu_set_t), &cpuset);
}

#endif
