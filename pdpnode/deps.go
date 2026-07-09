// Package pdpnode runs PDP workflows without the curio sealing/PoRep dependency graph.
package pdpnode

import (
	"context"
	"net/http"
	"strings"

	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/api"
	curiodeps "github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/curiochain"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/lazy"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/piecestore"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/chunker"
	pdpwallet "github.com/filecoin-project/curio/pdp/wallet"
	"github.com/filecoin-project/curio/tasks/message"
)

var log = logging.Logger("pdpnode")

const defaultMachineHost = "127.0.0.1:skiff"

// Skiff does not expose the curio /remote storage handler; paths.NewLocal still
// needs a syntactically valid URL for the sector index. Machine-host is an
// opaque harmony identity and is not used here.
const skiffLocalStorageURL = "http://127.0.0.1:0/remote"

// Deps holds PDP-node runtime dependencies.
type Deps struct {
	Cfg               *config.CurioConfig
	DB                *harmonydb.DB
	Chain             api.Chain
	chainCloser       func()
	Bstore            curiochain.CurioBlockstore
	Stor              *paths.Remote
	LocalStore        *paths.Local
	LocalPaths        paths.LocalStorage
	Si                paths.SectorIndex
	PieceIO           piecestore.PieceIO
	IndexStore        *indexstore.IndexStore
	SectorReader      *pieceprovider.SectorReader
	CachedPieceReader *cachedreader.CachedPieceReader
	ServeChunker      *chunker.ServeChunker
	EthClient         *lazy.Lazy[ethchain.EthClient]
	Sender            *message.Sender
	Al                *curioalerting.AlertingSystem
	Alert             *alertmanager.AlertNow
	MachineHost       string
	Name              string
	MachineID         int64
}

// Open initializes PDP-node dependencies from CLI flags and a single base config layer.
func Open(ctx context.Context, cctx *cli.Context) (*Deps, error) {
	skiffDockerLog("opening database connection")
	repoPath := cctx.String(curiodeps.FlagRepoPath)
	if err := ensureRepo(repoPath); err != nil {
		return nil, err
	}

	db, err := curiodeps.MakeDB(cctx)
	if err != nil {
		return nil, err
	}

	if err := ensureSkiffBaseLayer(ctx, db); err != nil {
		return nil, xerrors.Errorf("ensure skiff base config: %w", err)
	}

	cfg, err := curiodeps.GetConfig(ctx, nil, db)
	if err != nil {
		return nil, xerrors.Errorf("load config: %w", err)
	}

	applySkiffDefaults(cfg)
	if !cfg.Subsystems.EnablePDP {
		cfg.Subsystems.EnablePDP = true
	}
	applySkiffDockerListen(cfg)
	if !skiffDockerMode() && (cfg.Subsystems.GuiAddress == "" || strings.HasPrefix(cfg.Subsystems.GuiAddress, "0.0.0.0")) {
		port := "4701"
		if parts := strings.Split(cfg.Subsystems.GuiAddress, ":"); len(parts) == 2 && parts[1] != "" {
			port = parts[1]
		}
		cfg.Subsystems.GuiAddress = "127.0.0.1:" + port
	}

	machineHost := cctx.String("machine-host")
	if machineHost == "" {
		machineHost = defaultMachineHost
	}

	skiffDockerLog("initializing storage")

	al := curioalerting.NewAlertingSystem()
	si := paths.NewDBIndex(al, db)

	localPaths, err := newLocalStorage(skiffStorageRoot(cctx, cfg, repoPath), db.ReadOnly())
	if err != nil {
		return nil, err
	}

	sa, err := curiodeps.StorageAuth(cfg.Apis.StorageRPCSecret)
	if err != nil {
		return nil, xerrors.Errorf("storage auth: %w", err)
	}

	localStore, err := paths.NewLocal(ctx, localPaths, si, skiffLocalStorageURL)
	if err != nil {
		return nil, err
	}

	stor, err := paths.NewRemote(localStore, si, http.Header(sa), 1000, &paths.DefaultPartialFileHandler{})
	if err != nil {
		return nil, xerrors.Errorf("remote store: %w", err)
	}

	skiffDockerLog("connecting to chain API")
	chain, chainCloser, err := curiodeps.GetFullNodeAPIV1Curio(cctx, cfg.Apis)
	if err != nil {
		return nil, err
	}

	var indexStore *indexstore.IndexStore
	if db.ReadOnly() {
		indexStore = indexstore.NewReadonlyIndexStore(cfg)
		skiffDockerLog("readonly database mode: skipping cassandra index store")
	} else {
		dbHost := cctx.String("db-host-cql")
		if dbHost == "" {
			dbHost = cctx.String("db-host")
		}
		skiffDockerLog("starting index store on %s:%d", dbHost, cctx.Int("db-cassandra-port"))
		indexStore, err = indexstore.NewIndexStore(strings.Split(dbHost, ","), cctx.Int("db-cassandra-port"), cfg)
		if err != nil {
			return nil, xerrors.Errorf("index store: %w", err)
		}
	}
	if err := indexStore.Start(ctx, false); err != nil {
		return nil, xerrors.Errorf("start index store: %w", err)
	}

	sectorReader := pieceprovider.NewSectorReader(stor, si)
	ppr := pieceprovider.NewPieceParkReader(stor, si)
	cpr := cachedreader.NewCachedPieceReader(db, sectorReader, ppr, indexStore)
	serveChunker := chunker.NewServeChunker(db, sectorReader, indexStore, cpr)

	ethLazy := lazy.MakeLazy(func() (ethchain.EthClient, error) {
		return curiodeps.GetEthClient(cctx, cfg.Apis)
	})

	name := cctx.String("name")

	d := &Deps{
		Cfg:               cfg,
		DB:                db,
		Chain:             chain,
		chainCloser:       chainCloser,
		Bstore:            curiochain.NewChainBlockstore(chain),
		Stor:              stor,
		LocalStore:        localStore,
		LocalPaths:        localPaths,
		Si:                si,
		PieceIO:           piecestore.New(stor, localStore, si),
		IndexStore:        indexStore,
		SectorReader:      sectorReader,
		CachedPieceReader: cpr,
		ServeChunker:      serveChunker,
		EthClient:         ethLazy,
		Al:                al,
		Alert:             alertmanager.NewAlertNow(db, machineHost),
		MachineHost:       machineHost,
		Name:              name,
		MachineID:         -1,
	}

	hasKey, err := pdpwallet.HasPDPKey(ctx, db)
	if err != nil {
		log.Warnf("checking PDP wallet: %s", err)
	} else if !hasKey {
		log.Warn("PDP signing key not configured")
		d.Alert.AddAlert("PDP wallet not configured. Create or assign a key on the PDP page.")
	}

	sender, _ := message.NewSender(chain, chain, db, cfg.Fees.MaximizeFeeCap)
	d.Sender = sender

	skiffDockerLog("startup dependencies ready")
	return d, nil
}

func (d *Deps) Close() {
	if d.chainCloser != nil {
		d.chainCloser()
	}
}
