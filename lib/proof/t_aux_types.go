package proof

import (
	"io"

	"golang.org/x/xerrors"
)

type StoreConfig struct {
	// A directory in which data (a merkle tree) can be persisted.
	Path string

	// A unique identifier used to help specify the on-disk store location for this particular data.
	ID string

	// The number of elements in the DiskStore. This field is optional, and unused internally.
	Size *uint64

	// The number of merkle tree rows_to_discard then cache on disk.
	RowsToDiscard uint64
}

type TAuxLabels struct {
	Labels []StoreConfig
}

type TemporaryAux struct {
	Labels      TAuxLabels
	TreeDConfig StoreConfig
	TreeRConfig StoreConfig
	TreeCConfig StoreConfig
}

func DecodeStoreConfig(r io.Reader) (StoreConfig, error) {
	var sc StoreConfig
	var err error

	sc.Path, err = ReadString(r)
	if err != nil {
		return StoreConfig{}, xerrors.Errorf("failed to decode path: %w", err)
	}

	sc.ID, err = ReadString(r)
	if err != nil {
		return StoreConfig{}, xerrors.Errorf("failed to decode ID: %w", err)
	}

	// Size in an option, so prefixed with a 0x01 byte if present or 0x00 if not.
	var b [1]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return StoreConfig{}, xerrors.Errorf("failed to read size present byte: %w", err)
	}
	if b[0] == 0x01 {
		size, err := ReadLE[uint64](r)
		if err != nil {
			return StoreConfig{}, xerrors.Errorf("failed to read size: %w", err)
		}
		sc.Size = &size
	}

	sc.RowsToDiscard, err = ReadLE[uint64](r)
	if err != nil {
		return StoreConfig{}, xerrors.Errorf("failed to read rows to discard: %w", err)
	}

	return sc, nil
}

func DecodeLabels(r io.Reader) (*TAuxLabels, error) {
	numLabels, err := ReadLE[uint64](r)
	if err != nil {
		return nil, xerrors.Errorf("failed to read number of labels: %w", err)
	}

	labels := make([]StoreConfig, numLabels)
	for i := range labels {
		labels[i], err = DecodeStoreConfig(r)
		if err != nil {
			return nil, xerrors.Errorf("failed to decode label %d: %w", i, err)
		}
	}

	return &TAuxLabels{Labels: labels}, nil
}

func DecodeTAux(r io.Reader) (*TemporaryAux, error) {
	labels, err := DecodeLabels(r)
	if err != nil {
		return nil, xerrors.Errorf("failed to decode labels: %w", err)
	}

	treeDConfig, err := DecodeStoreConfig(r)
	if err != nil {
		return nil, xerrors.Errorf("failed to decode treeDConfig: %w", err)
	}

	treeRConfig, err := DecodeStoreConfig(r)
	if err != nil {
		return nil, xerrors.Errorf("failed to decode treeRConfig: %w", err)
	}

	treeCConfig, err := DecodeStoreConfig(r)
	if err != nil {
		return nil, xerrors.Errorf("failed to decode treeCConfig: %w", err)
	}

	return &TemporaryAux{
		Labels:      *labels,
		TreeDConfig: treeDConfig,
		TreeRConfig: treeRConfig,
		TreeCConfig: treeCConfig,
	}, nil
}

func EncodeStoreConfig(w io.Writer, sc StoreConfig) error {
	if err := WriteString(w, sc.Path); err != nil {
		return xerrors.Errorf("failed to encode path: %w", err)
	}

	if err := WriteString(w, sc.ID); err != nil {
		return xerrors.Errorf("failed to encode ID: %w", err)
	}

	if sc.Size != nil {
		if _, err := w.Write([]byte{0x01}); err != nil {
			return xerrors.Errorf("failed to write size present byte: %w", err)
		}
		if err := WriteLE(w, *sc.Size); err != nil {
			return xerrors.Errorf("failed to write size: %w", err)
		}
	} else {
		if _, err := w.Write([]byte{0x00}); err != nil {
			return xerrors.Errorf("failed to write size absent byte: %w", err)
		}
	}

	if err := WriteLE(w, sc.RowsToDiscard); err != nil {
		return xerrors.Errorf("failed to write rows to discard: %w", err)
	}

	return nil
}

func EncodeLabels(w io.Writer, labels TAuxLabels) error {
	if err := WriteLE(w, uint64(len(labels.Labels))); err != nil {
		return xerrors.Errorf("failed to write number of labels: %w", err)
	}

	for i, label := range labels.Labels {
		if err := EncodeStoreConfig(w, label); err != nil {
			return xerrors.Errorf("failed to encode label %d: %w", i, err)
		}
	}

	return nil
}

func EncodeTAux(w io.Writer, taux TemporaryAux) error {
	if err := EncodeLabels(w, taux.Labels); err != nil {
		return xerrors.Errorf("failed to encode labels: %w", err)
	}

	if err := EncodeStoreConfig(w, taux.TreeDConfig); err != nil {
		return xerrors.Errorf("failed to encode treeDConfig: %w", err)
	}

	if err := EncodeStoreConfig(w, taux.TreeRConfig); err != nil {
		return xerrors.Errorf("failed to encode treeRConfig: %w", err)
	}

	if err := EncodeStoreConfig(w, taux.TreeCConfig); err != nil {
		return xerrors.Errorf("failed to encode treeCConfig: %w", err)
	}

	return nil
}
