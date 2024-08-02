package sealsupra

import (
	"bufio"
	"fmt"
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
			PC1Writer:       1,
			PC1Reader:       2,
			PC1Orchestrator: 3,
			PC2Reader:       0,
			PC2Hasher:       1,
			PC2HasherCPU:    0,
			PC2Writer:       0,
			PC2WriterCores:  1,
			C1Reader:        0,
		},
	}

	sectorsPerThread := 1
	if dualHashers {
		sectorsPerThread = 2
	}

	ccxFreeCores := info.CoresPerL3 - 1 // one core per ccx goes to the coordinator
	ccxFreeThreads := ccxFreeCores * info.ThreadsPerCore
	sectorsPerCCX := ccxFreeThreads * sectorsPerThread

	requiredThreads := batchSize / sectorsPerThread
	requiredCCX := (batchSize + sectorsPerCCX - 1) / sectorsPerCCX
	requiredCores := requiredCCX + requiredThreads/info.ThreadsPerCore

	if requiredCores > info.CoreCount {
		return config, fmt.Errorf("not enough cores available for hashers")
	}

	coresLeftover := info.CoreCount - requiredCores

	const minOverheadCores = 4
	if coresLeftover < minOverheadCores {
		return config, fmt.Errorf("not enough cores available for overhead")
	}

	nextFreeCore := minOverheadCores

	// Assign cores for PC2 processes
	if coresLeftover > nextFreeCore {
		config.Topology.PC2Writer = nextFreeCore
		nextFreeCore++
	}

	if coresLeftover > nextFreeCore {
		config.Topology.PC2Hasher = nextFreeCore
		nextFreeCore++
	}

	if coresLeftover > nextFreeCore {
		config.Topology.PC2HasherCPU = nextFreeCore
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

	sectorConfig := SectorConfig{
		Sectors:      batchSize,
		Coordinators: []CoordinatorConfig{},
	}

	ccxCores := make([]int, 0)
	for i := 0; i < info.CoreCount; i += info.CoresPerL3 {
		ccxCores = append(ccxCores, i)
	}

	for i := requiredCores; i > 0; {
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

	sb.WriteString("# Configuration for supra_seal\n")
	sb.WriteString("spdk: {\n")
	sb.WriteString("  # PCIe identifiers of NVMe drives to use to store layers\n")
	sb.WriteString("  nvme = [ \n")
	for _, device := range config.NVMeDevices {
		sb.WriteString(fmt.Sprintf("           \"%s\",\n", device))
	}
	sb.WriteString("         ];\n")
	sb.WriteString("}\n\n")

	sb.WriteString("# CPU topology for various parallel sector counts\n")
	sb.WriteString("topology:\n")
	sb.WriteString("{\n")
	sb.WriteString("  pc1: {\n")
	sb.WriteString(fmt.Sprintf("    writer       = %d;\n", config.Topology.PC1Writer))
	sb.WriteString(fmt.Sprintf("    reader       = %d;\n", config.Topology.PC1Reader))
	sb.WriteString(fmt.Sprintf("    orchestrator = %d;\n", config.Topology.PC1Orchestrator))
	sb.WriteString("    qpair_reader = 0;\n")
	sb.WriteString("    qpair_writer = 1;\n")
	sb.WriteString("    reader_sleep_time = 250;\n")
	sb.WriteString("    writer_sleep_time = 500;\n")
	sb.WriteString("    hashers_per_core = 2;\n\n")

	sb.WriteString("    sector_configs: (\n")
	for i, sectorConfig := range config.Topology.SectorConfigs {
		sb.WriteString("      {\n")
		sb.WriteString(fmt.Sprintf("        sectors = %d;\n", sectorConfig.Sectors))
		sb.WriteString("        coordinators = (\n")
		for i, coord := range sectorConfig.Coordinators {
			sb.WriteString(fmt.Sprintf("          { core = %d;\n", coord.Core))
			sb.WriteString(fmt.Sprintf("            hashers = %d; }", coord.Hashers))
			if i < len(sectorConfig.Coordinators)-1 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("        )\n")
		sb.WriteString("      }")

		// if not last, add a comma
		if i < len(config.Topology.SectorConfigs)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("    )\n")
	sb.WriteString("  },\n")

	sb.WriteString("  pc2: {\n")
	sb.WriteString(fmt.Sprintf("    reader = %d;\n", config.Topology.PC2Reader))
	sb.WriteString(fmt.Sprintf("    hasher = %d;\n", config.Topology.PC2Hasher))
	sb.WriteString(fmt.Sprintf("    hasher_cpu = %d;\n", config.Topology.PC2HasherCPU))
	sb.WriteString(fmt.Sprintf("    writer = %d;\n", config.Topology.PC2Writer))
	sb.WriteString(fmt.Sprintf("    writer_cores = %d;\n", config.Topology.PC2WriterCores))
	sb.WriteString("    sleep_time = 200;\n")
	sb.WriteString("    qpair = 2;\n")
	sb.WriteString("  },\n")

	sb.WriteString("  c1: {\n")
	sb.WriteString(fmt.Sprintf("    reader = %d;\n", config.Topology.C1Reader))
	sb.WriteString("    sleep_time = 200;\n")
	sb.WriteString("    qpair = 3;\n")
	sb.WriteString("  }\n")

	sb.WriteString("}\n")

	return sb.String()
}
