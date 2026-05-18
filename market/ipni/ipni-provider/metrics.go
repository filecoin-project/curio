package ipni_provider

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

type metricResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *metricResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *metricResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *metricResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

var (
	ipniProviderHTTPRequestBuckets = []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000}
	ipniAnnounceRoundTripBuckets   = []float64{10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000}

	providerTag, _ = tag.NewKey("provider")
	contentTag, _  = tag.NewKey("content")
	statusTag, _   = tag.NewKey("status")
	resultTag, _   = tag.NewKey("result")

	ipniProviderHTTPRequests = stats.Int64(
		"ipni_provider_http_requests_total",
		"Total number of inbound IPNI provider HTTP requests.",
		stats.UnitDimensionless,
	)
	ipniProviderHTTPRequestDuration = stats.Float64(
		"ipni_provider_http_request_milliseconds",
		"Duration of inbound IPNI provider HTTP requests in milliseconds.",
		stats.UnitMilliseconds,
	)
	ipniAnnounceAttempts = stats.Int64(
		"ipni_announce_attempts_total",
		"Total number of IPNI direct announce attempts.",
		stats.UnitDimensionless,
	)
	ipniAnnounceHTTPRoundTripDuration = stats.Float64(
		"ipni_announce_http_roundtrip_milliseconds",
		"Duration of outbound IPNI announce HTTP round trips in milliseconds.",
		stats.UnitMilliseconds,
	)
)

func init() {
	err := view.Register(
		&view.View{
			Measure:     ipniProviderHTTPRequests,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{providerTag, contentTag, statusTag},
		},
		&view.View{
			Measure:     ipniProviderHTTPRequestDuration,
			Aggregation: view.Distribution(ipniProviderHTTPRequestBuckets...),
			TagKeys:     []tag.Key{providerTag, contentTag, statusTag},
		},
		&view.View{
			Measure:     ipniAnnounceAttempts,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{providerTag, resultTag},
		},
		&view.View{
			Measure:     ipniAnnounceHTTPRoundTripDuration,
			Aggregation: view.Distribution(ipniAnnounceRoundTripBuckets...),
			TagKeys:     []tag.Key{providerTag, statusTag},
		},
	)
	if err != nil {
		panic(err)
	}
}

func recordProviderHTTPRequest(provider, content string, status int, took time.Duration) {
	statusLabel := strconv.Itoa(status)
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(providerTag, provider),
		tag.Upsert(contentTag, content),
		tag.Upsert(statusTag, statusLabel),
	},
		ipniProviderHTTPRequests.M(1),
		ipniProviderHTTPRequestDuration.M(float64(took)/float64(time.Millisecond)),
	)
}

func recordAnnounceAttempt(provider, result string) {
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(providerTag, provider),
		tag.Upsert(resultTag, result),
	}, ipniAnnounceAttempts.M(1))
}

func observeAnnounceHTTPRoundTrip(provider, status string, took time.Duration) {
	_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
		tag.Upsert(providerTag, provider),
		tag.Upsert(statusTag, status),
	}, ipniAnnounceHTTPRoundTripDuration.M(float64(took)/float64(time.Millisecond)))
}
