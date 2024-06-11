package dealdata

import (
	"io"
	"net/http"

	"golang.org/x/xerrors"
)

type UrlPieceReader struct {
	Url     string
	RawSize int64 // the exact number of bytes read, if we read more or less that's an error

	readSoFar int64
	closed    bool
	active    io.ReadCloser // auto-closed on EOF
}

func (u *UrlPieceReader) Read(p []byte) (n int, err error) {
	// Check if we have already read the required amount of data
	if u.readSoFar >= u.RawSize {
		return 0, io.EOF
	}

	// If 'active' is nil, initiate the HTTP request
	if u.active == nil {
		resp, err := http.Get(u.Url)
		if err != nil {
			return 0, err
		}

		// Set 'active' to the response body
		u.active = resp.Body
	}

	// Calculate the maximum number of bytes we can read without exceeding RawSize
	toRead := u.RawSize - u.readSoFar
	if int64(len(p)) > toRead {
		p = p[:toRead]
	}

	n, err = u.active.Read(p)

	// Update the number of bytes read so far
	u.readSoFar += int64(n)

	// If the number of bytes read exceeds RawSize, return an error
	if u.readSoFar > u.RawSize {
		return n, xerrors.New("read beyond the specified RawSize")
	}

	// If EOF is reached, close the reader
	if err == io.EOF {
		cerr := u.active.Close()
		u.closed = true
		if cerr != nil {
			log.Errorf("error closing http piece reader: %s", cerr)
		}

		// if we're below the RawSize, return an unexpected EOF error
		if u.readSoFar < u.RawSize {
			log.Errorw("unexpected EOF", "readSoFar", u.readSoFar, "rawSize", u.RawSize, "url", u.Url)
			return n, io.ErrUnexpectedEOF
		}
	}

	return n, err
}

func (u *UrlPieceReader) Close() error {
	if !u.closed {
		u.closed = true
		return u.active.Close()
	}

	return nil
}
