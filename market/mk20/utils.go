package mk20

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
	"net/http"
	"net/url"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-data-segment/datasegment"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func (d *Deal) Validate(db *harmonydb.DB) (ErrorCode, error) {
	code, err := d.Products.Validate(db)
	if err != nil {
		return code, xerrors.Errorf("products validation failed: %w", err)
	}

	return d.Data.Validate(db)
}

func (d DataSource) Validate(db *harmonydb.DB) (ErrorCode, error) {

	if !d.PieceCID.Defined() {
		return ErrBadProposal, xerrors.Errorf("piece cid is not defined")
	}

	if d.Size == 0 {
		return ErrBadProposal, xerrors.Errorf("piece size is 0")
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

			for _, p := range d.SourceAggregate.Pieces {
				if !p.PieceCID.Defined() {
					return ErrMalformedDataSource, xerrors.Errorf("piece cid is not defined")
				}

				if p.Size == 0 {
					return ErrMalformedDataSource, xerrors.Errorf("piece size is 0")
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
					if p.SourceHTTP.RawSize == 0 {
						return ErrMalformedDataSource, xerrors.Errorf("raw size is 0 for sub piece in aggregate")
					}

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

				if p.SourceOffline != nil {
					if p.SourceOffline.RawSize == 0 {
						return ErrMalformedDataSource, xerrors.Errorf("raw size is 0 for sub piece in aggregate")
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

		if d.SourceHTTP.RawSize == 0 {
			return ErrMalformedDataSource, xerrors.Errorf("raw size is 0")
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

		if d.SourceOffline.RawSize == 0 {
			return ErrMalformedDataSource, xerrors.Errorf("raw size is 0")
		}
	}

	if d.SourceHttpPut != nil {
		code, err := IsDataSourceEnabled(db, d.SourceHttpPut.Name())
		if err != nil {
			return code, err
		}
		if d.SourceHttpPut.RawSize == 0 {
			return ErrMalformedDataSource, xerrors.Errorf("raw size is 0")
		}
	}

	raw, err := d.RawSize()
	if err != nil {
		return ErrBadProposal, err
	}

	if padreader.PaddedSize(raw).Padded() != d.Size {
		return ErrBadProposal, xerrors.Errorf("invalid size")
	}

	return Ok, nil
}

func (d DataSource) RawSize() (uint64, error) {
	if d.Format.Aggregate != nil {
		if d.Format.Aggregate.Type == AggregateTypeV1 {
			if d.SourceAggregate != nil {
				var pinfos []abi.PieceInfo
				for _, piece := range d.SourceAggregate.Pieces {
					pinfos = append(pinfos, abi.PieceInfo{
						PieceCID: piece.PieceCID,
						Size:     piece.Size,
					})
				}
				_, asize, err := datasegment.ComputeDealPlacement(pinfos)
				if err != nil {
					return 0, err
				}
				next := 1 << (64 - bits.LeadingZeros64(asize+256))
				if abi.PaddedPieceSize(next) != d.Size {
					return 0, xerrors.Errorf("invalid aggregate size")
				}

				a, err := datasegment.NewAggregate(abi.PaddedPieceSize(next), pinfos)
				if err != nil {
					return 0, err
				}

				return uint64(a.DealSize.Unpadded()), nil
			}
		}
	}

	if d.SourceHTTP != nil {
		return d.SourceHTTP.RawSize, nil
	}

	if d.SourceOffline != nil {
		return d.SourceOffline.RawSize, nil
	}

	if d.SourceHttpPut != nil {
		return d.SourceHttpPut.RawSize, nil
	}

	return 0, xerrors.Errorf("no source defined")
}

func (d Products) Validate(db *harmonydb.DB) (ErrorCode, error) {
	if d.DDOV1 == nil {
		return ErrBadProposal, xerrors.Errorf("no products")
	}

	return d.DDOV1.Validate(db)
}

type DBDeal struct {
	Identifier      string          `db:"id"`
	SpID            int64           `db:"sp_id"`
	PieceCID        string          `db:"piece_cid"`
	Size            int64           `db:"size"`
	Format          json.RawMessage `db:"format"`
	SourceHTTP      json.RawMessage `db:"source_http"`
	SourceAggregate json.RawMessage `db:"source_aggregate"`
	SourceOffline   json.RawMessage `db:"source_offline"`
	SourceHttpPut   json.RawMessage `db:"source_http_put"`
	DDOv1           json.RawMessage `db:"ddo_v1"`
	Error           sql.NullString  `db:"error"`
}

func (d *Deal) ToDBDeal() (*DBDeal, error) {
	var err error
	// Marshal SourceHTTP (optional)
	var sourceHTTPBytes []byte
	if d.Data.SourceHTTP != nil {
		sourceHTTPBytes, err = json.Marshal(d.Data.SourceHTTP)
		if err != nil {
			return nil, fmt.Errorf("marshal source_http: %w", err)
		}
	} else {
		sourceHTTPBytes = []byte("null")
	}

	// Marshal SourceAggregate (optional)
	var sourceAggregateBytes []byte
	if d.Data.SourceAggregate != nil {
		sourceAggregateBytes, err = json.Marshal(d.Data.SourceAggregate)
		if err != nil {
			return nil, fmt.Errorf("marshal source_aggregate: %w", err)
		}
		if len(d.Data.SourceAggregate.Pieces) > 0 && len(d.Data.SourceAggregate.Pieces) != len(d.Data.Format.Aggregate.Sub) {
			var subPieces []PieceDataFormat
			for _, p := range d.Data.SourceAggregate.Pieces {
				subPieces = append(subPieces, PieceDataFormat{
					Car:       p.Format.Car,
					Raw:       p.Format.Raw,
					Aggregate: p.Format.Aggregate,
				})
			}
			d.Data.Format.Aggregate.Sub = subPieces
		}
	} else {
		sourceAggregateBytes = []byte("null")
	}

	// Marshal SourceOffline (optional)
	var sourceOfflineBytes []byte
	if d.Data.SourceOffline != nil {
		sourceOfflineBytes, err = json.Marshal(d.Data.SourceOffline)
		if err != nil {
			return nil, fmt.Errorf("marshal source_offline: %w", err)
		}
	} else {
		sourceOfflineBytes = []byte("null")
	}

	var sourceHttpPutBytes []byte
	if d.Data.SourceHttpPut != nil {
		sourceHttpPutBytes, err = json.Marshal(d.Data.SourceHttpPut)
		if err != nil {
			return nil, fmt.Errorf("marshal source_http_put: %w", err)
		}
	} else {
		sourceHttpPutBytes = []byte("null")
	}

	// Marshal Format (always present)
	formatBytes, err := json.Marshal(d.Data.Format)
	if err != nil {
		return nil, fmt.Errorf("marshal format: %w", err)
	}

	var spid abi.ActorID

	var ddov1 []byte
	if d.Products.DDOV1 != nil {
		ddov1, err = json.Marshal(d.Products.DDOV1)
		if err != nil {
			return nil, fmt.Errorf("marshal ddov1: %w", err)
		}
		spidInt, err := address.IDFromAddress(d.Products.DDOV1.Provider)
		if err != nil {
			return nil, fmt.Errorf("parse provider address: %w", err)
		}
		spid = abi.ActorID(spidInt)
	} else {
		ddov1 = []byte("null")
	}

	return &DBDeal{
		Identifier:      d.Identifier.String(),
		SpID:            int64(spid),
		PieceCID:        d.Data.PieceCID.String(),
		Size:            int64(d.Data.Size),
		Format:          formatBytes,
		SourceHTTP:      sourceHTTPBytes,
		SourceAggregate: sourceAggregateBytes,
		SourceOffline:   sourceOfflineBytes,
		SourceHttpPut:   sourceHttpPutBytes,
		DDOv1:           ddov1,
	}, nil
}

func (d *Deal) SaveToDB(tx *harmonydb.Tx) error {
	dbDeal, err := d.ToDBDeal()
	if err != nil {
		return xerrors.Errorf("to db deal: %w", err)
	}

	n, err := tx.Exec(`INSERT INTO deals (id, sp_id, piece_cid, size, format, source_http, source_aggregate, source_offline, source_http_put, ddo_v1) 
                  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		dbDeal.Identifier,
		dbDeal.SpID,
		dbDeal.PieceCID,
		dbDeal.Size,
		dbDeal.Format,
		dbDeal.SourceHTTP,
		dbDeal.SourceAggregate,
		dbDeal.SourceOffline,
		dbDeal.SourceHttpPut,
		dbDeal.DDOv1)
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
	err := tx.Select(&dbDeal, `SELECT * FROM market_mk20_deal WHERE id = $1`, id.String())
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
	err := db.Select(ctx, &dbDeal, `SELECT * FROM market_mk20_deal WHERE id = $1`, id.String())
	if err != nil {
		return nil, xerrors.Errorf("getting deal from DB: %w", err)
	}
	if len(dbDeal) != 1 {
		return nil, xerrors.Errorf("expected 1 deal, got %d", len(dbDeal))
	}
	return dbDeal[0].ToDeal()
}

func (d *DBDeal) ToDeal() (*Deal, error) {
	var ds DataSource
	var products Products

	// Unmarshal each field into the corresponding sub-structs (nil will remain nil if json is "null" or empty)
	if err := json.Unmarshal(d.Format, &ds.Format); err != nil {
		return nil, fmt.Errorf("unmarshal format: %w", err)
	}

	if len(d.SourceHTTP) > 0 && string(d.SourceHTTP) != "null" {
		var sh DataSourceHTTP
		if err := json.Unmarshal(d.SourceHTTP, &sh); err != nil {
			return nil, fmt.Errorf("unmarshal source_http: %w", err)
		}
		ds.SourceHTTP = &sh
	}

	if len(d.SourceAggregate) > 0 && string(d.SourceAggregate) != "null" {
		var sa DataSourceAggregate
		if err := json.Unmarshal(d.SourceAggregate, &sa); err != nil {
			return nil, fmt.Errorf("unmarshal source_aggregate: %w", err)
		}
		ds.SourceAggregate = &sa
	}

	if len(d.SourceOffline) > 0 && string(d.SourceOffline) != "null" {
		var so DataSourceOffline
		if err := json.Unmarshal(d.SourceOffline, &so); err != nil {
			return nil, fmt.Errorf("unmarshal source_offline: %w", err)
		}
		ds.SourceOffline = &so
	}

	if len(d.DDOv1) > 0 && string(d.DDOv1) != "null" {
		if err := json.Unmarshal(d.DDOv1, &products.DDOV1); err != nil {
			return nil, fmt.Errorf("unmarshal ddov1: %w", err)
		}
	}

	// Convert identifier
	id, err := ulid.Parse(d.Identifier)
	if err != nil {
		return nil, fmt.Errorf("parse identifier: %w", err)
	}

	// Convert CID
	c, err := cid.Decode(d.PieceCID)
	if err != nil {
		return nil, fmt.Errorf("decode piece_cid: %w", err)
	}

	// Assign remaining fields
	ds.PieceCID = c
	ds.Size = abi.PaddedPieceSize(d.Size)

	return &Deal{
		Identifier: id,
		Data:       ds,
		Products:   products,
	}, nil
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
