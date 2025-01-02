// Copyright Supranational LLC

#ifndef __TOPOLOGY_T_HPP__
#define __TOPOLOGY_T_HPP__

#include <vector>
#include <map>
#include <set>
#include <assert.h>
#include <thread_pool_t.hpp>
#include "../sealing/constants.hpp"
#include <libconfig.h++>

// This is used to size buffers. There will be some waste in cases where there are
// fewer hashers per coordinator but it is minimal. This should be sized according to
// the number of hashers that can run in a CCX.
const size_t MAX_HASHERS_PER_COORD = 14;

//using namespace std;
using namespace libconfig;

class topology_t {
public:
  struct coordinator_t {
    size_t hashers_per_core;
    int core;
    size_t num_hashers;

    coordinator_t(size_t _hashers_per_core, int _core, size_t _num_hashers) :
      hashers_per_core(_hashers_per_core), core(_core), num_hashers(_num_hashers)
    {}
    
    size_t num_sectors() const {
      return num_hashers * NODES_PER_HASHER;
    }

    int get_hasher_core(size_t i) const {
      if (hashers_per_core == 1) {
        return core + 1 + i;
      }

      if (i & 0x1) {
        // Odd hasher, it's on the hyperthread
        return core + 1 + i / hashers_per_core + get_physical_cores();
      }
      return core + 1 + i / hashers_per_core;
    }
  };

  struct sector_config_t {
    size_t hashers_per_core;
    size_t sectors;
    std::vector<coordinator_t> coordinators;

    size_t num_coordinators() const {
      return coordinators.size();
    }

    int get_coordinator_core(size_t i) const {
      return coordinators[i].core;
    }
    
    size_t num_hashers() const {
      size_t count = 0;
      for (size_t i = 0; i < num_coordinators(); i++) {
        count += coordinators[i].num_hashers;
      }
      return count;
    }

    size_t num_sectors() const {
      return num_hashers() * NODES_PER_HASHER;
    }

    size_t num_hashing_cores() const {
      return num_coordinators() + (num_hashers() + hashers_per_core - 1) / hashers_per_core;
    }
  };
    
public:
  static int get_physical_cores() {
    return std::thread::hardware_concurrency() / 2;
  }

  size_t                            hashers_per_core;
  std::set<std::string>             nvme_addrs;
  std::map<size_t, sector_config_t> sector_configs;

  // Core numbers
  int pc1_reader;
  int pc1_writer;
  int pc1_orchestrator;
  int pc1_qpair_reader;
  int pc1_qpair_writer;
  int pc1_writer_sleep_time;
  int pc1_reader_sleep_time;
  int pc2_reader;
  int pc2_hasher;
  int pc2_hasher_cpu;
  int pc2_writer;
  int pc2_writer_cores;
  int pc2_sleep_time;
  int pc2_qpair;
  int c1_reader;
  int c1_sleep_time;
  int c1_qpair;

  topology_t(const char* filename) {
    Config cfg;
    try {
      cfg.readFile(filename);
    } catch(const FileIOException &fioex) {
      std::cerr << "Could not read config file " << filename << std::endl;
      exit(1);
    } catch(const ParseException &pex) {
      std::cerr << "Parse error at " << pex.getFile() << ":" << pex.getLine()
                << " - " << pex.getError() << std::endl;
      exit(1);
    }
    const Setting& root = cfg.getRoot();
  
    try {
      const Setting& nvme = root["spdk"]["nvme"];
      for (int i = 0; i < nvme.getLength(); i++) {
        std::string nvme_id = nvme[i];
        nvme_addrs.insert(nvme_id);
      }

      hashers_per_core = (int)root["topology"]["pc1"]["hashers_per_core"];
      if (hashers_per_core != 1 && hashers_per_core != 2) {
        printf("hashers_per_core must be 1 or 2, got %ld\n", hashers_per_core);
        exit(1);
      }
      
      const Setting& topology_pc1_topos = root["topology"]["pc1"]["sector_configs"];
    
      for (int i = 0; i < topology_pc1_topos.getLength(); i++) {
        Setting& coord = topology_pc1_topos[i];
        int sectors = coord["sectors"];

        sector_config_t sector_config;
        sector_config.sectors = sectors;
        sector_config.hashers_per_core = hashers_per_core;

        std::cout << "sectors " << sectors << std::endl;
        Setting& coordinators_cfg = coord["coordinators"];
        for (int j = 0; j < coordinators_cfg.getLength(); j++) {
          int core = coordinators_cfg[j]["core"];
          int hashers = coordinators_cfg[j]["hashers"];
          assert ((size_t)hashers <= MAX_HASHERS_PER_COORD);
          std::cout << "  coord " << core << " hashers " << hashers << std::endl;
          sector_config.coordinators.push_back(coordinator_t(hashers_per_core, core, hashers));
        }
        sector_configs.insert(std::pair((size_t)sector_config.sectors, sector_config));
      }

      pc1_reader       = root["topology"]["pc1"]["reader"];
      pc1_writer       = root["topology"]["pc1"]["writer"];
      pc1_orchestrator = root["topology"]["pc1"]["orchestrator"];
      pc1_qpair_reader = root["topology"]["pc1"]["qpair_reader"];
      pc1_qpair_writer = root["topology"]["pc1"]["qpair_writer"];
      pc1_reader_sleep_time = root["topology"]["pc1"]["reader_sleep_time"];
      pc1_writer_sleep_time = root["topology"]["pc1"]["writer_sleep_time"];
      pc2_reader       = root["topology"]["pc2"]["reader"];
      pc2_hasher       = root["topology"]["pc2"]["hasher"];
      pc2_hasher_cpu   = root["topology"]["pc2"]["hasher_cpu"];
      pc2_writer       = root["topology"]["pc2"]["writer"];
      pc2_writer_cores = root["topology"]["pc2"]["writer_cores"];
      pc2_sleep_time   = root["topology"]["pc2"]["sleep_time"];
      pc2_qpair        = root["topology"]["pc2"]["qpair"];
      c1_reader        = root["topology"]["c1"]["reader"];
      c1_sleep_time    = root["topology"]["c1"]["sleep_time"];
      c1_qpair         = root["topology"]["c1"]["qpair"];
    } catch(const SettingNotFoundException &nfex) {
      // Ignore.
    }
  }

  std::set<std::string> get_allowed_nvme() {
    return nvme_addrs;
  }
  
  sector_config_t* get_sector_config(size_t parallel_sectors) {
    const auto& it = sector_configs.find(parallel_sectors);
    if (it == sector_configs.end()) {
      return nullptr;
    }
    return &it->second;
  }
  
  void print(size_t parallel_sectors) {
    sector_config_t* config = get_sector_config(parallel_sectors);
    
    printf("Num coordinators:  %ld\n", config->num_coordinators());
    printf("Num hashers:       %ld\n", config->num_hashers());
    printf("Num sectors:       %ld\n", config->num_sectors());
    printf("Num hashing cores: %ld\n", config->num_hashing_cores());
    printf("core   process0      HT   process1\n");
    size_t sector = 0;
    for (size_t i = 0; i < config->num_coordinators(); i++) {
      printf("%2d     coord%-2ld\n", config->coordinators[i].core, i);
      for (size_t j = 0; j < config->coordinators[i].num_hashers; j++) {
        if (hashers_per_core == 1 || j == config->coordinators[i].num_hashers - 1) {
          printf("%2d      %2ld,%2ld        %2d\n",
                 config->coordinators[i].get_hasher_core(j),
                 sector, sector + 1,
                 config->coordinators[i].get_hasher_core(j) + get_physical_cores());
          sector += 2;
        } else {
          printf("%2d      %2ld,%2ld        %2d     %2ld,%2ld\n",
                 config->coordinators[i].get_hasher_core(j),
                 sector, sector + 1,
                 config->coordinators[i].get_hasher_core(j + 1),
                 sector + 2, sector + 3);
          j++;
          sector += 4;
        }
      }
    }    
  }
};

#endif
