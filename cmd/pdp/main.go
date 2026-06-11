package main

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"

	curiobuild "github.com/filecoin-project/curio/build"
	curiodeps "github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/pdpnode"
)

func main() {
	app := &cli.App{
		Name:  "pdp",
		Usage: "PDP storage service",
		Version: curiobuild.UserVersion(),
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "color", Usage: "use color in display output"},
			&cli.StringFlag{
				Name:    "db-host",
				EnvVars: []string{"CURIO_DB_HOST", "CURIO_HARMONYDB_HOSTS"},
				Value:   "127.0.0.1",
			},
			&cli.StringFlag{
				Name:        "db-host-cql",
				EnvVars:     []string{"CURIO_DB_HOST_CQL"},
				Value:       "",
				DefaultText: "<--db-host>",
			},
			&cli.StringFlag{
				Name:    "db-name",
				EnvVars: []string{"CURIO_DB_NAME", "CURIO_HARMONYDB_NAME"},
				Value:   "yugabyte",
			},
			&cli.StringFlag{
				Name:    "db-user",
				EnvVars: []string{"CURIO_DB_USER", "CURIO_HARMONYDB_USERNAME"},
				Value:   "yugabyte",
			},
			&cli.StringFlag{
				Name:    "db-password",
				EnvVars: []string{"CURIO_DB_PASSWORD", "CURIO_HARMONYDB_PASSWORD"},
				Value:   "yugabyte",
			},
			&cli.StringFlag{
				Name:    "db-port",
				EnvVars: []string{"CURIO_DB_PORT", "CURIO_HARMONYDB_PORT"},
				Value:   "5433",
			},
			&cli.IntFlag{
				Name:    "db-cassandra-port",
				EnvVars: []string{"CURIO_DB_CASSANDRA_PORT", "CURIO_INDEXDB_PORT"},
				Value:   9042,
			},
			&cli.BoolFlag{
				Name:    "db-load-balance",
				EnvVars: []string{"CURIO_DB_LOAD_BALANCE", "CURIO_HARMONYDB_LOAD_BALANCE"},
				Value:   true,
			},
			&cli.StringFlag{
				Name:    curiodeps.FlagRepoPath,
				EnvVars: []string{"CURIO_REPO_PATH"},
				Value:   "~/.curio",
			},
			&cli.StringFlag{
				Name:    "machine-host",
				EnvVars: []string{"PDP_MACHINE_HOST"},
				Usage:   "harmony machine identity (not a public listener)",
				Value:   "127.0.0.1:pdp",
			},
			&cli.StringFlag{
				Name:    "name",
				EnvVars: []string{"PDP_NODE_NAME"},
				Usage:   "custom node name",
			},
			&cli.BoolFlag{
				Name:  "manage-fdlimit",
				Value: true,
			},
		},
		Before: func(c *cli.Context) error {
			if c.IsSet("color") {
				color.NoColor = !c.Bool("color")
			}
			p, err := homedir.Expand(c.String(curiodeps.FlagRepoPath))
			if err != nil {
				return err
			}
			return c.Set(curiodeps.FlagRepoPath, p)
		},
		Action: pdpnode.Run,
	}

	app.Setup()
	if err := app.Run(os.Args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}
}
