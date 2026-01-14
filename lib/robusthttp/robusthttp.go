// Package robusthttp implements automatic retry logic for HTTP transfers that
// can recover from TCP streams that become "stuck" at extremely low throughput.
//
// # The Problem: TCP Streams Getting Stuck
//
// On multi-gigabit network connections, especially when multiple TCP flows compete
// for bandwidth, individual streams can become trapped in a state where they transmit
// only a few bytes per second despite having abundant link capacity available. This
// phenomenon occurs both on local networks and over the internet, with long-haul
// cross-continent routes being particularly susceptible.
//
// # Why TCP Streams Get Stuck
//
// TCP's congestion control algorithms (Cubic, BBR, Reno, etc.) attempt to find the
// available bandwidth by probing: they increase the sending rate until packet loss
// occurs, then back off. When multiple flows compete, several failure modes emerge:
//
//  1. Congestion Window Collapse: When packet loss occurs, TCP drastically reduces
//     its congestion window (cwnd). On high-bandwidth, high-latency paths (large BDP),
//     recovery can take minutes because the additive increase phase grows cwnd by
//     only ~1 MSS per RTT. A flow that loses packets while competing with aggressive
//     flows may never recover its fair share.
//
//  2. Bufferbloat-Induced Starvation: When network buffers are oversized, competing
//     flows can fill queues with thousands of packets. A flow that experiences loss
//     during this state backs off, while the competing flows continue to hold queue
//     space. The backed-off flow's packets consistently arrive at the tail of deep
//     queues, experiencing enormous RTTs (seconds) and further timeouts.
//
//  3. Retransmission Timeout (RTO) Spirals: After loss, if the retransmitted packets
//     are also lost (common under congestion), RTO doubles exponentially (up to 60-120
//     seconds). The flow becomes effectively dormant while competing flows absorb the
//     freed capacity. The standard RTO minimum of 200ms-1s further delays recovery.
//
//  4. Receive Window Limitations: On some paths, especially through middleboxes or
//     with asymmetric routing, receive window updates can be delayed or lost, causing
//     the sender to stall waiting for window space that the receiver has already
//     advertised but the sender never received.
//
//  5. Path Asymmetry on Long Routes: Cross-continent connections often traverse
//     different physical paths for forward and return traffic. ACK packets returning
//     on congested reverse paths can be delayed or lost, starving the sender of
//     acknowledgments and causing spurious retransmissions or window stalls.
//
// # Why Restarting Helps
//
// Establishing a new TCP connection provides a fresh start that bypasses many of
// these stuck states:
//
//   - Fresh Congestion Window: New connections start with initial cwnd (typically
//     10 MSS) and can use slow-start to rapidly probe for available bandwidth,
//     rather than being trapped in a collapsed cwnd with linear recovery.
//
//   - Queue Position Reset: The new flow's packets enter queues without the
//     accumulated RTT debt of the stuck flow. On paths with AQM (Active Queue
//     Management like fq_codel), new flows get their own queue slot.
//
//   - RTO Timer Reset: The new connection has a fresh RTO estimate based on
//     current RTT measurements, rather than an exponentially backed-off timer.
//
//   - Potential Path Diversity: ECMP (Equal-Cost Multi-Path) routing may assign
//     the new flow to a different physical path, avoiding congested links entirely.
//
//   - Updated Receive Window: The receiver re-advertises its current window,
//     resolving any stale window state from the old connection.
//
// # How This Package Works
//
// RobustGet creates an HTTP reader that monitors transfer rate and automatically
// recovers from stuck connections:
//
//  1. Rate Monitoring: Each read operation is tracked. Periodically (every few
//     seconds), the transfer rate is computed and compared against a minimum
//     threshold that accounts for the number of concurrent transfers.
//
//  2. Adaptive Threshold: The minimum acceptable rate scales with the number of
//     active transfers (using logarithmic scaling) and respects the expected link
//     capacity. A single transfer must sustain higher throughput than when bandwidth
//     is split among many.
//
//  3. Connection Restart: When rate drops below threshold, the current connection
//     is closed and a new HTTP request is issued with a Range header starting at
//     the last successfully received byte offset.
//
//  4. Retry Limits: To prevent infinite retry loops on truly failed servers, a
//     maximum retry count (15 attempts) is enforced before returning an error.
//
// This approach is particularly effective for large file transfers (sectors, pieces)
// where the cost of restarting a 32GiB transfer from 10% progress is acceptable
// compared to waiting potentially hours for a stuck connection to recover (if it
// ever does).
package robusthttp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"
)

var log = logging.Logger("robusthttp")

type robustHttpResponse struct {
	getRC func() *RateCounter

	url     string
	headers http.Header

	cur             io.Reader
	curCloser       io.Closer
	atOff, dataSize int64

	// metrics counters
	retries   int64
	errs      int64
	finalized int32
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
				r.errs++
				r.retries++
				continue
			}
		}

		n, err = r.cur.Read(p)
		if err == io.EOF {
			_ = r.curCloser.Close()
			r.cur = nil
			log.Errorw("EOF reached in Read", "bytesRead", n)
			r.finalize(false)
			return n, err
		}
		if err != nil {
			log.Errorw("Read error", "error", err)
			_ = r.curCloser.Close()
			r.cur = nil

			if n > 0 {
				return n, nil
			}

			lastErr = err
			log.Errorw("robust http read error, will retry", "err", err, "i", i)
			r.errs++
			r.retries++
			continue
		}
		if n == 0 {
			_ = r.curCloser.Close()
			r.cur = nil
			log.Errorw("Read 0 bytes", "bytesRead", n)
			return 0, xerrors.Errorf("read 0 bytes")
		}

		return n, nil
	}

	r.finalize(true)
	return 0, xerrors.Errorf("http read failed after %d retries: lastErr: %w", maxRetryCount, lastErr)
}

func (r *robustHttpResponse) Close() error {
	log.Debug("Entering function Close")
	r.finalize(false)
	if r.curCloser != nil {
		return r.curCloser.Close()
	}
	log.Warnw("Exiting Close with no current closer")
	return nil
}

func (r *robustHttpResponse) finalize(failed bool) {
	if atomic.CompareAndSwapInt32(&r.finalized, 0, 1) {
		recordRequestClosed(r.atOff, r.retries, r.errs)
		if failed {
			recordReadFailure()
		}
		decActiveTransfers()
	}
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

	req.Header = r.headers.Clone()
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.atOff, r.dataSize-1))

	log.Debugw("Before sending HTTP request", "url", r.url, "cr", fmt.Sprintf("bytes=%d-%d", r.atOff, r.dataSize))
	resp, err := client.Do(req)
	if err != nil {
		log.Errorw("Error in client.Do", "error", err)
		return xerrors.Errorf("do request: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		if r.atOff > 0 {
			_ = resp.Body.Close()
			return xerrors.Errorf("server ignored range header (got 200 OK, expected 206 Partial Content)")
		}
		// 200 OK is fine if we requested from byte 0
	} else if resp.StatusCode != http.StatusPartialContent {
		log.Errorw("Unexpected HTTP status", "status", resp.StatusCode)
		_ = resp.Body.Close()
		return xerrors.Errorf("http status: %d", resp.StatusCode)
	}

	if nc == nil {
		log.Errorw("Connection is nil after client.Do")
		_ = resp.Body.Close()
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
		rc.Release()
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
	recordRequestStarted()
	incActiveTransfers()

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
