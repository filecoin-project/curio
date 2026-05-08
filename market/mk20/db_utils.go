package mk20

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

type DBDDOV1 struct {
	DDO      *DDOV1 `json:"ddo"`
	DealID   int64  `json:"deal_id"`
	Complete bool   `json:"complete"`
	Error    string `json:"error"`
}

type DBPDPV1 struct {
	PDP      *PDPV1 `json:"pdp"`
	Complete bool   `json:"complete"`
	Error    string `json:"error"`
}

type DBDeal struct {
	Identifier  string          `db:"id"`
	Client      string          `db:"client"`
	PieceCIDV2  string          `db:"piece_cid_v2"`
	Data        json.RawMessage `db:"data"`
	DDOv1       json.RawMessage `db:"ddo_v1"`
	RetrievalV1 json.RawMessage `db:"retrieval_v1"`
	PDPV1       json.RawMessage `db:"pdp_v1"`
}

func (d *Deal) ToDBDeal() (*DBDeal, error) {
	ddeal := DBDeal{
		Identifier: d.Identifier.String(),
		Client:     d.Client,
	}

	if d.Data != nil {
		dataBytes, err := json.Marshal(d.Data)
		if err != nil {
			return nil, fmt.Errorf("marshal data: %w", err)
		}
		ddeal.PieceCIDV2 = d.Data.PieceCID.String()
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

	var pieceCid any

	if dbDeal.PieceCIDV2 != "" {
		pieceCid = dbDeal.PieceCIDV2
	} else {
		pieceCid = nil
	}

	n, err := tx.Exec(`INSERT INTO market_mk20_deal (id, client, piece_cid_v2, data, ddo_v1, retrieval_v1, pdp_v1) 
                  VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		dbDeal.Identifier,
		dbDeal.Client,
		pieceCid,
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

func (d *Deal) UpdateDealWithTx(tx *harmonydb.Tx) error {
	dbDeal, err := d.ToDBDeal()
	if err != nil {
		return xerrors.Errorf("to db deal: %w", err)
	}

	var pieceCid any

	if dbDeal.PieceCIDV2 != "" {
		pieceCid = dbDeal.PieceCIDV2
	} else {
		pieceCid = nil
	}

	n, err := tx.Exec(`UPDATE market_mk20_deal SET 
                            piece_cid_v2 = $1, 
                            data = $2, 
                            ddo_v1 = $3,
                            retrieval_v1 = $4,
                            pdp_v1 = $5
                            WHERE id = $6`, pieceCid, dbDeal.Data, dbDeal.DDOv1, dbDeal.RetrievalV1, dbDeal.PDPV1, d.Identifier.String())
	if err != nil {
		return xerrors.Errorf("update deal: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("update deal: expected 1 row affected, got %d", n)
	}
	return nil
}

func (d *Deal) UpdateDeal(tx *harmonydb.Tx) error {
	dbDeal, err := d.ToDBDeal()
	if err != nil {
		return xerrors.Errorf("to db deal: %w", err)
	}

	var pieceCid any

	if dbDeal.PieceCIDV2 != "" {
		pieceCid = dbDeal.PieceCIDV2
	} else {
		pieceCid = nil
	}

	n, err := tx.Exec(`UPDATE market_mk20_deal SET 
                            piece_cid_v2 = $1, 
                            data = $2, 
                            ddo_v1 = $3,
                            retrieval_v1 = $4,
                            pdp_v1 = $5
                            WHERE id = $6`, pieceCid, dbDeal.Data, dbDeal.DDOv1, dbDeal.RetrievalV1, dbDeal.PDPV1, d.Identifier.String())
	if err != nil {
		return xerrors.Errorf("update deal: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("update deal: expected 1 row affected, got %d", n)
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

	deal.Client = d.Client

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
