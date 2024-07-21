package hapi

import (
	"context"
	"embed"
	"text/template"

	"github.com/gorilla/mux"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/curiochain"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
)

//go:embed web/*
var templateFS embed.FS

func Routes(r *mux.Router, deps *deps.Deps) error {
	t, err := makeTemplate().ParseFS(templateFS, "web/*")
	if err != nil {
		return xerrors.Errorf("parse templates: %w", err)
	}

	a := &app{
		db:   deps.DB,
		t:    t,
		deps: deps,
		stor: store.ActorStore(context.Background(), blockstore.NewReadCachedBlockstore(blockstore.NewAPIBlockstore(deps.Chain), curiochain.ChainBlockCache)),
	}

	go a.watchActor()

	// index page (simple info)
	r.HandleFunc("/simpleinfo/actorsummary", a.actorSummary) // #3 + Watch

	// sector info page
	r.HandleFunc("/sector/{sp}/{id}/resume", a.sectorResume)
	r.HandleFunc("/sector/{sp}/{id}/remove", a.sectorRemove)
	return nil
}

func makeTemplate() *template.Template {
	return template.New("").Funcs(template.FuncMap{
		"toHumanBytes": func(b int64) string {
			return types.SizeStr(types.NewInt(uint64(b)))
		},
	})
}

var log = logging.Logger("curio/web")
