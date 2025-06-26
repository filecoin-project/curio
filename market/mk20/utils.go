package mk20

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/commcidv2"

	"github.com/filecoin-project/lotus/lib/sigs"
)

func (d *Deal) Validate(db *harmonydb.DB, cfg *config.MK20Config) (ErrorCode, error) {
	if d.Client.Empty() {
		return ErrBadProposal, xerrors.Errorf("no client")
	}

	code, err := d.ValidateSignature()
	if err != nil {
		return code, xerrors.Errorf("signature validation failed: %w", err)
	}

	code, err = d.Products.Validate(db, cfg)
	if err != nil {
		return code, xerrors.Errorf("products validation failed: %w", err)
	}

	return d.Data.Validate(db)
}

func (d *Deal) ValidateSignature() (ErrorCode, error) {
	if len(d.Signature) == 0 {
		return ErrBadProposal, xerrors.Errorf("no signature")
	}

	sig := &crypto.Signature{}
	err := sig.UnmarshalBinary(d.Signature)
	if err != nil {
		return ErrBadProposal, xerrors.Errorf("invalid signature")
	}

	msg, err := d.Identifier.MarshalBinary()
	if err != nil {
		return ErrBadProposal, xerrors.Errorf("invalid identifier")
	}

	if sig.Type == crypto.SigTypeBLS || sig.Type == crypto.SigTypeSecp256k1 || sig.Type == crypto.SigTypeDelegated {
		err = sigs.Verify(sig, d.Client, msg)
		if err != nil {
			return ErrBadProposal, xerrors.Errorf("invalid signature")
		}
		return Ok, nil
	}

	// Add more types if required in Future
	return ErrBadProposal, xerrors.Errorf("invalid signature type")
}

func (d DataSource) Validate(db *harmonydb.DB) (ErrorCode, error) {

	err := ValidatePieceCID(d.PieceCID)
	if err != nil {
		return ErrBadProposal, err
	}

	if d.SourceOffline != nil && d.SourceHTTP != nil && d.SourceAggregate != nil && d.SourceHttpPut != nil {
		return ErrBadProposal, xerrors.Errorf("multiple sources defined for data source")
	}

	if d.SourceOffline == nil && d.SourceHTTP == nil && d.SourceAggregate == nil && d.SourceHttpPut == nil {
		return ErrBadProposal, xerrors.Errorf("no source defined for data source")
	}

	var fcar, fagg, fraw bool

	if d.Format.Car != nil {
		fcar = true
	}

	if d.Format.Aggregate != nil {
		fagg = true

		if d.Format.Aggregate.Type != AggregateTypeV1 {
			return ErrMalformedDataSource, xerrors.Errorf("aggregate type not supported")
		}

		if d.SourceAggregate != nil {
			code, err := IsDataSourceEnabled(db, d.SourceAggregate.Name())
			if err != nil {
				return code, err
			}

			if len(d.SourceAggregate.Pieces) == 0 {
				return ErrMalformedDataSource, xerrors.Errorf("no pieces in aggregate")
			}

			if len(d.SourceAggregate.Pieces) == 1 {
				return ErrMalformedDataSource, xerrors.Errorf("aggregate must have at least 2 pieces")
			}

			for _, p := range d.SourceAggregate.Pieces {
				err := ValidatePieceCID(p.PieceCID)
				if err != nil {
					return ErrMalformedDataSource, xerrors.Errorf("invalid piece cid")
				}

				var ifcar, ifraw bool

				if p.Format.Car != nil {
					ifcar = true
				}

				if p.Format.Aggregate != nil {
					return ErrMalformedDataSource, xerrors.Errorf("aggregate of aggregate is not supported")
				}

				if p.Format.Raw != nil {
					ifraw = true
				}

				if !ifcar && !ifraw {
					return ErrMalformedDataSource, xerrors.Errorf("no format defined for sub piece in aggregate")
				}

				if ifcar && ifraw {
					return ErrMalformedDataSource, xerrors.Errorf("multiple formats defined for sub piece in aggregate")
				}

				if p.SourceAggregate != nil {
					return ErrMalformedDataSource, xerrors.Errorf("aggregate of aggregate is not supported")
				}

				if p.SourceOffline == nil && p.SourceHTTP == nil {
					return ErrMalformedDataSource, xerrors.Errorf("no source defined for sub piece in aggregate")
				}

				if p.SourceOffline != nil && p.SourceHTTP != nil {
					return ErrMalformedDataSource, xerrors.Errorf("multiple sources defined for sub piece in aggregate")
				}

				if p.SourceHTTP != nil {
					if len(p.SourceHTTP.URLs) == 0 {
						return ErrMalformedDataSource, xerrors.Errorf("no urls defined for sub piece in aggregate")
					}

					for _, u := range p.SourceHTTP.URLs {
						_, err := url.Parse(u.URL)
						if err != nil {
							return ErrMalformedDataSource, xerrors.Errorf("invalid url")
						}
					}
				}
			}
		} else {
			if len(d.Format.Aggregate.Sub) == 0 {
				return ErrMalformedDataSource, xerrors.Errorf("no sub pieces defined under aggregate")
			}
		}
	}

	if d.Format.Raw != nil {
		fraw = true
	}

	if !fcar && !fagg && !fraw {
		return ErrBadProposal, xerrors.Errorf("no format defined")
	}

	if fcar && fagg || fcar && fraw || fagg && fraw {
		return ErrBadProposal, xerrors.Errorf("multiple formats defined")
	}

	if d.SourceHTTP != nil {
		code, err := IsDataSourceEnabled(db, d.SourceHTTP.Name())
		if err != nil {
			return code, err
		}

		if len(d.SourceHTTP.URLs) == 0 {
			return ErrMalformedDataSource, xerrors.Errorf("no urls defined")
		}

		for _, u := range d.SourceHTTP.URLs {
			_, err := url.Parse(u.URL)
			if err != nil {
				return ErrMalformedDataSource, xerrors.Errorf("invalid url")
			}
		}
	}

	if d.SourceOffline != nil {
		code, err := IsDataSourceEnabled(db, d.SourceOffline.Name())
		if err != nil {
			return code, err
		}
	}

	if d.SourceHttpPut != nil {
		code, err := IsDataSourceEnabled(db, d.SourceHttpPut.Name())
		if err != nil {
			return code, err
		}
	}

	return Ok, nil
}

func ValidatePieceCID(c cid.Cid) error {
	if !c.Defined() {
		return xerrors.Errorf("piece cid is not defined")
	}

	if c.Prefix().Codec != cid.Raw {
		return xerrors.Errorf("piece cid is not raw")
	}

	commp, err := commcidv2.CommPFromPCidV2(c)
	if err != nil {
		return xerrors.Errorf("invalid piece cid: %w", err)
	}

	if commp.PieceInfo().Size == 0 {
		return xerrors.Errorf("piece size is 0")
	}

	if commp.PayloadSize() == 0 {
		return xerrors.Errorf("payload size is 0")
	}

	if padreader.PaddedSize(commp.PayloadSize()).Padded() != commp.PieceInfo().Size {
		return xerrors.Errorf("invalid piece size")
	}

	return nil
}

type PieceInfo struct {
	PieceCIDV1 cid.Cid             `json:"piece_cid"`
	Size       abi.PaddedPieceSize `json:"size"`
	RawSize    uint64              `json:"raw_size"`
}

func (d *Deal) RawSize() (uint64, error) {
	commp, err := commcidv2.CommPFromPCidV2(d.Data.PieceCID)
	if err != nil {
		return 0, xerrors.Errorf("invalid piece cid: %w", err)
	}
	return commp.PayloadSize(), nil
}

func (d *Deal) Size() (abi.PaddedPieceSize, error) {
	commp, err := commcidv2.CommPFromPCidV2(d.Data.PieceCID)
	if err != nil {
		return 0, xerrors.Errorf("invalid piece cid: %w", err)
	}
	return commp.PieceInfo().Size, nil
}

func (d *Deal) PieceInfo() (*PieceInfo, error) {
	return GetPieceInfo(d.Data.PieceCID)
}

func GetPieceInfo(c cid.Cid) (*PieceInfo, error) {
	commp, err := commcidv2.CommPFromPCidV2(c)
	if err != nil {
		return nil, xerrors.Errorf("invalid piece cid: %w", err)
	}
	return &PieceInfo{
		PieceCIDV1: commp.PCidV1(),
		Size:       commp.PieceInfo().Size,
		RawSize:    commp.PayloadSize(),
	}, nil
}

func (d Products) Validate(db *harmonydb.DB, cfg *config.MK20Config) (ErrorCode, error) {
	if d.DDOV1 != nil {
		code, err := d.DDOV1.Validate(db, cfg)
		if err != nil {
			return code, err
		}
	}
	if d.RetrievalV1 != nil {
		code, err := d.RetrievalV1.Validate(db, cfg)
		if err != nil {
			return code, err
		}
	}
	if d.PDPV1 != nil {
		code, err := d.PDPV1.Validate(db, cfg)
		if err != nil {
			return code, err
		}
	}
	return Ok, nil
}

type DBDDOV1 struct {
	DDO      *DDOV1         `json:"ddo"`
	DealID   string         `json:"deal_id"`
	Complete bool           `json:"complete"`
	Error    sql.NullString `json:"error"`
}

type DBPDPV1 struct {
	PDP      *PDPV1         `json:"pdp"`
	Complete bool           `json:"complete"`
	Error    sql.NullString `json:"error"`
}

type DBDeal struct {
	Identifier  string          `db:"id"`
	Client      string          `db:"client"`
	PieceCIDV2  sql.NullString  `db:"piece_cid_v2"`
	PieceCID    sql.NullString  `db:"piece_cid"`
	Size        sql.NullInt64   `db:"piece_size"`
	RawSize     sql.NullInt64   `db:"raw_size"`
	Data        json.RawMessage `db:"data"`
	DDOv1       json.RawMessage `db:"ddo_v1"`
	RetrievalV1 json.RawMessage `db:"retrieval_v1"`
	PDPV1       json.RawMessage `db:"pdp_v1"`
}

func (d *Deal) ToDBDeal() (*DBDeal, error) {
	ddeal := DBDeal{
		Identifier: d.Identifier.String(),
		Client:     d.Client.String(),
	}

	if d.Data != nil {
		dataBytes, err := json.Marshal(d.Data)
		if err != nil {
			return nil, fmt.Errorf("marshal data: %w", err)
		}
		commp, err := commcidv2.CommPFromPCidV2(d.Data.PieceCID)
		if err != nil {
			return nil, fmt.Errorf("invalid piece cid: %w", err)
		}
		ddeal.PieceCIDV2.String = d.Data.PieceCID.String()
		ddeal.PieceCIDV2.Valid = true
		ddeal.PieceCID.String = commp.PCidV1().String()
		ddeal.PieceCID.Valid = true
		ddeal.Size.Int64 = int64(commp.PieceInfo().Size)
		ddeal.Size.Valid = true
		ddeal.RawSize.Int64 = int64(commp.PayloadSize())
		ddeal.RawSize.Valid = true
		ddeal.Data = dataBytes
	} else {
		ddeal.Data = []byte("null")
	}

	if d.Products.DDOV1 != nil {
		dddov1 := DBDDOV1{
			DDO: d.Products.DDOV1,
		}
		ddov1, err := json.Marshal(dddov1)
		if err != nil {
			return nil, fmt.Errorf("marshal ddov1: %w", err)
		}
		ddeal.DDOv1 = ddov1
	} else {
		ddeal.DDOv1 = []byte("null")
	}

	if d.Products.RetrievalV1 != nil {
		rev, err := json.Marshal(d.Products.RetrievalV1)
		if err != nil {
			return nil, fmt.Errorf("marshal retrievalv1: %w", err)
		}
		ddeal.RetrievalV1 = rev
	} else {
		ddeal.RetrievalV1 = []byte("null")
	}

	if d.Products.PDPV1 != nil {
		dbpdpv1 := DBPDPV1{
			PDP: d.Products.PDPV1,
		}
		pdpv1, err := json.Marshal(dbpdpv1)
		if err != nil {
			return nil, fmt.Errorf("marshal pdpv1: %w", err)
		}
		ddeal.PDPV1 = pdpv1
	} else {
		ddeal.PDPV1 = []byte("null")
	}

	return &ddeal, nil
}

func (d *Deal) SaveToDB(tx *harmonydb.Tx) error {
	dbDeal, err := d.ToDBDeal()
	if err != nil {
		return xerrors.Errorf("to db deal: %w", err)
	}

	n, err := tx.Exec(`INSERT INTO market_mk20_deal (id, client, piece_cid_v2, piece_cid, piece_size, raw_size, data, ddo_v1, retrieval_v1, pdp_v1) 
                  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		dbDeal.Identifier,
		dbDeal.Client,
		dbDeal.PieceCIDV2,
		dbDeal.PieceCID,
		dbDeal.Size,
		dbDeal.RawSize,
		dbDeal.Data,
		dbDeal.DDOv1,
		dbDeal.RetrievalV1,
		dbDeal.PDPV1)
	if err != nil {
		return xerrors.Errorf("insert deal: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("insert deal: expected 1 row affected, got %d", n)
	}
	return nil
}

func (d *Deal) UpdateDeal(tx *harmonydb.Tx) error {
	dbDeal, err := d.ToDBDeal()
	if err != nil {
		return xerrors.Errorf("to db deal: %w", err)
	}

	n, err := tx.Exec(`UPDATE market_mk20_deal SET 
                            piece_cid_v2 = $1, 
                            piece_cid = $2, 
                            piece_size = $3, 
                            raw_size = $4, 
                            data = $5, 
                            ddo_v1 = $6,
                            retrieval_v1 = $7,
                            pdp_v1 = $8`, dbDeal.PieceCIDV2, dbDeal.PieceCID, dbDeal.Size, dbDeal.RawSize,
		dbDeal.Data, dbDeal.DDOv1, dbDeal.RetrievalV1, dbDeal.PDPV1)
	if err != nil {
		return xerrors.Errorf("insert deal: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("insert deal: expected 1 row affected, got %d", n)
	}
	return nil
}

func DealFromTX(tx *harmonydb.Tx, id ulid.ULID) (*Deal, error) {
	var dbDeal []DBDeal
	err := tx.Select(&dbDeal, `SELECT 
    								id,
									client,
									data, 
									ddo_v1,
									retrieval_v1,
									pdp_v1 FROM market_mk20_deal WHERE id = $1`, id.String())
	if err != nil {
		return nil, xerrors.Errorf("getting deal from DB: %w", err)
	}
	if len(dbDeal) != 1 {
		return nil, xerrors.Errorf("expected 1 deal, got %d", len(dbDeal))
	}
	return dbDeal[0].ToDeal()
}

func DealFromDB(ctx context.Context, db *harmonydb.DB, id ulid.ULID) (*Deal, error) {
	var dbDeal []DBDeal
	err := db.Select(ctx, &dbDeal, `SELECT 
										id,
										client,
										data, 
										ddo_v1,
										retrieval_v1,
										pdp_v1 FROM market_mk20_deal WHERE id = $1`, id.String())
	if err != nil {
		return nil, xerrors.Errorf("getting deal from DB: %w", err)
	}
	if len(dbDeal) != 1 {
		return nil, xerrors.Errorf("expected 1 deal, got %d", len(dbDeal))
	}
	return dbDeal[0].ToDeal()
}

func (d *DBDeal) ToDeal() (*Deal, error) {
	var deal Deal

	if len(d.Data) > 0 && string(d.Data) != "null" {
		var ds DataSource
		if err := json.Unmarshal(d.Data, &ds); err != nil {
			return nil, fmt.Errorf("unmarshal data: %w", err)
		}
		deal.Data = &ds
	}

	if len(d.DDOv1) > 0 && string(d.DDOv1) != "null" {
		var dddov1 DBDDOV1
		if err := json.Unmarshal(d.DDOv1, &dddov1); err != nil {
			return nil, fmt.Errorf("unmarshal ddov1: %w", err)
		}
		deal.Products.DDOV1 = dddov1.DDO
	}

	if len(d.RetrievalV1) > 0 && string(d.RetrievalV1) != "null" {
		var rev RetrievalV1
		if err := json.Unmarshal(d.RetrievalV1, &rev); err != nil {
			return nil, fmt.Errorf("unmarshal retrievalv1: %w", err)
		}
		deal.Products.RetrievalV1 = &rev
	}

	if len(d.PDPV1) > 0 && string(d.PDPV1) != "null" {
		var dddov1 DBPDPV1
		if err := json.Unmarshal(d.PDPV1, &dddov1); err != nil {
			return nil, fmt.Errorf("unmarshal pdpv1: %w", err)
		}
		deal.Products.PDPV1 = dddov1.PDP
	}

	id, err := ulid.Parse(d.Identifier)
	if err != nil {
		return nil, fmt.Errorf("parse id: %w", err)
	}
	deal.Identifier = id

	client, err := address.NewFromString(d.Client)
	if err != nil {
		return nil, fmt.Errorf("parse client: %w", err)
	}
	deal.Client = client

	return &deal, nil
}

func DBDealsToDeals(deals []*DBDeal) ([]*Deal, error) {
	var result []*Deal
	for _, d := range deals {
		deal, err := d.ToDeal()
		if err != nil {
			return nil, err
		}
		result = append(result, deal)
	}
	return result, nil
}

type ProviderDealRejectionInfo struct {
	HTTPCode int
	Reason   string
}

// DealStatusResponse represents the response of a deal's status, including its current state and an optional error message.
type DealStatusResponse struct {

	// State indicates the current processing state of the deal as a DealState value.
	State DealState `json:"status"`

	// ErrorMsg is an optional field containing error details associated with the deal's current state if an error occurred.
	ErrorMsg string `json:"error_msg"`
}

// DealStatus represents the status of a deal, including the HTTP code and an optional response detailing the deal's state and error message.
type DealStatus struct {

	// Response provides details about the deal's status, such as its current state and any associated error messages, if available.
	Response *DealStatusResponse

	// HTTPCode represents the HTTP status code providing additional context about the deal status or possible errors.
	HTTPCode int
}

// DealState represents the current status of a deal in the system as a string value.
type DealState string

const (

	// DealStateAccepted represents the state where a deal has been accepted and is pending further processing in the system.
	DealStateAccepted DealState = "accepted"

	// DealStateProcessing represents the state of a deal currently being processed in the pipeline.
	DealStateProcessing DealState = "processing"

	// DealStateSealing indicates that the deal is currently being sealed in the system.
	DealStateSealing DealState = "sealing"

	// DealStateIndexing represents the state where a deal is undergoing indexing in the system.
	DealStateIndexing DealState = "indexing"

	// DealStateFailed indicates that the deal has failed due to an error during processing, sealing, or indexing.
	DealStateFailed DealState = "failed"

	// DealStateComplete indicates that the deal has successfully completed all processing and is finalized in the system.
	DealStateComplete DealState = "complete"
)

// SupportedContracts represents a collection of contract addresses supported by a system or application.
type SupportedContracts struct {
	// Contracts represents a list of supported contract addresses in string format.
	Contracts []string `json:"contracts"`
}

func NewULID() (ulid.ULID, error) {
	return ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
}

func (dsh *DataSourceHTTP) Name() DataSourceName {
	return DataSourceNameHTTP
}

func (dso *DataSourceOffline) Name() DataSourceName {
	return DataSourceNameOffline
}

func (dsa *DataSourceAggregate) Name() DataSourceName {
	return DataSourceNameAggregate
}

func (dsh *DataSourceHttpPut) Name() DataSourceName {
	return DataSourceNamePut
}

func IsDataSourceEnabled(db *harmonydb.DB, name DataSourceName) (ErrorCode, error) {
	var enabled bool

	err := db.QueryRow(context.Background(), `SELECT enabled FROM market_mk20_data_source WHERE name = $1`, name).Scan(&enabled)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return http.StatusInternalServerError, xerrors.Errorf("data source %s is not enabled", name)
		}
	}
	if !enabled {
		return ErrUnsupportedDataSource, xerrors.Errorf("data source %s is not enabled", name)
	}
	return Ok, nil
}

func IsProductEnabled(db *harmonydb.DB, name ProductName) (ErrorCode, error) {
	var enabled bool

	err := db.QueryRow(context.Background(), `SELECT enabled FROM market_mk20_products WHERE name = $1`, name).Scan(&enabled)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return http.StatusInternalServerError, xerrors.Errorf("data source %s is not enabled", name)
		}
		return ErrUnsupportedProduct, xerrors.Errorf("product %s is not supported by the provider", name)
	}
	if !enabled {
		return ErrProductNotEnabled, xerrors.Errorf("product %s is not enabled", name)
	}
	return Ok, nil
}

// SupportedProducts represents a collection of products supported by the SP.
type SupportedProducts struct {
	// Contracts represents a list of supported contract addresses in string format.
	Products []string `json:"products"`
}

// SupportedDataSources represents a collection of dats sources supported by the SP.
type SupportedDataSources struct {
	// Contracts represents a list of supported contract addresses in string format.
	Sources []string `json:"sources"`
}

// StartUpload represents metadata for initiating an upload operation, containing the chunk size of the data to be uploaded.
type StartUpload struct {
	ChunkSize int64 `json:"chunk_size"`
}

// UploadStatus represents the status of a file upload process, including progress and missing chunks.
type UploadStatus struct {

	// TotalChunks represents the total number of chunks required for the upload.
	TotalChunks int `json:"total_chunks"`

	// Uploaded represents the number of chunks successfully uploaded.
	Uploaded int `json:"uploaded"`

	// Missing represents the number of chunks that are not yet uploaded.
	Missing int `json:"missing"`

	// UploadedChunks is a slice containing the indices of successfully uploaded chunks.
	UploadedChunks []int `json:"uploaded_chunks"`

	//MissingChunks is a slice containing the indices of missing chunks.
	MissingChunks []int `json:"missing_chunks"`
}
