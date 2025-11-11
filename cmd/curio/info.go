package main

import (
	"fmt"

	"github.com/dustin/go-humanize"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/cmd/curio/rpc"
)

var infoCmd = &cli.Command{
	Name:  "info",
	Usage: translations.T("Get Curio node info"),
	Action: func(cctx *cli.Context) error {
		api, closer, err := rpc.GetCurioAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()

		info, err := api.Info(cctx.Context)
		if err != nil {
			return err
		}
		fmt.Printf("Node Info:\n")
		fmt.Printf("ID: %d\n", info.ID)
		if info.Name.Valid {
			fmt.Printf("Name: %s\n", info.Name.String)
		}
		fmt.Printf("CPU: %d\n", info.CPU)
		fmt.Printf("RAM: %s\n", humanize.Bytes(uint64(info.RAM)))
		fmt.Printf("GPU: %.2f\n", info.GPU)
		fmt.Printf("Schedulable: %t\n", !info.Unschedulable)
		fmt.Printf("HostPort: %s\n", info.HostPort)
		if info.Tasks.Valid {
			fmt.Printf("Tasks: %s\n", info.Tasks.String)
		} else {
			fmt.Printf("Tasks: None\n")
		}
		if info.Layers.Valid {
			fmt.Printf("Layers: %s\n", info.Layers.String)
		} else {
			fmt.Printf("Layers: None\n")
		}
		if info.Miners.Valid {
			fmt.Printf("Miners: %s\n", info.Miners.String)
		} else {
			fmt.Printf("Miners: None\n")
		}
		fmt.Printf("LastContact: %s\n", info.LastContact)
		if info.StartupTime.Valid {
			fmt.Printf("StartupTime: %s\n", info.StartupTime.Time)
		} else {
			fmt.Printf("StartupTime: N/A\n")
		}
		return nil
	},
}
