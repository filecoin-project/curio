package pdpnode

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/shutdown"
)

// Run starts the PDP node until shutdown.
func Run(cctx *cli.Context) error {
	ctx := context.Background()
	maxboomDockerLog("starting")
	if cctx.Bool("manage-fdlimit") {
		manageFdLimit()
	}

	d, err := Open(ctx, cctx)
	if err != nil {
		return err
	}
	defer d.Close()

	if _, err := StartAdmin(ctx, d); err != nil {
		return xerrors.Errorf("admin http: %w", err)
	}
	maxboomDockerLog("admin GUI listening on http://%s", d.Cfg.Subsystems.GuiAddress)

	taskRes, err := RegisterTasks(ctx, d)
	if err != nil {
		return xerrors.Errorf("register tasks: %w", err)
	}
	defer taskRes.Engine.GracefullyTerminate()

	if err := StartPublic(ctx, d, taskRes); err != nil {
		return xerrors.Errorf("public http: %w", err)
	}

	shutdownChan := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		close(shutdownChan)
	}()

	<-shutdown.MonitorShutdown(shutdownChan)
	return nil
}
