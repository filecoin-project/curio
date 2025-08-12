package indexing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/maurl"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
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
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/chunker"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
	"github.com/filecoin-project/curio/market/ipni/types"
	"github.com/filecoin-project/curio/market/mk20"
)

type PDPIPNITask struct {
	db  *harmonydb.DB
	cpr *cachedreader.CachedPieceReader
	sc  *ffi.SealCalls
	cfg *config.CurioConfig
	max taskhelp.Limiter
}

func NewPDPIPNITask(db *harmonydb.DB, sc *ffi.SealCalls, cpr *cachedreader.CachedPieceReader, cfg *config.CurioConfig, max taskhelp.Limiter) *PDPIPNITask {
	return &PDPIPNITask{
		db:  db,
		cpr: cpr,
		sc:  sc,
		cfg: cfg,
		max: max,
	}
}

func (P *PDPIPNITask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var tasks []struct {
		ID       string `db:"id"`
		CtxID    []byte `db:"context_id"`
		Rm       bool   `db:"is_rm"`
		Prov     string `db:"provider"`
		Complete bool   `db:"complete"`
	}

	err = P.db.Select(ctx, &tasks, `SELECT 
											id,
											context_id,
											is_rm,
											provider,
											complete
										FROM 
											pdp_ipni_task
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
		log.Infow("IPNI task already complete", "task_id", taskID)
		return true, nil
	}

	var pinfo types.PieceInfo
	err = pinfo.UnmarshalCBOR(bytes.NewReader(task.CtxID))
	if err != nil {
		return false, xerrors.Errorf("unmarshaling piece info: %w", err)
	}

	pcid2 := pinfo.PieceCID

	pi, err := commcidv2.CommPFromPCidV2(pcid2)
	if err != nil {
		return false, xerrors.Errorf("getting piece info from piece cid: %w", err)
	}

	var lnk ipld.Link

	if pinfo.Payload {
		reader, _, err := P.cpr.GetSharedPieceReader(ctx, pcid2)
		if err != nil {
			return false, xerrors.Errorf("getting piece reader from piece park: %w", err)
		}

		defer reader.Close()

		recs := make(chan indexstore.Record, 1)

		var eg errgroup.Group
		addFail := make(chan struct{})
		var interrupted bool
		var subPieces []mk20.DataSource
		chk := chunker.NewInitialChunker()

		eg.Go(func() error {
			defer close(addFail)
			for rec := range recs {
				serr := chk.Accept(rec.Cid.Hash(), int64(rec.Offset), rec.Size)
				if serr != nil {
					addFail <- struct{}{}
					return serr
				}
			}
			return nil
		})

		id, serr := ulid.Parse(task.ID)
		if serr != nil {
			return false, xerrors.Errorf("parsing task id: %w", serr)
		}
		deal, serr := mk20.DealFromDB(ctx, P.db, id)
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
				subPieces = deal.Data.Format.Aggregate.Sub
				_, _, interrupted, err = IndexAggregate(pcid2, reader, pi.PieceInfo().Size, subPieces, recs, addFail)
			}
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

		lnk, err = chk.Finish(ctx, P.db, pcid2)
		if err != nil {
			return false, xerrors.Errorf("chunking CAR multihash iterator: %w", err)
		}
	} else {
		chk := chunker.NewInitialChunker()
		err = chk.Accept(pcid2.Hash(), 0, pi.PayloadSize())
		if err != nil {
			return false, xerrors.Errorf("adding index to chunk: %w", err)
		}
		lnk, err = chk.Finish(ctx, P.db, pcid2)
		if err != nil {
			return false, xerrors.Errorf("chunking CAR multihash iterator: %w", err)
		}
	}

	// make sure we still own the task before writing ad chains
	if !stillOwned() {
		return false, nil
	}

	_, err = P.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		var prev string
		err = tx.QueryRow(`SELECT head FROM ipni_head WHERE provider = $1`, task.Prov).Scan(&prev)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, xerrors.Errorf("querying previous head: %w", err)
		}

		var md []byte
		if pinfo.Payload {
			mds := metadata.IpfsGatewayHttp{}
			mdb, err := mds.MarshalBinary()
			if err != nil {
				return false, xerrors.Errorf("marshaling metadata: %w", err)
			}
			md = mdb
		} else {
			mds := metadata.FilecoinPieceHttp{}
			mdb, err := mds.MarshalBinary()
			if err != nil {
				return false, xerrors.Errorf("marshaling metadata: %w", err)
			}
			md = mdb
		}

		var privKey []byte
		err = tx.QueryRow(`SELECT priv_key FROM ipni_peerid WHERE sp_id = $1`, -1).Scan(&privKey)
		if err != nil {
			return false, xerrors.Errorf("failed to get private ipni-libp2p key for PDP: %w", err)
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
			u, err := url.Parse(fmt.Sprintf("https://%s", P.cfg.HTTP.DomainName))
			if err != nil {
				return false, xerrors.Errorf("parsing announce address domain: %w", err)
			}
			if build.BuildType != build.BuildMainnet && build.BuildType != build.BuildCalibnet {
				ls := strings.Split(P.cfg.HTTP.ListenAddress, ":")
				u, err = url.Parse(fmt.Sprintf("http://%s:%s", P.cfg.HTTP.DomainName, ls[1]))
				if err != nil {
					return false, xerrors.Errorf("parsing announce address domain: %w", err)
				}
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

		_, err = tx.Exec(`SELECT insert_ad_and_update_head($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			ad.(cidlink.Link).Cid.String(), adv.ContextID, md, pcid2.String(), pi.PieceInfo().PieceCID.String(), pi.PieceInfo().Size, adv.IsRm, adv.Provider, strings.Join(adv.Addresses, "|"),
			adv.Signature, adv.Entries.String())

		if err != nil {
			return false, xerrors.Errorf("adding advertisement to the database: %w", err)
		}

		n, err := tx.Exec(`UPDATE pdp_ipni_task SET complete = true WHERE task_id = $1`, taskID)
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

	log.Infow("IPNI task complete", "task_id", taskID)

	return true, nil
}

func (P *PDPIPNITask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (P *PDPIPNITask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPIPNI",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 1 << 30,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(30*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return P.schedule(context.Background(), taskFunc)
		}),
		Max: P.max,
	}
}

func (P *PDPIPNITask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	if P.cfg.Market.StorageMarketConfig.IPNI.Disable {
		return nil
	}

	// schedule submits
	var stop bool
	for !stop {
		var markComplete *string
		var markCompletePayload *string

		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var pendings []struct {
				ID                string                `db:"id"`
				PieceCid          string                `db:"piece_cid"`
				Size              abi.UnpaddedPieceSize `db:"piece_size"`
				PieceCidV2        string                `db:"piece_cid_v2"`
				Announce          bool                  `db:"announce"`
				AnnouncePayload   bool                  `db:"announce_payload"`
				IndexingCreatedAt time.Time             `db:"indexing_created_at"`
				Announced         bool                  `db:"announced"`
				AnnouncedPayload  bool                  `db:"announced_payload"`
			}

			err := tx.Select(&pendings, `SELECT
											  id,
											  piece_cid_v2,
											  piece_cid,
											  piece_size, 
											  raw_size,
											  indexing,
											  announce,
											  announce_payload,
											  announced,
											  announced_payload,
											  indexing_created_at
											FROM pdp_pipeline
											WHERE indexed = TRUE 
											  AND complete = FALSE
											LIMIT 1;`)
			if err != nil {
				return false, xerrors.Errorf("getting pending IPNI announcing tasks: %w", err)
			}

			if len(pendings) == 0 {
				return false, nil
			}

			p := pendings[0]

			// Mark deal is complete if:
			// 1. We don't need to announce anything
			// 2. Both type of announcements are done
			if !(p.Announce && p.AnnouncePayload) || (p.AnnouncePayload && p.AnnouncedPayload) {
				var n int
				n, err = tx.Exec(`UPDATE pdp_pipeline SET complete = TRUE WHERE id = $1`, p.ID)

				if err != nil {
					return false, xerrors.Errorf("store IPNI success: updating pipeline: %w", err)
				}
				if n != 1 {
					return false, xerrors.Errorf("store IPNI success: updated %d rows", n)
				}

				n, err = tx.Exec(`UPDATE market_mk20_deal
							SET pdp_v1 = jsonb_set(pdp_v1, '{complete}', 'true'::jsonb, true)
							WHERE id = $1;`, p.ID)
				if err != nil {
					return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
				}
				if n != 1 {
					return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
				}

				stop = false // we found a task to schedule, keep going
				return true, nil
			}

			var privKey []byte
			var peerIDStr string
			err = tx.QueryRow(`SELECT priv_key, peer_id FROM ipni_peerid WHERE sp_id = $1`, -1).Scan(&privKey, &peerIDStr)
			if err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					return false, xerrors.Errorf("failed to get private libp2p key for PDP: %w", err)
				}

				var pkey []byte

				err = tx.QueryRow(`SELECT priv_key FROM eth_keys WHERE role = 'pdp'`).Scan(&pkey)
				if err != nil {
					return false, xerrors.Errorf("failed to get private eth key for PDP: %w", err)
				}

				pk, err := crypto.UnmarshalPrivateKey(pkey)
				if err != nil {
					return false, xerrors.Errorf("unmarshaling private key: %w", err)
				}

				pid, err := peer.IDFromPublicKey(pk.GetPublic())
				if err != nil {
					return false, xerrors.Errorf("getting peer ID: %w", err)
				}

				n, err := tx.Exec(`INSERT INTO ipni_peerid (sp_id, priv_key, peer_id) VALUES ($1, $2, $3) ON CONFLICT(sp_id) DO NOTHING `, -1, privKey, pid.String())
				if err != nil {
					return false, xerrors.Errorf("failed to to insert the key into DB: %w", err)
				}

				if n == 0 {
					return false, xerrors.Errorf("failed to insert the key into db")
				}

				peerIDStr = pid.String()
			}

			pid, err := peer.Decode(peerIDStr)
			if err != nil {
				return false, fmt.Errorf("decoding peer ID: %w", err)
			}

			pcid, err := cid.Parse(p.PieceCid)
			if err != nil {
				return false, xerrors.Errorf("parsing piece CID: %w", err)
			}

			// If we need to announce payload and haven't done so, then do it first
			if p.AnnouncePayload && !p.AnnouncedPayload {
				pi := types.PieceInfo{
					PieceCID: pcid,
					Payload:  true,
				}

				b := new(bytes.Buffer)
				err = pi.MarshalCBOR(b)
				if err != nil {
					return false, xerrors.Errorf("marshaling piece info: %w", err)
				}

				_, err = tx.Exec(`SELECT insert_pdp_ipni_task($1, $2, $3, $4, $5)`, b.Bytes(), false, p.ID, pid.String(), id)
				if err != nil {
					if harmonydb.IsErrUniqueContraint(err) {
						ilog.Infof("Another IPNI announce task already present for piece %s and payload %d in deal %s", p.PieceCid, p.AnnouncePayload, p.ID)
						stop = false // we found a sector to work on, keep going
						markCompletePayload = &p.ID
						return false, nil
					}
					if strings.Contains(err.Error(), "already published") {
						ilog.Infof("Piece %s in deal %s is already published", p.PieceCid, p.ID)
						stop = false // we found a sector to work on, keep going
						markCompletePayload = &p.ID
						return false, nil
					}
					return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
				}
				stop = false
				markCompletePayload = &p.ID

				// Return early while commiting so we mark complete for payload announcement
				return true, nil
			}

			// If we don't need to announce payload, mark it as complete so pipeline does not try that
			if !p.AnnouncePayload && !p.AnnouncedPayload {
				stop = false
				markCompletePayload = &p.ID
				// Rerun early without commiting so we mark complete for payload announcement
				return false, nil
			}

			// If we need to announce piece and haven't done so then do it
			if p.Announce && !p.Announced {
				pi := types.PieceInfo{
					PieceCID: pcid,
					Payload:  false,
				}
				b := new(bytes.Buffer)
				err = pi.MarshalCBOR(b)
				if err != nil {
					return false, xerrors.Errorf("marshaling piece info: %w", err)
				}

				_, err = tx.Exec(`SELECT insert_pdp_ipni_task($1, $2, $3, $4, $5)`, b.Bytes(), false, p.ID, pid.String(), id)
				if err != nil {
					if harmonydb.IsErrUniqueContraint(err) {
						ilog.Infof("Another IPNI announce task already present for piece %s and payload %d in deal %s", p.PieceCid, p.AnnouncePayload, p.ID)
						stop = false // we found a sector to work on, keep going
						markComplete = &p.ID
						return false, nil

					}
					if strings.Contains(err.Error(), "already published") {
						ilog.Infof("Piece %s in deal %s is already published", p.PieceCid, p.ID)
						stop = false // we found a sector to work on, keep going
						markComplete = &p.ID
						return false, nil

					}
					return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
				}
				stop = false
				markComplete = &p.ID

				// Return early while commiting so we mark complete for piece announcement
				return true, nil
			}

			// If we don't need to announce piece, mark it as complete so pipeline does not try that
			if !p.Announce && !p.Announced {
				stop = false
				markComplete = &p.ID
				// Rerun early without commiting so we mark complete for payload announcement
				return false, nil
			}

			return false, xerrors.Errorf("no task to schedule")
		})

		if markComplete != nil {
			n, err := P.db.Exec(ctx, `UPDATE pdp_pipeline SET announced = TRUE WHERE id = $1`, *markComplete)
			if err != nil {
				log.Errorf("store IPNI success: updating pipeline: %w", err)
			}
			if n != 1 {
				log.Errorf("store IPNI success: updated %d rows", n)
			}
		}

		if markCompletePayload != nil {
			n, err := P.db.Exec(ctx, `UPDATE pdp_pipeline SET announced_payload = TRUE WHERE id = $1`, *markCompletePayload)
			if err != nil {
				log.Errorf("store IPNI success: updating pipeline: %w", err)
			}
			if n != 1 {
				log.Errorf("store IPNI success: updated %d rows", n)
			}
		}
	}

	return nil
}

func (P *PDPIPNITask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPIPNITask{}
var _ = harmonytask.Reg(&PDPIPNITask{})
