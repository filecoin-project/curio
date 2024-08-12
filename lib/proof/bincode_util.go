package proof

import (
	"encoding/binary"
	"io"

	"golang.org/x/xerrors"
)

func ReadLE[T any](r io.Reader) (T, error) {
	var out T
	err := binary.Read(r, binary.LittleEndian, &out)
	return out, err
}

func ReadString(r io.Reader) (string, error) {
	l, err := ReadLE[uint64](r)
	if err != nil {
		return "", xerrors.Errorf("failed to read string length: %w", err)
	}

	buf := make([]byte, l)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", xerrors.Errorf("failed to read string: %w", err)
	}

	return string(buf), nil
}

func WriteLE[T any](w io.Writer, data T) error {
	return binary.Write(w, binary.LittleEndian, data)
}

func WriteString(w io.Writer, s string) error {
	if err := WriteLE(w, uint64(len(s))); err != nil {
		return xerrors.Errorf("failed to write string length: %w", err)
	}
	if _, err := w.Write([]byte(s)); err != nil {
		return xerrors.Errorf("failed to write string: %w", err)
	}
	return nil
}
