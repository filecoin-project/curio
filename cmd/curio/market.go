package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/reqcontext"
	"github.com/filecoin-project/curio/market/storageingest"
)

var marketCmd = &cli.Command{
	Name: "market",
	Subcommands: []*cli.Command{
		marketSealCmd,
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

		return storageingest.SealNow(ctx, dep.Chain, dep.DB, act, abi.SectorNumber(sector), cctx.Bool("synthetic"))
	},
}

var marketAddOfflineURLCmd = &cli.Command{
	Name:  "add-url",
	Usage: "Add URL to fetch data for offline deals",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "file",
			Usage: "CSV file location to use for multiple deal input. Each line in the file should be in the format 'uuid,raw size,url,header1,header2...'\"",
		},
		&cli.StringSliceFlag{
			Name:    "header",
			Aliases: []string{"H"},
			Usage:   "Custom `HEADER` to include in the HTTP request",
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

		uuid := cctx.Args().First()

		sizeStr := cctx.Args().Get(1)
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil {
			return xerrors.Errorf("parsing size: %w", err)
		}

		if cctx.IsSet("header") {
			// Split the header into key-value
			header := http.Header{}
			headerValue := cctx.StringSlice("header")
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
