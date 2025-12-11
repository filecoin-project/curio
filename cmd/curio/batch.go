package main

import (
	"fmt"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/lib/supraffi"
)

var batchCmd = &cli.Command{
	Name:  "batch",
	Usage: translations.T("Manage batch sealing operations"),
	Subcommands: []*cli.Command{
		batchSetupCmd,
	},
}

var batchSetupCmd = &cli.Command{
	Name:  "setup",
	Usage: translations.T("Setup SPDK for batch sealing (configures hugepages and binds NVMe devices)"),
	Description: translations.T(`Setup SPDK for batch sealing operations.

This command automatically:
- Downloads SPDK if not already available
- Configures 1GB hugepages (36 pages minimum)
- Binds NVMe devices for use with SupraSeal

Requires root/sudo access for SPDK setup operations.`),
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "hugepages",
			Usage: translations.T("Number of 1GB hugepages to configure"),
			Value: 36,
		},
		&cli.IntFlag{
			Name:  "min-pages",
			Usage: translations.T("Minimum number of hugepages required"),
			Value: 36,
		},
	},
	Action: func(cctx *cli.Context) error {
		nrHuge := cctx.Int("hugepages")
		minPages := cctx.Int("min-pages")

		fmt.Println("Setting up SPDK for batch sealing...")
		fmt.Printf("Configuring %d hugepages (minimum required: %d)\n", nrHuge, minPages)

		err := supraffi.CheckAndSetupSPDK(nrHuge, minPages)
		if err != nil {
			return xerrors.Errorf("SPDK setup failed: %w\n\n"+
				"Please ensure you have:\n"+
				"1. Root/sudo access for SPDK setup\n"+
				"2. Raw NVMe devices available (no filesystems on them)\n"+
				"3. Sufficient hugepages configured (see documentation)", err)
		}

		fmt.Println("✓ SPDK setup completed successfully")
		fmt.Println("\nNext steps:")
		fmt.Println("1. Verify hugepages: cat /proc/meminfo | grep Huge")
		fmt.Println("2. Configure your batch sealing layer (see documentation)")
		fmt.Println("3. Start batch sealing operations")

		return nil
	},
}
