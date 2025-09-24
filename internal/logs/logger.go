// internal/logs/logger.go
package logs

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger = *zap.Logger
type Field = zap.Field

func New(level string) Logger {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	// set level from env handled by caller; default info:
	// (if you added SetLevel previously, keep that flow. Otherwise:)
	cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	l, _ := cfg.Build()
	return l
}

func F(k string, v any) Field { return zap.Any(k, v) }

func RequestLogger(l Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrw := &wrap{ResponseWriter: w, code: 0} // 0 means "not written"
		isWS := isWebSocketUpgrade(r)

		next.ServeHTTP(wrw, r)

		code := wrw.code
		if code == 0 {
			// Nothing wrote a header. If this was a WS upgrade,
			// the true status is 101. Otherwise treat as 200.
			if isWS {
				code = http.StatusSwitchingProtocols
			} else {
				code = http.StatusOK
			}
		}

		// Quiet the noise: log WS upgrades at debug instead of info.
		fields := []Field{
			F("method", r.Method),
			F("path", r.URL.Path),
			F("code", code),
			F("dur_ms", time.Since(start).Milliseconds()),
			F("ip", r.RemoteAddr),
		}
		if isWS {
			l.Debug("http", fields...)
		} else {
			l.Info("http", fields...)
		}
	})
}

func isWebSocketUpgrade(r *http.Request) bool {
	// RFC 6455: Connection: Upgrade and Upgrade: websocket (case-insensitive)
	if !headerContainsToken(r.Header, "Connection", "upgrade") {
		return false
	}
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func headerContainsToken(h http.Header, key, token string) bool {
	for _, v := range h.Values(key) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

type wrap struct {
	http.ResponseWriter
	code int // 0 means "not set"
}

func (w *wrap) WriteHeader(statusCode int) {
	w.code = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Forward optional interfaces so websocket upgrader works:

func (w *wrap) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}
func (w *wrap) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *wrap) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}
func (w *wrap) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(w.ResponseWriter, r)
}
