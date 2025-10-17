// Package deps provides the dependencies for the curio node.
package deps

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gbrlsnchs/jwt/v3"
	logging "github.com/ipfs/go-log/v2"
	"github.com/kr/pretty"
	"github.com/samber/lo"
	"github.com/urfave/cli/v2"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/deps/stats"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/curiochain"
	"github.com/filecoin-project/curio/lib/multictladdr"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/repo"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/chunker"
	"github.com/filecoin-project/curio/tasks/message"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/lazy"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	lrepo "github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/lotus/storage/sealer"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
)

var log = logging.Logger("curio/deps")

func MakeDB(cctx *cli.Context) (*harmonydb.DB, error) {
	// #1 CLI opts
	fromCLI := func() (*harmonydb.DB, error) {
		dbConfig := harmonydb.Config{
			Username:    cctx.String("db-user"),
			Password:    cctx.String("db-password"),
			Hosts:       strings.Split(cctx.String("db-host"), ","),
			Database:    cctx.String("db-name"),
			Port:        cctx.String("db-port"),
			LoadBalance: cctx.Bool("db-load-balance"),
		}
		return harmonydb.NewFromConfig(dbConfig)
	}

	readToml := func(path string) (*harmonydb.DB, error) {
		cfg, err := config.FromFile(path)
		if err != nil {
			return nil, err
		}
		if c, ok := cfg.(*config.StorageMiner); ok {
			return harmonydb.NewFromConfig(c.HarmonyDB)
		}
		return nil, errors.New("not a miner config")
	}

	// #2 Try local miner config
	fromMinerEnv := func() (*harmonydb.DB, error) {
		v := os.Getenv("LOTUS_MINER_PATH")
		if v == "" {
			return nil, errors.New("no miner env")
		}
		return readToml(filepath.Join(v, "config.toml"))

	}

	fromMiner := func() (*harmonydb.DB, error) {
		u, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		return readToml(filepath.Join(u, ".lotusminer/config.toml"))
	}

	for _, f := range []func() (*harmonydb.DB, error){fromCLI, fromMinerEnv, fromMiner} {
		db, err := f()
		if err != nil {
			continue
		}
		return db, nil
	}
	log.Error("Could not connect to db. Please verify that your YugabyteDB is running and the env vars or CLI args are correct.")
	log.Error("If running as a service, please ensure that the service is running with the correct env vars in /etc/curio.env file.")
	log.Error("If running locally, please ensure that the env vars are set correctly in your shell. Run `curio --help` for more info.")
	return fromCLI() //in-case it's not about bad config.
}

type JwtPayload struct {
	Allow []auth.Permission
}

func StorageAuth(apiKey string) (sealer.StorageAuth, error) {
	if apiKey == "" {
		return nil, xerrors.Errorf("no api key provided")
	}

	rawKey, err := base64.StdEncoding.DecodeString(apiKey)
	if err != nil {
		return nil, xerrors.Errorf("decoding api key: %w", err)
	}

	key := jwt.NewHS256(rawKey)

	p := JwtPayload{
		Allow: []auth.Permission{"admin"},
	}

	token, err := jwt.Sign(&p, key)
	if err != nil {
		return nil, err
	}

	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+string(token))
	return sealer.StorageAuth(headers), nil
}

func GetDeps(ctx context.Context, cctx *cli.Context) (*Deps, error) {
	var deps Deps
	return &deps, deps.PopulateRemainingDeps(ctx, cctx, true)
}

type Deps struct {
	Layers            []string
	Cfg               *config.CurioConfig // values
	DB                *harmonydb.DB       // has itest capability
	Chain             api.Chain
	Bstore            curiochain.CurioBlockstore
	Verif             storiface.Verifier
	As                *multictladdr.MultiAddressSelector
	Maddrs            map[dtypes.MinerAddress]bool
	ProofTypes        map[abi.RegisteredSealProof]bool
	Stor              *paths.Remote
	Al                *curioalerting.AlertingSystem
	Si                paths.SectorIndex
	LocalStore        *paths.Local
	LocalPaths        *paths.BasicLocalStorage
	Prover            storiface.Prover
	ListenAddr        string
	Name              string
	MachineID         *int64
	Alert             *alertmanager.AlertNow
	IndexStore        *indexstore.IndexStore
	SectorReader      *pieceprovider.SectorReader
	CachedPieceReader *cachedreader.CachedPieceReader
	ServeChunker      *chunker.ServeChunker
	EthClient         *lazy.Lazy[*ethclient.Client]
	Sender            *message.Sender
}

const (
	FlagRepoPath = "repo-path"
)

func (deps *Deps) PopulateRemainingDeps(ctx context.Context, cctx *cli.Context, makeRepo bool) error {
	var err error
	if makeRepo {
		// Open repo
		repoPath := cctx.String(FlagRepoPath)
		fmt.Println("repopath", repoPath)
		r, err := lrepo.NewFS(repoPath)
		if err != nil {
			return err
		}

		ok, err := r.Exists()
		if err != nil {
			return err
		}
		if !ok {
			if err := r.Init(repo.Curio); err != nil {
				return err
			}
		}
	}

	if deps.DB == nil {
		deps.DB, err = MakeDB(cctx)
		if err != nil {
			return err
		}
	}
	if deps.Layers == nil {
		deps.Layers = append([]string{"base"}, cctx.StringSlice("layers")...) // Always stack on top of "base" layer
	}

	if deps.Cfg == nil {
		// The config feeds into task runners & their helpers
		deps.Cfg, err = GetConfig(cctx.Context, cctx.StringSlice("layers"), deps.DB)
		if err != nil {
			return xerrors.Errorf("populate config: %w", err)
		}
	}

	log.Debugw("config", "config", deps.Cfg)

	if deps.Verif == nil {
		deps.Verif = ffiwrapper.ProofVerifier
	}

	if deps.As == nil {
		deps.As, err = multictladdr.AddressSelector(deps.Cfg.Addresses)()
		if err != nil {
			return err
		}
	}

	if deps.Al == nil {
		deps.Al = curioalerting.NewAlertingSystem()
	}

	if deps.Si == nil {
		deps.Si = paths.NewDBIndex(deps.Al, deps.DB)
	}

	if deps.Chain == nil {
		var fullCloser func()
		cfgApiInfo := deps.Cfg.Apis.ChainApiInfo
		if v := os.Getenv("FULLNODE_API_INFO"); v != "" {
			cfgApiInfo = []string{v}
		}
		deps.Chain, fullCloser, err = GetFullNodeAPIV1Curio(cctx, cfgApiInfo)
		if err != nil {
			return err
		}

		go func() {
			<-ctx.Done()
			fullCloser()
		}()
	}

	if deps.EthClient == nil {
		deps.EthClient = lazy.MakeLazy(func() (*ethclient.Client, error) {
			cfgApiInfo := deps.Cfg.Apis.ChainApiInfo
			if v := os.Getenv("FULLNODE_API_INFO"); v != "" {
				cfgApiInfo = []string{v}
			}
			return GetEthClient(cctx, cfgApiInfo)
		})
	}

	if deps.Bstore == nil {
		deps.Bstore = curiochain.NewChainBlockstore(deps.Chain)
	}

	deps.LocalPaths = &paths.BasicLocalStorage{
		PathToJSON: path.Join(cctx.String(FlagRepoPath), "storage.json"),
	}

	if deps.ListenAddr == "" {
		listenAddr := cctx.String("listen")
		const unspecifiedAddress = "0.0.0.0"
		addressSlice := strings.Split(listenAddr, ":")
		if ip := net.ParseIP(addressSlice[0]); ip != nil {
			if ip.String() == unspecifiedAddress {
				rip, err := deps.DB.GetRoutableIP()
				if err != nil {
					return err
				}
				deps.ListenAddr = rip + ":" + addressSlice[1]
			} else {
				deps.ListenAddr = ip.String() + ":" + addressSlice[1]
			}
		}
	}

	if deps.Alert == nil {
		deps.Alert = alertmanager.NewAlertNow(deps.DB, deps.ListenAddr)
	}

	if cctx.IsSet("gui-listen") {
		deps.Cfg.Subsystems.GuiAddress = cctx.String("gui-listen")
	}
	if deps.LocalStore == nil {
		deps.LocalStore, err = paths.NewLocal(ctx, deps.LocalPaths, deps.Si, "http://"+deps.ListenAddr+"/remote")
		if err != nil {
			return err
		}
	}

	sa, err := StorageAuth(deps.Cfg.Apis.StorageRPCSecret)
	if err != nil {
		log.Errorf("error creating storage auth: %s, %v", err, pretty.Sprint(deps.Cfg))
		return xerrors.Errorf(`'%w' while parsing the config toml's 
	[Apis]
	StorageRPCSecret=%v
Get it with: jq .PrivateKey ~/.lotus-miner/keystore/MF2XI2BNNJ3XILLQOJUXMYLUMU`, err, deps.Cfg.Apis.StorageRPCSecret)
	}
	if deps.Stor == nil {
		deps.Stor = paths.NewRemote(deps.LocalStore, deps.Si, http.Header(sa), 1000, &paths.DefaultPartialFileHandler{})
	}

	if deps.Maddrs == nil {
		deps.Maddrs = map[dtypes.MinerAddress]bool{}
	}
	if len(deps.Maddrs) == 0 {
		for _, s := range deps.Cfg.Addresses {
			for _, s := range s.MinerAddresses {
				addr, err := address.NewFromString(s)
				if err != nil {
					return err
				}
				deps.Maddrs[dtypes.MinerAddress(addr)] = true
			}
		}
	}

	if deps.ProofTypes == nil {
		deps.ProofTypes = map[abi.RegisteredSealProof]bool{}
	}
	if len(deps.ProofTypes) == 0 {
		for maddr := range deps.Maddrs {
			spt, err := sealProofType(maddr, deps.Chain)
			if err != nil {
				return err
			}
			deps.ProofTypes[spt] = true
		}

	}
	if deps.Cfg.Subsystems.EnableProofShare {
		deps.ProofTypes[abi.RegisteredSealProof_StackedDrg32GiBV1_1] = true
		// deps.ProofTypes[abi.RegisteredSealProof_StackedDrg64GiBV1_1] = true TODO REVIEW UNCOMMENT
	}

	if deps.Cfg.Subsystems.EnableWalletExporter {
		spIDs := []address.Address{}
		for maddr := range deps.Maddrs {
			spIDs = append(spIDs, address.Address(maddr))
		}

		stats.StartWalletExporter(ctx, deps.DB, deps.Chain, spIDs)
	}

	if deps.Name == "" {
		deps.Name = cctx.String("name")
	}

	if deps.MachineID == nil {
		deps.MachineID = new(int64)
		*deps.MachineID = -1
	}

	if deps.IndexStore == nil {
		dbHost := cctx.String("db-host-cql")
		if dbHost == "" {
			dbHost = cctx.String("db-host")
		}

		deps.IndexStore = indexstore.NewIndexStore(strings.Split(dbHost, ","), cctx.Int("db-cassandra-port"), deps.Cfg)
		err = deps.IndexStore.Start(cctx.Context, false)
		if err != nil {
			return xerrors.Errorf("failed to start index store: %w", err)
		}
	}

	if deps.SectorReader == nil {
		deps.SectorReader = pieceprovider.NewSectorReader(deps.Stor, deps.Si)
	}

	if deps.CachedPieceReader == nil {
		ppr := pieceprovider.NewPieceParkReader(deps.Stor, deps.Si)
		deps.CachedPieceReader = cachedreader.NewCachedPieceReader(deps.DB, deps.SectorReader, ppr, deps.IndexStore)
	}

	if deps.ServeChunker == nil {
		deps.ServeChunker = chunker.NewServeChunker(deps.DB, deps.SectorReader, deps.IndexStore, deps.CachedPieceReader)
	}

	if deps.Prover == nil {
		deps.Prover = ffiwrapper.ProofProver
	}

	return nil
}

func sealProofType(maddr dtypes.MinerAddress, fnapi api.Chain) (abi.RegisteredSealProof, error) {
	mi, err := fnapi.StateMinerInfo(context.TODO(), address.Address(maddr), types.EmptyTSK)
	if err != nil {
		return 0, err
	}
	networkVersion, err := fnapi.StateNetworkVersion(context.TODO(), types.EmptyTSK)
	if err != nil {
		return 0, err
	}

	// node seal proof type does not decide whether or not we use synthetic porep
	return miner.PreferredSealProofTypeFromWindowPoStType(networkVersion, mi.WindowPoStProofType, false)
}

func LoadConfigWithUpgrades(text string, curioConfigWithDefaults *config.CurioConfig) (toml.MetaData, error) {
	return config.LoadConfigWithUpgrades(text, curioConfigWithDefaults)
}

func GetConfig(ctx context.Context, layers []string, db *harmonydb.DB) (*config.CurioConfig, error) {
	err := updateBaseLayer(ctx, db)
	if err != nil {
		return nil, err
	}

	curioConfig := config.DefaultCurioConfig()
	err = ApplyLayers(ctx, db, curioConfig, layers)
	if err != nil {
		return nil, err
	}
	err = config.EnableChangeDetection(db, curioConfig, layers, config.FixTOML)
	if err != nil {
		return nil, err
	}
	return curioConfig, nil
}

func ApplyLayers(ctx context.Context, db *harmonydb.DB, curioConfig *config.CurioConfig, layers []string) error {
	configs, err := config.GetConfigs(ctx, db, layers)
	if err != nil {
		return err
	}
	return config.ApplyLayers(ctx, curioConfig, configs, config.FixTOML)
}

func updateBaseLayer(ctx context.Context, db *harmonydb.DB) error {
	_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Get existing base from DB
		text := ""
		err = tx.QueryRow(`SELECT config FROM harmony_config WHERE title=$1`, "base").Scan(&text)
		if err != nil {
			if strings.Contains(err.Error(), pgx.ErrNoRows.Error()) {
				return false, fmt.Errorf("missing layer 'base' ")
			}
			return false, fmt.Errorf("could not read layer 'base': %w", err)
		}

		// Load the existing configuration
		cfg := config.DefaultCurioConfig()
		metadata, err := LoadConfigWithUpgrades(text, cfg)
		if err != nil {
			return false, fmt.Errorf("could not read base layer, bad toml %s: %w", text, err)
		}

		// Capture unknown fields
		keys := removeUnknownEntries(metadata.Keys(), metadata.Undecoded())
		unrecognizedFields := extractUnknownFields(keys, text)

		// Convert the updated config back to TOML string
		cb, err := config.ConfigUpdate(cfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
		if err != nil {
			return false, xerrors.Errorf("cannot update base config: %w", err)
		}

		// Merge unknown fields back into the updated config
		finalConfig, err := mergeUnknownFields(string(cb), unrecognizedFields)
		if err != nil {
			return false, xerrors.Errorf("cannot merge unknown fields: %w", err)
		}

		// Check if we need to update the DB
		if text == finalConfig {
			return false, nil
		}

		// Save the updated base with merged comments
		_, err = tx.Exec("UPDATE harmony_config SET config=$1 WHERE title='base'", finalConfig)
		if err != nil {
			return false, xerrors.Errorf("cannot update base config: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return err
	}

	return nil
}

func extractUnknownFields(knownKeys []toml.Key, originalConfig string) map[string]interface{} {
	// Parse the original config into a raw map
	var rawConfig map[string]interface{}
	err := toml.Unmarshal([]byte(originalConfig), &rawConfig)
	if err != nil {
		log.Warnw("Failed to parse original config for unknown fields", "error", err)
		return nil
	}

	// Collect all recognized keys
	recognizedKeys := map[string]struct{}{}
	for _, key := range knownKeys {
		recognizedKeys[strings.Join(key, ".")] = struct{}{}
	}

	// Identify unrecognized fields
	unrecognizedFields := map[string]interface{}{}
	for key, value := range rawConfig {
		if _, recognized := recognizedKeys[key]; !recognized {
			unrecognizedFields[key] = value
		}
	}
	return unrecognizedFields
}

func removeUnknownEntries(array1, array2 []toml.Key) []toml.Key {
	// Create a set from array2 for fast lookup
	toRemove := make(map[string]struct{}, len(array2))
	for _, key := range array2 {
		toRemove[key.String()] = struct{}{}
	}

	// Filter array1, keeping only elements not in toRemove
	var result []toml.Key
	for _, key := range array1 {
		if _, exists := toRemove[key.String()]; !exists {
			result = append(result, key)
		}
	}

	return result
}

func mergeUnknownFields(updatedConfig string, unrecognizedFields map[string]interface{}) (string, error) {
	// Parse the updated config into a raw map
	var updatedConfigMap map[string]interface{}
	err := toml.Unmarshal([]byte(updatedConfig), &updatedConfigMap)
	if err != nil {
		return "", fmt.Errorf("failed to parse updated config: %w", err)
	}

	// Merge unrecognized fields
	for key, value := range unrecognizedFields {
		if _, exists := updatedConfigMap[key]; !exists {
			updatedConfigMap[key] = value
		}
	}

	// Convert back into TOML
	b := new(bytes.Buffer)
	encoder := toml.NewEncoder(b)
	err = encoder.Encode(updatedConfigMap)
	if err != nil {
		return "", fmt.Errorf("failed to marshal final config: %w", err)
	}

	return b.String(), nil
}

func GetDefaultConfig(comment bool) (string, error) {
	c := config.DefaultCurioConfig()
	cb, err := config.ConfigUpdate(c, nil, config.Commented(comment), config.DefaultKeepUncommented(), config.NoEnv())
	if err != nil {
		return "", err
	}
	return string(cb), nil
}

func GetAPI(ctx context.Context, cctx *cli.Context) (*harmonydb.DB, *config.CurioConfig, api.Chain, jsonrpc.ClientCloser, *lazy.Lazy[*ethclient.Client], error) {
	db, err := MakeDB(cctx)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	layers := cctx.StringSlice("layers")

	cfg, err := GetConfig(cctx.Context, layers, db)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	cfgApiInfo := cfg.Apis.ChainApiInfo
	if v := os.Getenv("FULLNODE_API_INFO"); v != "" {
		cfgApiInfo = []string{v}
	}

	full, fullCloser, err := GetFullNodeAPIV1Curio(cctx, cfgApiInfo)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	ethClient := lazy.MakeLazy(func() (*ethclient.Client, error) {
		return GetEthClient(cctx, cfgApiInfo)
	})

	return db, cfg, full, fullCloser, ethClient, nil
}
func GetDepsCLI(ctx context.Context, cctx *cli.Context) (*Deps, error) {
	db, cfg, full, fullCloser, ethClient, err := GetAPI(ctx, cctx)
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		fullCloser()
	}()

	maddrs := map[dtypes.MinerAddress]bool{}
	if len(maddrs) == 0 {
		for _, s := range cfg.Addresses {
			for _, s := range s.MinerAddresses {
				addr, err := address.NewFromString(s)
				if err != nil {
					return nil, err
				}
				maddrs[dtypes.MinerAddress(addr)] = true
			}
		}
	}

	return &Deps{
		Cfg:       cfg,
		DB:        db,
		Chain:     full,
		Maddrs:    maddrs,
		EthClient: ethClient,
	}, nil
}

type CreateMinerConfigChainAPI interface {
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (lapi.MinerInfo, error)
}

func CreateMinerConfig(ctx context.Context, full CreateMinerConfigChainAPI, db *harmonydb.DB, miners []string, info string) error {
	var titles []string
	err := db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
	if err != nil {
		return fmt.Errorf("cannot reach the db. Ensure that Yugabyte flags are set correctly to"+
			" reach Yugabyte: %s", err.Error())
	}

	// setup config
	curioConfig := config.DefaultCurioConfig()

	for _, addr := range miners {
		maddr, err := address.NewFromString(addr)
		if err != nil {
			return xerrors.Errorf("Invalid address: %s", addr)
		}

		_, err = full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("Failed to get miner info: %w", err)
		}

		curioConfig.Addresses = append(curioConfig.Addresses, config.CurioAddresses{
			PreCommitControl:      []string{},
			CommitControl:         []string{},
			DealPublishControl:    []string{},
			TerminateControl:      []string{},
			DisableOwnerFallback:  false,
			DisableWorkerFallback: false,
			MinerAddresses:        []string{addr},
			BalanceManager:        config.DefaultBalanceManager(),
		})
	}

	{
		sk, err := io.ReadAll(io.LimitReader(rand.Reader, 32))
		if err != nil {
			return err
		}

		curioConfig.Apis.StorageRPCSecret = base64.StdEncoding.EncodeToString(sk)
	}

	{
		curioConfig.Apis.ChainApiInfo = append(curioConfig.Apis.ChainApiInfo, info)
	}

	curioConfig.Addresses = lo.Filter(curioConfig.Addresses, func(a config.CurioAddresses, _ int) bool {
		return len(a.MinerAddresses) > 0
	})

	// If no base layer is present
	if !lo.Contains(titles, "base") {
		cb, err := config.ConfigUpdate(curioConfig, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
		if err != nil {
			return xerrors.Errorf("Failed to generate default config: %w", err)
		}
		cfg := string(cb)
		_, err = db.Exec(ctx, "INSERT INTO harmony_config (title, config) VALUES ('base', $1)", cfg)
		if err != nil {
			return xerrors.Errorf("failed to insert the 'base' into the database: %w", err)
		}
		fmt.Printf("The base layer has been updated with miner[s] %s\n", miners)
		return nil
	}

	// if base layer is present
	baseCfg := config.DefaultCurioConfig()
	var baseText string
	err = db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
	if err != nil {
		return xerrors.Errorf("Cannot load base config from database: %w", err)
	}
	_, err = LoadConfigWithUpgrades(baseText, baseCfg)
	if err != nil {
		return xerrors.Errorf("Cannot parse base config: %w", err)
	}

	baseCfg.Addresses = append(baseCfg.Addresses, curioConfig.Addresses...)
	baseCfg.Addresses = lo.Filter(baseCfg.Addresses, func(a config.CurioAddresses, _ int) bool {
		return len(a.MinerAddresses) > 0
	})

	cb, err := config.ConfigUpdate(baseCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	if err != nil {
		return xerrors.Errorf("cannot interpret config: %w", err)
	}
	_, err = db.Exec(ctx, "UPDATE harmony_config SET config=$1 WHERE title='base'", string(cb))
	if err != nil {
		return xerrors.Errorf("cannot update base config: %w", err)
	}
	fmt.Printf("The base layer has been updated with miner[s] %s\n", miners)
	return nil
}
