package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/config"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/health"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/hub"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/logs"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/metrics"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/middleware"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/rendezvous"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/ws"
)

func main() {
	// 1) Config + logger
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	logger := logs.New("srv")

	wsRL := middleware.New(cfg.WSRatePerMin)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 2) Mux + core endpoints
	mux := http.NewServeMux()
	mux.Handle("/healthz", health.Healthz())
	mux.Handle("/readyz", health.Readyz())
	mux.Handle(cfg.MetricsRoute, metrics.Handler())

	// 3) Rendezvous API (rate-limited if configured)
	rz := rendezvous.NewStore(cfg.RoomTTL)
	rz.StartJanitor(ctx)
	rzHandler := http.StripPrefix("/rendezvous", rz.Routes())
	httpRL := middleware.New(cfg.HTTPRatePerMin)
	rzHandler = httpRL.Middleware()(rzHandler)
	mux.Handle("/rendezvous/", rzHandler)

	// 4) WebSocket signaling (big-handler compatible) + WS rate limit + tuning
	h := hub.New()
	wsHandler := ws.NewWSHandler(
		h,
		cfg.CORSOrigins, // exact origins; ignored when DevMode=true
		nil,             // use handler's default slog logger
		cfg.DevMode,     // allow all origins in dev
		ws.WithBuffers(cfg.WSReadBuf, cfg.WSWriteBuf),
		ws.WithLimits(cfg.WSMaxMsg, cfg.Heartbeat),
		ws.WithRateLimiter(wsRL),
	)
	mux.Handle("/ws", wsHandler)

	// 5) HTTP server with timeouts
	srv := &http.Server{
		Addr:              cfg.BindAddr(),
		Handler:           logs.Middleware(logger)(mux),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	// 6) Serve (TLS if cert+key are set)
	errCh := make(chan error, 1)
	go func() {
		var err error
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			log.Printf("serving HTTPS on %s", cfg.BindAddr())
			err = srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			log.Printf("serving HTTP on %s", cfg.BindAddr())
			err = srv.ListenAndServe()
		}
		errCh <- err
	}()

	// 7) Block until we’re told to stop (signal) or the server fails
	select {
	case <-ctx.Done():
		// graceful shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown error: %v", err)
		}
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}
}
