package main

import (
	"fmt"
	"github.com/fatih/color"
	"github.com/filecoin-project/curio/tasks/sealsupra"
	"github.com/urfave/cli/v2"
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
	Usage: "See layout of batch sealer threads",
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
			fmt.Printf("Required Threads: %d\n", batchSize/sectorsPerThread)
			requiredCCX := (batchSize + sectorsPerCCX - 1) / sectorsPerCCX
			fmt.Printf("Required CCX: %d\n", requiredCCX)

			requiredCores := requiredCCX + batchSize/sectorsPerThread/info.ThreadsPerCore
			fmt.Printf("Required Cores: %d hasher (+4 minimum for non-hashers)\n", requiredCores)

			enoughCores := requiredCores <= info.CoreCount
			if enoughCores {
				fmt.Printf("Enough cores available for hashers %s\n", color.GreenString("✔"))
			} else {
				fmt.Printf("Not enough cores available for hashers %s\n", color.RedString("✘"))
				return
			}

			coresLeftover := info.CoreCount - requiredCores
			fmt.Printf("Non-hasher cores: %d\n", coresLeftover)

			const minOverheadCores = 4

			type CoreNum = int // core number, 0-based

			var (
				// core assignments for non-hasher work
				// defaults are the absolutely worst case of just 4 cores available

				pc1writer       CoreNum = 1
				pc1reader       CoreNum = 2
				pc1orchestrator CoreNum = 3

				pc2reader     CoreNum = 0
				pc2hasher     CoreNum = 1
				pc2hasher_cpu CoreNum = 0
				pc2writer     CoreNum = 0

				c1reader CoreNum = 0

				pc2writer_cores int = 1
			)

			if coresLeftover < minOverheadCores {
				fmt.Printf("Not enough cores for coordination %s\n", color.RedString("✘"))
				return
			} else {
				fmt.Printf("Enough cores for coordination %s\n", color.GreenString("✔"))
			}

			nextFreeCore := minOverheadCores

			// first move pc2 to individual cores
			if coresLeftover > nextFreeCore {
				pc2writer = nextFreeCore
				nextFreeCore++
			} else {
				color.Yellow("! P2 writer will share a core with P2 reader, performance may be impacted")
			}

			if coresLeftover > nextFreeCore {
				pc2hasher = nextFreeCore
				nextFreeCore++
			} else {
				color.Yellow("! P2 hasher will share a core with P2 writer, performance may be impacted")
			}

			if coresLeftover > nextFreeCore {
				pc2hasher_cpu = nextFreeCore
				nextFreeCore++
			} else {
				color.Yellow("! P2 hasher_cpu will share a core with P2 reader, performance may be impacted")
			}

			if coresLeftover > nextFreeCore {
				// might be fine to sit on core0, but let's not do that
				pc2reader = nextFreeCore
				c1reader = nextFreeCore
				nextFreeCore++
			}

			// add p2 writer cores, up to 8 total
			if coresLeftover > nextFreeCore {
				// swap pc2reader with pc2writer
				pc2writer, pc2reader = pc2reader, pc2writer

				for i := 0; i < 7; i++ {
					if coresLeftover > nextFreeCore {
						pc2writer_cores++
						nextFreeCore++
					}
				}
			}

			fmt.Println()
			fmt.Printf("pc1 writer: %d\n", pc1writer)
			fmt.Printf("pc1 reader: %d\n", pc1reader)
			fmt.Printf("pc1 orchestrator: %d\n", pc1orchestrator)
			fmt.Println()
			fmt.Printf("pc2 reader: %d\n", pc2reader)
			fmt.Printf("pc2 hasher: %d\n", pc2hasher)
			fmt.Printf("pc2 hasher_cpu: %d\n", pc2hasher_cpu)
			fmt.Printf("pc2 writer: %d\n", pc2writer)
			fmt.Printf("pc2 writer_cores: %d\n", pc2writer_cores)
			fmt.Println()
			fmt.Printf("c1 reader: %d\n", c1reader)
			fmt.Println()

			unoccupiedCores := coresLeftover - nextFreeCore
			fmt.Printf("Unoccupied Cores: %d\n\n", unoccupiedCores)

			var ccxCores []CoreNum // first core in each CCX
			for i := 0; i < info.CoreCount; i += info.CoresPerL3 {
				ccxCores = append(ccxCores, i)
			}

			type sectorCoreConfig struct {
				core    CoreNum // coordinator core
				hashers CoreNum // number of hasher cores
			}
			var coreConfigs []sectorCoreConfig

			for i := requiredCores; i > 0; {
				firstCCXCoreNum := ccxCores[len(ccxCores)-1]
				toAssign := min(i, info.CoresPerL3)

				// shift up the first core if possible so that cores on the right are used first
				coreNum := firstCCXCoreNum + info.CoresPerL3 - toAssign

				coreConfigs = append(coreConfigs, sectorCoreConfig{
					core:    coreNum,
					hashers: (toAssign - 1) * info.ThreadsPerCore,
				})

				i -= toAssign
				if toAssign == info.CoresPerL3 {
					ccxCores = ccxCores[:len(ccxCores)-1]
					if len(ccxCores) == 0 {
						break
					}
				}
			}

			// reverse the order
			for i, j := 0, len(coreConfigs)-1; i < j; i, j = i+1, j-1 {
				coreConfigs[i], coreConfigs[j] = coreConfigs[j], coreConfigs[i]
			}

			fmt.Println("{")
			fmt.Printf("  sectors = %d;\n", batchSize)
			fmt.Println("  coordinators = (")
			for i, config := range coreConfigs {
				fmt.Printf("    { core = %d;\n      hashers = %d; }", config.core, config.hashers)
				if i < len(coreConfigs)-1 {
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