package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host         string
	Port         int
	RoomTTL      time.Duration
	Heartbeat    time.Duration
	Handshake    time.Duration
	MetricsRoute string

	DevMode     bool
	CORSOrigins []string
	WSReadBuf   int
	WSWriteBuf  int
	WSMaxMsg    int64
	// HTTP server timeouts
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration

	// TLS (if both set -> serve HTTPS)
	TLSCertFile string
	TLSKeyFile  string

	// Simple per-minute rate limits (0 disables)
	WSRatePerMin   int
	HTTPRatePerMin int
}

func (c Config) BindAddr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

func Load() Config {
	return Config{
		Host:              getenv("HOST", "0.0.0.0"),
		Port:              getenvInt("PORT", 8080),
		RoomTTL:           getenvDur("ROOM_TTL", 10*time.Minute),
		Heartbeat:         getenvDur("WS_HEARTBEAT", 60*time.Second),
		Handshake:         getenvDur("WS_HANDSHAKE", 10*time.Second),
		MetricsRoute:      getenv("METRICS_ROUTE", "/metrics"),
		DevMode:           strings.EqualFold(getenv("DEV", "false"), "true"),
		CORSOrigins:       splitCSV(getenv("CORS_ORIGINS", "")),
		WSReadBuf:         getenvInt("WS_READ_BUFFER", 64<<10),
		WSWriteBuf:        getenvInt("WS_WRITE_BUFFER", 64<<10),
		WSMaxMsg:          int64(getenvInt("WS_MAX_MSG", 1<<20)),
		ReadHeaderTimeout: getenvDur("READ_HEADER_TIMEOUT", 5*time.Second),
		WriteTimeout:      getenvDur("WRITE_TIMEOUT", 0),
		IdleTimeout:       getenvDur("IDLE_TIMEOUT", 0),
		TLSCertFile:       getenv("TLS_CERT_FILE", ""),
		TLSKeyFile:        getenv("TLS_KEY_FILE", ""),
		WSRatePerMin:      getenvInt("WS_RATE_PER_MIN", 0),
		HTTPRatePerMin:    getenvInt("HTTP_RATE_PER_MIN", 0),
	}
}

// internal/config/config.go
func (c Config) Validate() error {
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("invalid PORT: %d", c.Port)
	}
	if c.WSMaxMsg <= 1024 {
		return fmt.Errorf("WS_MAX_MSG too small: %d", c.WSMaxMsg)
	}
	if c.Heartbeat <= 0 {
		return fmt.Errorf("WS_HEARTBEAT must be >0")
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("both TLS_CERT_FILE and TLS_KEY_FILE must be set, or none")
	}
	return nil
}

func splitCSV(v string) []string {
	if v == "" || v == "*" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

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
