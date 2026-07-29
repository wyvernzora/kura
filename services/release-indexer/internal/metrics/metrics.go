package metrics

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

type HTTP struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	routes   map[string]string
}

func newHTTP(reg prometheus.Registerer, namespace string, routes map[string]string) *HTTP {
	return &HTTP{
		requests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests.",
		}, []string{"method", "path", "status"}),
		duration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "path"}),
		routes: routes,
	}
}

func (m *HTTP) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		captured := httpsnoop.CaptureMetrics(next, w, r)
		path := m.route(r.URL.Path)
		status := strconv.Itoa(captured.Code)
		m.requests.WithLabelValues(r.Method, path, status).Inc()
		m.duration.WithLabelValues(r.Method, path).Observe(time.Since(start).Seconds())
	})
}

// releasePrefix is the one route family whose identifier sits mid-path,
// so neither an exact nor a prefix map entry can label it.
const releasePrefix = "/api/v1/releases/"

func (m *HTTP) route(path string) string {
	if route, ok := m.routes[path]; ok {
		return route
	}
	// Collapse the infohash so the label stays low-cardinality, but keep
	// the magnet sub-resource distinct from release detail — they are
	// different traffic and merging them hides one behind the other.
	if rest, ok := strings.CutPrefix(path, releasePrefix); ok && rest != "" {
		if hash, found := strings.CutSuffix(rest, "/magnet"); found && !strings.Contains(hash, "/") {
			return releasePrefix + "{infohash}/magnet"
		}
		if !strings.Contains(rest, "/") {
			return releasePrefix + "{infohash}"
		}
	}
	for prefix, route := range m.routes {
		if strings.HasSuffix(prefix, "/") && strings.HasPrefix(path, prefix) {
			return route
		}
	}
	return "other"
}

func (m *HTTP) Route(path string) string { return m.route(path) }

type Metrics struct {
	HTTP                *HTTP
	handler             http.Handler
	ingestBatches       *prometheus.CounterVec
	ingestPosts         *prometheus.CounterVec
	ingestBatchSize     prometheus.Histogram
	sourceCrawls        *prometheus.CounterVec
	sourceCrawlDuration *prometheus.HistogramVec
	sourceCrawlPosts    *prometheus.CounterVec
	queueClaims         *prometheus.CounterVec
	queueClaimedItems   prometheus.Counter
	queueClaimBatchSize prometheus.Histogram
	submits             *prometheus.CounterVec
	submitConfidence    *prometheus.HistogramVec
}

func New(version, commit string, qs queueStatsProvider) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	registerBuildInfo(reg, "kura_indexer", version, commit)
	reg.MustRegister(&queueCollector{source: qs})
	auto := promauto.With(reg)
	m := &Metrics{
		handler: promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
		HTTP: newHTTP(reg, "kura_indexer", map[string]string{
			"/healthz":                      "/healthz",
			"/metrics":                      "/metrics",
			"/api/v1/releases":              "/api/v1/releases",
			"/api/v1/releases/ingest":       "/api/v1/releases/ingest",
			"/api/v1/releases/queue/claim":  "/api/v1/releases/queue/claim",
			"/api/v1/releases/queue/stats":  "/api/v1/releases/queue/stats",
			"/api/v1/releases/queue/submit": "/api/v1/releases/queue/submit",
		}),
		ingestBatches: auto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kura_indexer",
			Subsystem: "ingest",
			Name:      "batches_total",
			Help:      "Total ingest batches.",
		}, []string{"result"}),
		ingestPosts: auto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kura_indexer",
			Subsystem: "ingest",
			Name:      "posts_total",
			Help:      "Total ingest posts by source and result.",
		}, []string{"source", "result"}),
		ingestBatchSize: auto.NewHistogram(prometheus.HistogramOpts{
			Namespace: "kura_indexer",
			Subsystem: "ingest",
			Name:      "batch_size",
			Help:      "Posts per ingest batch.",
			Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
		}),
		sourceCrawls: auto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kura_indexer",
			Subsystem: "source",
			Name:      "crawls_total",
			Help:      "Scheduled source crawls by result.",
		}, []string{"source", "result"}),
		sourceCrawlDuration: auto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "kura_indexer",
			Subsystem: "source",
			Name:      "crawl_duration_seconds",
			Help:      "Scheduled source crawl duration.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"source"}),
		sourceCrawlPosts: auto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kura_indexer",
			Subsystem: "source",
			Name:      "crawl_posts_total",
			Help:      "Posts returned by scheduled source crawls.",
		}, []string{"source"}),
		queueClaims: auto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kura_indexer",
			Subsystem: "queue",
			Name:      "claims_total",
			Help:      "Total queue claim requests.",
		}, []string{"result"}),
		queueClaimedItems: auto.NewCounter(prometheus.CounterOpts{
			Namespace: "kura_indexer",
			Subsystem: "queue",
			Name:      "claimed_items_total",
			Help:      "Total queue items claimed.",
		}),
		queueClaimBatchSize: auto.NewHistogram(prometheus.HistogramOpts{
			Namespace: "kura_indexer",
			Subsystem: "queue",
			Name:      "claim_batch_size",
			Help:      "Items per non-empty queue claim.",
			Buckets:   []float64{1, 2, 5, 10, 25, 50, 100},
		}),
		submits: auto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kura_indexer",
			Subsystem: "submit",
			Name:      "total",
			Help:      "Total matcher submissions.",
		}, []string{"status", "result"}),
		submitConfidence: auto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "kura_indexer",
			Subsystem: "submit",
			Name:      "confidence",
			Help:      "Successful matcher submission confidence.",
			Buckets:   []float64{0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 0.99, 1},
		}, []string{"status"}),
	}
	for _, source := range api.Sources() {
		for _, result := range []string{"new", "updated", "duplicate", "conflict", "skipped", "error"} {
			m.ingestPosts.WithLabelValues(source, result).Add(0)
		}
		for _, result := range []string{"ok", "crawl_error", "ingest_error"} {
			m.sourceCrawls.WithLabelValues(source, result).Add(0)
		}
		m.sourceCrawlPosts.WithLabelValues(source).Add(0)
	}
	return m
}

func (m *Metrics) Handler() http.Handler { return m.handler }

func (m *Metrics) IngestBatch(size int, result string) {
	if m == nil {
		return
	}
	m.ingestBatches.WithLabelValues(result).Inc()
	m.ingestBatchSize.Observe(float64(size))
}

func (m *Metrics) IngestPost(source, result string) {
	if m == nil {
		return
	}
	if source == "" {
		source = "unknown"
	}
	m.ingestPosts.WithLabelValues(source, result).Inc()
}

func (m *Metrics) SourceCrawl(source, result string, posts int, duration time.Duration) {
	if m == nil {
		return
	}
	m.sourceCrawls.WithLabelValues(source, result).Inc()
	m.sourceCrawlDuration.WithLabelValues(source).Observe(duration.Seconds())
	if posts > 0 {
		m.sourceCrawlPosts.WithLabelValues(source).Add(float64(posts))
	}
}

func (m *Metrics) QueueClaim(count int, result string) {
	if m == nil {
		return
	}
	m.queueClaims.WithLabelValues(result).Inc()
	if count > 0 {
		m.queueClaimedItems.Add(float64(count))
		m.queueClaimBatchSize.Observe(float64(count))
	}
}

func (m *Metrics) Submit(status, result string, confidence *float64) {
	if m == nil {
		return
	}
	status = submitStatus(status)
	m.submits.WithLabelValues(status, result).Inc()
	if result == "ok" && confidence != nil && (status == "matched" || status == "suppressed") {
		m.submitConfidence.WithLabelValues(status).Observe(*confidence)
	}
}

func submitStatus(status string) string {
	switch status {
	case "matched", "unmatched", "suppressed":
		return status
	default:
		return "invalid"
	}
}

type queueStatsProvider interface {
	QueueStats(ctx context.Context) (store.QueueStats, error)
	CatalogStats(ctx context.Context) (store.CatalogStats, error)
}

type queueCollector struct {
	source queueStatsProvider
}

var queueItemsDesc = prometheus.NewDesc(
	"kura_indexer_queue_items",
	"Current queue items by state.",
	[]string{"state"},
	nil,
)

var queueStatsScrapeOKDesc = prometheus.NewDesc(
	"kura_indexer_queue_stats_scrape_ok",
	"Whether queue stats were available during the metrics scrape.",
	nil,
	nil,
)

var catalogRawPostsDesc = prometheus.NewDesc(
	"kura_indexer_catalog_raw_posts",
	"Current number of raw release posts.",
	nil,
	nil,
)

var catalogInfohashesDesc = prometheus.NewDesc(
	"kura_indexer_catalog_infohashes",
	"Current number of unique release infohashes.",
	nil,
	nil,
)

var catalogRefsDesc = prometheus.NewDesc(
	"kura_indexer_catalog_refs",
	"Current number of unique non-empty refs.",
	nil,
	nil,
)

var catalogStatsScrapeOKDesc = prometheus.NewDesc(
	"kura_indexer_catalog_stats_scrape_ok",
	"Whether catalog stats were available during the metrics scrape.",
	nil,
	nil,
)

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- queueItemsDesc
	ch <- queueStatsScrapeOKDesc
	ch <- catalogRawPostsDesc
	ch <- catalogInfohashesDesc
	ch <- catalogRefsDesc
	ch <- catalogStatsScrapeOKDesc
}

func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cs, err := c.source.CatalogStats(ctx)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(catalogStatsScrapeOKDesc, prometheus.GaugeValue, 0)
	} else {
		ch <- prometheus.MustNewConstMetric(catalogStatsScrapeOKDesc, prometheus.GaugeValue, 1)
		ch <- prometheus.MustNewConstMetric(catalogRawPostsDesc, prometheus.GaugeValue, float64(cs.RawPosts))
		ch <- prometheus.MustNewConstMetric(catalogInfohashesDesc, prometheus.GaugeValue, float64(cs.Infohashes))
		ch <- prometheus.MustNewConstMetric(catalogRefsDesc, prometheus.GaugeValue, float64(cs.Refs))
	}

	qs, err := c.source.QueueStats(ctx)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(queueStatsScrapeOKDesc, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(queueStatsScrapeOKDesc, prometheus.GaugeValue, 1)
	for state, value := range map[string]int{
		"claimable":  qs.Available,
		"leased":     qs.Leased,
		"unmatched":  qs.Unmatched,
		"matched":    qs.Matched,
		"suppressed": qs.Suppressed,
		"exhausted":  qs.Exhausted,
	} {
		ch <- prometheus.MustNewConstMetric(queueItemsDesc, prometheus.GaugeValue, float64(value), state)
	}
}

func registerBuildInfo(reg prometheus.Registerer, namespace, version, commit string) {
	promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "Build metadata.",
	}, []string{"version", "commit"}).WithLabelValues(version, commit).Set(1)
}
