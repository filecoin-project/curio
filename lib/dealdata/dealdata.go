package dealdata

import (
	"context"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/filler"
	"github.com/filecoin-project/go-commp-utils/nonffi"
	"github.com/filecoin-project/go-commp-utils/zerocomm"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/storage/pipeline/lib/nullreader"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
	"io"
	"net/url"
	"strconv"
)

var log = logging.Logger("dealdata")

type dealMetadata struct {
	PieceIndex int64  `db:"piece_index"`
	PieceCID   string `db:"piece_cid"`
	PieceSize  int64  `db:"piece_size"`

	DataUrl     *string `db:"data_url"`
	DataHeaders *[]byte `db:"data_headers"`
	DataRawSize *int64  `db:"data_raw_size"`
}

type DealData struct {
	CommD      cid.Cid
	Data       io.Reader
	IsUnpadded bool
	Close      func()
}

func DealDataSDRPoRep(ctx context.Context, db *harmonydb.DB, spId, sectorNumber int64, spt abi.RegisteredSealProof) (*DealData, error) {
	var pieces []dealMetadata
	err := db.Select(ctx, &pieces, `
		SELECT piece_index, piece_cid, piece_size, data_url, data_headers, data_raw_size
		FROM sectors_sdr_initial_pieces
		WHERE sp_id = $1 AND sector_number = $2 ORDER BY piece_index ASC`, spId, sectorNumber)
	if err != nil {
		return nil, xerrors.Errorf("getting pieces: %w", err)
	}

	return getDealMetadata(ctx, db, spt, pieces)
}

func getDealMetadata(ctx context.Context, db *harmonydb.DB, spt abi.RegisteredSealProof, pieces []dealMetadata) (*DealData, error) {
	ssize, err := spt.SectorSize()
	if err != nil {
		return nil, xerrors.Errorf("getting sector size: %w", err)
	}

	var out DealData

	var closers []io.Closer
	defer func() {
		if out.Close != nil {
			return // clean return
		}

		for _, c := range closers {
			if err := c.Close(); err != nil {
				log.Errorw("error closing piece reader", "error", err)
			}
		}
	}()

	if len(pieces) > 0 {
		var pieceInfos []abi.PieceInfo
		var pieceReaders []io.Reader
		var offset abi.UnpaddedPieceSize
		var allocated abi.UnpaddedPieceSize

		for _, p := range pieces {
			// make pieceInfo
			c, err := cid.Parse(p.PieceCID)
			if err != nil {
				return nil, xerrors.Errorf("parsing piece cid: %w", err)
			}

			allocated += abi.UnpaddedPieceSize(*p.DataRawSize)

			pads, padLength := ffiwrapper.GetRequiredPadding(offset.Padded(), abi.PaddedPieceSize(p.PieceSize))
			offset += padLength.Unpadded()

			for _, pad := range pads {
				pieceInfos = append(pieceInfos, abi.PieceInfo{
					Size:     pad,
					PieceCID: zerocomm.ZeroPieceCommitment(pad.Unpadded()),
				})
				pieceReaders = append(pieceReaders, nullreader.NewNullReader(pad.Unpadded()))
			}

			pieceInfos = append(pieceInfos, abi.PieceInfo{
				Size:     abi.PaddedPieceSize(p.PieceSize),
				PieceCID: c,
			})

			offset += abi.UnpaddedPieceSize(*p.DataRawSize)

			// make pieceReader
			if p.DataUrl != nil {
				dataUrl := *p.DataUrl

				goUrl, err := url.Parse(dataUrl)
				if err != nil {
					return nil, xerrors.Errorf("parsing data URL: %w", err)
				}

				if goUrl.Scheme == "pieceref" {
					// url is to a piece reference

					refNum, err := strconv.ParseInt(goUrl.Opaque, 10, 64)
					if err != nil {
						return nil, xerrors.Errorf("parsing piece reference number: %w", err)
					}

					// get pieceID
					var pieceID []struct {
						PieceID storiface.PieceNumber `db:"piece_id"`
					}
					err = db.Select(ctx, &pieceID, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, refNum)
					if err != nil {
						return nil, xerrors.Errorf("getting pieceID: %w", err)
					}

					if len(pieceID) != 1 {
						return nil, xerrors.Errorf("expected 1 pieceID, got %d", len(pieceID))
					}

					pr, err := sc.PieceReader(ctx, pieceID[0].PieceID)
					if err != nil {
						return nil, xerrors.Errorf("getting piece reader: %w", err)
					}

					closers = append(closers, pr)

					reader, _ := padreader.New(pr, uint64(*p.DataRawSize))
					pieceReaders = append(pieceReaders, reader)
				} else {
					reader, _ := padreader.New(&UrlPieceReader{
						Url:     dataUrl,
						RawSize: *p.DataRawSize,
					}, uint64(*p.DataRawSize))
					pieceReaders = append(pieceReaders, reader)
				}

			} else { // padding piece (w/o fr32 padding, added in TreeD)
				pieceReaders = append(pieceReaders, nullreader.NewNullReader(abi.PaddedPieceSize(p.PieceSize).Unpadded()))
			}
		}

		fillerSize, err := filler.FillersFromRem(abi.PaddedPieceSize(ssize).Unpadded() - allocated)
		if err != nil {
			return nil, xerrors.Errorf("failed to calculate the final padding: %w", err)
		}
		for _, fil := range fillerSize {
			pieceInfos = append(pieceInfos, abi.PieceInfo{
				Size:     fil.Padded(),
				PieceCID: zerocomm.ZeroPieceCommitment(fil),
			})
			pieceReaders = append(pieceReaders, nullreader.NewNullReader(fil))
		}

		out.CommD, err = nonffi.GenerateUnsealedCID(spt, pieceInfos)
		if err != nil {
			return nil, xerrors.Errorf("computing CommD: %w", err)
		}

		out.Data = io.MultiReader(pieceReaders...)
		out.IsUnpadded = true
	} else {
		out.CommD = zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(ssize).Unpadded())
		out.Data = nullreader.NewNullReader(abi.UnpaddedPieceSize(ssize))
		out.IsUnpadded = false // nullreader includes fr32 zero bits
	}

	out.Close = func() {
		for _, c := range closers {
			if err := c.Close(); err != nil {
				log.Errorw("error closing piece reader", "error", err)
			}
		}
	}

	return &out, nil
}
