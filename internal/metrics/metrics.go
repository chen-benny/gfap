package metrics

import (
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	PagesProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pages_processed",
		Help: "The total number of pages processed",
	})
	VideoFound = promauto.NewCounter(prometheus.CounterOpts{
		Name: "video_found",
		Help: "The total number of video pages found",
	})
	TargetsFound = promauto.NewCounter(prometheus.CounterOpts{
		Name: "targets_found",
		Help: "The total number of target videos found",
	})
	Errors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "errors",
		Help: "The total number of fetch errors",
	})
	QueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "queue_size",
		Help: "Current number of URLs in flight",
	})
	FetchDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "fetch_duration_seconds",
		Help:    "HTTP fetch latency in seconds",
		Buckets: []float64{0.1, 0.5, 1, 2, 5},
	})
)

// Serve starts the metrics HTTP server on the given port
// stopFn is called when POST /stop is received, triggering graceful shutdown
func Serve(port string, stopFn func()) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		log.Println("[INFO] Stop requested via /stop")
		stopFn()
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("stopping\n"))
	})
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
