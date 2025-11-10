package indexing

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-varint"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/cachedreader"
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

	if len(tasks) == 0 {
		return true, nil
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 ipni task params, got %d", len(tasks))
	}

	task := tasks[0]

	if task.Complete {
		log.Infow("IPNI task already complete", "task_id", taskID)
		return true, nil
	}

	if task.Rm {
		comm, err := P.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
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
			err = tx.QueryRow(`SELECT priv_key FROM ipni_peerid WHERE sp_id = $1`, -1).Scan(&privKey)
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

		if !comm {
			return false, xerrors.Errorf("store IPNI success: failed to commit the transaction")
		}

		log.Infow("IPNI task complete", "task_id", taskID)
		return true, nil
	}

	pinfo := &types.PdpIpniContext{}
	err = pinfo.Unmarshal(task.CtxID)
	if err != nil {
		return false, xerrors.Errorf("unmarshaling piece info: %w", err)
	}

	pcid2 := pinfo.PieceCID

	pieceCid, rawSize, err := commcid.PieceCidV1FromV2(pcid2)
	if err != nil {
		return false, xerrors.Errorf("getting piece cid v1 from piece cid v2: %w", err)
	}

	size := padreader.PaddedSize(rawSize).Padded()

	var lnk ipld.Link

	if pinfo.Payload {
		reader, _, err := P.cpr.GetSharedPieceReader(ctx, pcid2, false)
		if err != nil {
			return false, xerrors.Errorf("getting piece reader from piece park: %w", err)
		}

		defer func() {
			_ = reader.Close()
		}()

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
				_, _, interrupted, err = IndexAggregate(pcid2, reader, size, subPieces, recs, addFail)
			} else {
				return false, xerrors.Errorf("invalid aggregate type")
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

		// Wait till is finished
		err = eg.Wait()
		if err != nil {
			return false, xerrors.Errorf("adding index to chunk (interrupted %t): %w", interrupted, err)
		}

		// make sure we still own the task before writing to the database
		if !stillOwned() {
			return false, nil
		}

		lnk, err = chk.Finish(ctx, P.db, pcid2, false)
		if err != nil {
			return false, xerrors.Errorf("chunking CAR multihash iterator: %w", err)
		}
	} else {
		chk := chunker.NewInitialChunker()
		err = chk.Accept(pcid2.Hash(), 0, uint64(size))
		if err != nil {
			return false, xerrors.Errorf("adding index to chunk: %w", err)
		}
		lnk, err = chk.Finish(ctx, P.db, pcid2, true)
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
			ad.(cidlink.Link).Cid.String(), adv.ContextID, md, pcid2.String(), pieceCid.String(), size, adv.IsRm, adv.Provider, strings.Join(adv.Addresses, "|"),
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
		Name: "PDPIpni",
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
		var markComplete, markCompletePayload, complete *string
		var isRm bool

		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var pendings []struct {
				ID               string `db:"id"`
				PieceCid         string `db:"piece_cid_v2"`
				IsRM             bool   `db:"is_rm"`
				Announce         bool   `db:"announce"`
				AnnouncePayload  bool   `db:"announce_payload"`
				Announced        bool   `db:"announced"`
				AnnouncedPayload bool   `db:"announced_payload"`
			}

			err := tx.Select(&pendings, `WITH unioned AS (
											  SELECT
												dp.id,
												dp.piece_cid_v2,
												dp.announce,
												dp.announce_payload,
												dp.announced,
												dp.announced_payload,
												FALSE AS is_rm
											  FROM pdp_pipeline dp
											  WHERE dp.indexed = TRUE
												AND dp.complete = FALSE
											
											  UNION ALL
											
											  SELECT
												pc.id,
												pc.piece_cid_v2,
												pc.announce,
												pc.announce_payload,
												pc.announced,
												pc.announced_payload,
												TRUE AS is_rm
											  FROM piece_cleanup pc
											  WHERE pc.after_cleanup = TRUE
												AND pc.complete = FALSE
											)
											SELECT *
											FROM unioned
											LIMIT 1;
											`)
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
			if (!p.Announce && !p.AnnouncePayload) || (p.Announced && p.AnnouncedPayload) { //nolint:staticcheck
				isRm = p.IsRM
				complete = &p.ID
				return false, nil
			}

			var privKey []byte
			var peerIDStr string
			err = tx.QueryRow(`SELECT priv_key, peer_id FROM ipni_peerid WHERE sp_id = $1`, -1).Scan(&privKey, &peerIDStr)
			if err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					return false, xerrors.Errorf("failed to get private libp2p key for PDP: %w", err)
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
				pi := &types.PdpIpniContext{
					PieceCID: pcid,
					Payload:  true,
				}

				iContext, err := pi.Marshal()
				if err != nil {
					return false, xerrors.Errorf("marshaling piece info: %w", err)
				}

				_, err = tx.Exec(`SELECT insert_pdp_ipni_task($1, $2, $3, $4, $5)`, iContext, p.IsRM, p.ID, pid.String(), id)
				if err != nil {
					if harmonydb.IsErrUniqueContraint(err) {
						ilog.Infof("Another IPNI announce task already present for piece %s and payload %d with RM %t in deal %s", p.PieceCid, p.AnnouncePayload, p.IsRM, p.ID)
						stop = false // we found a sector to work on, keep going
						isRm = p.IsRM
						markCompletePayload = &p.ID
						return false, nil
					}
					if strings.Contains(err.Error(), "already published") {
						ilog.Infof("Piece %s in deal %s is already published", p.PieceCid, p.ID)
						stop = false // we found a sector to work on, keep going
						isRm = p.IsRM
						markCompletePayload = &p.ID
						return false, nil
					}
					return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
				}
				stop = false
				isRm = p.IsRM
				markCompletePayload = &p.ID
				// Return early while commiting so we mark complete for payload announcement
				return true, nil
			}

			// If we don't need to announce payload, mark it as complete so pipeline does not try that
			if !p.AnnouncePayload && !p.AnnouncedPayload {
				stop = false
				isRm = p.IsRM
				markCompletePayload = &p.ID
				// Rerun early without commiting so we mark complete for payload announcement
				return false, nil
			}

			// If we need to announce piece and haven't done so then do it
			if p.Announce && !p.Announced {
				pi := &types.PdpIpniContext{
					PieceCID: pcid,
					Payload:  false,
				}

				iContext, err := pi.Marshal()
				if err != nil {
					return false, xerrors.Errorf("marshaling piece info: %w", err)
				}

				_, err = tx.Exec(`SELECT insert_pdp_ipni_task($1, $2, $3, $4, $5)`, iContext, p.IsRM, p.ID, pid.String(), id)
				if err != nil {
					if harmonydb.IsErrUniqueContraint(err) {
						ilog.Infof("Another IPNI announce task already present for piece %s and payload %d with RM %t in deal %s", p.PieceCid, p.AnnouncePayload, p.IsRM, p.ID)
						stop = false // we found a sector to work on, keep going
						markComplete = &p.ID
						isRm = p.IsRM
						return false, nil

					}
					if strings.Contains(err.Error(), "already published") {
						ilog.Infof("Piece %s in deal %s is already published", p.PieceCid, p.ID)
						stop = false // we found a sector to work on, keep going
						markComplete = &p.ID
						isRm = p.IsRM
						return false, nil

					}
					return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
				}
				stop = false
				isRm = p.IsRM
				markComplete = &p.ID
				// Return early while commiting so we mark complete for piece announcement
				return true, nil
			}

			// If we don't need to announce piece, mark it as complete so pipeline does not try that
			if !p.Announce && !p.Announced {
				stop = false
				isRm = p.IsRM
				markComplete = &p.ID
				// Rerun early without commiting so we mark complete for payload announcement
				return false, nil
			}

			return false, xerrors.Errorf("no task to schedule")
		})

		if markComplete != nil {
			var n int
			var err error
			if isRm {
				n, err = P.db.Exec(ctx, `UPDATE piece_cleanup SET announced = TRUE WHERE id = $1`, *markComplete)
			} else {
				n, err = P.db.Exec(ctx, `UPDATE pdp_pipeline SET announced = TRUE WHERE id = $1`, *markComplete)
			}

			if err != nil {
				log.Errorf("store IPNI success: updating pipeline: %w", err)
			}
			if n != 1 {
				log.Errorf("store IPNI success: updated %d rows", n)
			}
		}

		if markCompletePayload != nil {
			var n int
			var err error
			if isRm {
				n, err = P.db.Exec(ctx, `UPDATE piece_cleanup SET announced_payload = TRUE WHERE id = $1`, *markCompletePayload)
			} else {
				n, err = P.db.Exec(ctx, `UPDATE pdp_pipeline SET announced_payload = TRUE WHERE id = $1`, *markCompletePayload)
			}
			if err != nil {
				log.Errorf("store IPNI success: updating pipeline: %w", err)
			}
			if n != 1 {
				log.Errorf("store IPNI success: updated %d rows", n)
			}
		}

		if complete != nil {
			comm, err := P.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
				if isRm {
					n, err := tx.Exec(`UPDATE piece_cleanup SET complete = TRUE WHERE id = $1`, *complete)
					if err != nil {
						return false, xerrors.Errorf("updating piece cleanup pipeline: %w", err)
					}
					if n != 1 {
						return false, xerrors.Errorf("expected to update 1 row in piece_cleanup but updated %d rows", n)
					}
					return true, nil
				}
				n, err := tx.Exec(`UPDATE pdp_pipeline SET complete = TRUE WHERE id = $1`, *complete)

				if err != nil {
					return false, xerrors.Errorf("updating pipeline: %w", err)
				}
				if n != 1 {
					return false, xerrors.Errorf("expected to update 1 row but updated %d rows", n)
				}

				n, err = tx.Exec(`UPDATE market_mk20_deal
							SET pdp_v1 = jsonb_set(pdp_v1, '{complete}', 'true'::jsonb, true)
							WHERE id = $1;`, *complete)
				if err != nil {
					return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
				}
				if n != 1 {
					return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
				}

				stop = false // we found a task to schedule, keep going
				ilog.Debugf("Deal %s is marked as complete", *complete)
				return true, nil
			}, harmonydb.OptionRetry())
			if err != nil {
				return xerrors.Errorf("marking deal as complete: %w", err)
			}
			if !comm {
				return xerrors.Errorf("marking deal as complete: failed to commit transaction")
			}
		}
	}

	return nil
}

func (P *PDPIPNITask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPIPNITask{}
var _ = harmonytask.Reg(&PDPIPNITask{})
