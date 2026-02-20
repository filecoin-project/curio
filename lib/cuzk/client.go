package cuzk

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var log = logging.Logger("cuzk")

// Client wraps a gRPC connection to the cuzk proving daemon.
// It maintains a long-lived connection and provides high-level
// methods for submitting proofs and querying daemon status.
type Client struct {
	addr string

	mu   sync.Mutex
	conn *grpc.ClientConn
	svc  ProvingEngineClient

	proveTimeout time.Duration
	maxPending   int
}

// NewClient creates a new cuzk client targeting the given address.
// The address can be a unix socket path (e.g., "unix:///run/curio/cuzk.sock")
// or a TCP address (e.g., "127.0.0.1:9820").
// Connection is established lazily on first use.
func NewClient(addr string, maxPending int, proveTimeout time.Duration) *Client {
	return &Client{
		addr:         addr,
		maxPending:   maxPending,
		proveTimeout: proveTimeout,
	}
}

// connect establishes the gRPC connection if not already connected.
func (c *Client) connect() (ProvingEngineClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.svc != nil {
		return c.svc, nil
	}

	target := c.addr
	if !strings.Contains(target, "://") && !strings.HasPrefix(target, "unix:") {
		// bare host:port, connect as TCP
		target = "dns:///" + target
	}

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(256<<20), grpc.MaxCallSendMsgSize(256<<20)),
	)
	if err != nil {
		return nil, xerrors.Errorf("connecting to cuzk daemon at %s: %w", c.addr, err)
	}

	c.conn = conn
	c.svc = NewProvingEngineClient(conn)

	log.Infow("connected to cuzk daemon", "addr", c.addr)
	return c.svc, nil
}

// Close tears down the gRPC connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.svc = nil
		return err
	}
	return nil
}

// Prove submits a proof request and blocks until the result is ready.
// This is a convenience wrapper around the gRPC Prove RPC.
func (c *Client) Prove(ctx context.Context, req *SubmitProofRequest) (*AwaitProofResponse, error) {
	svc, err := c.connect()
	if err != nil {
		return nil, err
	}

	if c.proveTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.proveTimeout)
		defer cancel()
	}

	resp, err := svc.Prove(ctx, &ProveRequest{Submit: req})
	if err != nil {
		return nil, xerrors.Errorf("cuzk Prove RPC: %w", err)
	}

	result := resp.Result
	if result == nil {
		return nil, xerrors.Errorf("cuzk Prove returned nil result")
	}

	switch result.Status {
	case AwaitProofResponse_COMPLETED:
		return result, nil
	case AwaitProofResponse_FAILED:
		return nil, xerrors.Errorf("cuzk proof failed: %s", result.ErrorMessage)
	case AwaitProofResponse_CANCELLED:
		return nil, xerrors.Errorf("cuzk proof cancelled")
	case AwaitProofResponse_TIMEOUT:
		return nil, xerrors.Errorf("cuzk proof timed out")
	default:
		return nil, xerrors.Errorf("cuzk proof unknown status: %v", result.Status)
	}
}

// GetStatus queries the daemon for current queue sizes and GPU status.
func (c *Client) GetStatus(ctx context.Context) (*GetStatusResponse, error) {
	svc, err := c.connect()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return svc.GetStatus(ctx, &GetStatusRequest{})
}

// HasCapacity checks whether the cuzk daemon has room for more proofs of the given kind.
// It queries GetStatus and compares pending counts against maxPending.
// Returns the number of additional tasks that can be accepted (0 means full).
func (c *Client) HasCapacity(ctx context.Context, kind ProofKind) (int, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return 0, xerrors.Errorf("querying cuzk status for backpressure: %w", err)
	}

	var totalPending uint32
	for _, q := range status.Queues {
		totalPending += q.Pending + q.InProgress
	}

	remaining := c.maxPending - int(totalPending)
	if remaining < 0 {
		remaining = 0
	}

	log.Debugw("cuzk capacity check",
		"kind", kind.String(),
		"totalPending", totalPending,
		"maxPending", c.maxPending,
		"remaining", remaining)

	return remaining, nil
}

// Enabled returns true if the client is configured (has a non-empty address).
func (c *Client) Enabled() bool {
	return c.addr != ""
}

// String returns a human-readable description of the client.
func (c *Client) String() string {
	if !c.Enabled() {
		return "cuzk(disabled)"
	}
	return fmt.Sprintf("cuzk(%s)", c.addr)
}
