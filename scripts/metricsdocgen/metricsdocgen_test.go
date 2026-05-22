package main

import "testing"

func TestOpenCensusViewAggregationDefinesPrometheusTypeAndLabels(t *testing.T) {
	source := []byte(`
package sample

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	callTag, _ = tag.NewKey("call")
	latency = stats.Float64("psvc_latency_seconds", "latency", stats.UnitSeconds)
	total = stats.Int64("psvc_total", "total", stats.UnitDimensionless)
	current = stats.Int64("psvc_current", "current", stats.UnitDimensionless)
)

func init() {
	_ = view.Register(
		&view.View{Measure: latency, Aggregation: view.Distribution(1, 2), TagKeys: []tag.Key{callTag}},
		&view.View{Measure: total, Aggregation: view.Sum()},
		&view.View{Measure: current, Aggregation: view.LastValue()},
	)
}
`)

	metrics := metricMap(parseMetricsFromSource("tasks/proofshare/metrics.go", source))

	assertMetric(t, metrics, "curio_psvc_latency_seconds", "Histogram", []string{"call"})
	assertMetric(t, metrics, "curio_psvc_total", "Counter", nil)
	assertMetric(t, metrics, "curio_psvc_current", "Gauge", nil)
}

func TestOpenCensusExporterNameMatchesNamespaceAndSanitization(t *testing.T) {
	source := []byte(`
package sample

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
)

var (
	withSlash = stats.Int64("http/request_count", "requests", stats.UnitDimensionless)
	withPrefix = stats.Int64("curio_cachedreader_cache_hits", "hits", stats.UnitDimensionless)
)

func init() {
	_ = view.Register(
		&view.View{Measure: withSlash, Aggregation: view.Sum()},
		&view.View{Measure: withPrefix, Aggregation: view.Sum()},
	)
}
`)

	metrics := metricMap(parseMetricsFromSource("market/retrieval/remoteblockstore/metric.go", source))

	assertMetric(t, metrics, "curio_http_request_count", "Counter", nil)
	assertMetric(t, metrics, "curio_curio_cachedreader_cache_hits", "Counter", nil)
}

func TestViewVariablesAndAggregationVariablesAreResolved(t *testing.T) {
	source := []byte(`
package sample

import (
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var pathKey, _ = tag.NewKey("path")
var defaultDistribution = view.Distribution(1, 5)
var latency = stats.Float64("http/latency_ms", "latency", stats.UnitMilliseconds)
var latencyView = &view.View{
	Measure: latency,
	Aggregation: defaultDistribution,
	TagKeys: []tag.Key{pathKey},
}

func init() {
	_ = view.Register(latencyView)
}
`)

	metrics := metricMap(parseMetricsFromSource("market/retrieval/remoteblockstore/metric.go", source))

	assertMetric(t, metrics, "curio_http_latency_ms", "Histogram", []string{"path"})
}

func TestRegisteredExternalLotusViewsAreDocumented(t *testing.T) {
	source := []byte(`
package sample

import (
	"go.opencensus.io/stats/view"
	lm "github.com/filecoin-project/lotus/metrics"
)

func init() {
	_ = view.Register(
		lm.DagStorePRInitCountView,
		lm.DagStorePRBytesRequestedView,
	)
}
`)

	metrics := metricMap(parseMetricsFromSource("market/retrieval/remoteblockstore/metric.go", source))

	assertMetric(t, metrics, "curio_dagstore_pr_init_count", "Counter", []string{"network"})
	assertMetric(t, metrics, "curio_dagstore_pr_requested_bytes", "Counter", []string{"pr_type", "network"})
}

func TestDirectPrometheusVecUsesPrometheusTypeAndLabels(t *testing.T) {
	source := []byte(`
package sample

import "github.com/prometheus/client_golang/prometheus"

var retries = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name: "curio_psvc_retry_count",
	Help: "retries",
}, []string{"call"})
`)

	metrics := metricMap(parseMetricsFromSource("tasks/proofshare/metrics.go", source))

	assertMetric(t, metrics, "curio_psvc_retry_count", "Histogram", []string{"call"})
}

func TestCategoryFromPathUsesLongestPrefix(t *testing.T) {
	key, name := categoryFromPath("tasks/sealsupra/metrics.go")
	if key != "15_Batching Metrics" {
		t.Fatalf("key = %s, want 15_Batching Metrics", key)
	}
	if name != "Batching Metrics" {
		t.Fatalf("name = %s, want Batching Metrics", name)
	}
}

func metricMap(metrics []Metric) map[string]Metric {
	out := make(map[string]Metric, len(metrics))
	for _, metric := range metrics {
		out[metric.Name] = metric
	}
	return out
}

func assertMetric(t *testing.T, metrics map[string]Metric, name, typ string, labels []string) {
	t.Helper()

	metric, ok := metrics[name]
	if !ok {
		t.Fatalf("missing metric %s in %#v", name, metrics)
	}
	if metric.Type != typ {
		t.Fatalf("metric %s type = %s, want %s", name, metric.Type, typ)
	}
	if len(metric.Labels) != len(labels) {
		t.Fatalf("metric %s labels = %#v, want %#v", name, metric.Labels, labels)
	}
	for i := range labels {
		if metric.Labels[i] != labels[i] {
			t.Fatalf("metric %s labels = %#v, want %#v", name, metric.Labels, labels)
		}
	}
}
