package metrics

import (
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	reg           = prometheus.NewRegistry()
	WSConnections = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nt_ws_connections_total", Help: "Total WS connections",
	})
	WSMessages = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nt_ws_messages_total", Help: "WS messages by type",
	}, []string{"type"})
	WSErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nt_ws_errors_total", Help: "WS errors",
	})
	RoomsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nt_rooms_active", Help: "Active rooms",
	})
	PeersActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nt_peers_active", Help: "Active peers",
	})
	totalPeers atomic.Int64
)

func Init() {
	reg.MustRegister(WSConnections, WSMessages, WSErrors, RoomsActive, PeersActive)
}

func Handler() http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

// Helpers for rooms package to update gauges:

func SetRooms(n int) { RoomsActive.Set(float64(n)) }

func SetPeers(n int) {
	PeersActive.Set(float64(n))
	totalPeers.Store(int64(n))
}
