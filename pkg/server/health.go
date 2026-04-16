package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"
)

type Metrics struct {
	RequestsTotal  uint64
	RequestsActive uint64
	ErrorsTotal    uint64

	Uptime time.Duration

	MemAlloc     uint64
	MemSys       uint64
	NumGoroutine int32
}

func (m *Metrics) toJSON() []byte {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	m.MemAlloc = ms.Alloc
	m.MemSys = ms.TotalAlloc
	m.NumGoroutine = int32(runtime.NumGoroutine())

	data, _ := json.MarshalIndent(m, "", "  ")
	return data
}

func (m *Metrics) RecordRequest() {
	atomic.AddUint64(&m.RequestsTotal, 1)
	atomic.AddUint64(&m.RequestsActive, 1)
}

func (m *Metrics) RecordRequestDone(err error) {
	atomic.AddUint64(&m.RequestsActive, ^uint64(0)) // wraparound: effectively -1
	if err != nil {
		atomic.AddUint64(&m.ErrorsTotal, 1)
	}
}

type HealthServer struct {
	metrics *Metrics
	server  *http.Server
	ready   atomic.Bool
}

func NewHealthServer(port int, metrics *Metrics) *HealthServer {
	mux := http.NewServeMux()
	hs := &HealthServer{
		metrics: metrics,
		ready:   atomic.Bool{},
	}
	hs.ready.Store(true)

	mux.HandleFunc("/health", hs.handleHealth)
	mux.HandleFunc("/healthz", hs.handleHealthz)
	mux.HandleFunc("/metrics", hs.handleMetrics)

	hs.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return hs
}

func (h *HealthServer) Start() {
	go h.server.ListenAndServe()
}

func (h *HealthServer) Close(ctx context.Context) error {
	return h.server.Shutdown(ctx)
}

func (h *HealthServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if h.ready.Load() {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "healthy",
			"metrics": h.metrics,
		})
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "unhealthy",
		})
	}
}

func (h *HealthServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if h.ready.Load() {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("UNHEALTHY"))
	}
}

func (h *HealthServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(h.metrics.toJSON())
}

func (h *HealthServer) SetReady(ready bool) {
	h.ready.Store(ready)
}
