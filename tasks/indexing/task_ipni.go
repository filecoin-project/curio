package indexing

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/multiformats/go-varint"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/commcidv2"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/chunker"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
	"github.com/filecoin-project/curio/market/mk20"
)

var ilog = logging.Logger("ipni")

type IPNITask struct {
	db            *harmonydb.DB
	pieceProvider *pieceprovider.SectorReader
	cpr           *cachedreader.CachedPieceReader
	sc            *ffi.SealCalls
	cfg           *config.CurioConfig
	max           taskhelp.Limiter
}

func NewIPNITask(db *harmonydb.DB, sc *ffi.SealCalls, pieceProvider *pieceprovider.SectorReader, cpr *cachedreader.CachedPieceReader, cfg *config.CurioConfig, max taskhelp.Limiter) *IPNITask {
	return &IPNITask{
		db:            db,
		pieceProvider: pieceProvider,
		cpr:           cpr,
		sc:            sc,
		cfg:           cfg,
		max:           max,
	}
}

func (I *IPNITask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var tasks []struct {
		SPID     int64                   `db:"sp_id"`
		ID       sql.NullString          `db:"id"`
		Sector   abi.SectorNumber        `db:"sector"`
		Proof    abi.RegisteredSealProof `db:"reg_seal_proof"`
		Offset   int64                   `db:"sector_offset"`
		CtxID    []byte                  `db:"context_id"`
		Rm       bool                    `db:"is_rm"`
		Prov     string                  `db:"provider"`
		Complete bool                    `db:"complete"`
	}

	err = I.db.Select(ctx, &tasks, `SELECT 
											sp_id,
											id,
											sector, 
											reg_seal_proof,
											sector_offset,
											context_id,
											is_rm,
											provider,
											complete
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

	if task.Complete {
		log.Infow("IPNI task already complete", "task_id", taskID, "sector", task.Sector, "proof", task.Proof, "offset", task.Offset)
		return true, nil
	}

	if task.Rm {
		comm, err := I.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			var ads []struct {
				ContextID []byte `db:"context_id"`
				IsRm      bool   `db:"is_rm"`
				Previous  string `db:"previous"`
				Provider  string `db:"provider"`
				Addresses string `db:"addresses"`
				Entries   string `db:"entries"`
				Metadata  []byte `db:"metadata"`
				Pcid2     string `db:"piece_cid_v2"`
				Pcid1     string `db:"piece_cid"`
				Size      int64  `db:"piece_size"`
			}

			// Get the latest Ad
			err = tx.Select(&ads, `SELECT 
										context_id,
										is_rm, 
										previous, 
										provider, 
										addresses, 
										entries,
										metadata,
										piece_cid_v2,
										piece_cid,
										piece_size
										FROM ipni 
										WHERE context_id = $1 
										  AND provider = $2
										  ORDER BY order_number DESC
										  LIMIT 1`, task.CtxID, task.Prov)

			if err != nil {
				return false, xerrors.Errorf("getting ad from DB: %w", err)
			}

			if len(ads) == 0 {
				return false, xerrors.Errorf("not original ad found for removal ad")
			}

			if len(ads) > 1 {
				return false, xerrors.Errorf("expected 1 ad but got %d", len(ads))
			}

			a := ads[0]

			e, err := cid.Parse(a.Entries)
			if err != nil {
				return false, xerrors.Errorf("parsing entry CID: %w", err)
			}

			var prev string

			err = tx.QueryRow(`SELECT head FROM ipni_head WHERE provider = $1`, task.Prov).Scan(&prev)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return false, xerrors.Errorf("querying previous head: %w", err)
			}

			prevCID, err := cid.Parse(prev)
			if err != nil {
				return false, xerrors.Errorf("parsing previous CID: %w", err)
			}

			var privKey []byte
			err = tx.QueryRow(`SELECT priv_key FROM ipni_peerid WHERE sp_id = $1`, task.SPID).Scan(&privKey)
			if err != nil {
				return false, xerrors.Errorf("failed to get private ipni-libp2p key for PDP: %w", err)
			}

			pkey, err := crypto.UnmarshalPrivateKey(privKey)
			if err != nil {
				return false, xerrors.Errorf("unmarshaling private key: %w", err)
			}

			adv := schema.Advertisement{
				PreviousID: cidlink.Link{Cid: prevCID},
				Provider:   a.Provider,
				Addresses:  strings.Split(a.Addresses, "|"),
				Entries:    cidlink.Link{Cid: e},
				ContextID:  a.ContextID,
				IsRm:       true,
				Metadata:   a.Metadata,
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

			_, err = tx.Exec(`SELECT insert_ad_and_update_head($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				ad.(cidlink.Link).Cid.String(), adv.ContextID, a.Metadata, a.Pcid2, a.Pcid1, a.Size, adv.IsRm, adv.Provider, strings.Join(adv.Addresses, "|"),
				adv.Signature, adv.Entries.String())

			if err != nil {
				return false, xerrors.Errorf("adding advertisement to the database: %w", err)
			}

			n, err := tx.Exec(`UPDATE ipni_task SET complete = true WHERE task_id = $1`, taskID)
			if err != nil {
				return false, xerrors.Errorf("failed to mark IPNI task complete: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("updated %d rows", n)
			}

			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return false, xerrors.Errorf("store IPNI success: %w", err)
		}

		if !comm {
			return false, xerrors.Errorf("store IPNI success: failed to commit the transaction")
		}

		log.Infow("IPNI task complete", "task_id", taskID)
		return true, nil
	}

	var pi abi.PieceInfo
	err = pi.UnmarshalCBOR(bytes.NewReader(task.CtxID))
	if err != nil {
		return false, xerrors.Errorf("unmarshaling piece info: %w", err)
	}

	var rawSize abi.UnpaddedPieceSize
	err = I.db.QueryRow(ctx, `SELECT raw_size FROM market_piece_deal WHERE piece_cid = $1 AND piece_length = $2 LIMIT 1`, pi.PieceCID.String(), pi.Size).Scan(&rawSize)
	if err != nil {
		return false, xerrors.Errorf("querying raw size: %w", err)
	}

	pcid2, err := commcidv2.PieceCidV2FromV1(pi.PieceCID, uint64(rawSize))
	if err != nil {
		return false, xerrors.Errorf("getting piece CID v2: %w", err)
	}

	// Try to read unsealed sector first (mk12 deal)
	reader, err := I.pieceProvider.ReadPiece(ctx, storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(task.SPID),
			Number: task.Sector,
		},
		ProofType: task.Proof,
	}, storiface.PaddedByteIndex(task.Offset).Unpadded(), pi.Size.Unpadded(), pi.PieceCID)
	if err != nil {
		serr := err
		// Try to read piece (mk20 deal)
		reader, _, err = I.cpr.GetSharedPieceReader(ctx, pcid2, false)
		if err != nil {
			return false, xerrors.Errorf("getting piece reader from sector and piece park: %w, %w", serr, err)
		}
	}

	defer func() {
		_ = reader.Close()
	}()

	var isMK20 bool

	if task.ID.Valid {
		_, err := ulid.Parse(task.ID.String)
		if err == nil {
			isMK20 = true
		} else {
			_, err := uuid.Parse(task.ID.String)
			if err != nil {
				return false, xerrors.Errorf("parsing task id: %w", err)
			}
		}
	}

	recs := make(chan indexstore.Record, 1)

	var eg errgroup.Group
	addFail := make(chan struct{})
	var interrupted bool
	var subPieces []mk20.DataSource
	chk := chunker.NewInitialChunker()

	eg.Go(func() error {
		defer close(addFail)
		for rec := range recs {
			// CAR sections are [varint (length), CID, blockData]
			combinedSize := rec.Size + uint64(rec.Cid.ByteLen())
			lenSize := uint64(varint.UvarintSize(combinedSize))
			sectionSize := combinedSize + lenSize
			serr := chk.Accept(rec.Cid.Hash(), int64(rec.Offset), sectionSize)
			if serr != nil {
				addFail <- struct{}{}
				return serr
			}
		}
		return nil
	})

	if isMK20 {
		id, serr := ulid.Parse(task.ID.String)
		if serr != nil {
			return false, xerrors.Errorf("parsing task id: %w", serr)
		}
		deal, serr := mk20.DealFromDB(ctx, I.db, id)
		if serr != nil {
			return false, xerrors.Errorf("getting deal from db: %w", serr)
		}

		if deal.Data.Format.Raw != nil {
			return false, xerrors.Errorf("raw data not supported")
		}

		if deal.Data.Format.Car != nil {
			_, interrupted, err = IndexCAR(reader, 4<<20, recs, addFail)
		}

		if deal.Data.Format.Aggregate != nil {
			if deal.Data.Format.Aggregate.Type > 0 {
				var found bool
				if len(deal.Data.Format.Aggregate.Sub) > 0 {
					subPieces = deal.Data.Format.Aggregate.Sub
					found = true
				}
				if len(deal.Data.SourceAggregate.Pieces) > 0 {
					subPieces = deal.Data.SourceAggregate.Pieces
					found = true
				}
				if !found {
					return false, xerrors.Errorf("no sub pieces for aggregate mk20 deal")
				}
				_, _, interrupted, err = IndexAggregate(pcid2, reader, pi.Size, subPieces, recs, addFail)
			} else {
				return false, xerrors.Errorf("invalid aggregate type")
			}
		}

	} else {
		_, interrupted, err = IndexCAR(reader, 4<<20, recs, addFail)
	}

	if err != nil {
		// Chunking itself failed, stop early
		close(recs) // still safe to close, chk.Accept() will exit on channel close
		// wait for chk.Accept() goroutine to finish cleanly
		_ = eg.Wait()
		return false, xerrors.Errorf("chunking failed: %w", err)
	}

	// Close the channel
	close(recs)

	// Wait till  is finished
	err = eg.Wait()
	if err != nil {
		return false, xerrors.Errorf("adding index to chunk (interrupted %t): %w", interrupted, err)
	}

	// make sure we still own the task before writing to the database
	if !stillOwned() {
		return false, nil
	}

	lnk, err := chk.Finish(ctx, I.db, pcid2, false)
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
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
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

		{
			u, err := url.Parse(fmt.Sprintf("https://%s", I.cfg.HTTP.DomainName))
			if err != nil {
				return false, xerrors.Errorf("parsing announce address domain: %w", err)
			}
			if build.BuildType != build.BuildMainnet && build.BuildType != build.BuildCalibnet {
				ls := strings.Split(I.cfg.HTTP.ListenAddress, ":")
				u, err = url.Parse(fmt.Sprintf("http://%s:%s", I.cfg.HTTP.DomainName, ls[1]))
				if err != nil {
					return false, xerrors.Errorf("parsing announce address domain: %w", err)
				}
			}

			addr, err := FromURLWithPort(u)
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

		_, err = tx.Exec(`SELECT insert_ad_and_update_head($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			ad.(cidlink.Link).Cid.String(), adv.ContextID, md, pcid2.String(), pi.PieceCID.String(), pi.Size, adv.IsRm, adv.Provider, strings.Join(adv.Addresses, "|"),
			adv.Signature, adv.Entries.String())

		if err != nil {
			return false, xerrors.Errorf("adding advertisement to the database: %w", err)
		}

		n, err := tx.Exec(`UPDATE ipni_task SET complete = true WHERE task_id = $1`, taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to mark IPNI task complete: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("updated %d rows", n)
		}

		return true, nil

	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("store IPNI success: %w", err)
	}

	log.Infow("IPNI task complete", "task_id", taskID, "sector", task.Sector, "proof", task.Proof, "offset", task.Offset)

	return true, nil
}

func (I *IPNITask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	type task struct {
		TaskID    harmonytask.TaskID `db:"task_id"`
		ID        string             `db:"id"`
		StorageID string             `db:"storage_id"`
		IsRm      bool               `db:"is_rm"`
	}

	if storiface.FTUnsealed != 1 {
		panic("storiface.FTUnsealed != 1")
	}

	if storiface.FTPiece != 32 {
		panic("storiface.FTPiece != 32")
	}

	ctx := context.Background()

	indIDs := make([]int64, len(ids))
	for i, id := range ids {
		indIDs[i] = int64(id)
	}

	var tasks []task

	err := I.db.Select(ctx, &tasks, `
		SELECT task_id, id, is_rm FROM ipni_task WHERE task_id = ANY($1)`, indIDs)
	if err != nil {
		return nil, xerrors.Errorf("getting task details: %w", err)
	}

	var mk12TaskIds []harmonytask.TaskID
	var mk20TaskIds []harmonytask.TaskID

	for _, t := range tasks {
		if t.IsRm {
			return &ids[0], nil // If this is rm task then storage is not needed
		}
		_, err := ulid.Parse(t.ID)
		if err == nil {
			mk20TaskIds = append(mk20TaskIds, t.TaskID)
		} else {
			_, err := uuid.Parse(t.ID)
			if err != nil {
				return nil, xerrors.Errorf("parsing task id: %w", err)
			}
			mk12TaskIds = append(mk12TaskIds, t.TaskID)
		}
	}

	var finalTasks []task

	if len(mk12TaskIds) > 0 {
		var mk12Tasks []task
		err := I.db.Select(ctx, &mk12Tasks, `
			SELECT dp.task_id, dp.id, l.storage_id FROM ipni_task dp
				INNER JOIN sector_location l ON dp.sp_id = l.miner_id AND dp.sector = l.sector_num
				WHERE dp.task_id = ANY ($1) AND l.sector_filetype = 1`, indIDs)
		if err != nil {
			return nil, xerrors.Errorf("getting storage details: %w", err)
		}
		finalTasks = append(finalTasks, mk12Tasks...)
	}

	if len(mk20TaskIds) > 0 {
		var mk20Tasks []task
		err := I.db.Select(ctx, &mk20Tasks, `
							SELECT
							  dp.task_id,
							  dp.id,
							  l.storage_id
							FROM ipni_task dp
							JOIN market_piece_deal   mpd ON mpd.id    = dp.id
							JOIN parked_piece_refs    pr ON pr.ref_id = mpd.piece_ref
							JOIN sector_location       l ON l.miner_id = 0
														 AND l.sector_num = pr.piece_id
														 AND l.sector_filetype = 32
							WHERE dp.task_id = ANY ($1);
							`, indIDs)
		if err != nil {
			return nil, xerrors.Errorf("getting storage details: %w", err)
		}
		finalTasks = append(finalTasks, mk20Tasks...)
	}

	ls, err := I.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}

	acceptables := map[harmonytask.TaskID]bool{}

	for _, t := range ids {
		acceptables[t] = true
	}

	for _, t := range finalTasks {
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
		IAmBored: passcall.Every(30*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return I.schedule(context.Background(), taskFunc)
		}),
		Max: I.max,
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
		var mk20, isRM bool

		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var pendings []itask

			err := tx.Select(&pendings, `WITH unioned AS (
												SELECT
												  uuid, 
												  sp_id, 
												  sector,
												  piece_cid, 
												  piece_size, 
												  sector_offset,
												  reg_seal_proof,
												  raw_size,
												  should_index,
												  announce,
												  indexing_created_at,
												  FALSE as mk20,
												  FALSE as is_rm
												FROM market_mk12_deal_pipeline
												WHERE sealed = TRUE
												  AND indexed = TRUE 
												  AND complete = FALSE
												
												UNION ALL
												
												SELECT
												  id AS uuid,
												  sp_id,
												  sector,
												  piece_cid,
												  piece_size,
												  sector_offset,
												  reg_seal_proof,
												  raw_size,
												  indexing AS should_index,
												  announce,
												  indexing_created_at,
												  TRUE as mk20,
												  FALSE as is_rm
												FROM market_mk20_pipeline
												WHERE sealed = TRUE
												  AND indexed = TRUE 
												  AND complete = FALSE

												UNION ALL

												SELECT
												  id AS uuid,
												  sp_id,
												  sector_number AS sector,
												  piece_cid,
												  piece_size,
												  0::bigint AS sector_offset,
												  0::bigint AS reg_seal_proof,
												  0::bigint AS raw_size,
												  TRUE AS should_index,
												  TRUE AS announce,
												  TIMEZONE('UTC', NOW()) AS indexing_created_at,
												  TRUE as mk20,
												  TRUE as is_rm
												FROM piece_cleanup
												WHERE after_cleanup = TRUE
												  AND complete = FALSE
											)
											SELECT *
											FROM unioned
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
				stop = false // we found a task to schedule, keep going
				markComplete = &p.UUID
				mk20 = p.Mk20
				isRM = p.IsRM
				return false, nil
			}

			var privKey []byte
			err = tx.QueryRow(`SELECT priv_key FROM ipni_peerid WHERE sp_id = $1`, p.SpID).Scan(&privKey)
			if err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
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

			_, err = tx.Exec(`SELECT insert_ipni_task($1, $2, $3, $4, $5, $6, $7, $8, $9)`, p.UUID, p.SpID,
				p.Sector, p.Proof, p.Offset, b.Bytes(), p.IsRM, pid.String(), id)
			if err != nil {
				if harmonydb.IsErrUniqueContraint(err) {
					ilog.Infof("Another IPNI announce task already present for piece %s and RM %t in deal %s", p.PieceCid, p.IsRM, p.UUID)
					// SET "complete" status to true for this deal, so it is not considered next time
					markComplete = &p.UUID
					mk20 = p.Mk20
					isRM = p.IsRM
					stop = false // we found a sector to work on, keep going
					return false, nil
				}
				if strings.Contains(err.Error(), "already published") {
					ilog.Infof("Piece %s in deal %s is already published", p.PieceCid, p.UUID)
					// SET "complete" status to true for this deal, so it is not considered next time
					markComplete = &p.UUID
					mk20 = p.Mk20
					isRM = p.IsRM
					stop = false // we found a sector to work on, keep going
					return false, nil
				}
				return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			markComplete = &p.UUID
			mk20 = p.Mk20
			isRM = p.IsRM
			return true, nil
		})

		if markComplete != nil {
			var n int
			var err error
			if isRM {
				n, err = I.db.Exec(ctx, `UPDATE piece_cleanup SET complete = TRUE WHERE id = $1 AND complete = FALSE`, *markComplete)
			} else {
				if mk20 {
					n, err = I.db.Exec(ctx, `UPDATE market_mk20_pipeline SET complete = TRUE WHERE id = $1 AND complete = FALSE`, *markComplete)
				} else {
					n, err = I.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET complete = TRUE WHERE uuid = $1 AND complete = FALSE`, *markComplete)
				}
			}

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

func FromURLWithPort(u *url.URL) (multiaddr.Multiaddr, error) {
	h := u.Hostname()
	var addr *multiaddr.Multiaddr
	if n := net.ParseIP(h); n != nil {
		ipAddr, err := manet.FromIP(n)
		if err != nil {
			return nil, err
		}
		addr = &ipAddr
	} else {
		// domain name
		ma, err := multiaddr.NewComponent(multiaddr.ProtocolWithCode(multiaddr.P_DNS).Name, h)
		if err != nil {
			return nil, err
		}
		mab := multiaddr.Cast(ma.Bytes())
		addr = &mab
	}
	pv := u.Port()
	if pv != "" {
		port, err := multiaddr.NewComponent(multiaddr.ProtocolWithCode(multiaddr.P_TCP).Name, pv)
		if err != nil {
			return nil, err
		}
		wport := multiaddr.Join(*addr, port)
		addr = &wport
	} else {
		// default ports for http and https
		var port *multiaddr.Component
		var err error
		switch u.Scheme {
		case "http":
			port, err = multiaddr.NewComponent(multiaddr.ProtocolWithCode(multiaddr.P_TCP).Name, "80")
			if err != nil {
				return nil, err
			}
		case "https":
			port, err = multiaddr.NewComponent(multiaddr.ProtocolWithCode(multiaddr.P_TCP).Name, "443")
		}
		if err != nil {
			return nil, err
		}
		wport := multiaddr.Join(*addr, port)
		addr = &wport
	}

	http, err := multiaddr.NewComponent(u.Scheme, "")
	if err != nil {
		return nil, err
	}

	joint := multiaddr.Join(*addr, http)
	if u.Path != "" {
		httppath, err := multiaddr.NewComponent(multiaddr.ProtocolWithCode(multiaddr.P_HTTP_PATH).Name, url.PathEscape(u.Path))
		if err != nil {
			return nil, err
		}
		joint = multiaddr.Join(joint, httppath)
	}

	return joint, nil
}
