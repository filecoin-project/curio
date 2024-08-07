package sealsupra

import (
	"bufio"
	"fmt"
	"github.com/samber/lo"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type SystemInfo struct {
	ProcessorCount int
	CoreCount      int
	ThreadsPerCore int
	CoresPerL3     int
}

type CoordinatorConfig struct {
	Core    int
	Hashers int
}

type SectorConfig struct {
	Sectors      int
	Coordinators []CoordinatorConfig
}

type TopologyConfig struct {
	PC1Writer       int
	PC1Reader       int
	PC1Orchestrator int
	PC2Reader       int
	PC2Hasher       int
	PC2HasherCPU    int
	PC2Writer       int
	PC2WriterCores  int
	C1Reader        int
	SectorConfigs   []SectorConfig
}

type SupraSealConfig struct {
	NVMeDevices []string
	Topology    TopologyConfig

	// Diagnostic fields (not part of the config)
	RequiredThreads int
	RequiredCCX     int
	RequiredCores   int
	UnoccupiedCores int

	P2WrRdOverlap   bool
	P2HsP1WrOverlap bool
	P2HcP2RdOverlap bool
}

func GetSystemInfo() (*SystemInfo, error) {
	cmd := exec.Command("hwloc-ls")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error running hwloc-ls: %v", err)
	}

	info := &SystemInfo{}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	l3Regex := regexp.MustCompile(`L3 L#(\d+)`)
	puRegex := regexp.MustCompile(`PU L#(\d+)`)
	coreRegex := regexp.MustCompile(`Core L#(\d+)`)
	packageRegex := regexp.MustCompile(`Package L#(\d+)`)

	var currentL3Cores int
	var lastL3Index int = -1
	var threadCount int

	for scanner.Scan() {
		line := scanner.Text()

		if l3Match := l3Regex.FindStringSubmatch(line); l3Match != nil {
			l3Index, _ := strconv.Atoi(l3Match[1])
			if lastL3Index != -1 && l3Index != lastL3Index {
				if info.CoresPerL3 == 0 || currentL3Cores < info.CoresPerL3 {
					info.CoresPerL3 = currentL3Cores
				}
				currentL3Cores = 0
			}
			lastL3Index = l3Index
		}

		if coreRegex.MatchString(line) {
			info.CoreCount++
			currentL3Cores++
		}

		if puRegex.MatchString(line) {
			threadCount++
		}

		if packageRegex.MatchString(line) {
			info.ProcessorCount++
		}
	}

	// Handle the last L3 cache
	if info.CoresPerL3 == 0 || currentL3Cores < info.CoresPerL3 {
		info.CoresPerL3 = currentL3Cores
	}

	if info.CoreCount > 0 {
		info.ThreadsPerCore = threadCount / info.CoreCount
	}

	return info, nil
}

func GenerateSupraSealConfig(info SystemInfo, dualHashers bool, batchSize int, nvmeDevices []string) (SupraSealConfig, error) {
	config := SupraSealConfig{
		NVMeDevices: nvmeDevices,
		Topology: TopologyConfig{
			// Start with a somewhat optimal layout for top-level P1 processes
			PC1Writer:       1,
			PC1Reader:       2, // High load
			PC1Orchestrator: 3, // High load

			// Now cram P2 processes into the remaining ~2 cores
			PC2Reader:      0,
			PC2Hasher:      1, // High load
			PC2HasherCPU:   0,
			PC2Writer:      0, // High load
			PC2WriterCores: 1, // ^
			C1Reader:       0,
		},

		P2WrRdOverlap:   true,
		P2HsP1WrOverlap: true,
		P2HcP2RdOverlap: true,
	}

	sectorsPerThread := 1
	if dualHashers {
		sectorsPerThread = 2
	}

	ccxFreeCores := info.CoresPerL3 - 1 // one core per ccx goes to the coordinator
	ccxFreeThreads := ccxFreeCores * info.ThreadsPerCore
	sectorsPerCCX := ccxFreeThreads * sectorsPerThread

	config.RequiredThreads = batchSize / sectorsPerThread
	config.RequiredCCX = (batchSize + sectorsPerCCX - 1) / sectorsPerCCX
	config.RequiredCores = config.RequiredCCX + config.RequiredThreads/info.ThreadsPerCore

	if config.RequiredCores > info.CoreCount {
		return config, fmt.Errorf("not enough cores available for hashers")
	}

	coresLeftover := info.CoreCount - config.RequiredCores

	const minOverheadCores = 4
	if coresLeftover < minOverheadCores {
		return config, fmt.Errorf("not enough cores available for overhead")
	}

	nextFreeCore := minOverheadCores

	// Assign cores for PC2 processes
	if coresLeftover > nextFreeCore {
		config.Topology.PC2Writer = nextFreeCore
		config.P2WrRdOverlap = false
		nextFreeCore++
	}

	if coresLeftover > nextFreeCore {
		config.Topology.PC2Hasher = nextFreeCore
		config.P2HsP1WrOverlap = false
		nextFreeCore++
	}

	if coresLeftover > nextFreeCore {
		config.Topology.PC2HasherCPU = nextFreeCore
		config.P2HcP2RdOverlap = false
		nextFreeCore++
	}

	if coresLeftover > nextFreeCore {
		config.Topology.PC2Reader = nextFreeCore
		config.Topology.C1Reader = nextFreeCore
		nextFreeCore++
	}

	// Add P2 writer cores, up to 8 total
	if coresLeftover > nextFreeCore {
		config.Topology.PC2Writer, config.Topology.PC2Reader = config.Topology.PC2Reader, config.Topology.PC2Writer

		for i := 0; i < 7 && coresLeftover > nextFreeCore; i++ {
			config.Topology.PC2WriterCores++
			nextFreeCore++
		}
	}

	config.UnoccupiedCores = coresLeftover - nextFreeCore

	sectorConfig := SectorConfig{
		Sectors:      batchSize,
		Coordinators: []CoordinatorConfig{},
	}

	ccxCores := make([]int, 0)
	for i := 0; i < info.CoreCount; i += info.CoresPerL3 {
		ccxCores = append(ccxCores, i)
	}

	for i := config.RequiredCores; i > 0; {
		firstCCXCoreNum := ccxCores[len(ccxCores)-1]
		toAssign := min(i, info.CoresPerL3)

		coreNum := firstCCXCoreNum + info.CoresPerL3 - toAssign

		sectorConfig.Coordinators = append(sectorConfig.Coordinators, CoordinatorConfig{
			Core:    coreNum,
			Hashers: (toAssign - 1) * info.ThreadsPerCore,
		})

		i -= toAssign
		if toAssign == info.CoresPerL3 {
			ccxCores = ccxCores[:len(ccxCores)-1]
			if len(ccxCores) == 0 {
				break
			}
		}
	}

	// Reverse the order of coordinators
	for i, j := 0, len(sectorConfig.Coordinators)-1; i < j; i, j = i+1, j-1 {
		sectorConfig.Coordinators[i], sectorConfig.Coordinators[j] = sectorConfig.Coordinators[j], sectorConfig.Coordinators[i]
	}

	config.Topology.SectorConfigs = append(config.Topology.SectorConfigs, sectorConfig)

	return config, nil
}

func FormatSupraSealConfig(config SupraSealConfig) string {
	var sb strings.Builder

	w := func(s string) { sb.WriteString(s); sb.WriteByte('\n') }

	w("# Configuration for supra_seal")
	w("spdk: {")
	w("  # PCIe identifiers of NVMe drives to use to store layers")
	w("  nvme = [ ")

	quotedNvme := lo.Map(config.NVMeDevices, func(d string, i int) string { return `           "` + d + `"` })
	w(strings.Join(quotedNvme, ",\n"))

	w("         ];")
	w("}")
	w("")
	w("# CPU topology for various parallel sector counts")
	w("topology:")
	w("{")
	w("  pc1: {")
	w(fmt.Sprintf("    writer       = %d;", config.Topology.PC1Writer))
	w(fmt.Sprintf("    reader       = %d;", config.Topology.PC1Reader))
	w(fmt.Sprintf("    orchestrator = %d;", config.Topology.PC1Orchestrator))
	w("    qpair_reader = 0;")
	w("    qpair_writer = 1;")
	w("    reader_sleep_time = 250;")
	w("    writer_sleep_time = 500;")
	w("    hashers_per_core = 2;")
	w("")
	w("    sector_configs: (")
	for i, sectorConfig := range config.Topology.SectorConfigs {
		w("      {")
		w(fmt.Sprintf("        sectors = %d;", sectorConfig.Sectors))
		w("        coordinators = (")
		for i, coord := range sectorConfig.Coordinators {
			sb.WriteString(fmt.Sprintf("          { core = %d;\n", coord.Core))
			sb.WriteString(fmt.Sprintf("            hashers = %d; }", coord.Hashers))
			if i < len(sectorConfig.Coordinators)-1 {
				sb.WriteString(",")
			}
			sb.WriteByte('\n')
		}
		w("        )")
		sb.WriteString("      }")

		if i < len(config.Topology.SectorConfigs)-1 {
			sb.WriteString(",")
		}
		sb.WriteByte('\n')
	}
	w("    )")
	w("  },")
	w("")
	w("  pc2: {")
	w(fmt.Sprintf("    reader = %d;", config.Topology.PC2Reader))
	w(fmt.Sprintf("    hasher = %d;", config.Topology.PC2Hasher))
	w(fmt.Sprintf("    hasher_cpu = %d;", config.Topology.PC2HasherCPU))
	w(fmt.Sprintf("    writer = %d;", config.Topology.PC2Writer))
	w(fmt.Sprintf("    writer_cores = %d;", config.Topology.PC2WriterCores))
	w("    sleep_time = 200;")
	w("    qpair = 2;")
	w("  },")
	w("")
	w("  c1: {")
	w(fmt.Sprintf("    reader = %d;", config.Topology.C1Reader))
	w("    sleep_time = 200;")
	w("    qpair = 3;")
	w("  }")
	w("}")

	return sb.String()
}
