// Package metrics owns the bridge's Prometheus registry. Caddy measures the
// gateway's HTTP edge (caddy_http_*); this registry covers what Caddy cannot
// see — which MCP tool an agent called and how it went, since every tool call
// is the same POST to the same handler at the edge.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "kura_mcp"

// Metrics is the bridge's metric bundle. Methods are nil-safe so the MCP
// server can hold an optional *Metrics without guarding call sites.
type Metrics struct {
	handler      http.Handler
	toolCalls    *prometheus.CounterVec
	toolDuration *prometheus.HistogramVec
}

// New builds the registry with build info, Go/process collectors, and the
// per-tool call metrics.
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
		toolCalls: auto.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "tool",
			Name:      "calls_total",
			Help:      "MCP tool calls by tool and result. Cardinality is bounded by the registered tool catalog.",
		}, []string{"tool", "result"}),
		toolDuration: auto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "tool",
			Name:      "duration_seconds",
			Help:      "MCP tool call duration, including the leaf round-trip.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"tool"}),
	}
}

// Handler serves the Prometheus exposition endpoint.
func (m *Metrics) Handler() http.Handler { return m.handler }

// ToolCall records one completed MCP tool call.
func (m *Metrics) ToolCall(tool string, elapsed time.Duration, err error) {
	if m == nil {
		return
	}
	result := "ok"
	if err != nil {
		result = "error"
	}
	m.toolCalls.WithLabelValues(tool, result).Inc()
	m.toolDuration.WithLabelValues(tool).Observe(elapsed.Seconds())
}
