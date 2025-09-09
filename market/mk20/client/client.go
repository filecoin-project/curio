package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/market/mk20"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/wallet"
)

var log = logging.Logger("mk20-client")

type Client struct {
	http *HTTPClient
}

func NewClient(baseURL string, client address.Address, wallet *wallet.LocalWallet) *Client {
	s := NewAuth(client, wallet)
	hclient := NewHTTPClient(baseURL, HourlyCurioAuthHeader(s))
	return &Client{
		http: hclient,
	}
}

func (c *Client) CreateDataSet(ctx context.Context, client, recordKeeper string, extraData []byte) (ulid.ULID, error) {
	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		return ulid.ULID{}, xerrors.Errorf("failed to create ULID: %w", err)
	}

	deal := &mk20.Deal{
		Identifier: id,
		Client:     client,
		Products: mk20.Products{
			PDPV1: &mk20.PDPV1{
				CreateDataSet: true,
				RecordKeeper:  recordKeeper,
				ExtraData:     extraData,
			},
		},
	}

	rerr := c.http.Store(ctx, deal)
	if rerr.Error != nil {
		return ulid.ULID{}, rerr.Error
	}
	if rerr.Status != 200 {
		return ulid.ULID{}, rerr.HError()
	}
	return id, nil
}

func (c *Client) RemoveDataSet(ctx context.Context, client, recordKeeper string, extraData []byte, dataSetID *uint64) (ulid.ULID, error) {
	if dataSetID == nil {
		return ulid.ULID{}, nil
	}

	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		return ulid.ULID{}, xerrors.Errorf("failed to create ULID: %w", err)
	}

	deal := &mk20.Deal{
		Identifier: id,
		Client:     client,
		Products: mk20.Products{
			PDPV1: &mk20.PDPV1{
				DeleteDataSet: true,
				DataSetID:     dataSetID,
				RecordKeeper:  recordKeeper,
				ExtraData:     extraData,
			},
		},
	}

	rerr := c.http.Store(ctx, deal)
	if rerr.Error != nil {
		return ulid.ULID{}, rerr.Error
	}
	if rerr.Status != 200 {
		return ulid.ULID{}, rerr.HError()
	}
	return id, nil
}

func (c *Client) addPiece(ctx context.Context, client, recordKeeper string, extraData []byte, dataSetID *uint64, dataSource *mk20.DataSource, ret *mk20.RetrievalV1) (ulid.ULID, error) {
	if dataSetID == nil {
		return ulid.ULID{}, nil
	}

	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		return ulid.ULID{}, xerrors.Errorf("failed to create ULID: %w", err)
	}

	deal := &mk20.Deal{
		Identifier: id,
		Client:     client,
		Data:       dataSource,
		Products: mk20.Products{
			PDPV1: &mk20.PDPV1{
				AddPiece:     true,
				DataSetID:    dataSetID,
				RecordKeeper: recordKeeper,
				ExtraData:    extraData,
			},
			RetrievalV1: ret,
		},
	}

	rerr := c.http.Store(ctx, deal)
	if rerr.Error != nil {
		return ulid.ULID{}, rerr.Error
	}
	if rerr.Status != 200 {
		return ulid.ULID{}, rerr.HError()
	}
	return id, nil
}

func (c *Client) RemovePiece(ctx context.Context, client, recordKeeper string, extraData []byte, dataSetID *uint64, pieceIDs []uint64) (ulid.ULID, error) {
	if dataSetID == nil {
		return ulid.ULID{}, xerrors.Errorf("dataSetID is required")
	}

	if len(pieceIDs) == 0 {
		return ulid.ULID{}, xerrors.Errorf("at least one pieceID is required")
	}

	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		return ulid.ULID{}, xerrors.Errorf("failed to create ULID: %w", err)
	}

	deal := &mk20.Deal{
		Identifier: id,
		Client:     client,
		Products: mk20.Products{
			PDPV1: &mk20.PDPV1{
				DeletePiece:  true,
				DataSetID:    dataSetID,
				RecordKeeper: recordKeeper,
				ExtraData:    extraData,
				PieceIDs:     pieceIDs,
			},
		},
	}

	rerr := c.http.Store(ctx, deal)
	if rerr.Error != nil {
		return ulid.ULID{}, rerr.Error
	}
	if rerr.Status != 200 {
		return ulid.ULID{}, rerr.HError()
	}
	return id, nil
}

func (c *Client) CreateDataSource(pieceCID cid.Cid, car, raw, aggregate, index, withCDN bool, aggregateType mk20.AggregateType, sub []mk20.DataSource) (*mk20.Deal, error) {
	if car && raw && aggregate || car && raw || car && aggregate || raw && aggregate {
		return nil, xerrors.Errorf("only one data format is supported")
	}

	if !car && (index || withCDN) {
		return nil, xerrors.Errorf("only car data format supports IPFS style CDN retrievals")
	}

	err := mk20.ValidatePieceCID(pieceCID)
	if err != nil {
		return nil, err
	}

	dataSource := &mk20.DataSource{
		PieceCID: pieceCID,
	}

	if car {
		dataSource.Format.Car = &mk20.FormatCar{}
	}

	if raw {
		dataSource.Format.Raw = &mk20.FormatBytes{}
	}

	if aggregate {
		if len(sub) <= 1 {
			return nil, xerrors.Errorf("must provide at least two sub data source")
		}

		if aggregateType == mk20.AggregateTypeNone {
			return nil, xerrors.Errorf("must provide valid aggregateType")
		}

		dataSource.Format.Aggregate = &mk20.FormatAggregate{
			Type: aggregateType,
			Sub:  sub,
		}
	}

	ret := &mk20.Deal{
		Data: dataSource,
		Products: mk20.Products{
			RetrievalV1: &mk20.RetrievalV1{
				Indexing:        index,
				AnnouncePiece:   true,
				AnnouncePayload: withCDN,
			},
		},
	}

	return ret, nil
}

func (c *Client) AddPieceWithHTTP(ctx context.Context, client, recordKeeper string, extraData []byte, dataSetID *uint64, pieceCID cid.Cid, car, raw, index, withCDN bool, aggregateType mk20.AggregateType, sub []mk20.DataSource, urls []mk20.HttpUrl) (ulid.ULID, error) {
	var aggregate bool

	if aggregateType == mk20.AggregateTypeV1 {
		aggregate = true
	}

	d, err := c.CreateDataSource(pieceCID, car, raw, aggregate, index, withCDN, aggregateType, sub)
	if err != nil {
		return ulid.ULID{}, xerrors.Errorf("failed to create data source: %w", err)
	}

	d.Data.SourceHTTP = &mk20.DataSourceHTTP{
		URLs: urls,
	}

	return c.addPiece(ctx, client, recordKeeper, extraData, dataSetID, d.Data, d.Products.RetrievalV1)
}

func (c *Client) AddPieceWithAggregate(ctx context.Context, client, recordKeeper string, extraData []byte, dataSetID *uint64, pieceCID cid.Cid, index, withCDN bool, aggregateType mk20.AggregateType, sub []mk20.DataSource) (ulid.ULID, error) {
	d, err := c.CreateDataSource(pieceCID, false, false, true, index, withCDN, aggregateType, sub)
	if err != nil {
		return ulid.ULID{}, xerrors.Errorf("failed to create data source: %w", err)
	}

	d.Data.SourceAggregate = &mk20.DataSourceAggregate{
		Pieces: sub,
	}

	d.Data.Format.Aggregate.Sub = nil

	return c.addPiece(ctx, client, recordKeeper, extraData, dataSetID, d.Data, d.Products.RetrievalV1)
}

func (c *Client) AddPieceWithPut(ctx context.Context, client, recordKeeper string, extraData []byte, dataSetID *uint64, pieceCID cid.Cid, car, raw, index, withCDN bool, aggregateType mk20.AggregateType, sub []mk20.DataSource) (ulid.ULID, error) {
	var aggregate bool

	if aggregateType == mk20.AggregateTypeV1 {
		aggregate = true
	}

	d, err := c.CreateDataSource(pieceCID, car, raw, aggregate, index, withCDN, aggregateType, sub)
	if err != nil {
		return ulid.ULID{}, xerrors.Errorf("failed to create data source: %w", err)
	}

	d.Data.SourceHttpPut = &mk20.DataSourceHttpPut{}

	return c.addPiece(ctx, client, recordKeeper, extraData, dataSetID, d.Data, d.Products.RetrievalV1)
}

func (c *Client) AddPieceWithPutStreaming(ctx context.Context, client, recordKeeper string, extraData []byte, dataSetID *uint64, car, raw, aggregate, index, withCDN bool) (ulid.ULID, error) {
	if car && raw && aggregate || car && raw || car && aggregate || raw && aggregate {
		return ulid.ULID{}, xerrors.Errorf("only one data format is supported")
	}

	if !car && (index || withCDN) {
		return ulid.ULID{}, xerrors.Errorf("only car data format supports IPFS style CDN retrievals")
	}

	ret := &mk20.RetrievalV1{
		Indexing:        index,
		AnnouncePiece:   true,
		AnnouncePayload: withCDN,
	}

	return c.addPiece(ctx, client, recordKeeper, extraData, dataSetID, nil, ret)
}

func (c *Client) DealStatus(ctx context.Context, dealID string) (*mk20.DealProductStatusResponse, error) {
	id, err := ulid.Parse(dealID)
	if err != nil {
		return nil, xerrors.Errorf("parsing deal id: %w", err)
	}

	status, rerr := c.http.Status(ctx, id)
	if rerr.Error != nil {
		return nil, rerr.Error
	}
	if rerr.Status != 200 {
		return nil, rerr.HError()
	}

	return status, nil
}

func (c *Client) DealUpdate(ctx context.Context, dealID string, deal *mk20.Deal) error {
	id, err := ulid.Parse(dealID)
	if err != nil {
		return xerrors.Errorf("parsing deal id: %w", err)
	}
	rerr := c.http.Update(ctx, id, deal)
	if rerr.Error != nil {
		return rerr.Error
	}
	if rerr.Status != 200 {
		return rerr.HError()
	}
	return nil
}

func (c *Client) DealUploadSerial(ctx context.Context, dealID string, r io.Reader) error {
	id, err := ulid.Parse(dealID)
	if err != nil {
		return xerrors.Errorf("parsing deal id: %w", err)
	}
	rerr := c.http.UploadSerial(ctx, id, r)
	if rerr.Error != nil {
		return rerr.Error
	}
	if rerr.Status != 200 {
		return rerr.HError()
	}
	return nil
}

func (c *Client) DealUploadSerialFinalize(ctx context.Context, dealID string, deal *mk20.Deal) error {
	id, err := ulid.Parse(dealID)
	if err != nil {
		return xerrors.Errorf("parsing deal id: %w", err)
	}
	rerr := c.http.UploadSerialFinalize(ctx, id, deal)
	if rerr.Error != nil {
		return rerr.Error
	}
	if rerr.Status != 200 {
		return rerr.HError()
	}
	return nil
}

func (c *Client) DealChunkUploadInit(ctx context.Context, dealID string, fileSize, chunkSize int64) error {
	id, err := ulid.Parse(dealID)
	if err != nil {
		return xerrors.Errorf("parsing deal id: %w", err)
	}
	metadata := &mk20.StartUpload{
		RawSize:   uint64(fileSize),
		ChunkSize: chunkSize,
	}
	rerr := c.http.UploadInit(ctx, id, metadata)
	if rerr.Error != nil {
		return rerr.Error
	}
	if rerr.Status != 200 {
		return rerr.HError()
	}
	return nil
}

func (c *Client) DealChunkUpload(ctx context.Context, dealID string, chunk int, r io.Reader) error {
	id, err := ulid.Parse(dealID)
	if err != nil {
		return xerrors.Errorf("parsing deal id: %w", err)
	}
	rerr := c.http.UploadChunk(ctx, id, chunk, r)
	if rerr.Error != nil {
		return rerr.Error
	}
	if rerr.Status != 200 {
		return rerr.HError()
	}
	return nil
}

func (c *Client) DealChunkUploadFinalize(ctx context.Context, dealID string, deal *mk20.Deal) error {
	id, err := ulid.Parse(dealID)
	if err != nil {
		return xerrors.Errorf("parsing deal id: %w", err)
	}
	rerr := c.http.UploadSerialFinalize(ctx, id, deal)
	if rerr.Error != nil {
		return rerr.Error
	}
	if rerr.Status != 200 {
		return rerr.HError()
	}
	return nil
}

func (c *Client) DealChunkedUpload(ctx context.Context, dealID string, size, chunkSize int64, r io.ReaderAt) error {
	id, err := ulid.Parse(dealID)
	if err != nil {
		return xerrors.Errorf("parsing deal id: %w", err)
	}
	metadata := &mk20.StartUpload{
		RawSize:   uint64(size),
		ChunkSize: chunkSize,
	}

	_, rerr := c.http.UploadStatus(ctx, id)
	if rerr.Error != nil {
		return rerr.Error
	}

	if rerr.Status != 200 && rerr.Status != int(mk20.UploadStatusCodeUploadNotStarted) {
		return rerr.HError()
	}

	if rerr.Status == int(mk20.UploadStatusCodeUploadNotStarted) {
		// Start the upload
		rerr = c.http.UploadInit(ctx, id, metadata)
		if rerr.Error != nil {
			return rerr.Error
		}
		if rerr.Status != 200 {
			return rerr.HError()
		}
	}

	numChunks := int((size + chunkSize - 1) / chunkSize)

	for {
		status, rerr := c.http.UploadStatus(ctx, id)
		if rerr.Error != nil {
			return rerr.Error
		}
		if rerr.Status != 200 {
			return rerr.HError()
		}

		log.Debugw("upload status", "status", status)

		if status.TotalChunks != numChunks {
			return xerrors.Errorf("expected %d chunks, got %d", numChunks, status.TotalChunks)
		}

		if status.Missing == 0 {
			break
		}

		log.Warnw("missing chunks", "missing", status.Missing)
		// Try to upload missing chunks
		for _, chunk := range status.MissingChunks {
			start := int64(chunk-1) * chunkSize
			end := start + chunkSize
			if end > size {
				end = size
			}
			log.Debugw("uploading chunk", "start", start, "end", end)
			buf := make([]byte, end-start)
			_, err := r.ReadAt(buf, start)
			if err != nil {
				return xerrors.Errorf("failed to read chunk: %w", err)
			}

			rerr = c.http.UploadChunk(ctx, id, chunk, bytes.NewReader(buf))
			if rerr.Error != nil {
				return rerr.Error
			}
			if rerr.Status != 200 {
				return rerr.HError()
			}
		}
	}

	log.Infow("upload complete")

	rerr = c.http.UploadFinalize(ctx, id, nil)
	if rerr.Error != nil {
		return rerr.Error
	}
	if rerr.Status != 200 {
		return rerr.HError()
	}
	return nil
}

func KeyFromClientAddress(clientAddress address.Address) (key string) {
	switch clientAddress.Protocol() {
	case address.BLS:
		return "bls"
	case address.SECP256K1:
		return "secp256k1"
	case address.Delegated:
		return "delegated"
	default:
		return ""
	}
}

type ClientAuth struct {
	client address.Address
	wallet *wallet.LocalWallet
}

func (c *ClientAuth) Sign(digest []byte) ([]byte, error) {
	sign, err := c.wallet.WalletSign(context.Background(), c.client, digest, lapi.MsgMeta{Type: lapi.MTDealProposal})
	if err != nil {
		return nil, err
	}

	return sign.MarshalBinary()
}

func (c *ClientAuth) PublicKeyBytes() []byte {
	return c.client.Bytes()
}

func (c *ClientAuth) Type() string {
	return KeyFromClientAddress(c.client)
}

var _ Signer = &ClientAuth{}

func NewAuth(client address.Address, wallet *wallet.LocalWallet) Signer {
	return &ClientAuth{
		client: client,
		wallet: wallet,
	}
}
