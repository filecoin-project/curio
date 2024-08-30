package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/reqcontext"
	"github.com/filecoin-project/curio/market/storageIngest"
)

var marketCmd = &cli.Command{
	Name: "market",
	Subcommands: []*cli.Command{
		marketSealCmd,
		marketImportdataCmd,
		marketAddOfflineURLCmd,
	},
}

var marketSealCmd = &cli.Command{
	Name:  "seal",
	Usage: "start sealing a deal sector early",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "actor",
			Usage:    "Specify actor address to start sealing sectors for",
			Required: true,
		},
		&cli.BoolFlag{
			Name:  "synthetic",
			Usage: "Use synthetic PoRep",
			Value: false,
		},
	},
	ArgsUsage: "<sector>",
	Action: func(cctx *cli.Context) error {
		act, err := address.NewFromString(cctx.String("actor"))
		if err != nil {
			return xerrors.Errorf("parsing --actor: %w", err)
		}

		if cctx.Args().Len() > 1 {
			return xerrors.Errorf("specify only one sector")
		}

		sec := cctx.Args().First()

		sector, err := strconv.ParseUint(sec, 10, 64)
		if err != nil {
			return xerrors.Errorf("failed to parse the sector number: %w", err)
		}

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		return storageIngest.SealNow(ctx, dep.Chain, dep.DB, act, abi.SectorNumber(sector), cctx.Bool("synthetic"))
	},
}

var marketImportdataCmd = &cli.Command{
	Name:  "import-data",
	Usage: "Import data for offline deal",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "actor",
			Usage:    "Specify actor address to start sealing sectors for",
			Required: true,
		},
	},
	ArgsUsage: "<deal UUID> <file> <host:port>",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 3 {
			return xerrors.Errorf("incorrect number of arguments")
		}

		idStr := cctx.Args().First()

		id, err := uuid.Parse(idStr)
		if err != nil {
			return err
		}

		fileStr := cctx.Args().Get(1)
		fpath, err := homedir.Expand(fileStr)
		if err != nil {
			return err
		}

		f, err := os.Open(fpath)
		if err != nil {
			return err
		}

		defer func() {
			_ = f.Close()
		}()

		st, err := f.Stat()
		if err != nil {
			return err
		}

		rawSize := st.Size()

		fUrl := "file:///" + fpath

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		comm, err := dep.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			var details []struct {
				Offline bool                `db:"offline"`
				Piece   string              `db:"piece_cid"`
				Size    abi.PaddedPieceSize `db:"piece_size"`
			}
			err = tx.Select(&details, `SELECT offline, piece_cid, piece_size FROM market_mk12_deals WHERE uuid = $1`, id.String())
			if err != nil {
				return false, xerrors.Errorf("getting deal details from DB: %w", err)
			}

			if len(details) != 1 {
				return false, xerrors.Errorf("expected 1 row but got %d", len(details))
			}

			deal := details[0]

			if !deal.Offline {
				return false, xerrors.Errorf("provided deal %s is an online deal", id.String())
			}

			if abi.UnpaddedPieceSize(rawSize).Padded() != deal.Size {
				return false, xerrors.Errorf("piece size mismatch: database %d and calculated %d", deal.Size, abi.UnpaddedPieceSize(rawSize).Padded())
			}

			var pieceID int64
			// Attempt to select the piece ID first
			err = tx.QueryRow(`SELECT id FROM parked_pieces WHERE piece_cid = $1`, deal.Piece).Scan(&pieceID)

			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// Piece does not exist, attempt to insert
					err = tx.QueryRow(`
							INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size)
							VALUES ($1, $2, $3)
							ON CONFLICT (piece_cid) DO NOTHING
							RETURNING id`, deal.Piece, deal.Size, rawSize).Scan(&pieceID)
					if err != nil {
						return false, xerrors.Errorf("inserting new parked piece and getting id: %w", err)
					}
				} else {
					// Some other error occurred during select
					return false, xerrors.Errorf("checking existing parked piece: %w", err)
				}
			}

			// Add parked_piece_ref
			var refID int64
			err = tx.QueryRow(`INSERT INTO parked_piece_refs (piece_id, data_url)
        			VALUES ($1, $2) RETURNING ref_id`, pieceID, fUrl).Scan(&refID)
			if err != nil {
				return false, xerrors.Errorf("inserting parked piece ref: %w", err)
			}

			pieceIDUrl := url.URL{
				Scheme: "pieceref",
				Opaque: fmt.Sprintf("%d", refID),
			}

			// Insert the offline deal into the deal pipeline
			_, err = tx.Exec(`INSERT INTO market_mk12_deal_pipeline (url, raw_size)
								VALUES ($1, $2) WHERE uuid = $3 ON CONFLICT (uuid) DO NOTHING`,
				pieceIDUrl, rawSize)
			if err != nil {
				return false, xerrors.Errorf("inserting deal into deal pipeline: %w", err)
			}

			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return err
		}
		if !comm {
			return xerrors.Errorf("failed to commit the transaction")
		}
		return nil
	},
}

var marketAddOfflineURLCmd = &cli.Command{
	Name:  "add-url",
	Usage: "Add URL to fetch data for offline deals",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "file",
			Usage:    "CSV file location to use for multiple deal input. Each line in the file should be in the format 'uuid,raw size,url,header1,header2...'\"",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "header",
			Aliases:  []string{"H"},
			Usage:    "Custom `HEADER` to include in the HTTP request",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "url",
			Aliases:  []string{"u"},
			Usage:    "`URL` to send the request to",
			Required: true,
		},
	},
	ArgsUsage: "<deal UUID> <raw size/car size>",
	Action: func(cctx *cli.Context) error {
		if !cctx.IsSet("file") && cctx.Args().Len() != 2 {
			return xerrors.Errorf("incorrect number of arguments")
		}

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		if cctx.IsSet("file") {
			// Read file line by line
			fileStr := cctx.String("file")
			loc, err := homedir.Expand(fileStr)
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
				// Extract pieceCid, pieceSize and MinerAddr from line
				parts := strings.SplitN(line, ",", 4)
				if parts[0] == "" || parts[1] == "" || parts[2] == "" {
					return fmt.Errorf("empty column value in the input file at %s", line)
				}

				uuid := parts[0]
				size, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse size %w", err)
				}

				url := parts[2]

				if parts[3] != "" {
					header := http.Header{}
					for _, s := range strings.Split(parts[3], ",") {
						key, value, found := strings.Cut(s, ":")
						if !found {
							return fmt.Errorf("invalid header format, expected key:value")
						}
						header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
					}

					hdr, err := json.Marshal(header)
					if err != nil {
						return xerrors.Errorf("marshalling headers: %w", err)
					}
					_, err = dep.DB.Exec(ctx, `INSERT INTO market_offline_urls (
								uuid,
								url,
								headers,
								raw_size
							) VALUES ($1, $2, $3, $4);`,
						uuid, url, hdr, size)
					if err != nil {
						return xerrors.Errorf("adding details to DB: %w", err)
					}
				} else {
					_, err = dep.DB.Exec(ctx, `INSERT INTO market_offline_urls (
								uuid,
								url,
								raw_size
							) VALUES ($1, $2, $3, $4);`,
						uuid, url, size)
					if err != nil {
						return xerrors.Errorf("adding details to DB: %w", err)
					}
				}

				if err := scanner.Err(); err != nil {
					return err
				}
			}
		}

		url := cctx.String("url")

		headerValue := cctx.StringSlice("header")

		uuid := cctx.Args().First()

		sizeStr := cctx.Args().Get(1)
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil {
			return xerrors.Errorf("parsing size: %w", err)
		}

		if cctx.IsSet("header") {
			// Split the header into key-value
			header := http.Header{}
			for _, s := range headerValue {
				key, value, found := strings.Cut(s, ":")
				if !found {
					return fmt.Errorf("invalid header format, expected key:value")
				}
				header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
			}

			hdr, err := json.Marshal(header)
			if err != nil {
				return xerrors.Errorf("marshalling headers: %w", err)
			}

			_, err = dep.DB.Exec(ctx, `INSERT INTO market_offline_urls (
								uuid,
								url,
								headers,
								raw_size
							) VALUES ($1, $2, $3, $4);`,
				uuid, url, hdr, size)
			if err != nil {
				return xerrors.Errorf("adding details to DB: %w", err)
			}
		} else {
			_, err = dep.DB.Exec(ctx, `INSERT INTO market_offline_urls (
								uuid,
								url,
								raw_size
							) VALUES ($1, $2, $3, $4);`,
				uuid, url, size)
			if err != nil {
				return xerrors.Errorf("adding details to DB: %w", err)
			}
		}

		return nil
	},
}
