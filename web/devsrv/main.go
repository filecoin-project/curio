package main

import (
	"context"
	"fmt"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/web"
)

func main() {
	srv, err := web.GetSrv(context.Background(),
		&deps.Deps{
			Cfg: &config.CurioConfig{
				Subsystems: config.CurioSubsystemsConfig{GuiAddress: ":4702"},
			}},
		true)

	if err != nil {
		panic(err)
	}
	fmt.Println("Running on: ", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		panic(err)
	}
}
