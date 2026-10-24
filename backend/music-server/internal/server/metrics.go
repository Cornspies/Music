package server

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Counters bumped from free functions (logErr, logPanic, recordPlay) live at
// package level; everything else hangs off the Server's own registry.
var (
	errorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "music_errors_total",
		Help: "Errors and recovered panics written to the error log.",
	}, []string{"kind"})
	playsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "music_plays_total",
		Help: "Plays started since the server started.",
	})
)

type metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func newMetrics(s *Server) *metrics {
	m := &metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "music_http_requests_total",
			Help: "HTTP requests by method, route pattern and status code.",
		}, []string{"method", "route", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "music_http_request_duration_seconds",
			Help:    "Time to serve HTTP requests, by method and route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}
	// Present from the start, so alerts on increase() see the first error.
	errorsTotal.WithLabelValues("error")
	errorsTotal.WithLabelValues("panic")
	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.duration, errorsTotal, playsTotal,
		libraryCollector{s},
	)
	return m
}

// observe records one request. The route is the ServeMux pattern without its
// method, so /api/tracks/{id}/stream stays one series however many tracks
// there are; requests that matched no route share "unmatched".
func (m *metrics) observe(r *http.Request, code int, elapsed time.Duration) {
	if m == nil {
		return
	}
	route := r.Pattern
	if _, path, ok := strings.Cut(route, " "); ok {
		route = path
	}
	if route == "" {
		route = "unmatched"
	}
	m.requests.WithLabelValues(r.Method, route, strconv.Itoa(code)).Inc()
	m.duration.WithLabelValues(r.Method, route).Observe(elapsed.Seconds())
}

// MetricsHandler serves the Prometheus endpoint. With a token, scrapes must
// send it as a bearer token.
func (s *Server) MetricsHandler(token string) http.Handler {
	h := promhttp.HandlerFor(s.metrics.registry, promhttp.HandlerOpts{})
	if token == "" {
		return h
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// libraryCollector reads the library's size from the database on each scrape.
// It only reports totals, never anything about a single user or track.
type libraryCollector struct{ s *Server }

var (
	usersDesc     = prometheus.NewDesc("music_users", "Accounts on the server.", nil, nil)
	tracksDesc    = prometheus.NewDesc("music_tracks", "Tracks in all libraries.", nil, nil)
	bytesDesc     = prometheus.NewDesc("music_library_bytes", "Disk space used by uploaded tracks and their compatible copies.", nil, nil)
	playlistsDesc = prometheus.NewDesc("music_playlists", "Playlists on the server.", nil, nil)
	jobsDesc      = prometheus.NewDesc("music_metadata_jobs", "Metadata lookup jobs by state.", []string{"state"}, nil)
	queueDesc     = prometheus.NewDesc("music_background_queue", "Tracks waiting in an in-memory background queue.", []string{"queue"}, nil)
	listenDesc    = prometheus.NewDesc("music_listen_sessions", "Active listen-together sessions.", nil, nil)
)

func (libraryCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{usersDesc, tracksDesc, bytesDesc, playlistsDesc, jobsDesc, queueDesc, listenDesc} {
		ch <- d
	}
}

func (c libraryCollector) Collect(ch chan<- prometheus.Metric) {
	db := c.s.db
	count := func(desc *prometheus.Desc, query string) {
		var n float64
		if err := db.QueryRow(query).Scan(&n); err == nil {
			ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, n)
		}
	}
	count(usersDesc, `SELECT COUNT(1) FROM users`)
	count(tracksDesc, `SELECT COUNT(1) FROM tracks`)
	count(bytesDesc, `SELECT COALESCE(SUM(file_size + alt_size), 0) FROM tracks`)
	count(playlistsDesc, `SELECT COUNT(1) FROM playlists`)

	if rows, err := db.Query(`SELECT state, COUNT(1) FROM metadata_jobs GROUP BY state`); err == nil {
		for rows.Next() {
			var state string
			var n float64
			if rows.Scan(&state, &n) == nil {
				ch <- prometheus.MustNewConstMetric(jobsDesc, prometheus.GaugeValue, n, state)
			}
		}
		_ = rows.Close()
	}

	ch <- prometheus.MustNewConstMetric(queueDesc, prometheus.GaugeValue, float64(len(analysisQueue)), "analysis")
	ch <- prometheus.MustNewConstMetric(queueDesc, prometheus.GaugeValue, float64(len(audioInfoQueue)), "audio_info")

	hub := c.s.listen
	hub.mu.Lock()
	sessions := len(hub.sessions)
	hub.mu.Unlock()
	ch <- prometheus.MustNewConstMetric(listenDesc, prometheus.GaugeValue, float64(sessions))
}
