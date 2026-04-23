package helpers

import (
	"flag"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

func CreateCliContext(dir string, layers []string) (*cli.Context, error) {
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

	defaultLayers := []string{"seal", "market", "upgrade"}
	layers = append(layers, defaultLayers...)

	parseArgs := []string{rflag, "--listen=" + listenAddr, "--nosync", "--manage-fdlimit"}
	if len(layers) > 0 {
		parseArgs = append(parseArgs, "--layers="+strings.Join(layers, ","))
	}

	// Parse the flags with test values
	err = set.Parse(parseArgs)
	if err != nil {
		return nil, xerrors.Errorf("Error setting flag: %s\n", err)
	}

	// Create a cli.Context from the FlagSet
	app := cli.NewApp()
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = command

	return ctx, nil
}

// WaitForTCP waits until something accepts TCP connections on addr (e.g. "127.0.0.1:4700").
// Use after starting an RPC server in a goroutine so clients do not dial before Listen completes.
func WaitForTCP(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	dialAddr := loopbackDialAddr(addr)
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", dialAddr, 150*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, timeout, 25*time.Millisecond, "timeout waiting for TCP listener on %s", dialAddr)
}

// loopbackDialAddr maps a listen address to one we can dial from the same host.
func loopbackDialAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return net.JoinHostPort("127.0.0.1", port)
	default:
		return addr
	}
}
