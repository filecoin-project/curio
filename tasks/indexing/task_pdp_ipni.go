package indexing

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	carv2 "github.com/ipld/go-car/v2"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/maurl"
	"github.com/ipni/go-libipni/metadata"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-varint"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/market/ipni/chunker"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
)

const (
	PDP_SP_ID = -2 // This is the SP ID for PDP in the IPNI table
)

type PDPIPNITask struct {
	db  *harmonydb.DB
	cpr *cachedreader.CachedPieceReader
	cfg *config.CurioConfig
	max taskhelp.Limiter
}

func NewPDPIPNITask(db *harmonydb.DB, sc *ffi.SealCalls, cpr *cachedreader.CachedPieceReader, cfg *config.CurioConfig, max taskhelp.Limiter) *PDPIPNITask {
	return &PDPIPNITask{
		db:  db,
		cpr: cpr,
		cfg: cfg,
		max: max,
	}
}

func (P *PDPIPNITask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var tasks []struct {
		ID       int64  `db:"id"`
		PieceCID string `db:"piece_cid"`
		Size     int64  `db:"piece_padded_size"`
		Prov     string `db:"peer_id"`
	}

	err = P.db.Select(ctx, &tasks, `
									SELECT
										pr.id,
										pr.piece_cid,
										pp.piece_padded_size,
										ipni_peer.peer_id
									FROM
										pdp_piecerefs pr
									JOIN parked_piece_refs ppr ON pr.piece_ref = ppr.ref_id
									JOIN parked_pieces pp ON ppr.piece_id = pp.id
									CROSS JOIN ipni_peerid ipni_peer
									WHERE
										pr.ipni_task_id = $1
										AND ipni_peer.sp_id = $2`, taskID, PDP_SP_ID)
	if err != nil {
		return false, xerrors.Errorf("getting ipni task params: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 ipni task params, got %d", len(tasks))
	}

	task := tasks[0]

	var alreadyPublished bool
	err = P.db.QueryRow(ctx, `SELECT is_rm FROM ipni WHERE piece_cid = $1 AND piece_size = $2 ORDER BY order_number DESC LIMIT 1)`, task.PieceCID, task.Size).Scan(&alreadyPublished)
	if err != nil {
		return false, xerrors.Errorf("checking if piece is already published: %w", err)
	}

	if alreadyPublished {
		log.Infow("IPNI task already published", "task_id", taskID, "piece_cid", task.PieceCID)
		return true, nil
	}

	pcid, err := cid.Parse(task.PieceCID)
	if err != nil {
		return false, xerrors.Errorf("parsing piece CID: %w", err)
	}

	mds := metadata.IpfsGatewayHttp{}
	md, err := mds.MarshalBinary()
	if err != nil {
		return false, xerrors.Errorf("marshaling metadata: %w", err)
	}

	pi := &PdpIpniContext{
		PieceCID: pcid,
		Payload:  true,
	}

	iContext, err := pi.Marshal()
	if err != nil {
		return false, xerrors.Errorf("marshaling piece info: %w", err)
	}

	reader, _, err := P.cpr.GetSharedPieceReader(ctx, pcid)
	if err != nil {
		return false, xerrors.Errorf("getting piece reader: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	opts := []carv2.Option{carv2.ZeroLengthSectionAsEOF(true)}
	blockReader, err := carv2.NewBlockReader(bufio.NewReaderSize(reader, 4<<20), opts...)
	if err != nil {
		return false, fmt.Errorf("getting block reader over piece: %w", err)
	}

	chk := chunker.NewInitialChunker()

	blockMetadata, err := blockReader.SkipNext()
	for err == nil {
		// CAR sections are [varint (length), CID, blockData]
		combinedSize := blockMetadata.Size + uint64(blockMetadata.ByteLen())
		lenSize := uint64(varint.UvarintSize(combinedSize))
		sectionSize := combinedSize + lenSize
		if err := chk.Accept(blockMetadata.Hash(), int64(blockMetadata.SourceOffset), sectionSize); err != nil {
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

	lnk, err := chk.Finish(ctx, P.db, pcid)
	if err != nil {
		return false, xerrors.Errorf("chunking CAR multihash iterator: %w", err)
	}

	// make sure we still own the task before writing ad chains
	if !stillOwned() {
		return false, nil
	}

	_, err = P.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		var prev string
		err = tx.QueryRow(`SELECT head FROM ipni_head WHERE provider = $1`, task.Prov).Scan(&prev)
		if err != nil && err != pgx.ErrNoRows {
			return false, xerrors.Errorf("querying previous head: %w", err)
		}

		var privKey []byte
		err = tx.QueryRow(`SELECT priv_key FROM ipni_peerid WHERE sp_id = $1`, PDP_SP_ID).Scan(&privKey)
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
			u, err := url.Parse(fmt.Sprintf("https://%s:443", P.cfg.HTTP.DomainName))
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

			log.Infow("Announcing piece to IPNI", "piece", pi.PieceCID, "provider", task.Prov, "addr", addr.String(), "task", taskID)

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

		_, err = tx.Exec(`SELECT insert_ad_and_update_head($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			ad.(cidlink.Link).Cid.String(), adv.ContextID, task.PieceCID, task.Size, adv.IsRm, adv.Provider, strings.Join(adv.Addresses, "|"),
			adv.Signature, adv.Entries.String())

		if err != nil {
			return false, xerrors.Errorf("adding advertisement to the database: %w", err)
		}

		err = P.recordCompletion(ctx, taskID, task.ID)
		if err != nil {
			return false, xerrors.Errorf("recording IPNI task completion: %w", err)
		}

		return true, nil

	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("store IPNI success: %w", err)
	}

	log.Infow("IPNI task complete", "task_id", taskID, "piece_cid", task.PieceCID)

	return true, nil
}

func (P *PDPIPNITask) recordCompletion(ctx context.Context, taskID harmonytask.TaskID, id int64) error {
	comm, err := P.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {

		n, err := P.db.Exec(ctx, `UPDATE pdp_piecerefs SET needs_ipni = FALSE, ipni_task_id = NULL
									WHERE id = $1 AND ipni_task_id = $2`, id, taskID)
		if err != nil {
			return false, xerrors.Errorf("store indexing success: updating pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("store indexing success: updated %d rows", n)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("committing transaction: %w", err)
	}
	if !comm {
		return xerrors.Errorf("failed to commit transaction")
	}

	return nil
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
				log.Debug("No pending PDP IPNI tasks found")
				return false, nil
			}

			// Setup PDP IPNI private key if this is our first IPNI task
			if _, err := PDPInitProvider(tx); err != nil {
				return false, xerrors.Errorf("initializing PDP IPNI provider: %w", err)
			}

			pending := pendings[0]
			_, err = tx.Exec(`UPDATE pdp_piecerefs SET ipni_task_id = $1
						 WHERE ipni_task_id IS NULL AND id = $2`, id, pending.ID)
			if err != nil {
				return false, xerrors.Errorf("updating PDP ipni task id: %w", err)
			}

			log.Debugf("PDP IPNI task scheduled for pending IPNI task %d", pending.ID)

			stop = false
			return true, nil
		})
	}

	return nil
}

func (P *PDPIPNITask) Adder(taskFunc harmonytask.AddTaskFunc) {}

// The ipni provider key for pdp is at PDP_SP_ID
func PDPInitProvider(tx *harmonydb.Tx) (peer.ID, error) {
	var peerID string
	err := tx.QueryRow(`SELECT peer_id FROM ipni_peerid WHERE sp_id = $1`, PDP_SP_ID).Scan(&peerID)
	if err != nil {
		if err != pgx.ErrNoRows {
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

		n, err := tx.Exec(`INSERT INTO ipni_peerid (priv_key, peer_id, sp_id) VALUES ($1, $2, $3)`, privKey, peerID, PDP_SP_ID)
		if err != nil {
			return "", xerrors.Errorf("failed to to insert the key into DB: %w", err)
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

var _ harmonytask.TaskInterface = &PDPIPNITask{}
var _ = harmonytask.Reg(&PDPIPNITask{})

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
