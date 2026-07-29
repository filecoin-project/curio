package pdpnode

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/filecoin-project/curio/cuhttp"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/web"
)

// StartPublic serves the customer-facing PDP API.
func StartPublic(ctx context.Context, d *Deps, sd *TaskResult) error {
	cfg := d.Cfg.HTTP
	if !cfg.Enable {
		return nil
	}
	if d.DB.ReadOnly() {
		log.Info("readonly database mode: skipping public PDP HTTP server")
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

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("admin GUI server failed: %s", err)
		}
	}()

	return srv, nil
}

func adminGUIURL(cfg *config.CurioConfig) string {
	if !cfg.Subsystems.EnableWebGui {
		return ""
	}
	addr := strings.TrimSpace(cfg.Subsystems.GuiAddress)
	if addr == "" {
		return ""
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}

// announceReady logs the operator-facing endpoints once startup is complete.
func announceReady(d *Deps) {
	if gui := adminGUIURL(d.Cfg); gui != "" {
		log.Infof("Curio ready — admin GUI: %s", gui)
		skiffDockerLog("ready — admin GUI: %s", gui)
		return
	}
	log.Info("Curio ready (admin GUI disabled)")
}
