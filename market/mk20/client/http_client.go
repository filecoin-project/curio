package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/market/mk20"
)

const MarketPath = "/market/mk20"

// HTTPClient is a thin wrapper around Curio Market 2.0 REST endpoints.
type HTTPClient struct {
	BaseURL          string
	HTTP             *http.Client
	AuthHeader       func(context.Context) (key string, value string, err error)
	AuthHeaderString string
}

// New returns a HTTPClient with sane defaults.
func New(baseURL string, opts ...Option) *HTTPClient {
	c := &HTTPClient{
		BaseURL: baseURL + MarketPath,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// --- options ---------------------------------------------------------------

type Option func(*HTTPClient)

func WithAuth(h func(context.Context) (string, string, error)) Option {
	return func(c *HTTPClient) { c.AuthHeader = h }
}

func WithAuthString(s string) Option {
	return func(c *HTTPClient) { c.AuthHeaderString = s }
}

// --- low‑level helper ------------------------------------------------------

func (c *HTTPClient) do(ctx context.Context, method, p string, body io.Reader, v any) *Error {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path.Clean("/"+p), body)
	if err != nil {
		return &Error{
			Status: 0,
			Error:  err,
		}
	}

	if c.AuthHeaderString != "" {
		if c.AuthHeader != nil {
			k, vHdr, err := c.AuthHeader(ctx)
			if err != nil {
				return &Error{
					Status: 0,
					Error:  err,
				}
			}
			req.Header.Set(k, vHdr)
		}
	} else {
		req.Header.Set("Authorization", c.AuthHeaderString)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return &Error{
			Status: 0,
			Error:  err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		msg, err := io.ReadAll(resp.Body)
		if err != nil {
			return &Error{Status: resp.StatusCode, Error: err}
		}
		return &Error{Status: resp.StatusCode, Message: string(msg)}
	}

	if v != nil {
		err = json.NewDecoder(resp.Body).Decode(v)
		if err != nil {
			return &Error{Status: resp.StatusCode, Error: err}
		}
	}
	return nil
}

// Error wraps non‑2xx responses.
type Error struct {
	Status  int
	Message string
	Error   error
}

func (e *Error) HError() error {
	return xerrors.Errorf("%s", fmt.Sprintf("curio: %d – %s", e.Status, e.Message))
}

// --- public methods (one per path) ----------------------------------------

// /contracts
func (c *HTTPClient) Contracts(ctx context.Context) ([]string, *Error) {
	var out []string
	err := c.do(ctx, http.MethodGet, "/contracts", nil, &out)
	return out, err
}

// /products
func (c *HTTPClient) Products(ctx context.Context) ([]string, *Error) {
	var out []string
	err := c.do(ctx, http.MethodGet, "/products", nil, &out)
	return out, err
}

// /sources
func (c *HTTPClient) Sources(ctx context.Context) ([]string, *Error) {
	var out []string
	err := c.do(ctx, http.MethodGet, "/sources", nil, &out)
	return out, err
}

// /status/{id}
func (c *HTTPClient) Status(ctx context.Context, id ulid.ULID) (*mk20.DealProductStatusResponse, *Error) {
	var out mk20.DealProductStatusResponse
	err := c.do(ctx, http.MethodGet, "/status/"+id.String(), nil, &out)
	return &out, err
}

// /store  (POST)
func (c *HTTPClient) Store(ctx context.Context, deal *mk20.Deal) *Error {
	b, merr := json.Marshal(deal)
	if merr != nil {
		return &Error{Status: 0, Error: xerrors.Errorf("failed to marshal deal: %w", merr)}
	}
	err := c.do(ctx, http.MethodPost, "/store", bytes.NewReader(b), nil)
	return err
}

// /update/{id}  (GET in spec – unusual, but honoured)
func (c *HTTPClient) Update(ctx context.Context, id ulid.ULID, deal *mk20.Deal) *Error {
	b, merr := json.Marshal(deal)
	if merr != nil {
		return &Error{Status: 0, Error: xerrors.Errorf("failed to marshal deal: %w", merr)}
	}
	err := c.do(ctx, http.MethodGet, "/update/"+id.String(), bytes.NewReader(b), nil)
	return err
}

// Serial upload (small files) – PUT /upload/{id}
func (c *HTTPClient) UploadSerial(ctx context.Context, id ulid.ULID, r io.Reader) *Error {
	err := c.do(ctx, http.MethodPut, "/upload/"+id.String(), r, nil)
	return err
}

// Finalize serial upload – POST /upload/{id}
func (c *HTTPClient) UploadSerialFinalize(ctx context.Context, id ulid.ULID, deal *mk20.Deal) *Error {
	var err *Error
	if deal != nil {
		b, merr := json.Marshal(deal)
		if merr != nil {
			return &Error{Status: 0, Error: xerrors.Errorf("failed to marshal deal: %w", merr)}
		}
		err = c.do(ctx, http.MethodPost, "/upload/"+id.String(), bytes.NewReader(b), nil)
	} else {
		err = c.do(ctx, http.MethodPost, "/upload/"+id.String(), nil, nil)
	}

	return err
}

// Chunked upload workflow ---------------------------------------------------

// POST /uploads/{id}
func (c *HTTPClient) UploadInit(ctx context.Context, id ulid.ULID, metadata *mk20.StartUpload) *Error {
	if metadata == nil {
		return &Error{Status: 0, Error: xerrors.Errorf("metadata is required")}
	}
	b, merr := json.Marshal(metadata)
	if merr != nil {
		return &Error{Status: 0, Error: xerrors.Errorf("failed to marshal deal: %w", merr)}
	}
	err := c.do(ctx, http.MethodPost, "/uploads/"+id.String(), bytes.NewReader(b), nil)
	return err
}

// PUT /uploads/{id}/{chunk}
func (c *HTTPClient) UploadChunk(ctx context.Context, id ulid.ULID, chunk int, data io.Reader) *Error {
	path := "/uploads/" + id.String() + "/" + strconv.Itoa(chunk)
	err := c.do(ctx, http.MethodPut, path, data, nil)
	return err
}

// GET /uploads/{id}
func (c *HTTPClient) UploadStatus(ctx context.Context, id ulid.ULID) (*mk20.UploadStatus, *Error) {
	var out mk20.UploadStatus
	err := c.do(ctx, http.MethodGet, "/uploads/"+id.String(), nil, &out)
	return &out, err
}

// POST /uploads/finalize/{id}
func (c *HTTPClient) UploadFinalize(ctx context.Context, id ulid.ULID, deal *mk20.Deal) *Error {
	var err *Error
	if deal != nil {
		b, merr := json.Marshal(deal)
		if merr != nil {
			return &Error{Status: 0, Error: xerrors.Errorf("failed to marshal deal: %w", merr)}
		}
		err = c.do(ctx, http.MethodPost, "/uploads/finalize/"+id.String(), bytes.NewReader(b), nil)
	} else {
		err = c.do(ctx, http.MethodPost, "/uploads/finalize/"+id.String(), nil, nil)
	}
	return err
}
