package robusthttp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

var log = logging.Logger("robusthttp")

type robustHttpResponse struct {
	getRC func() *RateCounter

	url string
	headers http.Header

	cur             io.Reader
	curCloser       io.Closer
	atOff, dataSize int64
}

var maxRetryCount = 15

func (r *robustHttpResponse) Read(p []byte) (n int, err error) {
	defer func() {
		r.atOff += int64(n)
	}()

	var lastErr error

	for i := 0; i < maxRetryCount; i++ {
		if r.cur == nil {
			log.Debugw("Current response is nil, starting new request")

			if err := r.startReq(); err != nil {
				log.Errorw("Error in startReq", "error", err, "i", i)
				time.Sleep(1 * time.Second)
				lastErr = err
				continue
			}
		}

		n, err = r.cur.Read(p)
		if err == io.EOF {
			r.curCloser.Close()
			r.cur = nil
			log.Errorw("EOF reached in Read", "bytesRead", n)
			return n, err
		}
		if err != nil {
			log.Errorw("Read error", "error", err)
			r.curCloser.Close()
			r.cur = nil

			if n > 0 {
				return n, nil
			}

			lastErr = err
			log.Errorw("robust http read error, will retry", "err", err, "i", i)
			continue
		}
		if n == 0 {
			r.curCloser.Close()
			r.cur = nil
			log.Errorw("Read 0 bytes", "bytesRead", n)
			return 0, xerrors.Errorf("read 0 bytes")
		}

		return n, nil
	}

	return 0, xerrors.Errorf("http read failed after %d retries: lastErr: %w", maxRetryCount, lastErr)
}

func (r *robustHttpResponse) Close() error {
	log.Debug("Entering function Close")
	if r.curCloser != nil {
		return r.curCloser.Close()
	}
	log.Warnw("Exiting Close with no current closer")
	return nil
}

func (r *robustHttpResponse) startReq() error {
	log.Debugw("Entering function startReq", "url", r.url)
	dialer := &net.Dialer{
		Timeout: 20 * time.Second,
	}

	var nc net.Conn

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				log.Debugw("DialContext called", "network", network, "addr", addr)
				conn, err := dialer.DialContext(ctx, network, addr)
				if err != nil {
					log.Errorw("DialContext error", "error", err)
					return nil, err
				}

				nc = conn

				// Set a deadline for the whole operation, including reading the response
				if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
					log.Errorw("SetReadDeadline error", "error", err)
					return nil, xerrors.Errorf("set deadline: %w", err)
				}

				return conn, nil
			},
		},
	}

	req, err := http.NewRequest("GET", r.url, nil)
	if err != nil {
		log.Errorw("failed to create request", "err", err)
		return xerrors.Errorf("failed to create request")
	}

	req.Header = r.headers
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.atOff, r.dataSize-1))

	log.Debugw("Before sending HTTP request", "url", r.url, "cr", fmt.Sprintf("bytes=%d-%d", r.atOff, r.dataSize))
	resp, err := client.Do(req)
	if err != nil {
		log.Errorw("Error in client.Do", "error", err)
		return xerrors.Errorf("do request: %w", err)
	}

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		log.Errorw("Unexpected HTTP status", "status", resp.StatusCode)
		resp.Body.Close()
		return xerrors.Errorf("http status: %d", resp.StatusCode)
	}

	if nc == nil {
		log.Errorw("Connection is nil after client.Do")
		resp.Body.Close()
		return xerrors.Errorf("nc was nil")
	}

	var reqTxIdleTimeout = 4 * time.Second

	dlRead := &readerDeadliner{
		Reader:      resp.Body,
		setDeadline: nc.SetReadDeadline,
	}

	rc := r.getRC()
	rw := NewRateEnforcingReader(dlRead, rc, reqTxIdleTimeout)

	r.cur = rw
	r.curCloser = funcCloser(func() error {
		log.Debugw("Closing response body")
		rc.release()
		return resp.Body.Close()
	})

	log.Debugw("Exiting startReq with success")
	return nil
}

type funcCloser func() error

func (fc funcCloser) Close() error {
	return fc()
}

func RobustGet(url string, headers http.Header, dataSize int64, rcf func() *RateCounter) io.ReadCloser {
	return &robustHttpResponse{
		getRC:    rcf,
		url:      url,
		dataSize: dataSize,
		headers:  headers,
	}
}

type readerDeadliner struct {
	io.Reader
	setDeadline func(time.Time) error
}

func (rd *readerDeadliner) SetReadDeadline(t time.Time) error {
	return rd.setDeadline(t)
}