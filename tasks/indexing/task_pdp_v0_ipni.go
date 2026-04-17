package indexing

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/urlhelper"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/chunker"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
)

const (
	PDP_v0_SP_ID = -2 // This is the SP ID for PDP in the IPNI table
)

type PDPV0IPNITask struct {
	db  *harmonydb.DB
	cfg *config.CurioConfig
	max taskhelp.Limiter
	idx *indexstore.IndexStore
}

func NewPDPV0IPNITask(db *harmonydb.DB, cfg *config.CurioConfig, max taskhelp.Limiter, idx *indexstore.IndexStore) *PDPV0IPNITask {
	return &PDPV0IPNITask{
		db:  db,
		cfg: cfg,
		max: max,
		idx: idx,
	}
}

func (P *PDPV0IPNITask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var tasks []struct {
		ID       int64  `db:"id"`
		PieceCID string `db:"piece_cid"`
		Size     int64  `db:"piece_padded_size"`
		RawSize  int64  `db:"piece_raw_size"`
		Prov     string `db:"peer_id"`
	}

	err = P.db.Select(ctx, &tasks, `
									SELECT
										pr.id,
										pr.piece_cid,
										pp.piece_padded_size,
										pp.piece_raw_size,
										ipni_peer.peer_id
									FROM
										pdp_piecerefs pr
									JOIN parked_piece_refs ppr ON pr.piece_ref = ppr.ref_id
									JOIN parked_pieces pp ON ppr.piece_id = pp.id
									CROSS JOIN ipni_peerid ipni_peer
									WHERE
										pr.ipni_task_id = $1
										AND ipni_peer.sp_id = $2`, taskID, PDP_v0_SP_ID)
	if err != nil {
		return false, xerrors.Errorf("getting ipni task params: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 ipni task params, got %d", len(tasks))
	}

	task := tasks[0]

	var isRm bool
	err = P.db.QueryRow(ctx, `SELECT is_rm FROM ipni WHERE piece_cid = $1 AND piece_size = $2 ORDER BY order_number DESC LIMIT 1`, task.PieceCID, task.Size).Scan(&isRm)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, xerrors.Errorf("checking if piece is already published: %w", err)
	}

	if exists && !isRm {
		ilog.Infow("IPNI task already published", "task_id", taskID, "piece_cid", task.PieceCID)
		return true, nil
	}

	pcid, err := cid.Parse(task.PieceCID)
	if err != nil {
		return false, xerrors.Errorf("parsing piece CID: %w", err)
	}

	// Convert to PieceCIDv2 for IPNI ContextID
	pcidV2, err := commcid.PieceCidV2FromV1(pcid, uint64(task.RawSize))
	if err != nil {
		return false, xerrors.Errorf("converting piece CID to v2: %w", err)
	}

	mds := metadata.IpfsGatewayHttp{}
	md, err := mds.MarshalBinary()
	if err != nil {
		return false, xerrors.Errorf("marshaling metadata: %w", err)
	}

	pi := &PdpIpniContext{
		PieceCID: pcidV2,
		Payload:  true,
	}

	iContext, err := pi.Marshal()
	if err != nil {
		return false, xerrors.Errorf("marshaling piece info: %w", err)
	}

	chk := chunker.NewInitialChunker()
	offset := multihash.Multihash{0}
	ostr := offset.String()

	for {
		// Get the next EntriesChunkSize+1 (16384+1) entries for chunking
		// Use pcidv2 as indexStore will internally get us the correct index gainst v1 or v2 as present
		mhs, err := P.idx.GetPieceHashRange(ctx, pcidV2, offset, chunker.EntriesChunkSize+1, false)
		if err != nil {
			return false, xerrors.Errorf("getting piece hashes: %w", err)
		}

		if offset.String() == ostr && len(mhs) == 0 {
			return false, xerrors.Errorf("no index record found for piece %s", pcidV2.String())
		}

		if len(mhs) <= chunker.EntriesChunkSize && len(mhs) > 0 {
			// Send EntriesChunkSize (16384) or less entries for chunking
			err = chk.Accept(mhs)
			if err != nil {
				return false, xerrors.Errorf("adding index to chunk: %w", err)
			}
			break
		}

		if len(mhs) == chunker.EntriesChunkSize+1 {
			// Send EntriesChunkSize (16384) entries for chunking
			err = chk.Accept(mhs[:len(mhs)-1])
			if err != nil {
				return false, xerrors.Errorf("adding index to chunk: %w", err)
			}
			// Use the last entry as the offset for the next iteration
			offset = mhs[len(mhs)-1]
			continue
		}

		return false, xerrors.Errorf("number of index records is not expected: %d", len(mhs))
	}

	// make sure we still own the task before writing to the database
	if !stillOwned() {
		return false, nil
	}

	// Note: Till now we were saving everything with pcid1 and from now on it would be pcid2
	// server-chunker is backward compatible-aware so older pcid1 should still be served fine
	lnk, err := chk.Finish(ctx, P.db, pcidV2, false)
	if err != nil {
		return false, xerrors.Errorf("chunking CAR multihash iterator: %w", err)
	}

	for range ipniHeadCASRetries {
		if !stillOwned() {
			return false, nil
		}
		comm, err := P.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			var prev string
			err = tx.QueryRow(`SELECT head FROM ipni_head WHERE provider = $1`, task.Prov).Scan(&prev)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return false, xerrors.Errorf("querying previous head: %w", err)
			}

			var privKey []byte
			err = tx.QueryRow(`SELECT priv_key FROM ipni_peerid WHERE sp_id = $1`, PDP_v0_SP_ID).Scan(&privKey)
			if err != nil {
				return false, xerrors.Errorf("failed to get private ipni-libp2p key: %w", err)
			}

			pkey, err := crypto.UnmarshalPrivateKey(privKey)
			if err != nil {
				return false, xerrors.Errorf("unmarshaling private key: %w", err)
			}

			adv := schema.Advertisement{
				Provider:  task.Prov,
				Entries:   lnk,
				ContextID: iContext,
				Metadata:  md,
				IsRm:      false, // No PDP IPNI IsRms (yet)
			}

			{
				u, err := urlhelper.GetExternalURL(&P.cfg.HTTP)
				if err != nil {
					return false, xerrors.Errorf("getting external URL for IPNI: %w", err)
				}

				addr, err := urlhelper.FromURLWithPort(u)
				if err != nil {
					return false, xerrors.Errorf("converting URL to multiaddr: %w", err)
				}

				ilog.Infow("Announcing piece to IPNI", "piece", pi.PieceCID, "provider", task.Prov, "addr", addr.String(), "task", taskID)

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

			var inserted bool
			err = tx.QueryRow(`SELECT insert_ad_and_update_head_checked($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
				ad.(cidlink.Link).Cid.String(), adv.ContextID, md, pcidV2.String(), task.PieceCID, task.Size, adv.IsRm, adv.Provider, strings.Join(adv.Addresses, "|"),
				adv.Signature, adv.Entries.String(), nullableText(prev)).Scan(&inserted)
			if err != nil {
				return false, xerrors.Errorf("adding advertisement to the database: %w", err)
			}
			if !inserted {
				return false, nil
			}

			err = P.recordCompletion(tx, taskID, task.ID)
			if err != nil {
				return false, xerrors.Errorf("recording IPNI task completion: %w", err)
			}

			return true, nil

		}, harmonydb.OptionRetry())
		if err != nil {
			return false, xerrors.Errorf("store IPNI success: %w", err)
		}

		if comm {
			ilog.Infow("IPNI task complete", "task_id", taskID, "piece_cid", task.PieceCID)
			return true, nil
		}
	}
	return false, xerrors.Errorf("failed to publish piece to IPNI after %d attempts", ipniHeadCASRetries)
}

func (P *PDPV0IPNITask) recordCompletion(tx *harmonydb.Tx, taskID harmonytask.TaskID, id int64) error {
	n, err := tx.Exec(`UPDATE pdp_piecerefs SET needs_ipni = FALSE, ipni_task_id = NULL
									WHERE id = $1 AND ipni_task_id = $2`, id, taskID)
	if err != nil {
		return xerrors.Errorf("store indexing success: updating pipeline: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("store indexing success: updated %d rows", n)
	}
	return nil
}

func (P *PDPV0IPNITask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (P *PDPV0IPNITask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPv0_IPNI",
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

func (P *PDPV0IPNITask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	// schedule submits
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var pendings []struct {
				ID int64 `db:"id"`
			}

			err := tx.Select(&pendings, `SELECT id FROM pdp_piecerefs
												WHERE ipni_task_id IS NULL
												AND needs_ipni = TRUE
												ORDER BY created_at ASC LIMIT 1;`)
			if err != nil {
				return false, xerrors.Errorf("getting PDP pending indexing tasks: %w", err)
			}

			if len(pendings) == 0 {
				ilog.Debug("No pending PDP IPNI tasks found")
				return false, nil
			}

			// Setup PDP IPNI private key if this is our first IPNI task
			if _, err := PDPInitProvider(tx); err != nil {
				return false, xerrors.Errorf("initializing PDP IPNI provider: %w", err)
			}

			pending := pendings[0]
			n, err := tx.Exec(`UPDATE pdp_piecerefs SET ipni_task_id = $1
						 WHERE ipni_task_id IS NULL AND id = $2`, id, pending.ID)
			if err != nil {
				return false, xerrors.Errorf("updating PDP ipni task id: %w", err)
			}
			if n == 0 {
				return false, nil // Another task already claimed this piece
			}

			ilog.Debugf("PDP IPNI task scheduled for pending IPNI task %d", pending.ID)

			stop = false
			return true, nil
		})
	}

	return nil
}

func (P *PDPV0IPNITask) Adder(taskFunc harmonytask.AddTaskFunc) {}

// The ipni provider key for pdp is at PDP_v0_SP_ID
func PDPInitProvider(tx *harmonydb.Tx) (peer.ID, error) {
	var peerID string
	err := tx.QueryRow(`SELECT peer_id FROM ipni_peerid WHERE sp_id = $1`, PDP_v0_SP_ID).Scan(&peerID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", xerrors.Errorf("failed to get private libp2p key: %w", err)
		}

		// generate the ipni provider key
		pk, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return "", xerrors.Errorf("failed to generate a new key: %w", err)
		}

		privKey, err := crypto.MarshalPrivateKey(pk)
		if err != nil {
			return "", xerrors.Errorf("failed to marshal the private key: %w", err)
		}

		pid, err := peer.IDFromPublicKey(pk.GetPublic())
		if err != nil {
			return "", xerrors.Errorf("getting peer ID: %w", err)
		}
		peerID = pid.String()

		n, err := tx.Exec(`INSERT INTO ipni_peerid (priv_key, peer_id, sp_id) VALUES ($1, $2, $3)`, privKey, peerID, PDP_v0_SP_ID)
		if err != nil {
			return "", xerrors.Errorf("failed to insert the key into DB: %w", err)
		}

		if n == 0 {
			return "", xerrors.Errorf("failed to insert the key into db")
		}
	}
	pid, err := peer.Decode(peerID)
	if err != nil {
		return "", xerrors.Errorf("decoding peer ID: %w", err)
	}
	return pid, nil
}

var _ harmonytask.TaskInterface = &PDPV0IPNITask{}
var _ = harmonytask.Reg(&PDPV0IPNITask{})

// PdpIpniContext is used to generate the context bytes for PDP IPNI ads
type PdpIpniContext struct {
	// PieceCID is piece CID V2
	PieceCID cid.Cid

	// Payload determines if the IPNI ad is TransportFilecoinPieceHttp or TransportIpfsGatewayHttp
	Payload bool
}

// Marshal encodes the PdpIpniContext into a byte slice containing a single byte for Payload and the byte representation of PieceCID.
func (p *PdpIpniContext) Marshal() ([]byte, error) {
	pBytes := p.PieceCID.Bytes()
	if len(pBytes) > 63 {
		return nil, xerrors.Errorf("piece CID byte length exceeds 63")
	}
	payloadByte := make([]byte, 1)
	if p.Payload {
		payloadByte[0] = 1
	} else {
		payloadByte[0] = 0
	}
	return append(payloadByte, pBytes...), nil
}

// Unmarshal decodes the provided byte slice into the PdpIpniContext struct, validating its length and extracting the PieceCID and Payload values.
func (p *PdpIpniContext) Unmarshal(b []byte) error {
	if len(b) > 64 {
		return xerrors.Errorf("byte length exceeds 64")
	}
	if len(b) < 2 {
		return xerrors.Errorf("byte length is less than 2")
	}
	payload := b[0] == 1
	pcid, err := cid.Cast(b[1:])
	if err != nil {
		return err
	}

	p.PieceCID = pcid
	p.Payload = payload

	return nil
}
