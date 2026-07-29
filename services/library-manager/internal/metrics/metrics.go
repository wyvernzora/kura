// Package metrics owns the library-manager's Prometheus registry.
// Mirrors services/release-indexer/internal/metrics in shape: one
// Metrics bundle per process, its own registry (never the global one),
// exposed on a dedicated listener so a NetworkPolicy that permits a
// scrape does not also permit API calls.
package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

const namespace = "kura_library"

// Metrics is the library-manager's metric bundle. All methods are
// nil-safe so transports and the jobs registry can hold an optional
// *Metrics without guarding every call site.
type Metrics struct {
	handler http.Handler
	reg     *prometheus.Registry

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec

	jobsRunning prometheus.Gauge
	jobsTotal   *prometheus.CounterVec
	jobDuration *prometheus.HistogramVec
}

// New builds the registry with build info, Go/process collectors, HTTP
// metrics, and job metrics. Index gauges attach later via ObserveIndex
// once the index exists.
func New(version string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	auto := promauto.With(reg)
	auto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "Build metadata.",
	}, []string{"version"}).WithLabelValues(version).Set(1)
	return &Metrics{
		handler: promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
		reg:     reg,
		httpRequests: auto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests by routed pattern.",
		}, []string{"method", "route", "status"}),
		httpDuration: auto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "route"}),
		jobsRunning: auto.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "jobs",
			Name:      "running",
			Help:      "Async jobs currently running.",
		}),
		jobsTotal: auto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "jobs",
			Name:      "total",
			Help:      "Terminal async jobs by kind and state.",
		}, []string{"kind", "state"}),
		jobDuration: auto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "jobs",
			Name:      "duration_seconds",
			Help:      "Async job wall-clock duration.",
			// Jobs span sub-second index touches to multi-minute
			// library-wide scans on NFS.
			Buckets: []float64{0.1, 0.5, 1, 5, 15, 60, 300, 900, 3600},
		}, []string{"kind"}),
	}
}

// Handler serves the Prometheus exposition endpoint.
func (m *Metrics) Handler() http.Handler { return m.handler }

// WrapHTTP records request count + duration for every request. The
// route label is the ServeMux pattern that matched (r.Pattern, set by
// the mux during ServeHTTP), so cardinality stays bounded by the
// registered route table; unmatched requests collapse to "other".
func (m *Metrics) WrapHTTP(next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		route := routeLabel(r.Pattern)
		m.httpRequests.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
		m.httpDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// routeLabel strips the "METHOD " prefix ServeMux patterns carry; the
// method is its own label.
func routeLabel(pattern string) string {
	if pattern == "" {
		return "other"
	}
	if _, path, ok := strings.Cut(pattern, " "); ok {
		return path
	}
	return pattern
}

// JobStarted / JobTerminal are the jobs.Config hooks.
func (m *Metrics) JobStarted(string) {
	if m == nil {
		return
	}
	m.jobsRunning.Inc()
}

func (m *Metrics) JobTerminal(kind, state string, elapsed time.Duration) {
	if m == nil {
		return
	}
	m.jobsRunning.Dec()
	m.jobsTotal.WithLabelValues(kind, state).Inc()
	m.jobDuration.WithLabelValues(kind).Observe(elapsed.Seconds())
}

// SeriesFacts is the slice of one index row the scrape-time collector
// needs. Local shape so this package doesn't couple to the index's
// storage types.
type SeriesFacts struct {
	Status      string
	Airing      bool
	Resolutions []string
	Sources     []string
}

// ObserveIndex registers gauges computed from the live in-memory index
// at scrape time — no disk I/O. Called once at wiring after the index
// exists.
func (m *Metrics) ObserveIndex(rebuilding func() bool, rows func() []SeriesFacts) {
	if m == nil {
		return
	}
	m.reg.MustRegister(&indexCollector{rebuilding: rebuilding, rows: rows})
}

type indexCollector struct {
	rebuilding func() bool
	rows       func() []SeriesFacts
}

var (
	indexRebuildingDesc = prometheus.NewDesc(namespace+"_index_rebuilding",
		"1 while the library index is rebuilding, else 0.", nil, nil)
	indexSeriesDesc = prometheus.NewDesc(namespace+"_index_series",
		"Series currently tracked in the library index.", nil, nil)
	seriesStatusDesc = prometheus.NewDesc(namespace+"_series_status",
		"Series by rolled-up list status.", []string{"status"}, nil)
	seriesAiringDesc = prometheus.NewDesc(namespace+"_series_airing",
		"Series currently observed as airing (independent of status).", nil, nil)
	seriesResolutionDesc = prometheus.NewDesc(namespace+"_series_resolution",
		"Series with at least one active file at this resolution. A series counts once per distinct resolution, so the sum can exceed the series total.",
		[]string{"resolution"}, nil)
	seriesSourceDesc = prometheus.NewDesc(namespace+"_series_source",
		"Series with at least one active file from this source. A series counts once per distinct source, so the sum can exceed the series total.",
		[]string{"source"}, nil)
)

func (c *indexCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- indexRebuildingDesc
	ch <- indexSeriesDesc
	ch <- seriesStatusDesc
	ch <- seriesAiringDesc
	ch <- seriesResolutionDesc
	ch <- seriesSourceDesc
}

func (c *indexCollector) Collect(ch chan<- prometheus.Metric) {
	rebuilding := 0.0
	if c.rebuilding() {
		rebuilding = 1
	}
	ch <- prometheus.MustNewConstMetric(indexRebuildingDesc, prometheus.GaugeValue, rebuilding)

	rows := c.rows()
	ch <- prometheus.MustNewConstMetric(indexSeriesDesc, prometheus.GaugeValue, float64(len(rows)))

	// Pre-seed the closed status enum so absent statuses read 0
	// instead of vanishing between scrapes.
	statuses := map[string]int{
		string(api.ListStatusUntracked):  0,
		string(api.ListStatusComplete):   0,
		string(api.ListStatusIncomplete): 0,
		string(api.ListStatusError):      0,
	}
	airing := 0
	resolutions := map[string]int{}
	sources := map[string]int{}
	for _, row := range rows {
		statuses[row.Status]++
		if row.Airing {
			airing++
		}
		for _, r := range row.Resolutions {
			if r != "" {
				resolutions[r]++
			}
		}
		for _, s := range row.Sources {
			if s != "" {
				sources[s]++
			}
		}
	}
	for status, n := range statuses {
		ch <- prometheus.MustNewConstMetric(seriesStatusDesc, prometheus.GaugeValue, float64(n), status)
	}
	ch <- prometheus.MustNewConstMetric(seriesAiringDesc, prometheus.GaugeValue, float64(airing))
	for resolution, n := range resolutions {
		ch <- prometheus.MustNewConstMetric(seriesResolutionDesc, prometheus.GaugeValue, float64(n), resolution)
	}
	for source, n := range sources {
		ch <- prometheus.MustNewConstMetric(seriesSourceDesc, prometheus.GaugeValue, float64(n), source)
	}
}

// statusWriter captures the response status for the metric labels.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush forwards so SSE job streams keep flowing through the wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
