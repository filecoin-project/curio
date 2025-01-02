// Copyright Supranational LLC

#ifndef __DEBUG_HELPERS_HPP__
#define __DEBUG_HELPERS_HPP__

#include <cstdint>       // uint*
#include <iostream>      // printing
#include <iomanip>       // printing
#include <x86intrin.h>   // x86 intrinsics
#include "../sealing/data_structures.hpp" // global parameters

template<class P_>
void print_parameters();

void print_digest(uint32_t* digest);
void print_buffer(uint8_t* buf, size_t bytes);
void print_buffer_dec(uint8_t* buf, size_t bytes);
template<class T> void Log256(const __m256i & value);
template<class T> void Log128(const __m128i & value);
void print_digest_reorder(uint32_t* digest);

// TODO: print plain node
template<class C>
void print_node(parallel_node_t<C> *buf, size_t lines = 0,
                const char *prefix = nullptr, bool reverse = false);
void print_buf(uint8_t *buf, size_t lines = 0,
               const char *prefix = nullptr);
void print_parents_graph(uint32_t* parents);

#endif // __DEBUG_HELPERS_HPP__
