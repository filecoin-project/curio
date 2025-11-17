package dealdata

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/robusthttp"
)

var rcs = robusthttp.NewRateCounters[uuid.UUID](robusthttp.MinAvgGlobalLogPeerRate(10, 1000))

// CustoreScheme is a special url scheme indicating that a data URL is an http url withing the curio storage system
const CustoreScheme = "custore"

type UrlPieceReader struct {
	Url     string
	Headers http.Header
	RawSize int64 // the exact number of bytes read, if we read more or less that's an error

	kind string

	RemoteEndpointReader *paths.Remote // Only used for .ReadRemote which issues http requests for internal /remote endpoints

	readSoFar int64
	closed    bool
	active    io.ReadCloser // auto-closed on EOF
}

func NewUrlReader(rmt *paths.Remote, p string, h http.Header, rs int64, kind string) *UrlPieceReader {
	return &UrlPieceReader{
		Url:     p,
		RawSize: rs,
		Headers: h,
		kind:    kind,

		RemoteEndpointReader: rmt,
	}
}

func (u *UrlPieceReader) initiateRequest() error {
	goUrl, err := url.Parse(u.Url)
	if err != nil {
		return xerrors.Errorf("failed to parse the URL: %w", err)
	}

	if goUrl.Scheme == CustoreScheme {
		if u.RemoteEndpointReader == nil {
			return xerrors.New("RemoteEndpoint is nil")
		}

		goUrl.Scheme = "http"
		u.active, err = u.RemoteEndpointReader.ReadRemote(context.Background(), goUrl.String(), 0, 0)
		if err != nil {
			return xerrors.Errorf("error reading remote (%s): %w", goUrl.String(), err)
		}
		return nil
	}

	if goUrl.Scheme != "https" && goUrl.Scheme != "http" {
		return xerrors.Errorf("URL scheme %s not supported", goUrl.Scheme)
	}

	rd := robusthttp.RobustGet(goUrl.String(), u.Headers, u.RawSize, func() *robusthttp.RateCounter {
		return rcs.Get(uuid.New())
	})

	/* if resp.StatusCode != 200 {
		limitedReader := io.LimitReader(resp.Body, 1024)
		respBodyBytes, readErr := io.ReadAll(limitedReader)
		closeErr := resp.Body.Close()
		sanitizedBody := sanitize(respBodyBytes)
		errMsg := fmt.Sprintf("non-200 response code: %s. Response body: <msg>%s</msg>", resp.Status, sanitizedBody)
		if readErr != nil && readErr != io.EOF {
			return xerrors.Errorf("%s. Error reading response body: %w", errMsg, readErr)
		}
		if closeErr != nil {
			return xerrors.Errorf("%s. Error closing response body: %w", errMsg, closeErr)
		}
		return xerrors.New(errMsg)
	}
	*/
	// Set 'active' to the response body
	u.active = rd
	return nil
}

func (u *UrlPieceReader) Read(p []byte) (n int, err error) {
	// Check if we have already read the required amount of data
	if u.readSoFar >= u.RawSize {
		return 0, io.EOF
	}

	// If 'active' is nil, initiate the HTTP request
	if u.active == nil {
		err := u.initiateRequest()
		if err != nil {
			return 0, err
		}
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

func (u *UrlPieceReader) ReadSoFar() int64 {
	return u.readSoFar
}

func (u *UrlPieceReader) Close() error {
	if !u.closed {
		u.closed = true

		_ = stats.RecordWithTags(context.Background(),
			[]tag.Mutator{tag.Upsert(kindKey, u.kind)},
			Measures.DataRead.M(u.readSoFar),
		)

		if u.active == nil {
			return nil
		}

		return u.active.Close()
	}

	return nil
}
