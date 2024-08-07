package main

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/tasks/sealsupra"
)

var calcCmd = &cli.Command{
	Name:  "calc",
	Usage: "Math Utils",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "actor",
		},
	},
	Subcommands: []*cli.Command{
		calcBatchCpuCmd,
		calcSuprasealConfigCmd,
	},
}

var calcBatchCpuCmd = &cli.Command{
	Name:  "batch-cpu",
	Usage: "Analyze and display the layout of batch sealer threads",
	Description: `Analyze and display the layout of batch sealer threads on your CPU.

It provides detailed information about CPU utilization for batch sealing operations, including core allocation, thread
distribution for different batch sizes.`,
	Flags: []cli.Flag{
		&cli.BoolFlag{Name: "dual-hashers", Value: true},
	},
	Action: func(cctx *cli.Context) error {
		info, err := sealsupra.GetSystemInfo()
		if err != nil {
			return err
		}

		fmt.Println("Basic CPU Information")
		fmt.Println("")
		fmt.Printf("Processor count: %d\n", info.ProcessorCount)
		fmt.Printf("Core count: %d\n", info.CoreCount)
		fmt.Printf("Thread count: %d\n", info.CoreCount*info.ThreadsPerCore)
		fmt.Printf("Threads per core: %d\n", info.ThreadsPerCore)
		fmt.Printf("Cores per L3 cache (CCX): %d\n", info.CoresPerL3)
		fmt.Printf("L3 cache count (CCX count): %d\n", info.CoreCount/info.CoresPerL3)

		ccxFreeCores := info.CoresPerL3 - 1 // one core per ccx goes to the coordinator
		ccxFreeThreads := ccxFreeCores * info.ThreadsPerCore
		fmt.Printf("Hasher Threads per CCX: %d\n", ccxFreeThreads)

		sectorsPerThread := 1
		if cctx.Bool("dual-hashers") {
			sectorsPerThread = 2
		}

		sectorsPerCCX := ccxFreeThreads * sectorsPerThread
		fmt.Printf("Sectors per CCX: %d\n", sectorsPerCCX)

		fmt.Println("---------")

		printForBatchSize := func(batchSize int) {
			fmt.Printf("Batch Size: %s sectors\n", color.CyanString("%d", batchSize))
			fmt.Println()

			config, err := sealsupra.GenerateSupraSealConfig(*info, cctx.Bool("dual-hashers"), batchSize, nil)
			if err != nil {
				fmt.Printf("Error generating config: %s\n", err)
				return
			}

			fmt.Printf("Required Threads: %d\n", config.RequiredThreads)
			fmt.Printf("Required CCX: %d\n", config.RequiredCCX)
			fmt.Printf("Required Cores: %d hasher (+4 minimum for non-hashers)\n", config.RequiredCores)

			enoughCores := config.RequiredCores <= info.CoreCount
			if enoughCores {
				fmt.Printf("Enough cores available for hashers %s\n", color.GreenString("✔"))
			} else {
				fmt.Printf("Not enough cores available for hashers %s\n", color.RedString("✘"))
				return
			}

			fmt.Printf("Non-hasher cores: %d\n", info.CoreCount-config.RequiredCores)

			if config.P2WrRdOverlap {
				color.Yellow("! P2 writer will share a core with P2 reader, performance may be impacted")
			}
			if config.P2HsP1WrOverlap {
				color.Yellow("! P2 hasher will share a core with P1 writer, performance may be impacted")
			}
			if config.P2HcP2RdOverlap {
				color.Yellow("! P2 hasher_cpu will share a core with P2 reader, performance may be impacted")
			}

			fmt.Println()
			fmt.Printf("pc1 writer: %d\n", config.Topology.PC1Writer)
			fmt.Printf("pc1 reader: %d\n", config.Topology.PC1Reader)
			fmt.Printf("pc1 orchestrator: %d\n", config.Topology.PC1Orchestrator)
			fmt.Println()
			fmt.Printf("pc2 reader: %d\n", config.Topology.PC2Reader)
			fmt.Printf("pc2 hasher: %d\n", config.Topology.PC2Hasher)
			fmt.Printf("pc2 hasher_cpu: %d\n", config.Topology.PC2HasherCPU)
			fmt.Printf("pc2 writer: %d\n", config.Topology.PC2Writer)
			fmt.Printf("pc2 writer_cores: %d\n", config.Topology.PC2WriterCores)
			fmt.Println()
			fmt.Printf("c1 reader: %d\n", config.Topology.C1Reader)
			fmt.Println()

			fmt.Printf("Unoccupied Cores: %d\n\n", config.UnoccupiedCores)

			fmt.Println("{")
			fmt.Printf("  sectors = %d;\n", batchSize)
			fmt.Println("  coordinators = (")
			for i, coord := range config.Topology.SectorConfigs[0].Coordinators {
				fmt.Printf("    { core = %d;\n      hashers = %d; }", coord.Core, coord.Hashers)
				if i < len(config.Topology.SectorConfigs[0].Coordinators)-1 {
					fmt.Println(",")
				} else {
					fmt.Println()
				}
			}
			fmt.Println("  )")
			fmt.Println("}")

			fmt.Println("---------")
		}

		printForBatchSize(16)
		printForBatchSize(32)
		printForBatchSize(64)
		printForBatchSize(128)

		return nil
	},
}

var calcSuprasealConfigCmd = &cli.Command{
	Name:  "supraseal-config",
	Usage: "Generate a supra_seal configuration",
	Description: `Generate a supra_seal configuration for a given batch size.

This command outputs a configuration expected by SupraSeal. Main purpose of this command is for debugging and testing.
The config can be used directly with SupraSeal binaries to test it without involving Curio.`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "dual-hashers",
			Value: true,
			Usage: "Zen3 and later supports two sectors per thread, set to false for older CPUs",
		},
		&cli.IntFlag{
			Name:     "batch-size",
			Aliases:  []string{"b"},
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		sysInfo, err := sealsupra.GetSystemInfo()
		if err != nil {
			return err
		}

		config, err := sealsupra.GenerateSupraSealConfig(*sysInfo, cctx.Bool("dual-hashers"), cctx.Int("batch-size"), nil)
		if err != nil {
			return err
		}
		fmt.Println(sealsupra.FormatSupraSealConfig(config))
		return nil
	},
}
