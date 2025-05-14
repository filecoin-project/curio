package mk20

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/bits"
	"net/url"
	"time"

	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-data-segment/datasegment"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

type productAndDataSource struct {
	Products []dbProduct
	Data     []dbDataSource
}

func (d *Deal) Validate(pad *productAndDataSource) (int, error) {
	code, err := d.Products.Validate(pad.Products)
	if err != nil {
		return code, xerrors.Errorf("products validation failed: %w", err)
	}

	return d.Data.Validate(pad.Data)
}

func (d DataSource) Validate(dbDataSources []dbDataSource) (int, error) {
	if len(dbDataSources) == 0 {
		return ErrUnsupportedDataSource, xerrors.Errorf("no data sources enabled on the provider")
	}

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
		if d.Format.Car.Version != 1 && d.Format.Car.Version != 2 {
			return ErrMalformedDataSource, xerrors.Errorf("car version not supported")
		}
	}

	if d.Format.Aggregate != nil {
		fagg = true

		if d.Format.Aggregate.Type != AggregateTypeV1 {
			return ErrMalformedDataSource, xerrors.Errorf("aggregate type not supported")
		}

		if d.SourceAggregate != nil {
			code, err := d.SourceAggregate.IsEnabled(dbDataSources)
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
					if p.Format.Car.Version != 1 && p.Format.Car.Version != 2 {
						return ErrMalformedDataSource, xerrors.Errorf("car version not supported")
					}
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
		code, err := d.SourceHTTP.IsEnabled(dbDataSources)
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
		code, err := d.SourceOffline.IsEnabled(dbDataSources)
		if err != nil {
			return code, err
		}

		if d.SourceOffline.RawSize == 0 {
			return ErrMalformedDataSource, xerrors.Errorf("raw size is 0")
		}
	}

	if d.SourceHttpPut != nil {
		code, err := d.SourceHttpPut.IsEnabled(dbDataSources)
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

type dbProduct struct {
	Name    string `db:"name"`
	Enabled bool   `db:"enabled"`
}

func (d Products) Validate(dbProducts []dbProduct) (int, error) {
	if len(dbProducts) == 0 {
		return ErrProductNotEnabled, xerrors.Errorf("no products enabled on the provider")
	}

	if d.DDOV1 == nil {
		return ErrBadProposal, xerrors.Errorf("no products")
	}

	return d.DDOV1.Validate(dbProducts)
}

type DBDeal struct {
	Identifier      string          `db:"id"`
	PieceCID        string          `db:"piece_cid"`
	Size            int64           `db:"size"`
	Format          json.RawMessage `db:"format"`
	SourceHTTP      json.RawMessage `db:"source_http"`
	SourceAggregate json.RawMessage `db:"source_aggregate"`
	SourceOffline   json.RawMessage `db:"source_offline"`
	DDOv1           json.RawMessage `db:"ddov1"`
}

func (d *Deal) ToDBDeal() (*DBDeal, error) {

	// Marshal Format (always present)
	formatBytes, err := json.Marshal(d.Data.Format)
	if err != nil {
		return nil, fmt.Errorf("marshal format: %w", err)
	}

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

	var ddov1 []byte
	if d.Products.DDOV1 != nil {
		ddov1, err = json.Marshal(d.Products.DDOV1)
		if err != nil {
			return nil, fmt.Errorf("marshal ddov1: %w", err)
		}
	} else {
		ddov1 = []byte("null")
	}

	return &DBDeal{
		Identifier:      d.Identifier.String(),
		PieceCID:        d.Data.PieceCID.String(),
		Size:            int64(d.Data.Size),
		Format:          formatBytes,
		SourceHTTP:      sourceHTTPBytes,
		SourceAggregate: sourceAggregateBytes,
		SourceOffline:   sourceOfflineBytes,
		DDOv1:           ddov1,
	}, nil
}

func (d *Deal) SaveToDB(tx *harmonydb.Tx) error {
	dbDeal, err := d.ToDBDeal()
	if err != nil {
		return xerrors.Errorf("to db deal: %w", err)
	}

	n, err := tx.Exec(`INSERT INTO deals (id, piece_cid, size, format, source_http, source_aggregate, source_offline, ddov1) 
                  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		dbDeal.Identifier,
		dbDeal.PieceCID,
		dbDeal.Size,
		dbDeal.Format,
		dbDeal.SourceHTTP,
		dbDeal.SourceAggregate,
		dbDeal.SourceOffline,
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
	err := tx.Select(&dbDeal, `SELECT * FROM deals WHERE id = $1`, id.String())
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
	err := db.Select(ctx, &dbDeal, `SELECT * FROM deals WHERE id = $1`, id.String())
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

type DealStatusRequest struct {
	Identifier ulid.ULID        `json:"identifier"`
	Signature  crypto.Signature `json:"signature"`
}

type DealStatusResponse struct {
	State    DealState `json:"status"`
	ErrorMsg string    `json:"errormsg"`
}

type DealStatus struct {
	Response *DealStatusResponse
	HTTPCode int
}

type DealState string

const (
	DealStateAccepted   DealState = "accepted"
	DealStateProcessing DealState = "processing"
	DealStateSealing    DealState = "sealing"
	DealStateIndexing   DealState = "indexing"
	DealStateFailed     DealState = "failed"
	DealStateComplete   DealState = "complete"
)

type SupportedContracts struct {
	Contracts []string `json:"contracts"`
}

func NewULID() (ulid.ULID, error) {
	return ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
}
