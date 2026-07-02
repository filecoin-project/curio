package pdpnode

import (
	"context"
	"errors"
	"net/http"

	"github.com/filecoin-project/curio/cuhttp"
	"github.com/filecoin-project/curio/web"
)

// StartPublic serves the customer-facing PDP API.
func StartPublic(ctx context.Context, d *Deps, sd *TaskResult) error {
	cfg := d.Cfg.HTTP
	if !cfg.Enable {
		return nil
	}

	chiRouter := cuhttp.NewRouter(cuhttp.RouterConfig{DomainName: cfg.DomainName})
	cuhttp.MountStandardRoutes(chiRouter)

	if err := MountPublicRoutes(ctx, chiRouter, d, &sd.ServiceDeps); err != nil {
		return err
	}

	compressionMw, err := cuhttp.Compression(&cfg.CompressionLevels)
	if err != nil {
		return err
	}

	return cuhttp.StartServer(ctx, &cfg, d.DB, compressionMw(chiRouter), "public PDP HTTPS server")
}

// StartAdmin serves the private operator GUI (webrpc + config).
func StartAdmin(ctx context.Context, d *Deps) (*http.Server, error) {
	if !d.Cfg.Subsystems.EnableWebGui {
		return nil, nil
	}

	srv, err := web.GetSrv(ctx, d.CurioDeps(), false)
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	log.Infof("Admin GUI: http://%s", d.Cfg.Subsystems.GuiAddress)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("admin GUI server failed: %s", err)
		}
	}()

	return srv, nil
}
