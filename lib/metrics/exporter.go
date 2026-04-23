package metrics

import (
	"net/http"
	"sync"

	"contrib.go.opencensus.io/exporter/prometheus"
	promclient "github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

var (
	exporterOnce sync.Once
	exporterHTTP http.Handler
)

func Exporter() http.Handler {
	exporterOnce.Do(func() {
		// Prometheus globals are exposed as interfaces, but the prometheus
		// OpenCensus exporter expects a concrete *Registry. The concrete type of
		// the globals are actually *Registry, so we downcast them, staying
		// defensive in case things change under the hood.
		registry, ok := promclient.DefaultRegisterer.(*promclient.Registry)
		if !ok {
			log.Warnf("failed to export default prometheus registry; some metrics will be unavailable; unexpected type: %T", promclient.DefaultRegisterer)
		}
		exporter, err := prometheus.NewExporter(prometheus.Options{
			Registry:  registry,
			Namespace: "curio",
		})
		if err != nil {
			log.Errorf("could not create the prometheus stats exporter: %v", err)
			exporterHTTP = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "prometheus exporter unavailable", http.StatusServiceUnavailable)
			})
			return
		}
		exporterHTTP = exporter
	})

	return exporterHTTP
}
