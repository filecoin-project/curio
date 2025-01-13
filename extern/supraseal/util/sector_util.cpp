// Copyright Supranational LLC

#include <iostream>
#include "sector_util.hpp"

size_t get_sector_size_from_string(std::string& sector_size_string) {
  if      (sector_size_string ==  "32GiB") return 1UL << Sector32GB;
  else if (sector_size_string == "512MiB") return 1UL << Sector512MB;
#ifdef RUNTIME_SECTOR_SIZE
  else if (sector_size_string ==   "2KiB") return 1UL << Sector2KB;
  else if (sector_size_string ==   "4KiB") return 1UL << Sector4KB;
  else if (sector_size_string ==  "16KiB") return 1UL << Sector16KB;
  else if (sector_size_string ==  "32KiB") return 1UL << Sector32KB;

  else if (sector_size_string ==   "8MiB") return 1UL << Sector8MB;
  else if (sector_size_string ==  "16MiB") return 1UL << Sector16MB;

  else if (sector_size_string ==   "1GiB") return 1UL << Sector1GB;
  else if (sector_size_string ==  "64GiB") return 1UL << Sector64GB;
#endif
  else {
    std::cout << "Invalid sector size" << std::endl;
    exit(1);
  }
}
