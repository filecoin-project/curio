package client

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/mitchellh/go-homedir"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v16/verifreg"

	"github.com/filecoin-project/curio/market/mk20"
)

var log = logging.Logger("mk20-client")

type Client struct {
	http *HTTPClient
}

func NewClient(baseURL, auth string) *Client {
	hclient := New(baseURL, Option(WithAuthString(auth)))
	return &Client{
		http: hclient,
	}
}

func (c *Client) Deal(ctx context.Context, maddr, wallet address.Address, pieceCid cid.Cid, http_url, aggregateFile, contract_address, contract_method string, headers http.Header, put, index, announce, pdp bool, duration, allocation, proofSet int64) error {
	var d mk20.DataSource

	if aggregateFile != "" {
		d = mk20.DataSource{
			PieceCID: pieceCid,
			Format: mk20.PieceDataFormat{
				Aggregate: &mk20.FormatAggregate{
					Type: mk20.AggregateTypeV1,
				},
			},
		}

		var pieces []mk20.DataSource

		log.Debugw("using aggregate data source", "aggregate", aggregateFile)
		// Read file line by line
		loc, err := homedir.Expand(aggregateFile)
		if err != nil {
			return err
		}
		file, err := os.Open(loc)
		if err != nil {
			return err
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.Split(line, "\t")
			if len(parts) != 2 {
				return fmt.Errorf("invalid line format. Expected pieceCidV2, url at %s", line)
			}
			if parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("empty column value in the input file at %s", line)
			}

			pieceCid, err := cid.Parse(parts[0])
			if err != nil {
				return fmt.Errorf("failed to parse CID: %w", err)
			}

			url, err := url.Parse(parts[1])
			if err != nil {
				return fmt.Errorf("failed to parse url: %w", err)
			}

			pieces = append(pieces, mk20.DataSource{
				PieceCID: pieceCid,
				Format: mk20.PieceDataFormat{
					Car: &mk20.FormatCar{},
				},
				SourceHTTP: &mk20.DataSourceHTTP{
					URLs: []mk20.HttpUrl{
						{
							URL:      url.String(),
							Priority: 0,
							Fallback: true,
						},
					},
				},
			})

			if err := scanner.Err(); err != nil {
				return err
			}
		}
		d.SourceAggregate = &mk20.DataSourceAggregate{
			Pieces: pieces,
		}
	} else {
		if http_url == "" {
			if put {
				d = mk20.DataSource{
					PieceCID: pieceCid,
					Format: mk20.PieceDataFormat{
						Car: &mk20.FormatCar{},
					},
					SourceHttpPut: &mk20.DataSourceHttpPut{},
				}
			} else {
				d = mk20.DataSource{
					PieceCID: pieceCid,
					Format: mk20.PieceDataFormat{
						Car: &mk20.FormatCar{},
					},
					SourceOffline: &mk20.DataSourceOffline{},
				}
			}
		} else {
			url, err := url.Parse(http_url)
			if err != nil {
				return xerrors.Errorf("parsing http url: %w", err)
			}
			d = mk20.DataSource{
				PieceCID: pieceCid,
				Format: mk20.PieceDataFormat{
					Car: &mk20.FormatCar{},
				},
				SourceHTTP: &mk20.DataSourceHTTP{
					URLs: []mk20.HttpUrl{
						{
							URL:      url.String(),
							Headers:  headers,
							Priority: 0,
							Fallback: true,
						},
					},
				},
			}
		}
	}

	p := mk20.Products{
		DDOV1: &mk20.DDOV1{
			Provider:                   maddr,
			PieceManager:               wallet,
			Duration:                   abi.ChainEpoch(duration),
			ContractAddress:            contract_address,
			ContractVerifyMethod:       contract_method,
			ContractVerifyMethodParams: []byte("test bytes"),
		},
		RetrievalV1: &mk20.RetrievalV1{
			Indexing:        index,
			AnnouncePayload: announce,
		},
	}

	if pdp {
		ps := uint64(proofSet)
		p.PDPV1 = &mk20.PDPV1{
			AddRoot:    true,
			ProofSetID: &ps,
			ExtraData:  []byte("test bytes"), // TODO: Fix this
		}
	}

	if allocation != 0 {
		alloc := verifreg.AllocationId(allocation)
		p.DDOV1.AllocationId = &alloc
	}

	id, err := mk20.NewULID()
	if err != nil {
		return err
	}
	log.Debugw("generated deal id", "id", id)

	deal := mk20.Deal{
		Identifier: id,
		Client:     wallet,
		Data:       &d,
		Products:   p,
	}

	log.Debugw("deal", "deal", deal)

	rerr := c.http.Store(ctx, &deal)
	if rerr.Error != nil {
		return rerr.Error
	}
	if rerr.Status != 200 {
		return rerr.HError()
	}
	return nil
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

	numChunks := int(size / chunkSize)

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
