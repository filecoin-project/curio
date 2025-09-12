package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ipfs/go-cid"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/market/ipni/chunker"
)

var testDebugIpniChunks = &cli.Command{
	Name:  "ipni-piece-chunks",
	Usage: translations.T("generate ipni chunks from a file"),
	Action: func(c *cli.Context) error {
		ck := chunker.NewInitialChunker()

		f, err := os.Open(c.Args().First())
		if err != nil {
			return xerrors.Errorf("opening file: %w", err)
		}
		defer func() {
			_ = f.Close()
		}()

		opts := []carv2.Option{carv2.ZeroLengthSectionAsEOF(true)}
		blockReader, err := carv2.NewBlockReader(bufio.NewReaderSize(f, 4<<20), opts...)
		if err != nil {
			return fmt.Errorf("getting block reader over piece: %w", err)
		}

		blockMetadata, err := blockReader.SkipNext()
		for err == nil {
			if err := ck.Accept(blockMetadata.Hash(), int64(blockMetadata.Offset), blockMetadata.Size+40); err != nil {
				return xerrors.Errorf("accepting block: %w", err)
			}

			blockMetadata, err = blockReader.SkipNext()
		}
		if !errors.Is(err, io.EOF) {
			return xerrors.Errorf("reading block: %w", err)
		}

		_, err = ck.Finish(c.Context, nil, cid.Undef, false)
		if err != nil {
			return xerrors.Errorf("chunking CAR multihash iterator: %w", err)
		}

		return nil
	},
}
