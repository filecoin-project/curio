package sealsupra

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/samber/lo"
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

type AdditionalSystemInfo struct {
	CPUName           string
	MemorySize        string
	MemoryChannels    int
	InstalledModules  int
	MaxMemoryCapacity string
	MemoryType        string
	MemorySpeed       string
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

		if packageRegex.MatchString(line) {
			info.ProcessorCount++
		}

		if info.ProcessorCount > 1 {
			// in multi-socket systems, we only care about the first socket, rest are the same
			continue
		}

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

func FormatSupraSealConfig(config SupraSealConfig, system SystemInfo, additionalInfo AdditionalSystemInfo) string {
	var sb strings.Builder

	w := func(s string) { sb.WriteString(s); sb.WriteByte('\n') }

	w("# Configuration for supra_seal")
	w("")
	w("# Machine Specifications:")
	w(fmt.Sprintf("# CPU: %s", additionalInfo.CPUName))
	w(fmt.Sprintf("# Memory: %s", additionalInfo.MemorySize))
	w(fmt.Sprintf("# Memory Type: %s", additionalInfo.MemoryType))
	w(fmt.Sprintf("# Memory Speed: %s", additionalInfo.MemorySpeed))
	w(fmt.Sprintf("# Installed Memory Modules: %d", additionalInfo.InstalledModules))
	w(fmt.Sprintf("# Maximum Memory Capacity: %s", additionalInfo.MaxMemoryCapacity))
	w(fmt.Sprintf("# Memory Channels: %d", additionalInfo.MemoryChannels))
	w(fmt.Sprintf("# Processor Count: %d", system.ProcessorCount))
	w(fmt.Sprintf("# Core Count: %d", system.CoreCount))
	w(fmt.Sprintf("# Threads per Core: %d", system.ThreadsPerCore))
	w(fmt.Sprintf("# Cores per L3 Cache: %d", system.CoresPerL3))
	w("")
	w("# Diagnostic Information:")
	w(fmt.Sprintf("# Required Threads: %d", config.RequiredThreads))
	w(fmt.Sprintf("# Required CCX: %d", config.RequiredCCX))
	w(fmt.Sprintf("# Required Cores: %d", config.RequiredCores))
	w(fmt.Sprintf("# Unoccupied Cores: %d", config.UnoccupiedCores))
	w(fmt.Sprintf("# P2 Writer/Reader Overlap: %v", config.P2WrRdOverlap))
	w(fmt.Sprintf("# P2 Hasher/P1 Writer Overlap: %v", config.P2HsP1WrOverlap))
	w(fmt.Sprintf("# P2 Hasher CPU/P2 Reader Overlap: %v", config.P2HcP2RdOverlap))
	w("")
	w("spdk: {")
	w("  # PCIe identifiers of NVMe drives to use to store layers")
	w("  nvme = [ ")

	quotedNvme := lo.Map(config.NVMeDevices, func(d string, _ int) string { return `           "` + d + `"` })
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

	sectorConfigsStr := lo.Map(config.Topology.SectorConfigs, func(sectorConfig SectorConfig, i int) string {
		coordsStr := lo.Map(sectorConfig.Coordinators, func(coord CoordinatorConfig, j int) string {
			return fmt.Sprintf("          { core = %d;\n            hashers = %d; }%s\n",
				coord.Core, coord.Hashers, lo.Ternary(j < len(sectorConfig.Coordinators)-1, ",", ""))
		})

		return fmt.Sprintf("      {\n        sectors = %d;\n        coordinators = (\n%s        )\n      }%s\n",
			sectorConfig.Sectors, strings.Join(coordsStr, ""), lo.Ternary(i < len(config.Topology.SectorConfigs)-1, ",", ""))
	})

	w(strings.Join(sectorConfigsStr, ""))

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

func ExtractAdditionalSystemInfo() (AdditionalSystemInfo, error) {
	info := AdditionalSystemInfo{}

	// Extract CPU Name (unchanged)
	cpuInfoCmd := exec.Command("lscpu")
	cpuInfoOutput, err := cpuInfoCmd.Output()
	if err != nil {
		return info, fmt.Errorf("failed to execute lscpu: %v", err)
	}

	cpuInfoLines := strings.Split(string(cpuInfoOutput), "\n")
	for _, line := range cpuInfoLines {
		if strings.HasPrefix(line, "Model name:") {
			info.CPUName = strings.TrimSpace(strings.TrimPrefix(line, "Model name:"))
			break
		}
	}

	// Extract Memory Information
	memInfoCmd := exec.Command("dmidecode", "-t", "memory")
	memInfoOutput, err := memInfoCmd.Output()
	if err != nil {
		log.Warnf("failed to execute dmidecode: %v", err)
		return info, nil
	}

	memInfoLines := strings.Split(string(memInfoOutput), "\n")
	var totalMemory int64
	for _, line := range memInfoLines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Maximum Capacity:") {
			info.MaxMemoryCapacity = strings.TrimSpace(strings.TrimPrefix(line, "Maximum Capacity:"))
		} else if strings.HasPrefix(line, "Number Of Devices:") {
			info.MemoryChannels, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Number Of Devices:")))
		} else if strings.HasPrefix(line, "Size:") {
			if strings.Contains(line, "GB") {
				sizeStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "Size:"), "GB"))
				size, _ := strconv.ParseInt(sizeStr, 10, 64)
				if size > 0 {
					totalMemory += size
					info.InstalledModules++
				}
			}
		} else if strings.HasPrefix(line, "Type:") && info.MemoryType == "" {
			info.MemoryType = strings.TrimSpace(strings.TrimPrefix(line, "Type:"))
		} else if strings.HasPrefix(line, "Speed:") && info.MemorySpeed == "" {
			info.MemorySpeed = strings.TrimSpace(strings.TrimPrefix(line, "Speed:"))
		}
	}

	info.MemorySize = fmt.Sprintf("%d GB", totalMemory)

	return info, nil
}

func GenerateSupraSealConfigString(dualHashers bool, batchSize int, nvmeDevices []string) (string, error) {
	// Get system information
	sysInfo, err := GetSystemInfo()
	if err != nil {
		return "", fmt.Errorf("failed to get system info: %v", err)
	}

	// Generate SupraSealConfig
	config, err := GenerateSupraSealConfig(*sysInfo, dualHashers, batchSize, nvmeDevices)
	if err != nil {
		return "", fmt.Errorf("failed to generate SupraSeal config: %v", err)
	}

	// Get additional system information
	additionalInfo, err := ExtractAdditionalSystemInfo()
	if err != nil {
		return "", fmt.Errorf("failed to extract additional system info: %v", err)
	}

	// Format the config
	configString := FormatSupraSealConfig(config, *sysInfo, additionalInfo)

	return configString, nil
}
