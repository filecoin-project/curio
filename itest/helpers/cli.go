package helpers

import (
	"flag"
	"fmt"
	"net"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps"
)

func pickFreeListenAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", xerrors.Errorf("creating listen socket: %w", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String(), nil
}

func CreateCliContext(dir string) (*cli.Context, error) {
	// Define flags for the command
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "listen",
			Usage:   "host address and port the worker api will listen on",
			Value:   "0.0.0.0:12300",
			EnvVars: []string{"LOTUS_WORKER_LISTEN"},
		},
		&cli.BoolFlag{
			Name:  "nosync",
			Usage: "don't check full-node sync status",
		},
		&cli.BoolFlag{
			Name:   "halt-after-init",
			Usage:  "only run init, then return",
			Hidden: true,
		},
		&cli.BoolFlag{
			Name:  "manage-fdlimit",
			Usage: "manage open file limit",
			Value: true,
		},
		&cli.StringFlag{
			Name:  "storage-json",
			Usage: "path to json file containing storage config",
			Value: "~/.curio/storage.json",
		},
		&cli.StringSliceFlag{
			Name:    "layers",
			Aliases: []string{"l", "layer"},
			Usage:   "list of layers to be interpreted (atop defaults)",
		},
		&cli.StringFlag{
			Name:    deps.FlagRepoPath,
			EnvVars: []string{"CURIO_REPO_PATH"},
			Value:   "~/.curio",
		},
	}

	// Set up the command with flags
	command := &cli.Command{
		Name:  "simulate",
		Flags: flags,
		Action: func(c *cli.Context) error {
			fmt.Println("Listen address:", c.String("listen"))
			fmt.Println("No-sync:", c.Bool("nosync"))
			fmt.Println("Halt after init:", c.Bool("halt-after-init"))
			fmt.Println("Manage file limit:", c.Bool("manage-fdlimit"))
			fmt.Println("Storage config path:", c.String("storage-json"))
			fmt.Println("Journal path:", c.String("journal"))
			fmt.Println("Layers:", c.StringSlice("layers"))
			fmt.Println("Repo Path:", c.String(deps.FlagRepoPath))
			return nil
		},
	}

	// Create a FlagSet and populate it
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range flags {
		if err := f.Apply(set); err != nil {
			return nil, xerrors.Errorf("Error applying flag: %s\n", err)
		}
	}

	rflag := fmt.Sprintf("--%s=%s", deps.FlagRepoPath, dir)
	listenAddr, err := pickFreeListenAddr()
	if err != nil {
		return nil, err
	}

	// Parse the flags with test values
	err = set.Parse([]string{rflag, "--listen=" + listenAddr, "--nosync", "--manage-fdlimit", "--layers=seal"})
	if err != nil {
		return nil, xerrors.Errorf("Error setting flag: %s\n", err)
	}

	// Create a cli.Context from the FlagSet
	app := cli.NewApp()
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = command

	return ctx, nil
}

func CreateMarketCliContext(dir string) (*cli.Context, error) {
	// Define flags for the command
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "listen",
			Usage:   "host address and port the worker api will listen on",
			Value:   "0.0.0.0:12300",
			EnvVars: []string{"LOTUS_WORKER_LISTEN"},
		},
		&cli.BoolFlag{
			Name:  "nosync",
			Usage: "don't check full-node sync status",
		},
		&cli.BoolFlag{
			Name:   "halt-after-init",
			Usage:  "only run init, then return",
			Hidden: true,
		},
		&cli.BoolFlag{
			Name:  "manage-fdlimit",
			Usage: "manage open file limit",
			Value: true,
		},
		&cli.StringFlag{
			Name:  "storage-json",
			Usage: "path to json file containing storage config",
			Value: "~/.curio/storage.json",
		},
		&cli.StringSliceFlag{
			Name:    "layers",
			Aliases: []string{"l", "layer"},
			Usage:   "list of layers to be interpreted (atop defaults)",
		},
		&cli.StringFlag{
			Name:    deps.FlagRepoPath,
			EnvVars: []string{"CURIO_REPO_PATH"},
			Value:   "~/.curio",
		},
	}

	// Set up the command with flags
	command := &cli.Command{
		Name:  "simulate",
		Flags: flags,
		Action: func(c *cli.Context) error {
			return nil
		},
	}

	// Create a FlagSet and populate it
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range flags {
		if err := f.Apply(set); err != nil {
			return nil, xerrors.Errorf("Error applying flag: %s\n", err)
		}
	}

	rflag := fmt.Sprintf("--%s=%s", deps.FlagRepoPath, dir)
	listenAddr, err := pickFreeListenAddr()
	if err != nil {
		return nil, err
	}

	// Parse the flags with test values (including market layer)
	err = set.Parse([]string{rflag, "--listen=" + listenAddr, "--nosync", "--manage-fdlimit", "--layers=seal,market"})
	if err != nil {
		return nil, xerrors.Errorf("Error setting flag: %s\n", err)
	}

	// Create a cli.Context from the FlagSet
	app := cli.NewApp()
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = command

	return ctx, nil
}
