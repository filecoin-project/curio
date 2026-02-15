package remoteseal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/market/sealmarket"
)

// RSealClient is an HTTP client for calling remote seal provider endpoints.
type RSealClient struct {
	httpClient *http.Client
}

// NewRSealClient creates a new RSealClient with default timeout.
func NewRSealClient() *RSealClient {
	return &RSealClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// doPost performs a POST request to the given URL with a JSON body and decodes the JSON response.
func (c *RSealClient) doPost(ctx context.Context, url string, reqBody interface{}, respBody interface{}) error {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return xerrors.Errorf("marshaling request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return xerrors.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return xerrors.Errorf("performing request to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return xerrors.Errorf("unexpected status %d from %s: %s", resp.StatusCode, url, string(body))
	}

	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return xerrors.Errorf("decoding response from %s: %w", url, err)
		}
	}

	return nil
}

// doPostNoResponse performs a POST request expecting only a status code (no JSON body).
func (c *RSealClient) doPostNoResponse(ctx context.Context, url string, reqBody interface{}) error {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return xerrors.Errorf("marshaling request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return xerrors.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return xerrors.Errorf("performing request to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return xerrors.Errorf("unexpected status %d from %s: %s", resp.StatusCode, url, string(body))
	}

	return nil
}

// endpoint constructs a full URL from the provider base URL, the delegated seal path, and the endpoint name.
func endpoint(providerURL, ep string) string {
	return providerURL + sealmarket.DelegatedSealPath + ep
}

// CheckAvailable checks if the provider has an available slot.
// POST /remoteseal/delegated/v0/available
func (c *RSealClient) CheckAvailable(ctx context.Context, providerURL, token string) (*sealmarket.AvailableResponse, error) {
	reqBody := sealmarket.AuthorizeRequest{
		PartnerToken: token,
	}
	var resp sealmarket.AvailableResponse
	if err := c.doPost(ctx, endpoint(providerURL, "available"), &reqBody, &resp); err != nil {
		return nil, xerrors.Errorf("checking available: %w", err)
	}
	return &resp, nil
}

// SendOrder sends a seal order to the provider.
// POST /remoteseal/delegated/v0/order
func (c *RSealClient) SendOrder(ctx context.Context, providerURL, token string, req *sealmarket.OrderRequest) (*sealmarket.OrderResponse, error) {
	req.PartnerToken = token
	var resp sealmarket.OrderResponse
	if err := c.doPost(ctx, endpoint(providerURL, "order"), req, &resp); err != nil {
		return nil, xerrors.Errorf("sending order: %w", err)
	}
	return &resp, nil
}

// GetStatus polls the provider for the status of a remote seal job.
// POST /remoteseal/delegated/v0/status
func (c *RSealClient) GetStatus(ctx context.Context, providerURL, token string, req *sealmarket.StatusRequest) (*sealmarket.StatusResponse, error) {
	req.PartnerToken = token
	var resp sealmarket.StatusResponse
	if err := c.doPost(ctx, endpoint(providerURL, "status"), req, &resp); err != nil {
		return nil, xerrors.Errorf("getting status: %w", err)
	}
	return &resp, nil
}

// SendCommit1 sends the C1 seed to the provider and receives C1 output.
// POST /remoteseal/delegated/v0/commit1
func (c *RSealClient) SendCommit1(ctx context.Context, providerURL, token string, req *sealmarket.Commit1Request) (*sealmarket.Commit1Response, error) {
	req.PartnerToken = token
	var resp sealmarket.Commit1Response
	if err := c.doPost(ctx, endpoint(providerURL, "commit1"), req, &resp); err != nil {
		return nil, xerrors.Errorf("sending commit1: %w", err)
	}
	return &resp, nil
}

// SendFinalize tells the provider that layers can be dropped.
// POST /remoteseal/delegated/v0/finalize
func (c *RSealClient) SendFinalize(ctx context.Context, providerURL, token string, req *sealmarket.FinalizeRequest) error {
	req.PartnerToken = token
	if err := c.doPostNoResponse(ctx, endpoint(providerURL, "finalize"), req); err != nil {
		return xerrors.Errorf("sending finalize: %w", err)
	}
	return nil
}

// SendCleanup tells the provider to begin full cleanup of all sector data.
// POST /remoteseal/delegated/v0/cleanup
func (c *RSealClient) SendCleanup(ctx context.Context, providerURL, token string, req *sealmarket.CleanupRequest) error {
	req.PartnerToken = token
	if err := c.doPostNoResponse(ctx, endpoint(providerURL, "cleanup"), req); err != nil {
		return xerrors.Errorf("sending cleanup: %w", err)
	}
	return nil
}
