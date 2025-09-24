package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Host            string
	Port            int
	WSKey           string // optional shared key for WS (?key=)
	RoomTTL         time.Duration
	Heartbeat       time.Duration
	Handshake       time.Duration
	MaxPeersPerRoom int
	MetricsRoute    string
	LogLevel        string
	AllowCORS       bool
}

func FromEnv() Config {
	return Config{
		Host:            getenv("HOST", "0.0.0.0"),
		Port:            getenvInt("PORT", 8080),
		WSKey:           getenv("RENDEZVOUS_WS_KEY", ""),
		RoomTTL:         getenvDur("ROOM_TTL", 10*time.Minute),
		Heartbeat:       getenvDur("HEARTBEAT", 20*time.Second),
		Handshake:       getenvDur("HANDSHAKE_TIMEOUT", 5*time.Minute),
		MaxPeersPerRoom: getenvInt("MAX_PEERS_PER_ROOM", 2),
		MetricsRoute:    getenv("METRICS_ROUTE", "/metrics"),
		LogLevel:        getenv("LOG_LEVEL", "info"),
		AllowCORS:       getenv("ALLOW_CORS", "0") == "1",
	}
}

func (c Config) BindAddr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

// helpers

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
func getenvDur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
