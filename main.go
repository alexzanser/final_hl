package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

const (
	windowSize = 50
	addrEnv    = "SERVICE_ADDR"
)

type Metric struct {
	Device    string  `json:"device"`
	Timestamp int64   `json:"timestamp"`
	CPU       float64 `json:"cpu"`
	RPS       int     `json:"rps"`
}

type window struct {
	values []float64
	sum    float64
	sumsq  float64
	idx    int
	cnt    int
	mu     sync.Mutex
}

func newWindow() *window {
	return &window{values: make([]float64, windowSize)}
}

func (w *window) add(v float64) (mean, std float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cnt < windowSize {
		w.cnt++
	} else {
		old := w.values[w.idx]
		w.sum -= old
		w.sumsq -= old * old
	}
	w.values[w.idx] = v
	w.sum += v
	w.sumsq += v * v
	w.idx = (w.idx + 1) % windowSize
	mean = w.sum / float64(w.cnt)
	var variance float64
	if w.cnt > 1 {
		variance = (w.sumsq/float64(w.cnt) - mean*mean)
		if variance < 0 {
			variance = 0
		}
		std = math.Sqrt(variance)
	} else {
		std = 0
	}
	return
}

// Global state
var (
	rdb            *redis.Client
	ctx            = context.Background()
	metricsCh      = make(chan Metric, 20000)
	windows        = make(map[string]*window)
	windowsMu      sync.Mutex
	rpsCounter     = prometheus.NewCounter(prometheus.CounterOpts{Name: "service_rps_total", Help: "Total RPS received"})
	anomalyCounter = prometheus.NewCounter(prometheus.CounterOpts{Name: "service_anomalies_total", Help: "Total detected anomalies"})
	latencyHist    = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "service_handle_latency_seconds", Help: "Latency for handling requests"})
)

func init() {
	prometheus.MustRegister(rpsCounter, anomalyCounter, latencyHist)
}

func getWindow(device string) *window {
	windowsMu.Lock()
	defer windowsMu.Unlock()
	w, ok := windows[device]
	if !ok {
		w = newWindow()
		windows[device] = w
	}
	return w
}

func ingestHandler(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	defer func() {
		latencyHist.Observe(time.Since(t0).Seconds())
	}()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var single Metric
	if err := json.NewDecoder(r.Body).Decode(&single); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	processIncoming(single)
	rpsCounter.Add(float64(single.RPS))
	fmt.Fprintln(w, "ok")
}

func processIncoming(m Metric) {
	// store in Redis per-device list
	key := fmt.Sprintf("metrics:%s", m.Device)
	b, _ := json.Marshal(m)
	rdb.LPush(ctx, key, b)
	rdb.LTrim(ctx, key, 0, 199) // keep last 200
	select {
	case metricsCh <- m:
	default:
		// drop if channel full
	}
}

func analyzer() {
	for m := range metricsCh {
		w := getWindow(m.Device)
		mean, std := w.add(float64(m.RPS))
		z := 0.0
		if std > 0 {
			z = (float64(m.RPS) - mean) / std
		}
		if math.Abs(z) > 2.0 && w.cnt >= windowSize { // anomaly threshold
			anomalyCounter.Inc()
			// save anomaly detail
			key := fmt.Sprintf("anomalies:%s", m.Device)
			info := map[string]interface{}{"ts": m.Timestamp, "rps": m.RPS, "z": z}
			b, _ := json.Marshal(info)
			rdb.LPush(ctx, key, b)
			rdb.LTrim(ctx, key, 0, 999)
		}
	}
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	// simple stats: number of tracked devices
	windowsMu.Lock()
	n := len(windows)
	windowsMu.Unlock()
	fmt.Fprintf(w, "devices_tracked=%d\n", n)
}

func setupRedis() error {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "redis:6379"
	}
	rdb = redis.NewClient(&redis.Options{Addr: addr})
	return rdb.Ping(ctx).Err()
}

func main() {
	if err := setupRedis(); err != nil {
		log.Printf("redis not ready: %v\n", err)
	}
	go analyzer()

	http.HandleFunc("/ingest", ingestHandler)
	http.HandleFunc("/stats", statsHandler)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	http.Handle("/metrics", promhttp.Handler())

	srvAddr := os.Getenv(addrEnv)
	if srvAddr == "" {
		srvAddr = ":8080"
	}

	srv := &http.Server{Addr: srvAddr}

	go func() {
		log.Printf("listening on %s", srvAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down")
	ctxSh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctxSh)
}
