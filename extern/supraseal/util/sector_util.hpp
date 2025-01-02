// Copyright Supranational LLC

#ifndef __SECTOR_UTIL_HPP__
#define __SECTOR_UTIL_HPP__

#include <iostream>

#ifdef RUNTIME_SECTOR_SIZE
#define SECTOR_PARAMS_TABLE(FUNC)               \
  switch (sector_size) {                        \
  case 1UL << Sector64GB: {                     \
    sector_parameters64GB params;               \
    FUNC;                                       \
    break;                                      \
  }                                             \
  case 1UL << Sector32GB: {                     \
    sector_parameters32GB params;               \
    FUNC;                                       \
    break;                                      \
  }                                             \
  case 1UL << Sector1GB: {                      \
    sector_parameters1GB params;                \
    FUNC;                                       \
    break;                                      \
  }                                             \
  case 1UL << Sector512MB: {                    \
    sector_parameters512MB params;              \
    FUNC;                                       \
    break;                                      \
  }                                             \
  case 1UL << Sector16MB: {                     \
    sector_parameters16MB params;               \
    FUNC;                                       \
    break;                                      \
  }                                             \
  case 1UL << Sector8MB: {                      \
    sector_parameters8MB params;                \
    FUNC;                                       \
    break;                                      \
  }                                             \
  case 1UL << Sector32KB: {                     \
    sector_parameters32KB params;               \
    FUNC;                                       \
    break;                                      \
  }                                             \
  case 1UL << Sector16KB: {                     \
    sector_parameters16KB params;               \
    FUNC;                                       \
    break;                                      \
  }                                             \
  case 1UL << Sector4KB: {                      \
    sector_parameters4KB params;                \
    FUNC;                                       \
    break;                                      \
  }                                             \
  case 1UL << Sector2KB: {                      \
    sector_parameters2KB params;                \
    FUNC;                                       \
    break;                                      \
  }                                             \
  default: {                                    \
    std::cout                                   \
      << "Invalid sector size"                  \
      << std::endl;                             \
    exit(1);                                    \
  }                                             \
  }
#else
#define SECTOR_PARAMS_TABLE(FUNC)    \
  switch (sector_size) {             \
  case 1UL << Sector32GB: {          \
      sector_parameters32GB params;  \
      FUNC;                          \
      break;                         \
    }                                \
  case 1UL << Sector512MB: {         \
      sector_parameters512MB params; \
      FUNC;                          \
      break;                         \
    }                                \
    default: {                       \
      std::cout                      \
      << "Invalid sector size"       \
      << std::endl;                  \
      exit(1);                       \
    }                                \
  }
#endif

size_t get_sector_size_from_string(std::string& sector_size_string);

#endif
