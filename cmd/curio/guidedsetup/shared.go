package guidedsetup

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/namespace"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-statestore"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/types/sector"

	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/repo"
)

//go:generate go run github.com/filecoin-project/curio/scripts/cbongen-guidedsetup

const (
	FlagMinerRepo = "miner-repo"
)

const FlagMinerRepoDeprecation = "storagerepo"
const jWTSecretName = "auth-jwt-private"

var StorageMiner storageMiner

type storageMiner struct{}

func (storageMiner) SupportsStagingDeals() {}

func (storageMiner) Type() string {
	return "StorageMiner"
}

func (storageMiner) Config() interface{} {
	return config.DefaultStorageMiner()
}

func (storageMiner) APIFlags() []string {
	return []string{"miner-api-url"}
}

func (storageMiner) RepoFlags() []string {
	return []string{"miner-repo"}
}

func (storageMiner) APIInfoEnvVars() (primary string, fallbacks []string, deprecated []string) {
	return "MINER_API_INFO", nil, []string{"STORAGE_API_INFO"}
}

func SaveConfigToLayerMigrateSectors(db *harmonydb.DB, minerRepoPath, chainApiInfo string, unmigSectorShouldFail func() bool) (minerAddress address.Address, err error) {
	_, say := translations.SetupLanguage()
	ctx := context.Background()

	r, err := repo.NewFS(minerRepoPath)
	if err != nil {
		return minerAddress, err
	}

	ok, err := r.Exists()
	if err != nil {
		return minerAddress, err
	}

	if !ok {
		return minerAddress, fmt.Errorf("repo not initialized at: %s", minerRepoPath)
	}

	lr, err := r.LockRO(StorageMiner)
	if err != nil {
		return minerAddress, fmt.Errorf("locking repo: %w", err)
	}
	defer func() {
		ferr := lr.Close()
		if ferr != nil {
			fmt.Println("error closing repo: ", ferr)
		}
	}()

	cfgNode, err := lr.Config()
	if err != nil {
		return minerAddress, fmt.Errorf("getting node config: %w", err)
	}
	smCfg := cfgNode.(*config.StorageMiner)

	var titles []string
	err = db.Select(ctx, &titles, `SELECT title FROM harmony_config WHERE LENGTH(config) > 0`)
	if err != nil {
		return minerAddress, fmt.Errorf("miner cannot reach the db. Ensure the config toml's HarmonyDB entry"+
			" is setup to reach Yugabyte correctly: %s", err.Error())
	}

	// Copy over identical settings:

	buf, err := os.ReadFile(path.Join(lr.Path(), "config.toml"))
	if err != nil {
		return minerAddress, fmt.Errorf("could not read config.toml: %w", err)
	}
	curioCfg := config.DefaultCurioConfig()

	ensureEmptyArrays(curioCfg)
	_, err = deps.LoadConfigWithUpgrades(string(buf), curioCfg)

	if err != nil {
		return minerAddress, fmt.Errorf("could not decode toml: %w", err)
	}

	// Populate Miner Address
	mmeta, err := lr.Datastore(ctx, "/metadata")
	if err != nil {
		return minerAddress, xerrors.Errorf("opening miner metadata datastore: %w", err)
	}

	maddrBytes, err := mmeta.Get(ctx, datastore.NewKey("miner-address"))
	if err != nil {
		return minerAddress, xerrors.Errorf("getting miner address datastore entry: %w", err)
	}

	addr, err := address.NewFromBytes(maddrBytes)
	if err != nil {
		return minerAddress, xerrors.Errorf("parsing miner actor address: %w", err)
	}

	if err := MigrateSectors(ctx, addr, mmeta, db, func(nSectors int) {
		say(plain, "Migrating metadata for %d sectors.", nSectors)
	}, unmigSectorShouldFail); err != nil {
		return address.Address{}, xerrors.Errorf("migrating sectors: %w", err)
	}

	minerAddress = addr

	curioCfg.Addresses.Set([]config.CurioAddresses{{
		MinerAddresses:        []string{addr.String()},
		PreCommitControl:      smCfg.Addresses.PreCommitControl,
		CommitControl:         smCfg.Addresses.CommitControl,
		DealPublishControl:    smCfg.Addresses.DealPublishControl,
		TerminateControl:      smCfg.Addresses.TerminateControl,
		DisableOwnerFallback:  smCfg.Addresses.DisableOwnerFallback,
		DisableWorkerFallback: smCfg.Addresses.DisableWorkerFallback,
		BalanceManager:        config.DefaultBalanceManager(),
	}})

	ks, err := lr.KeyStore()
	if err != nil {
		return minerAddress, xerrors.Errorf("keystore err: %w", err)
	}
	js, err := ks.Get(jWTSecretName)
	if err != nil {
		return minerAddress, xerrors.Errorf("error getting JWTSecretName: %w", err)
	}

	curioCfg.Apis.StorageRPCSecret = base64.StdEncoding.EncodeToString(js.PrivateKey)

	curioCfg.Apis.ChainApiInfo = append(curioCfg.Apis.ChainApiInfo, chainApiInfo)
	// Express as configTOML
	configTOMLBytes, err := config.TransparentMarshal(curioCfg)
	if err != nil {
		return minerAddress, err
	}
	configTOML := bytes.NewBuffer(configTOMLBytes)

	if lo.Contains(titles, "base") {
		// append addresses
		var baseCfg = config.DefaultCurioConfig()
		var baseText string
		err = db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
		if err != nil {
			return minerAddress, xerrors.Errorf("Cannot load base config: %w", err)
		}
		ensureEmptyArrays(baseCfg)
		_, err := deps.LoadConfigWithUpgrades(baseText, baseCfg)
		if err != nil {
			return minerAddress, xerrors.Errorf("Cannot load base config: %w", err)
		}
		addrs := baseCfg.Addresses.Get()
		for _, addr := range addrs {
			ma := addr.MinerAddresses
			if lo.Contains(ma, addrs[0].MinerAddresses[0]) {
				goto skipWritingToBase
			}
		}
		// write to base
		{
			addrs := baseCfg.Addresses.Get()
			addrs = append(addrs, addrs[0])
			baseCfg.Addresses.Set(lo.Filter(addrs, func(a config.CurioAddresses, _ int) bool {
				return len(a.MinerAddresses) > 0
			}))
			if baseCfg.Apis.ChainApiInfo == nil {
				baseCfg.Apis.ChainApiInfo = append(baseCfg.Apis.ChainApiInfo, chainApiInfo)
			}
			if baseCfg.Apis.StorageRPCSecret == "" {
				baseCfg.Apis.StorageRPCSecret = curioCfg.Apis.StorageRPCSecret
			} else {
				// Drop the storage secret if base layer already has one
				curioCfg.Apis.StorageRPCSecret = ""
			}

			cb, err := config.ConfigUpdate(baseCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
			if err != nil {
				return minerAddress, xerrors.Errorf("cannot interpret config: %w", err)
			}
			_, err = db.Exec(ctx, "UPDATE harmony_config SET config=$1 WHERE title='base'", string(cb))
			if err != nil {
				return minerAddress, xerrors.Errorf("cannot update base config: %w", err)
			}
			say(plain, "Configuration 'base' was updated to include this miner's address (%s) and its wallet setup.", minerAddress)
		}
		say(plain, "Compare the configurations %s to %s. Changes between the miner IDs other than wallet addreses should be a new, minimal layer for runners that need it.", "base", "mig-"+curioCfg.Addresses.Get()[0].MinerAddresses[0])
	skipWritingToBase:
	} else {
		_, err = db.Exec(ctx, `INSERT INTO harmony_config (title, config) VALUES ('base', $1)
		 ON CONFLICT(title) DO UPDATE SET config=EXCLUDED.config`, configTOML)

		if err != nil {
			return minerAddress, xerrors.Errorf("Cannot insert base config: %w", err)
		}
		say(notice, "Configuration 'base' was created to resemble this lotus-miner's config.toml .")
	}

	{ // make a layer representing the migration
		layerName := fmt.Sprintf("mig-%s", curioCfg.Addresses.Get()[0].MinerAddresses[0])
		_, err = db.Exec(ctx, "DELETE FROM harmony_config WHERE title=$1", layerName)
		if err != nil {
			return minerAddress, xerrors.Errorf("Cannot delete existing layer: %w", err)
		}

		// Express as new toml to avoid adding StorageRPCSecret in more than 1 layer
		curioCfg.Apis.StorageRPCSecret = ""
		ctBytes, err := config.TransparentMarshal(curioCfg)
		if err != nil {
			return minerAddress, err
		}

		_, err = db.Exec(ctx, "INSERT INTO harmony_config (title, config) VALUES ($1, $2)", layerName, string(ctBytes))
		if err != nil {
			return minerAddress, xerrors.Errorf("Cannot insert layer after layer created message: %w", err)
		}
		say(plain, "Layer %s created. ", layerName)
	}

	dbSettings := getDBSettings(*smCfg)
	say(plain, "To work with the config: ")
	fmt.Println(code.Render(`curio ` + dbSettings + ` config edit base`))
	say(plain, `To run Curio: With machine or cgroup isolation, use the command (with example layer selection):`)
	fmt.Println(code.Render(`curio ` + dbSettings + ` run --layer=post`))
	return minerAddress, nil
}

func getDBSettings(smCfg config.StorageMiner) string {
	dbSettings := ""
	def := config.DefaultStorageMiner().HarmonyDB
	if def.Hosts[0] != smCfg.HarmonyDB.Hosts[0] {
		dbSettings += ` --db-host="` + strings.Join(smCfg.HarmonyDB.Hosts, ",") + `"`
	}
	if def.Port != smCfg.HarmonyDB.Port {
		dbSettings += " --db-port=" + smCfg.HarmonyDB.Port
	}
	if def.Username != smCfg.HarmonyDB.Username {
		dbSettings += ` --db-user="` + smCfg.HarmonyDB.Username + `"`
	}
	if def.Password != smCfg.HarmonyDB.Password {
		dbSettings += ` --db-password="` + smCfg.HarmonyDB.Password + `"`
	}
	if def.Database != smCfg.HarmonyDB.Database {
		dbSettings += ` --db-name="` + smCfg.HarmonyDB.Database + `"`
	}
	return dbSettings
}

func ensureEmptyArrays(cfg *config.CurioConfig) {
	if cfg.Addresses == nil {
		cfg.Addresses.Set([]config.CurioAddresses{})
	} else {
		addrs := cfg.Addresses.Get()
		for i := range addrs {
			if addrs[i].PreCommitControl == nil {
				addrs[i].PreCommitControl = []string{}
			}
			if addrs[i].CommitControl == nil {
				addrs[i].CommitControl = []string{}
			}
			if addrs[i].DealPublishControl == nil {
				addrs[i].DealPublishControl = []string{}
			}
			if addrs[i].TerminateControl == nil {
				addrs[i].TerminateControl = []string{}
			}
		}
		cfg.Addresses.Set(addrs)
	}
	if cfg.Apis.ChainApiInfo == nil {
		cfg.Apis.ChainApiInfo = []string{}
	}
}

func cidPtrToStrptr(c *cid.Cid) *string {
	if c == nil {
		return nil
	}
	s := c.String()
	return &s
}

func coalescePtrs[A any](a, b *A) *A {
	if a != nil {
		return a
	}
	return b
}

const sectorStorePrefix = "/sectors"

func MigrateSectors(ctx context.Context, maddr address.Address, mmeta datastore.Batching, db *harmonydb.DB, logMig func(int), unmigSectorShouldFail func() bool) error {
	mid, err := address.IDFromAddress(maddr)
	if err != nil {
		return xerrors.Errorf("getting miner ID: %w", err)
	}

	sts := statestore.New(namespace.Wrap(mmeta, datastore.NewKey(sectorStorePrefix)))

	var sectors []SectorInfo
	if err := sts.List(&sectors); err != nil {
		return xerrors.Errorf("getting sector list: %w", err)
	}

	logMig(len(sectors))

	migratableState := func(state sector.SectorState) bool {
		switch state {
		case sector.Proving, sector.Available, sector.UpdateActivating, sector.Removed:
			return true
		default:
			return false
		}
	}

	unmigratable := map[sector.SectorState]int{}

	for _, sector := range sectors {
		if !migratableState(sector.State) {
			unmigratable[sector.State]++
			continue
		}
	}

	if len(unmigratable) > 0 {
		fmt.Println("The following sector states are not migratable:")
		for state, count := range unmigratable {
			fmt.Printf("  %s: %d\n", state, count)
		}

		if unmigSectorShouldFail() {
			return xerrors.Errorf("aborting migration because sectors were found that are not migratable.")
		}
	}

	for i, sectr := range sectors {
		if !migratableState(sectr.State) || sectr.State == sector.Removed {
			continue
		}
		if i > 0 && i%1000 == 0 {
			fmt.Printf("Migration: %d / %d (%0.2f%%)\n", i, len(sectors), float64(i)/float64(len(sectors))*100)
		}

		_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			// Insert sector metadata
			_, err := tx.Exec(`
        INSERT INTO sectors_meta (sp_id, sector_num, reg_seal_proof, ticket_epoch, ticket_value,
                                  orig_sealed_cid, orig_unsealed_cid, cur_sealed_cid, cur_unsealed_cid,
                                  msg_cid_precommit, msg_cid_commit, msg_cid_update, seed_epoch, seed_value)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
        ON CONFLICT (sp_id, sector_num) DO UPDATE
        SET reg_seal_proof = excluded.reg_seal_proof, ticket_epoch = excluded.ticket_epoch, ticket_value = excluded.ticket_value,
            orig_sealed_cid = excluded.orig_sealed_cid, orig_unsealed_cid = excluded.orig_unsealed_cid, cur_sealed_cid = excluded.cur_sealed_cid,
            cur_unsealed_cid = excluded.cur_unsealed_cid, msg_cid_precommit = excluded.msg_cid_precommit, msg_cid_commit = excluded.msg_cid_commit,
            msg_cid_update = excluded.msg_cid_update, seed_epoch = excluded.seed_epoch, seed_value = excluded.seed_value`,
				mid,
				sectr.SectorNumber,
				sectr.SectorType,
				sectr.TicketEpoch,
				sectr.TicketValue,
				cidPtrToStrptr(sectr.CommR),
				cidPtrToStrptr(sectr.CommD),
				cidPtrToStrptr(coalescePtrs(sectr.UpdateSealed, sectr.CommR)),
				cidPtrToStrptr(coalescePtrs(sectr.UpdateUnsealed, sectr.CommD)),
				cidPtrToStrptr(sectr.PreCommitMessage),
				cidPtrToStrptr(sectr.CommitMessage),
				cidPtrToStrptr(sectr.ReplicaUpdateMessage),
				sectr.SeedEpoch,
				sectr.SeedValue,
			)
			if err != nil {
				b, _ := json.MarshalIndent(sectr, "", "  ")
				fmt.Println(string(b))

				return false, xerrors.Errorf("inserting/updating sectors_meta for sector %d: %w", sectr.SectorNumber, err)
			}

			// Process each piece within the sector
			for j, piece := range sectr.Pieces {
				dealID := int64(0)
				startEpoch := int64(0)
				endEpoch := int64(0)
				var pamJSON *string
				var dealProposalJSONStr *string

				if piece.HasDealInfo() {
					dealInfo := piece.DealInfo()
					if dealInfo.Impl().DealProposal != nil {
						dealID = int64(dealInfo.Impl().DealID)
					}

					startEpoch = int64(must.One(dealInfo.StartEpoch()))
					endEpoch = int64(must.One(dealInfo.EndEpoch()))
					if piece.Impl().PieceActivationManifest != nil {
						pam, err := json.Marshal(piece.Impl().PieceActivationManifest)
						if err != nil {
							return false, xerrors.Errorf("error marshalling JSON for piece %d in sector %d: %w", j, sectr.SectorNumber, err)
						}
						ps := string(pam)
						pamJSON = &ps
					}
					if piece.Impl().DealProposal != nil {
						dealProposalJSON, err := json.Marshal(piece.Impl().DealProposal)
						if err != nil {
							return false, xerrors.Errorf("error marshalling deal proposal JSON for piece %d in sector %d: %w", j, sectr.SectorNumber, err)
						}
						dp := string(dealProposalJSON)
						dealProposalJSONStr = &dp
					}

				}

				// Splitting the SQL statement for readability and adding new fields
				_, err = tx.Exec(`
					INSERT INTO sectors_meta_pieces (
						sp_id, sector_num, piece_num, piece_cid, piece_size, 
						requested_keep_data, raw_data_size, start_epoch, orig_end_epoch,
						f05_deal_id, ddo_pam, f05_deal_proposal
					) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
					ON CONFLICT (sp_id, sector_num, piece_num) DO UPDATE
					SET 
						piece_cid = excluded.piece_cid, 
						piece_size = excluded.piece_size, 
						requested_keep_data = excluded.requested_keep_data, 
						raw_data_size = excluded.raw_data_size,
						start_epoch = excluded.start_epoch, 
						orig_end_epoch = excluded.orig_end_epoch,
						f05_deal_id = excluded.f05_deal_id, 
						ddo_pam = excluded.ddo_pam,
						f05_deal_proposal = excluded.f05_deal_proposal`,
					mid,
					sectr.SectorNumber,
					j,
					piece.PieceCID(),
					piece.Piece().Size,
					piece.HasDealInfo(),
					nil, // raw_data_size might be calculated based on the piece size, or retrieved if available
					startEpoch,
					endEpoch,
					dealID,
					pamJSON,
					dealProposalJSONStr,
				)
				if err != nil {
					b, _ := json.MarshalIndent(sectr, "", "  ")
					fmt.Println(string(b))

					return false, xerrors.Errorf("inserting/updating sector_meta_pieces for sector %d, piece %d: %w", sectr.SectorNumber, j, err)
				}
			}

			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return xerrors.Errorf("processing sector %d: %w", sectr.SectorNumber, err)
		}
	}

	return nil
}

type SectorInfo struct {
	State        sector.SectorState
	SectorNumber abi.SectorNumber

	SectorType abi.RegisteredSealProof

	// Packing
	CreationTime int64 // unix seconds
	Pieces       []sector.SafeSectorPiece

	// PreCommit1
	TicketValue   abi.SealRandomness
	TicketEpoch   abi.ChainEpoch
	PreCommit1Out storiface.PreCommit1Out

	PreCommit1Fails uint64

	// PreCommit2
	CommD *cid.Cid
	CommR *cid.Cid // SectorKey
	Proof []byte

	PreCommitDeposit big.Int
	PreCommitMessage *cid.Cid
	PreCommitTipSet  types.TipSetKey

	PreCommit2Fails uint64

	// WaitSeed
	SeedValue abi.InteractiveSealRandomness
	SeedEpoch abi.ChainEpoch

	// Committing
	CommitMessage *cid.Cid
	InvalidProofs uint64 // failed proof computations (doesn't validate with proof inputs; can't compute)

	// CCUpdate
	CCUpdate             bool
	UpdateSealed         *cid.Cid
	UpdateUnsealed       *cid.Cid
	ReplicaUpdateProof   storiface.ReplicaUpdateProof
	ReplicaUpdateMessage *cid.Cid

	// Faults
	FaultReportMsg *cid.Cid

	// Termination
	TerminateMessage *cid.Cid
	TerminatedAt     abi.ChainEpoch

	// Remote import
	RemoteDataUnsealed        *storiface.SectorLocation
	RemoteDataSealed          *storiface.SectorLocation
	RemoteDataCache           *storiface.SectorLocation
	RemoteCommit1Endpoint     string
	RemoteCommit2Endpoint     string
	RemoteSealingDoneEndpoint string
	RemoteDataFinalized       bool

	// Debug
	LastErr string
}
