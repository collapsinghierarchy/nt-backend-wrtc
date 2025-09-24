package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/config"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/health"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/logs"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/metrics"
	rendezvous "github.com/collapsinghierarchy/nt-backend-wrtc/internal/rendezvouz"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/rooms"
	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/ws"
	"go.uber.org/zap"
)

func main() {
	cfg := config.FromEnv()
	logger := logs.New(cfg.LogLevel)
	defer logger.Sync()

	metrics.Init()

	store := rooms.NewStore(cfg, logger)
	defer store.Close()

	// rendezvous store for 4-digit codes -> appID (UUID)
	rv := rendezvous.NewStore(cfg.RoomTTL)

	mux := http.NewServeMux()
	// Health + readiness
	mux.Handle("/healthz", health.Healthz())
	mux.Handle("/readyz", health.Readyz())

	// Metrics
	mux.Handle(cfg.MetricsRoute, metrics.Handler())

	// Info
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"nt-backend-wrtc","ok":true}`))
	})

	// Mint rendezvous code
	mux.HandleFunc("/rendezvous/code", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		code, appID, exp, err := rv.CreateCode(r.Context())
		if err != nil {
			logger.Error("rv create code failed", logs.F("err", err))
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":      code,
			"appID":     appID,
			"expiresAt": exp.UTC().Format(time.RFC3339),
		})
	})

	// Redeem rendezvous code -> appID
	mux.HandleFunc("/rendezvous/redeem", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Code) == 0 {
			http.Error(w, "bad_json", http.StatusBadRequest)
			return
		}
		appID, exp, ok := rv.RedeemCode(r.Context(), body.Code)
		if !ok {
			http.Error(w, "not_found_or_expired", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"appID":     appID,
			"expiresAt": exp.UTC().Format(time.RFC3339),
		})
	})

	// WS: must include ?appID=<uuid>&side=A|B
	mux.Handle("/ws", ws.NewHandler(cfg, logger, store))

	srv := &http.Server{
		Addr:              cfg.BindAddr(),
		Handler:           logs.RequestLogger(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("listening", logs.F("addr", cfg.BindAddr()))
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	logger.Info("bye")
}
