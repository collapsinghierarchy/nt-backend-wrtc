package metrics

import (
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	reg = prometheus.NewRegistry()

	WSConnections = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nt_ws_connections_total", Help: "Total WS connections",
	})
	WSMessages = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nt_ws_messages_total", Help: "Total WS messages",
	}, []string{"type"})
	RoomsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nt_rooms_active", Help: "Active rooms",
	})
	PeersActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "nt_peers_active", Help: "Active peers",
	})
	totalPeers int64

	WSFrameSize = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nt_ws_frame_bytes",
		Help:    "WebSocket frame sizes",
		Buckets: []float64{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576},
	}, []string{"dir"})

	WSRTTSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "nt_ws_rtt_seconds",
		Help:    "WebSocket RTT (derived from ping/pong timestamps)",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
	})

	SignalMsg = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nt_signal_messages_total", Help: "Signaling messages by type",
	}, []string{"type"})
	SignalBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nt_signal_bytes_total", Help: "Signaling payload bytes",
	}, []string{"dir", "type"})

	SessionEstablished = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nt_session_established_total", Help: "Sessions established (ICE connected)",
	}, []string{"mode"})
	SessionFailed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "nt_session_failed_total", Help: "Sessions failed",
	}, []string{"reason"})
	SessionTTF = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "nt_session_time_to_first_flow_seconds",
		Help:    "Time to first media/data flow from join to established",
		Buckets: prometheus.ExponentialBuckets(0.05, 1.6, 12),
	})
)

func init() {
	reg.MustRegister(
		WSConnections, WSMessages, RoomsActive, PeersActive,
		WSFrameSize, WSRTTSeconds,
		SignalMsg, SignalBytes,
		SessionEstablished, SessionFailed, SessionTTF,
	)
}

func Handler() http.Handler { return promhttp.HandlerFor(reg, promhttp.HandlerOpts{}) }

func SetRooms(n int) { RoomsActive.Set(float64(n)) }
func SetPeers(n int) { PeersActive.Set(float64(n)); atomic.StoreInt64(&totalPeers, int64(n)) }
