package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"

	"github.com/docker/go-units"
	"github.com/fatih/color"
	logging "github.com/ipfs/go-log/v2"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	curiobuild "github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/cmd/curio/guidedsetup"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/fastparamfetch"
	"github.com/filecoin-project/curio/lib/panicreport"
	"github.com/filecoin-project/curio/lib/repo"
	"github.com/filecoin-project/curio/lib/reqcontext"

	proofparams "github.com/filecoin-project/lotus/build/proof-params"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/lib/tracing"
)

var log = logging.Logger("main")

const (
	FlagMinerRepo = "miner-repo"
)

func SetupLogLevels() {
	if _, set := os.LookupEnv("GOLOG_LOG_LEVEL"); !set {
		_ = logging.SetLogLevel("*", "INFO")
		_ = logging.SetLogLevel("harmonytask", "DEBUG")
		_ = logging.SetLogLevel("rpc", "ERROR")
	}
}

func setupCloseHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\r- Ctrl+C pressed in Terminal")
		_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
		panic(1)
	}()
}

func main() {
	SetupLogLevels()
	local := []*cli.Command{
		cliCmd,
		runCmd,
		configCmd,
		testCmd,
		webCmd,
		guidedsetup.GuidedsetupCmd,
		sealCmd,
		marketCmd,
		fetchParamCmd,
		ffiCmd,
		calcCmd,
	}

	jaeger := tracing.SetupJaegerTracing("curio")
	defer func() {
		if jaeger != nil {
			_ = jaeger.ForceFlush(context.Background())
		}
	}()

	for _, cmd := range local {
		cmd := cmd
		originBefore := cmd.Before
		cmd.Before = func(cctx *cli.Context) error {
			if jaeger != nil {
				_ = jaeger.Shutdown(cctx.Context)
			}
			jaeger = tracing.SetupJaegerTracing("curio/" + cmd.Name)

			if cctx.IsSet("color") {
				color.NoColor = !cctx.Bool("color")
			}

			if originBefore != nil {
				return originBefore(cctx)
			}

			return nil
		}
	}

	app := &cli.App{
		Name:                 "curio",
		Usage:                "Filecoin decentralized storage network provider",
		Version:              curiobuild.UserVersion(),
		EnableBashCompletion: true,
		Before: func(c *cli.Context) error {
			setupCloseHandler()
			cliutil.IsVeryVerbose = c.Bool("vv")
			return nil
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				// examined in the Before above
				Name:        "color",
				Usage:       "use color in display output",
				DefaultText: "depends on output being a TTY",
			},
			&cli.StringFlag{
				Name:    "panic-reports",
				EnvVars: []string{"CURIO_PANIC_REPORT_PATH"},
				Hidden:  true,
				Value:   "~/.curio", // should follow --repo default
			},
			&cli.StringFlag{
				Name:    "db-host",
				EnvVars: []string{"CURIO_DB_HOST", "CURIO_HARMONYDB_HOSTS"},
				Usage:   "Command separated list of hostnames for yugabyte cluster",
				Value:   "127.0.0.1",
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
			&cli.StringFlag{
				Name:    deps.FlagRepoPath,
				EnvVars: []string{"CURIO_REPO_PATH"},
				Value:   "~/.curio",
			},
			&cli.BoolFlag{ // disconnected from cli/util for dependency reasons. Not used in curio that way.
				Name:  "vv",
				Usage: "enables very verbose mode, useful for debugging the CLI",
			},
		},
		Commands: local,
		After: func(c *cli.Context) error {
			if r := recover(); r != nil {
				p, err := homedir.Expand(c.String(FlagMinerRepo))
				if err != nil {
					log.Errorw("could not expand repo path for panic report", "error", err)
					panic(r)
				}

				// Generate report in CURIO_PATH and re-raise panic
				panicreport.GeneratePanicReport(c.String("panic-reports"), p, c.App.Name)
				panic(r)
			}
			return nil
		},
	}
	app.Setup()
	app.Metadata["repoType"] = repo.Curio
	runApp(app)
}

var fetchParamCmd = &cli.Command{
	Name:      "fetch-params",
	Usage:     "Fetch proving parameters",
	ArgsUsage: "[sectorSize]",
	Action: func(cctx *cli.Context) error {
		if cctx.NArg() != 1 {
			return xerrors.Errorf("incorrect number of arguments")
		}
		sectorSizeInt, err := units.RAMInBytes(cctx.Args().First())
		if err != nil {
			return xerrors.Errorf("error parsing sector size (specify as \"32GiB\", for instance): %w", err)
		}
		sectorSize := uint64(sectorSizeInt)
		err = fastparamfetch.GetParams(reqcontext.ReqContext(cctx), proofparams.ParametersJSON(), proofparams.SrsJSON(), sectorSize)
		if err != nil {
			return xerrors.Errorf("fetching proof parameters: %w", err)
		}

		return nil
	},
}

func runApp(app *cli.App) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-c
		os.Exit(1)
	}()

	if err := app.Run(os.Args); err != nil {
		if os.Getenv("LOTUS_DEV") != "" {
			log.Warnf("%+v", err)
		} else {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n\n", err) // nolint:errcheck
		}

		var phe *PrintHelpErr
		if errors.As(err, &phe) {
			_ = cli.ShowCommandHelp(phe.Ctx, phe.Ctx.Command.Name)
		}
		os.Exit(1)
	}
}

type PrintHelpErr struct {
	Err error
	Ctx *cli.Context
}

func (e *PrintHelpErr) Error() string {
	return e.Err.Error()
}
