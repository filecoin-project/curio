package market

import (
	"bufio"
	"io"

	carv2 "github.com/ipld/go-car/v2"
	"github.com/multiformats/go-varint"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/storiface"
)

// GetRawSizeFromCarReader reads the CAR file from the provided reader and calculates its raw size
func GetRawSizeFromCarReader(reader storiface.Reader) (uint64, error) {
	if reader == nil {
		return 0, xerrors.Errorf("reader is nil")
	}

	version, err := carv2.ReadVersion(reader, carv2.ZeroLengthSectionAsEOF(true))
	if err != nil {
		return 0, xerrors.Errorf("failed to read car version: %w", err)
	}

	_, err = reader.Seek(0, 0)
	if err != nil {
		return 0, xerrors.Errorf("failed to seek to beginning of car: %w", err)
	}

	if version == 2 {
		rx, err := carv2.NewReader(reader, carv2.ZeroLengthSectionAsEOF(true))
		if err != nil {
			return 0, xerrors.Errorf("failed to create car reader: %w", err)
		}

		var carSize uint64

		if rx.Header.HasIndex() {
			ir, err := rx.IndexReader()
			if err != nil {
				return 0, xerrors.Errorf("failed to create index reader: %w", err)
			}

			n, err := io.Copy(io.Discard, ir)
			if err != nil {
				return 0, xerrors.Errorf("failed to read index: %w", err)
			}

			carSize = rx.Header.IndexOffset + uint64(n)
		} else {
			carSize = rx.Header.DataOffset + rx.Header.DataSize
		}
		return carSize, nil
	}

	if version == 1 {
		rd, err := carv2.NewBlockReader(bufio.NewReaderSize(reader, 4<<20), carv2.ZeroLengthSectionAsEOF(true))
		if err != nil {
			return 0, xerrors.Errorf("failed to create block reader: %w", err)
		}

		// Read the first block to know the header size
		b, err := rd.SkipNext()
		if err != nil && err != io.EOF {
			return 0, xerrors.Errorf("failed to read first block: %w", err)
		}

		if err == io.EOF {
			return 0, xerrors.Errorf("no blocks found")
		}

		if b == nil {
			return 0, xerrors.Errorf("block is nil")
		}

		// CAR sections are [varint (length), CID, blockData]
		combinedSize := b.Size + uint64(b.ByteLen())
		lenSize := uint64(varint.UvarintSize(combinedSize))
		sectionSize := combinedSize + lenSize

		length := b.SourceOffset + sectionSize

		for {
			b, err = rd.SkipNext()
			if err == io.EOF {
				break
			}
			if err != nil {
				return 0, xerrors.Errorf("failed to read next block: %w", err)
			}
			if b == nil {
				return 0, xerrors.Errorf("block is nil")
			}
			combinedSize = b.Size + uint64(b.ByteLen())
			lenSize = uint64(varint.UvarintSize(combinedSize))
			sectionSize = combinedSize + lenSize

			length += sectionSize
		}
		return length, nil
	}

	return 0, xerrors.Errorf("unsupported car version: %d", version)
}
