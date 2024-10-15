package indexing

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	carv2 "github.com/ipld/go-car/v2"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/maurl"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/chunker"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
)

var ilog = logging.Logger("ipni")

type IPNITask struct {
	db            *harmonydb.DB
	indexStore    *indexstore.IndexStore
	pieceProvider *pieceprovider.PieceProvider
	sc            *ffi.SealCalls
	cfg           *config.CurioConfig
}

func NewIPNITask(db *harmonydb.DB, sc *ffi.SealCalls, indexStore *indexstore.IndexStore, pieceProvider *pieceprovider.PieceProvider, cfg *config.CurioConfig) *IPNITask {

	return &IPNITask{
		db:            db,
		indexStore:    indexStore,
		pieceProvider: pieceProvider,
		sc:            sc,
		cfg:           cfg,
	}
}

func (I *IPNITask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var tasks []struct {
		SPID   int64                   `db:"sp_id"`
		Sector abi.SectorNumber        `db:"sector"`
		Proof  abi.RegisteredSealProof `db:"reg_seal_proof"`
		Offset int64                   `db:"sector_offset"`
		CtxID  []byte                  `db:"context_id"`
		Rm     bool                    `db:"is_rm"`
		Prov   string                  `db:"provider"`
	}

	err = I.db.Select(ctx, &tasks, `SELECT 
											sp_id, 
											sector, 
											reg_seal_proof,
											sector_offset,
											context_id,
											is_rm,
											provider	
										FROM 
											ipni_task
										WHERE 
											task_id = $1;`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting ipni task params: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 ipni task params, got %d", len(tasks))
	}

	task := tasks[0]

	var pi abi.PieceInfo
	err = pi.UnmarshalCBOR(bytes.NewReader(task.CtxID))
	if err != nil {
		return false, xerrors.Errorf("unmarshaling piece info: %w", err)
	}

	reader, err := I.pieceProvider.ReadPiece(ctx, storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(task.SPID),
			Number: task.Sector,
		},
		ProofType: task.Proof,
	}, storiface.UnpaddedByteIndex(abi.PaddedPieceSize(task.Offset).Unpadded()), pi.Size.Unpadded(), pi.PieceCID)
	if err != nil {
		return false, xerrors.Errorf("getting piece reader: %w", err)
	}

	opts := []carv2.Option{carv2.ZeroLengthSectionAsEOF(true)}
	blockReader, err := carv2.NewBlockReader(reader, opts...)
	if err != nil {
		return false, fmt.Errorf("getting block reader over piece: %w", err)
	}

	chk := chunker.NewInitialChunker()

	blockMetadata, err := blockReader.SkipNext()
	for err == nil {
		if err := chk.Accept(blockMetadata.Cid.Hash(), int64(blockMetadata.Offset)); err != nil {
			return false, xerrors.Errorf("accepting block: %w", err)
		}

		blockMetadata, err = blockReader.SkipNext()
	}
	if !errors.Is(err, io.EOF) {
		return false, xerrors.Errorf("reading block: %w", err)
	}

	// make sure we still own the task before writing to the database
	if !stillOwned() {
		return false, nil
	}

	lnk, err := chk.Finish(ctx, I.db, pi.PieceCID)
	if err != nil {
		return false, xerrors.Errorf("chunking CAR multihash iterator: %w", err)
	}

	// make sure we still own the task before writing ad chains
	if !stillOwned() {
		return false, nil
	}

	_, err = I.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		var prev string
		err = tx.QueryRow(`SELECT head FROM ipni_head WHERE provider = $1`, task.Prov).Scan(&prev)
		if err != nil && err != pgx.ErrNoRows {
			return false, xerrors.Errorf("querying previous head: %w", err)
		}

		mds := metadata.IpfsGatewayHttp{}
		md, err := mds.MarshalBinary()
		if err != nil {
			return false, xerrors.Errorf("marshaling metadata: %w", err)
		}

		var privKey []byte
		err = tx.QueryRow(`SELECT priv_key FROM ipni_peerid WHERE sp_id = $1`, task.SPID).Scan(&privKey)
		if err != nil {
			return false, xerrors.Errorf("failed to get private ipni-libp2p key for miner %d: %w", task.SPID, err)
		}

		pkey, err := crypto.UnmarshalPrivateKey(privKey)
		if err != nil {
			return false, xerrors.Errorf("unmarshaling private key: %w", err)
		}

		adv := schema.Advertisement{
			Provider:  task.Prov,
			Entries:   lnk,
			ContextID: task.CtxID,
			Metadata:  md,
			IsRm:      task.Rm,
		}

		for _, a := range I.cfg.Market.StorageMarketConfig.IPNI.AnnounceAddresses {
			u, err := url.Parse(strings.TrimSpace(a))
			if err != nil {
				return false, xerrors.Errorf("parsing announce address: %w", err)
			}
			addr, err := maurl.FromURL(u)
			if err != nil {
				return false, xerrors.Errorf("converting URL to multiaddr: %w", err)
			}
			adv.Addresses = append(adv.Addresses, addr.String())
		}

		if prev != "" {
			prevCID, err := cid.Parse(prev)
			if err != nil {
				return false, xerrors.Errorf("parsing previous CID: %w", err)
			}

			adv.PreviousID = cidlink.Link{Cid: prevCID}
		}

		err = adv.Sign(pkey)
		if err != nil {
			return false, xerrors.Errorf("signing the advertisement: %w", err)
		}

		err = adv.Validate()
		if err != nil {
			return false, xerrors.Errorf("validating the advertisement: %w", err)
		}

		adNode, err := adv.ToNode()
		if err != nil {
			return false, xerrors.Errorf("converting advertisement to node: %w", err)
		}

		ad, err := ipniculib.NodeToLink(adNode, schema.Linkproto)
		if err != nil {
			return false, xerrors.Errorf("converting advertisement to link: %w", err)
		}

		n, err := I.db.Exec(ctx, `SELECT insert_ad_and_update_head($1, $2, $3, $4, $5, $6, $7)`,
			ad.(cidlink.Link).Cid.String(), adv.ContextID, adv.IsRm, adv.Provider, strings.Join(adv.Addresses, "|"),
			adv.Signature, adv.Entries.String())

		if err != nil {
			return false, xerrors.Errorf("adding advertisement to the database: %w", err)
		}

		if n != 1 {
			return false, xerrors.Errorf("updated %d rows", n)
		}

		return true, nil

	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("store IPNI success: %w", err)
	}

	return true, nil
}

func (I *IPNITask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	var tasks []struct {
		TaskID       harmonytask.TaskID `db:"task_id"`
		SpID         int64              `db:"sp_id"`
		SectorNumber int64              `db:"sector"`
		StorageID    string             `db:"storage_id"`
	}

	if storiface.FTUnsealed != 1 {
		panic("storiface.FTUnsealed != 1")
	}

	ctx := context.Background()

	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	err := I.db.Select(ctx, &tasks, `
		SELECT dp.task_id, dp.sp_id, dp.sector, l.storage_id FROM ipni_task dp
			INNER JOIN sector_location l ON dp.sp_id = l.miner_id AND dp.sector = l.sector_num
			WHERE dp.task_id = ANY ($1) AND l.sector_filetype = 1
`, indIDs)
	if err != nil {
		return nil, xerrors.Errorf("getting tasks: %w", err)
	}

	ls, err := I.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}

	acceptables := map[harmonytask.TaskID]bool{}

	for _, t := range ids {
		acceptables[t] = true
	}

	for _, t := range tasks {
		if _, ok := acceptables[t.TaskID]; !ok {
			continue
		}

		for _, l := range ls {
			if string(l.ID) == t.StorageID {
				return &t.TaskID, nil
			}
		}
	}

	return nil, nil
}

func (I *IPNITask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "IPNI",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 1 << 30,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(5*time.Minute, func(taskFunc harmonytask.AddTaskFunc) error {
			return I.schedule(context.Background(), taskFunc)
		}),
	}
}

func (I *IPNITask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	// If IPNI is disabled then don't schedule any tasks
	// Deals should already be marked as complete by task_indexing
	// This check is to cover any edge case
	if I.cfg.Market.StorageMarketConfig.IPNI.Disable {
		return nil
	}

	// schedule submits
	var stop bool
	for !stop {
		var markComplete *string

		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var pendings []itask

			err := tx.Select(&pendings, `SELECT
    										uuid, 
											sp_id, 
											sector,
											piece_cid, 
											piece_size, 
											sector_offset,
											reg_seal_proof,
											raw_size,
											should_index,
											announce
											FROM market_mk12_deal_pipeline 
											WHERE sealed = TRUE
											AND indexed = TRUE 
											AND complete = FALSE
											ORDER BY indexing_created_at ASC
											LIMIT 1;`)
			if err != nil {
				return false, xerrors.Errorf("getting pending IPNI announcing tasks: %w", err)
			}

			if len(pendings) == 0 {
				return false, nil
			}

			p := pendings[0]

			// Skip IPNI if deal says not to announce or not to index (fast retrievals). If we announce without
			// indexing, it will cause issue with retrievals.
			if !p.Announce || !p.ShouldIndex {
				n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET complete = TRUE WHERE uuid = $1`, p.UUID)
				if err != nil {
					return false, xerrors.Errorf("store IPNI success: updating pipeline (1): %w", err)
				}
				if n != 1 {
					return false, xerrors.Errorf("store IPNI success: updated %d rows", n)
				}
				stop = false // we found a task to schedule, keep going
				return true, nil
			}

			var privKey []byte
			err = tx.QueryRow(`SELECT priv_key FROM ipni_peerid WHERE sp_id = $1`, p.SpID).Scan(&privKey)
			if err != nil {
				if err != pgx.ErrNoRows {
					return false, xerrors.Errorf("failed to get private libp2p key for miner %d: %w", p.SpID, err)
				}

				// generate the ipni provider key
				pk, _, err := crypto.GenerateEd25519Key(rand.Reader)
				if err != nil {
					return false, xerrors.Errorf("failed to generate a new key: %w", err)
				}

				privKey, err = crypto.MarshalPrivateKey(pk)
				if err != nil {
					return false, xerrors.Errorf("failed to marshal the private key: %w", err)
				}

				pid, err := peer.IDFromPublicKey(pk.GetPublic())
				if err != nil {
					return false, xerrors.Errorf("getting peer ID: %w", err)
				}

				n, err := tx.Exec(`INSERT INTO ipni_peerid (sp_id, priv_key, peer_id) VALUES ($1, $2, $3) ON CONFLICT(sp_id) DO NOTHING `, p.SpID, privKey, pid.String())
				if err != nil {
					return false, xerrors.Errorf("failed to to insert the key into DB: %w", err)
				}

				if n == 0 {
					return false, xerrors.Errorf("failed to insert the key into db")
				}
			}

			pkey, err := crypto.UnmarshalPrivateKey(privKey)
			if err != nil {
				return false, xerrors.Errorf("unmarshaling private key: %w", err)
			}

			pid, err := peer.IDFromPublicKey(pkey.GetPublic())
			if err != nil {
				return false, fmt.Errorf("getting peer ID: %w", err)
			}

			pcid, err := cid.Parse(p.PieceCid)
			if err != nil {
				return false, xerrors.Errorf("parsing piece CID: %w", err)
			}

			pi := abi.PieceInfo{
				PieceCID: pcid,
				Size:     p.Size,
			}

			b := new(bytes.Buffer)
			err = pi.MarshalCBOR(b)
			if err != nil {
				return false, xerrors.Errorf("marshaling piece info: %w", err)
			}

			_, err = tx.Exec(`SELECT insert_ipni_task($1, $2, $3, $4, $5, $6, $7, $8)`, p.SpID,
				p.Sector, p.Proof, p.Offset, b.Bytes(), false, pid.String(), id)
			if err != nil {
				if harmonydb.IsErrUniqueContraint(err) {
					ilog.Infof("Another IPNI announce task already present for piece %s in deal %s", p.PieceCid, p.UUID)
					// SET "complete" status to true for this deal, so it is not considered next time
					markComplete = &p.UUID
					stop = false // we found a sector to work on, keep going
					return true, nil
				}
				if strings.Contains(err.Error(), "already published") {
					ilog.Infof("Piece %s in deal %s is already published", p.PieceCid, p.UUID)
					// SET "complete" status to true for this deal, so it is not considered next time
					markComplete = &p.UUID
					stop = false // we found a sector to work on, keep going
					return false, nil
				}
				return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

		if markComplete != nil {
			n, err := I.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET complete = TRUE WHERE uuid = $1 AND complete = FALSE`, *markComplete)
			if err != nil {
				log.Errorf("store IPNI success: updating pipeline (2): %s", err)
			}
			if n != 1 {
				log.Errorf("store IPNI success: updated %d rows", n)
			}
		}
	}

	return nil
}

func (I *IPNITask) Adder(taskFunc harmonytask.AddTaskFunc) {}

func (I *IPNITask) GetSpid(db *harmonydb.DB, taskID int64) string {
	var spid string
	err := db.QueryRow(context.Background(), `SELECT sp_id FROM ipni_task WHERE task_id = $1`, taskID).Scan(&spid)
	if err != nil {
		log.Errorf("getting spid: %s", err)
		return ""
	}
	return spid
}

var _ = harmonytask.Reg(&IPNITask{})
var _ harmonytask.TaskInterface = &IPNITask{}
