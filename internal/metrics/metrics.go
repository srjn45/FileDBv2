// Package metrics provides Prometheus instrumentation for FileDB.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// CollectionStats is the minimal stats snapshot used by DBCollector.
type CollectionStats struct {
	Name         string
	RecordCount  uint64
	SegmentCount uint64
}

// Metrics holds all Prometheus instruments for FileDB.
type Metrics struct {
	reg                prometheus.Registerer
	CompactionTotal    *prometheus.CounterVec
	CompactionDuration *prometheus.HistogramVec
	GRPCDuration       *prometheus.HistogramVec
}

// New creates a Metrics and registers all instruments with reg.
// Pass prometheus.DefaultRegisterer for production use.
func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{reg: reg}

	m.CompactionTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "filedb_compaction_runs_total",
		Help: "Total number of compaction runs per collection.",
	}, []string{"collection"})

	m.CompactionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "filedb_compaction_duration_seconds",
		Help:    "Duration of compaction runs in seconds.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"collection"})

	m.GRPCDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "filedb_grpc_request_duration_seconds",
		Help:    "Duration of gRPC unary requests in seconds.",
		Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	}, []string{"method", "code"})

	reg.MustRegister(m.CompactionTotal, m.CompactionDuration, m.GRPCDuration)
	return m
}

// ObserveCompaction records one completed compaction run.
func (m *Metrics) ObserveCompaction(collection string, dur time.Duration) {
	m.CompactionTotal.WithLabelValues(collection).Inc()
	m.CompactionDuration.WithLabelValues(collection).Observe(dur.Seconds())
}

// ObserveGRPC records one completed gRPC unary request.
func (m *Metrics) ObserveGRPC(method, code string, dur time.Duration) {
	m.GRPCDuration.WithLabelValues(method, code).Observe(dur.Seconds())
}

// DBCollector is a prometheus.Collector that emits per-collection record and
// segment gauges by calling statsFunc at every scrape.
type DBCollector struct {
	statsFunc    func() []CollectionStats
	recordsDesc  *prometheus.Desc
	segmentsDesc *prometheus.Desc
}

// NewDBCollector returns a DBCollector backed by statsFunc and registers it
// with reg.
func NewDBCollector(reg prometheus.Registerer, statsFunc func() []CollectionStats) *DBCollector {
	c := &DBCollector{
		statsFunc: statsFunc,
		recordsDesc: prometheus.NewDesc(
			"filedb_collection_records_total",
			"Current number of live records in the collection.",
			[]string{"collection"}, nil,
		),
		segmentsDesc: prometheus.NewDesc(
			"filedb_collection_segments_total",
			"Current number of segment files in the collection.",
			[]string{"collection"}, nil,
		),
	}
	reg.MustRegister(c)
	return c
}

// Describe implements prometheus.Collector.
func (c *DBCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.recordsDesc
	ch <- c.segmentsDesc
}

// Collect implements prometheus.Collector.
func (c *DBCollector) Collect(ch chan<- prometheus.Metric) {
	for _, s := range c.statsFunc() {
		ch <- prometheus.MustNewConstMetric(
			c.recordsDesc, prometheus.GaugeValue,
			float64(s.RecordCount), s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			c.segmentsDesc, prometheus.GaugeValue,
			float64(s.SegmentCount), s.Name,
		)
	}
}

// Handler returns an http.Handler that serves the Prometheus metrics page.
// Pass prometheus.DefaultGatherer for the default registry.
func Handler(gatherer prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
}
