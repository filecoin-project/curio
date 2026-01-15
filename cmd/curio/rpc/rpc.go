// Package rpc provides all direct access to this node.
package rpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gbrlsnchs/jwt/v3"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/mitchellh/go-homedir"
	"github.com/multiformats/go-multihash"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/tag"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/api/client"
	ltypes "github.com/filecoin-project/curio/api/types"
	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/metrics"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/repo"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/web"

	lapi "github.com/filecoin-project/lotus/api"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/lib/rpcenc"
	lotusmetrics "github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/metrics/proxy"
	"github.com/filecoin-project/lotus/storage/pipeline/piece"
	"github.com/filecoin-project/lotus/storage/sealer/fsutil"
)

const metaFile = "sectorstore.json"

var log = logging.Logger("curio/rpc")
var permissioned = os.Getenv("LOTUS_DISABLE_AUTH_PERMISSIONED") != "1"

func CurioHandler(
	authv func(ctx context.Context, token string) ([]auth.Permission, error),
	remote http.HandlerFunc,
	a api.Curio,
	prometheusSD http.Handler,
	dependencies *deps.Deps,
	permissioned bool) http.Handler {
	mux := mux.NewRouter()
	readerHandler, readerServerOpt := rpcenc.ReaderParamDecoder()
	rpcServer := jsonrpc.NewServer(jsonrpc.WithServerErrors(lapi.RPCErrors), readerServerOpt)

	wapi := proxy.MetricedAPI[api.Curio, api.CurioStruct](a)
	if permissioned {
		wapi = lapi.PermissionedAPI[api.Curio, api.CurioStruct](wapi)
	}

	rpcServer.Register("Filecoin", wapi)

	//@magik6k should this be exposing build/curio.json instead?
	rpcServer.AliasMethod("rpc.discover", "Filecoin.Discover")

	mux.Handle("/rpc/v0", rpcServer)
	mux.Handle("/rpc/streams/v0/push/{uuid}", readerHandler)
	if dependencies.PeerRPC != nil {
		mux.Handle("/peer/v0", dependencies.PeerRPC) // Peer-to-peer WebSocket communication
	}
	mux.PathPrefix("/remote").HandlerFunc(remote)
	mux.Handle("/debug/metrics", metrics.Exporter())
	mux.Handle("/debug/service-discovery", prometheusSD)
	mux.PathPrefix("/").Handler(http.DefaultServeMux) // pprof

	if !permissioned {
		return mux
	}

	ah := &auth.Handler{
		Verify: authv,
		Next:   mux.ServeHTTP,
	}
	return ah
}

type CurioAPI struct {
	*deps.Deps
	paths.SectorIndex
	ShutdownChan chan struct{}
}

func (p *CurioAPI) Version(context.Context) ([]int, error) {
	return build.BuildVersionArray[:], nil
}

func (p *CurioAPI) StorageDetachLocal(ctx context.Context, path string) error {
	path, err := homedir.Expand(path)
	if err != nil {
		return xerrors.Errorf("expanding local path: %w", err)
	}

	// check that we have the path opened
	lps, err := p.LocalStore.Local(ctx)
	if err != nil {
		return xerrors.Errorf("getting local path list: %w", err)
	}

	var localPath *storiface.StoragePath
	for _, lp := range lps {
		if lp.LocalPath == path {
			lp := lp // copy to make the linter happy
			localPath = &lp
			break
		}
	}
	if localPath == nil {
		return xerrors.Errorf("no local paths match '%s'", path)
	}

	// drop from the persisted storage.json
	var found bool
	if err := p.LocalPaths.SetStorage(func(sc *storiface.StorageConfig) {
		out := make([]storiface.LocalPath, 0, len(sc.StoragePaths))
		for _, storagePath := range sc.StoragePaths {
			if storagePath.Path != path {
				out = append(out, storagePath)
				continue
			}
			found = true
		}
		sc.StoragePaths = out
	}); err != nil {
		return xerrors.Errorf("set storage config: %w", err)
	}
	if !found {
		// maybe this is fine?
		return xerrors.Errorf("path not found in storage.json")
	}

	// unregister locally, drop from sector index
	return p.LocalStore.ClosePath(ctx, localPath.ID)
}

func (p *CurioAPI) StorageLocal(ctx context.Context) (map[storiface.ID]string, error) {
	ps, err := p.LocalStore.Local(ctx)
	if err != nil {
		return nil, err
	}

	var out = make(map[storiface.ID]string)
	for _, path := range ps {
		out[path.ID] = path.LocalPath
	}

	return out, nil
}

func (p *CurioAPI) StorageStat(ctx context.Context, id storiface.ID) (fsutil.FsStat, error) {
	return p.Stor.FsStat(ctx, id)
}

// this method is currently unused, might be back when we get markets into curio
func (p *CurioAPI) AllocatePieceToSector(ctx context.Context, maddr address.Address, piece piece.PieceDealInfo, rawSize int64, source url.URL, header http.Header) (lapi.SectorOffset, error) {
	/*di, err := market.NewPieceIngester(ctx, p.Deps.DB, p.Deps.Full, maddr, true, time.Minute, false)
	if err != nil {
		return lapi.SectorOffset{}, xerrors.Errorf("failed to create a piece ingestor")
	}

	sector, err := di.AllocatePieceToSector(ctx, maddr, piece, rawSize, source, header)
	if err != nil {
		return lapi.SectorOffset{}, xerrors.Errorf("failed to add piece to a sector")
	}

	err = di.Seal()
	if err != nil {
		return lapi.SectorOffset{}, xerrors.Errorf("failed to start sealing the sector %d for actor %s", sector.Sector, maddr)
	}
	*/
	return lapi.SectorOffset{}, xerrors.Errorf("not implemented")
}

// Trigger shutdown
func (p *CurioAPI) Shutdown(context.Context) error {
	close(p.ShutdownChan)
	return nil
}

func (p *CurioAPI) StorageInit(ctx context.Context, path string, opts storiface.LocalStorageMeta) error {
	path, err := homedir.Expand(path)
	if err != nil {
		return xerrors.Errorf("expanding local path: %w", err)
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	_, err = os.Stat(filepath.Join(path, metaFile))
	if !os.IsNotExist(err) {
		if err == nil {
			return xerrors.Errorf("path is already initialized")
		}
		return err
	}
	if opts.ID == "" {
		opts.ID = storiface.ID(uuid.New().String())
	}
	if !opts.CanStore && !opts.CanSeal {
		return xerrors.Errorf("must specify at least one of --store or --seal")
	}
	b, err := json.MarshalIndent(opts, "", "  ")
	if err != nil {
		return xerrors.Errorf("marshaling storage config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(path, metaFile), b, 0644); err != nil {
		return xerrors.Errorf("persisting storage metadata (%s): %w", filepath.Join(path, metaFile), err)
	}
	return nil
}

func (p *CurioAPI) StorageAddLocal(ctx context.Context, path string) error {
	path, err := homedir.Expand(path)
	if err != nil {
		return xerrors.Errorf("expanding local path: %w", err)
	}

	if err := p.LocalStore.OpenPath(ctx, path); err != nil {
		return xerrors.Errorf("opening local path: %w", err)
	}

	if err := p.LocalPaths.SetStorage(func(sc *storiface.StorageConfig) {
		sc.StoragePaths = append(sc.StoragePaths, storiface.LocalPath{Path: path})
	}); err != nil {
		return xerrors.Errorf("get storage config: %w", err)
	}

	return nil
}

func (p *CurioAPI) StorageGenerateVanillaProof(ctx context.Context, maddr address.Address, sector abi.SectorNumber) ([]byte, error) {
	head, err := p.Chain.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting chain head: %w", err)
	}

	ts := head.Key()

	si, err := p.Chain.StateSectorGetInfo(ctx, maddr, sector, ts)
	if err != nil {
		return nil, xerrors.Errorf("getting sector info: %w", err)
	}

	di, err := p.Chain.StateSectorPartition(ctx, maddr, sector, ts)
	if err != nil {
		return nil, xerrors.Errorf("getting sector partition: %w", err)
	}

	nv, err := p.Chain.StateNetworkVersion(ctx, ts)
	if err != nil {
		return nil, xerrors.Errorf("getting network version: %w", err)
	}

	ppt, err := si.SealProof.RegisteredWindowPoStProofByNetworkVersion(nv)
	if err != nil {
		return nil, xerrors.Errorf("failed to get window post type: %w", err)
	}

	mi, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("failed to get miner id: %w", err)
	}

	minerID := abi.ActorID(mi)

	buf := new(bytes.Buffer)
	if err := maddr.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal address to cbor: %w", err)
	}

	rand, err := p.Chain.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_WindowedPoStChallengeSeed, head.Height(), buf.Bytes(), ts)
	if err != nil {
		return nil, xerrors.Errorf("failed to get chain randomness from beacon for window post (ts=%d; deadline=%d): %w", head.Height(), di, err)
	}

	rand[31] &= 0x3f

	postChallenges, err := ffi.GeneratePoStFallbackSectorChallenges(ppt, minerID, append(abi.PoStRandomness{}, rand...), []abi.SectorNumber{sector})
	if err != nil {
		return nil, xerrors.Errorf("generating fallback challenges: %v", err)
	}

	psc := storiface.PostSectorChallenge{
		SealProof:    si.SealProof,
		SectorNumber: sector,
		SealedCID:    si.SealedCID,
		Update:       si.SectorKeyCID != nil,
		Challenge:    postChallenges.Challenges[sector],
	}

	return p.Stor.GenerateSingleVanillaProof(ctx, minerID, psc, ppt)
}

func (p *CurioAPI) StorageRedeclare(ctx context.Context, filterId *storiface.ID, dropMissing bool) error {
	sl, err := p.LocalStore.Local(ctx)
	if err != nil {
		return xerrors.Errorf("getting local store: %w", err)
	}
	for _, id := range sl {
		if id.ID == *filterId {
			return p.LocalStore.Redeclare(ctx, filterId, dropMissing)
		}
	}
	return xerrors.Errorf("storage %s not found on the node", *filterId)
}

func (p *CurioAPI) LogList(ctx context.Context) ([]string, error) {
	return logging.GetSubsystems(), nil
}

func (p *CurioAPI) LogSetLevel(ctx context.Context, subsystem, level string) error {
	return logging.SetLogLevel(subsystem, level)
}

func (p *CurioAPI) Cordon(ctx context.Context) error {
	_, err := p.DB.Exec(ctx, `UPDATE harmony_machines SET unschedulable = $1 WHERE id = $2`, true, p.MachineID)
	if err != nil {
		return xerrors.Errorf("cordon failed: %w", err)
	}
	return nil
}

func (p *CurioAPI) Uncordon(ctx context.Context) error {
	_, err := p.DB.Exec(ctx, `UPDATE harmony_machines SET unschedulable = $1 WHERE id = $2`, false, p.MachineID)
	if err != nil {
		return xerrors.Errorf("uncordon failed: %w", err)
	}
	return nil
}

func (p *CurioAPI) Info(ctx context.Context) (*ltypes.NodeInfo, error) {
	var ni ltypes.NodeInfo
	err := p.DB.QueryRow(ctx, `
		SELECT
			hm.id,
			hm.cpu,
			hm.ram,
			hm.gpu,
			hm.host_and_port,
			hm.last_contact,
			hm.unschedulable,
			hmd.machine_name,
			hmd.startup_time,
			hmd.tasks,
			hmd.layers,
			hmd.miners
		FROM
			harmony_machines hm
		LEFT JOIN
			harmony_machine_details hmd ON hm.id = hmd.machine_id
		WHERE
		    hm.id=$1;
		`, p.MachineID).Scan(&ni.ID, &ni.CPU, &ni.RAM, &ni.GPU, &ni.HostPort, &ni.LastContact, &ni.Unschedulable, &ni.Name, &ni.StartupTime, &ni.Tasks, &ni.Layers, &ni.Miners)
	if err != nil {
		return nil, err
	}

	return &ni, nil
}

func (p *CurioAPI) IndexSamples(ctx context.Context, pcid cid.Cid) ([]multihash.Multihash, error) {
	var indexed bool
	err := p.DB.QueryRow(ctx, `SELECT indexed FROM market_piece_metadata WHERE piece_cid=$1`, pcid.String()).Scan(&indexed)
	if err != nil {
		return nil, xerrors.Errorf("failed to get piece metadata: %w", err)
	}
	if !indexed {
		return nil, xerrors.Errorf("piece %s is not indexed", pcid.String())
	}
	var chunks []struct {
		FirstCidStr    string `db:"first_cid"`
		NumberOfBlocks int64  `db:"num_blocks"`
	}

	err = p.DB.Select(ctx, &chunks, `SELECT 
    										first_cid, 
											num_blocks 
										FROM ipni_chunks 
										WHERE piece_cid = $1
										AND from_car= FALSE
										ORDER BY RANDOM()
										LIMIT 1`, pcid.String())

	if err != nil {
		return nil, xerrors.Errorf("failed to get piece chunks: %w", err)
	}

	if len(chunks) == 0 {
		return nil, xerrors.Errorf("no chunks found for piece %s", pcid.String())
	}

	chunk := chunks[0]

	cb, err := hex.DecodeString(chunk.FirstCidStr)
	if err != nil {
		return nil, xerrors.Errorf("decoding first CID: %w", err)
	}

	firstHash := multihash.Multihash(cb)

	return p.IndexStore.GetPieceHashRange(ctx, pcid, firstHash, chunk.NumberOfBlocks)
}

func ListenAndServe(ctx context.Context, dependencies *deps.Deps, shutdownChan chan struct{}) error {
	fh := &paths.FetchHandler{Local: dependencies.LocalStore, PfHandler: &paths.DefaultPartialFileHandler{}}
	remoteHandler := func(w http.ResponseWriter, r *http.Request) {
		if !auth.HasPerm(r.Context(), nil, lapi.PermAdmin) {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(struct{ Error string }{"unauthorized: missing admin permission"})
			return
		}

		fh.ServeHTTP(w, r)
	}

	var authVerify func(context.Context, string) ([]auth.Permission, error)
	{
		privateKey, err := base64.StdEncoding.DecodeString(dependencies.Cfg.Apis.StorageRPCSecret)
		if err != nil {
			return xerrors.Errorf("decoding storage rpc secret: %w", err)
		}
		authVerify = func(ctx context.Context, token string) ([]auth.Permission, error) {
			var payload deps.JwtPayload
			if _, err := jwt.Verify([]byte(token), jwt.NewHS256(privateKey), &payload); err != nil {
				return nil, xerrors.Errorf("JWT Verification failed: %w", err)
			}

			return payload.Allow, nil
		}
	}

	// Serve the RPC.
	srv := &http.Server{
		Handler: CurioHandler(
			authVerify,
			remoteHandler,
			&CurioAPI{dependencies, dependencies.Si, shutdownChan},
			prometheusServiceDiscovery(ctx, dependencies),
			dependencies,
			permissioned),
		ReadHeaderTimeout: time.Minute * 3,
		BaseContext: func(listener net.Listener) context.Context {
			ctx, _ := tag.New(context.Background(), tag.Upsert(lotusmetrics.APIInterface, "curio"))
			return ctx
		},
		Addr: dependencies.ListenAddr,
	}

	log.Infof("Setting up RPC server at %s", dependencies.ListenAddr)
	eg := errgroup.Group{}
	eg.Go(srv.ListenAndServe)

	if dependencies.Cfg.Subsystems.EnableWebGui {
		web, err := web.GetSrv(ctx, dependencies, false)
		if err != nil {
			return err
		}

		go func() {
			<-ctx.Done()
			log.Warn("Shutting down...")
			if err := srv.Shutdown(context.TODO()); err != nil {
				log.Errorf("shutting down RPC server failed: %s", err)
			}
			if err := web.Shutdown(context.Background()); err != nil {
				log.Errorf("shutting down web server failed: %s", err)
			}
			log.Warn("Graceful shutdown successful")
		}()

		uiAddress := dependencies.Cfg.Subsystems.GuiAddress
		if uiAddress == "" || uiAddress[0] == ':' || uiAddress == "0.0.0.0:4701" {
			split := strings.Split(uiAddress, ":")
			if len(split) == 2 {
				uiAddress = "localhost:" + split[1]
			}
		}

		log.Infof("GUI:  http://%s", uiAddress)
		eg.Go(web.ListenAndServe)
	}
	return eg.Wait()
}

func GetCurioAPI(ctx *cli.Context) (api.Curio, jsonrpc.ClientCloser, error) {
	addr, headers, err := cliutil.GetRawAPI(ctx, repo.Curio, "v0")
	if err != nil {
		return nil, nil, err
	}

	u, err := url.Parse(addr)
	if err != nil {
		return nil, nil, xerrors.Errorf("parsing miner api URL: %w", err)
	}

	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}

	addr = u.String()

	return client.NewCurioRpc(ctx.Context, addr, headers)
}

func prometheusServiceDiscovery(ctx context.Context, deps *deps.Deps) http.HandlerFunc {

	type host struct {
		Host   string `db:"host_and_port"`
		Layers string `db:"layers"`
	}

	type service struct {
		Targets []string          `json:"targets"`
		Labels  map[string]string `json:"labels"`
	}

	hnd := func(resp http.ResponseWriter, req *http.Request) {

		resp.Header().Set("Content-Type", "application/json")

		var hosts []host
		err := deps.DB.Select(ctx, &hosts, `
			SELECT
			    m.host_and_port,
			    md.layers
			FROM
			    harmony_machines m
			JOIN
			    harmony_machine_details md ON m.id = md.machine_id;`)
		if err != nil {
			log.Errorf("failed to fetch hosts: %s", err)
			_, err = resp.Write([]byte("[]"))
			if err != nil {
				log.Errorf("failed to write response: %s", err)
			}
		} else {
			var services []service
			for _, h := range hosts {
				services = append(services, service{
					Targets: []string{h.Host},
					Labels: map[string]string{
						"layers": h.Layers,
					},
				})
			}
			enc := json.NewEncoder(resp)
			if err := enc.Encode(services); err != nil {
				log.Errorf("failed to encode response: %s", err)
			}
		}
		resp.WriteHeader(http.StatusOK)
	}
	return hnd
}
