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

// ObserveIndex registers gauges computed from the live index at scrape
// time. Called once at wiring after the index exists.
func (m *Metrics) ObserveIndex(rebuilding func() bool, series func() int) {
	if m == nil {
		return
	}
	auto := promauto.With(m.reg)
	auto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "index",
		Name:      "rebuilding",
		Help:      "1 while the library index is rebuilding, else 0.",
	}, func() float64 {
		if rebuilding() {
			return 1
		}
		return 0
	})
	auto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "index",
		Name:      "series",
		Help:      "Series currently tracked in the library index.",
	}, func() float64 { return float64(series()) })
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
